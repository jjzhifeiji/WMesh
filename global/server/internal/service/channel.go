package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/audit"
	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/platform/secret"
	"wmesh/global/internal/store"
)

// Channel 管厂出站认领、MQTT 在线和 HTTPS 拉正文。不判厂内业务对错。
type Channel struct {
	*kernel // 认领和在线共用的库与审计。
}

// OfferEnroll 用建厂码换出待认领身份；码不对或已用完则拒绝。
func (s *Channel) OfferEnroll(ctx context.Context, enrollmentCode string) (EnrollmentOffer, error) {
	// 用建厂码哈希换待认领身份。码不对、已用完或工厂不可认领都记拒绝。
	offer, err := s.store.LookupEnrollment(ctx, secret.TokenHash(enrollmentCode))
	// 没有或已经用完就拒绝。
	if err != nil {
		// 认领被拒就留审计。
		_ = s.audit(ctx, nil, nil, nil, "enroll_factory", "channel", audit.Deny)
		// 建厂码不对或已用完，按这个原因拒绝。
		if errors.Is(err, domain.ErrInvalidEnrollment) {
			return EnrollmentOffer{}, domain.ErrInvalidEnrollment
		}
		return EnrollmentOffer{}, err
	}
	// 不能继续认领或新开操作。
	if err := s.guardEnrollable(ctx, offer.FactoryID); err != nil {
		// 认领被拒就留审计。
		_ = s.audit(ctx, nil, nil, &offer.FactoryID, "enroll_factory", offer.FactoryID.String(), audit.Deny)
		return EnrollmentOffer{}, err
	}
	// 出示成功才记允许。
	if err := s.audit(ctx, nil, nil, &offer.FactoryID, "enroll_factory", offer.FactoryID.String(), audit.Allow); err != nil {
		return EnrollmentOffer{}, err
	}
	return offer, nil
}

// ConfirmEnroll 厂端本地落库成功后登记签发公钥并作废建厂码。
func (s *Channel) ConfirmEnroll(ctx context.Context, factoryID uuid.UUID, publicKey []byte) error {
	// 停用、注销或落库失败都记拒绝。
	if err := s.guardEnrollable(ctx, factoryID); err != nil {
		// 确认认领被拒就留审计。
		_ = s.audit(ctx, nil, nil, &factoryID, "confirm_enroll", factoryID.String(), audit.Deny)
		return err
	}
	// 登记签发公钥并作废建厂码。
	if err := s.store.ConfirmEnrollment(ctx, factoryID, publicKey); err != nil {
		// 确认认领被拒就留审计。
		_ = s.audit(ctx, nil, nil, &factoryID, "confirm_enroll", factoryID.String(), audit.Deny)
		return err
	}
	// 认领成功才记允许。
	return s.audit(ctx, nil, nil, &factoryID, "confirm_enroll", factoryID.String(), audit.Allow)
}

// FactoryClaim 认领应答里回给厂端的治理状态、修订和短码。
type FactoryClaim struct {
	Status    string // 工厂治理状态：active / disabled / retired
	Revision  int64  // 治理修订，厂端只向前接收
	ShortCode string // 本厂短码，供厂端展示和发编号
}

// ClaimView 读认领后厂档的治理状态、修订和短码，HTTP 不再直接读库。
func (s *Channel) ClaimView(ctx context.Context, factoryID uuid.UUID) (FactoryClaim, error) {
	// 按工厂名录读出厂档，状态以库里为准。
	fac, err := s.store.FactoryByID(ctx, factoryID)
	// 读不到就交回原因，由调用方决定怎么回。
	if err != nil {
		return FactoryClaim{}, err
	}
	// 只交回认领应答要的三项，不外带别的字段。
	return FactoryClaim{Status: fac.Status, Revision: fac.LifecycleRevision, ShortCode: fac.ShortCode}, nil
}

// RequireFactoryKey 已认领且未注销；名录里没了或已注销都当注销。
func (s *Channel) RequireFactoryKey(ctx context.Context, factoryID uuid.UUID) error {
	// 已认领工厂才算有效；名录没了或已注销都拒绝。
	fac, err := s.store.FactoryByID(ctx, factoryID)
	// 没有这条就按不存在处理，不当成别的故障。
	if errors.Is(err, domain.ErrNotFound) {
		// 进门被拒就留审计。
		_ = s.audit(ctx, nil, nil, &factoryID, "hello_factory", factoryID.String(), audit.Deny)
		return domain.ErrFactoryRetired
	}
	// 这一步失败就停，避免留下半截。
	if err != nil {
		return err
	}
	// 停用仍算已认领，才能再推启用；注销则拒绝。
	if fac.Status == FactoryRetired {
		// 进门被拒就留审计。
		_ = s.audit(ctx, nil, nil, &factoryID, "hello_factory", factoryID.String(), audit.Deny)
		return domain.ErrFactoryRetired
	}
	// 必须已有签发公钥。
	_, err = s.store.FactoryPublicKey(ctx, factoryID)
	// 没有这条就按不存在处理，不当成别的故障。
	if errors.Is(err, domain.ErrNotFound) {
		// 进门被拒就留审计。
		_ = s.audit(ctx, nil, nil, &factoryID, "hello_factory", factoryID.String(), audit.Deny)
		return domain.ErrUnauthorized
	}
	// 这一步失败就停，避免留下半截。
	if err != nil {
		return err
	}
	return nil
}

// guardEnrollable 停用或注销的工厂不能再认领。
func (s *Channel) guardEnrollable(ctx context.Context, factoryID uuid.UUID) error {
	// 按工厂名录处理。
	fac, err := s.store.FactoryByID(ctx, factoryID)
	// 没有这家厂或状态不对就拒绝。
	if err != nil {
		return err
	}
	// 按工厂治理状态决定能否认领或下发。
	switch fac.Status {
	// 停用的厂不能认领，也不能新开操作。
	case FactoryDisabled:
		return domain.ErrFactoryDisabled
	// 已注销的厂不能再启用或认领。
	case FactoryRetired:
		return domain.ErrFactoryRetired
	// 其余情况走这里，避免漏掉一种状态。
	default:
		return nil
	}
}

// MarkChannelOnline 名录标在线；心跳不另记审计。
func (s *Channel) MarkChannelOnline(ctx context.Context, factoryID uuid.UUID) error {
	// 名录标在线。
	if err := s.store.MarkChannelOnline(ctx, factoryID); err != nil {
		// 标在线失败就留审计。
		_ = s.audit(ctx, nil, nil, &factoryID, "channel_up", factoryID.String(), audit.Deny)
		return err
	}
	// 上线成功才记允许。
	return s.audit(ctx, nil, nil, &factoryID, "channel_up", factoryID.String(), audit.Allow)
}

// TouchChannel 刷新最近心跳；已离线则忽略。
func (s *Channel) TouchChannel(ctx context.Context, factoryID uuid.UUID) error {
	// 刷新最近一次心跳。
	return s.store.TouchChannel(ctx, factoryID)
}

// MarkChannelOffline 名录标离线；只在仍显示在线时写断开时间。
func (s *Channel) MarkChannelOffline(ctx context.Context, factoryID uuid.UUID) error {
	// 名录标离线。
	if err := s.store.MarkChannelOffline(ctx, factoryID); err != nil {
		return err
	}
	// 断开成功才记允许。
	return s.audit(ctx, nil, nil, &factoryID, "channel_down", factoryID.String(), audit.Allow)
}

// ResetChannelPresence WAN 进程起来时清掉上一轮残留的在线标记。
func (s *Channel) ResetChannelPresence(ctx context.Context) error {
	// 清掉残留状态，失败就不能继续。
	return s.store.ResetChannelPresence(ctx)
}

// ReportFactoryRelease 记下该厂自报的前端和服务版本；心跳不上审计。
func (s *Channel) ReportFactoryRelease(ctx context.Context, factoryID uuid.UUID, webCode int64, webName string, svcCode int64, svcName string) error {
	// 版本号或版本名不齐则拒绝，避免名录写空。
	if webCode < 1 || svcCode < 1 || strings.TrimSpace(webName) == "" || strings.TrimSpace(svcName) == "" {
		return domain.ErrInvalidName
	}
	// 把厂端自报版本写入名录，离线展示时再清掉。
	return s.store.PutFactoryRelease(ctx, factoryID, webCode, strings.TrimSpace(webName), svcCode, strings.TrimSpace(svcName))
}

// maxClientLogins 一次保活最多收这么多台的最近登录，多出的丢掉。
const maxClientLogins = 1000

// ReportClientLogins 记下厂端上报的各台最近登录；坏条目跳过，只认属于该厂的设备，心跳不上审计。
func (s *Channel) ReportClientLogins(ctx context.Context, factoryID uuid.UUID, ups []ClientLoginUp) error {
	// 没带就不用碰库。
	if len(ups) == 0 {
		return nil
	}
	// 条数超过上限就截掉，避免一条保活撑满库写。
	if len(ups) > maxClientLogins {
		// 只留前面这些，下一轮还会再报。
		ups = ups[:maxClientLogins]
	}
	// 按需要预留位置，避免后面反复扩容。
	logins := make([]store.ClientLogin, 0, len(ups))
	// 逐条解析，坏的跳过不拖累别的。
	for _, u := range ups {
		// 设备身份解不开就跳过。
		cid, err := uuid.Parse(u.ClientID)
		// 身份不合法或为空就跳过这一条。
		if err != nil || cid == uuid.Nil {
			continue
		}
		// 登录时间解不开就跳过。
		at, err := time.Parse(time.RFC3339Nano, u.At)
		// 时间不合法就跳过这一条。
		if err != nil {
			continue
		}
		// 版本名去空白后按字数裁到上限，和库约束一致。
		name := []rune(strings.TrimSpace(u.AppVersionName))
		// 超长就裁掉多出的字。
		if len(name) > 80 {
			// 只留前八十个字。
			name = name[:80]
		}
		// 收下这一条，时间统一成 UTC。
		logins = append(logins, store.ClientLogin{ClientID: cid, At: at.UTC(), AppVersion: max(u.AppVersion, 0), AppVersionName: string(name)})
	}
	// 只改属于该厂且更新的行，别厂设备由库条件挡住。
	return s.store.NoteClientLogins(ctx, factoryID, logins)
}

// IssueContentLease 给该厂签发或续期内容租约；同一把 L，窗口最多 24 小时。
func (s *Channel) IssueContentLease(ctx context.Context, factoryID uuid.UUID) (ContentLease, error) {
	// 先看厂状态，注销的厂不能再续租解密文。
	fac, err := s.store.FactoryByID(ctx, factoryID)
	// 名录里没了与注销同口径，WAN 这侧收回租约。
	if errors.Is(err, domain.ErrNotFound) {
		// 租约被拒就留审计。
		_ = s.audit(ctx, nil, nil, &factoryID, "content_lease", factoryID.String(), audit.Deny)
		return ContentLease{}, domain.ErrFactoryRetired
	}
	// 读名录失败就停，不能在状态不明时发钥。
	if err != nil {
		return ContentLease{}, err
	}
	// 停用的厂还能重新启用，照常续租；注销则拒绝。
	if fac.Status == FactoryRetired {
		// 租约被拒就留审计。
		_ = s.audit(ctx, nil, nil, &factoryID, "content_lease", factoryID.String(), audit.Deny)
		return ContentLease{}, domain.ErrFactoryRetired
	}
	// 同一把 L 续期，窗口重新算 24 小时。
	lease, err := s.store.IssueContentLease(ctx, factoryID)
	// 失败则对方解不开或用不了。
	if err != nil {
		// 租约被拒就留审计。
		_ = s.audit(ctx, nil, nil, &factoryID, "content_lease", factoryID.String(), audit.Deny)
		return ContentLease{}, err
	}
	// 租约成功才记允许。
	if err := s.audit(ctx, nil, nil, &factoryID, "content_lease", factoryID.String(), audit.Allow); err != nil {
		return ContentLease{}, err
	}
	return lease, nil
}
