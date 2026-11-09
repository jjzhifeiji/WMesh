package service

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/audit"
	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/platform/id"
	"wmesh/global/internal/platform/secret"
	"wmesh/global/internal/store"
)

const (
	onboardingTTL        = 24 * time.Hour // 申请等批准的时限
	resetApprovedTTL     = time.Hour      // 找回批准后设新密码的时限
	onboardingMaxPending = 100            // 同类待批准申请上限，匿名入口不能无限写

	// OnboardingExpired 给现场看的状态：申请已过期、已用掉或不存在。
	OnboardingExpired = "expired"
)

// OnboardingRequest 是现场发起、等云端批准的一条申请。
type OnboardingRequest = store.OnboardingRequest

// OnboardingTicket 交给现场的申请凭据，密钥原文只在这里出现一次。
type OnboardingTicket struct {
	RequestID  uuid.UUID `json:"requestId"`  // 申请稳定身份
	Secret     string    `json:"secret"`     // 申请密钥原文，现场凭它查进度和完成接入
	VerifyCode string    `json:"verifyCode"` // 六位核对码，现场和云端比对
	ExpiresAt  time.Time `json:"expiresAt"`  // 申请到期时间
}

// OnboardingProgress 给现场看的申请进度，不含别的申请信息。
type OnboardingProgress struct {
	Status      string `json:"status"`      // pending / approved / rejected / expired
	FactoryName string `json:"factoryName"` // 工厂显示名，以管理员批准时的为准
}

// PendingOnboarding 是云端待接入列表。
type PendingOnboarding struct {
	Requests []OnboardingRequest `json:"requests"` // 还能批准的申请
}

// normalizeLabel 去掉首尾空白后须 1～64 字；厂名、登录名和显示名同一规则。
func normalizeLabel(name string) (string, error) {
	// 去掉首尾空白，全空格的名字当空名。
	name = strings.TrimSpace(name)
	// 按字数限长，空名也在这里拒绝。
	if n := utf8.RuneCountInString(name); n < 1 || n > 64 {
		return "", domain.ErrInvalidName
	}
	return name, nil
}

// newVerifyCode 生成六位数字核对码。
func newVerifyCode() (string, error) {
	// 取一个六位以内的随机数。
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	// 随机源失败就停，不发可预测的码。
	if err != nil {
		return "", err
	}
	// 补足六位，前导零也显示。
	return fmt.Sprintf("%06d", n.Int64()), nil
}

// createOnboarding 生成密钥和核对码并写入申请；密钥原文只交回一次。
func (s *kernel) createOnboarding(ctx context.Context, row OnboardingRequest) (OnboardingTicket, error) {
	// 生成申请密钥，只把哈希留在云端。
	code, err := secret.RandomToken()
	// 随机源失败就停。
	if err != nil {
		return OnboardingTicket{}, err
	}
	// 生成给人比对的核对码。
	verify, err := newVerifyCode()
	// 随机源失败就停。
	if err != nil {
		return OnboardingTicket{}, err
	}
	// 记下当前时刻，申请和到期都用它。
	now := time.Now().UTC()
	// 补全服务端生成的字段，调用方传不进来。
	row.ID, row.Status, row.SecretHash, row.VerifyCode, row.CreatedAt, row.ExpiresAt = id.New(), store.OnboardingPending, secret.TokenHash(code), verify, now, now.Add(onboardingTTL)
	// 写入申请，到上限或库错就停。
	if err := s.store.CreateOnboarding(ctx, row, onboardingMaxPending); err != nil {
		return OnboardingTicket{}, err
	}
	return OnboardingTicket{RequestID: row.ID, Secret: code, VerifyCode: verify, ExpiresAt: row.ExpiresAt}, nil
}

// ApplyOnboarding 现场申请接入新厂；工厂要等管理员批准才写入名录。
func (s *Channel) ApplyOnboarding(ctx context.Context, name, saLogin, saDisplay, version, source string) (OnboardingTicket, error) {
	// 厂名规范化，空名或太长先拒绝。
	name, err := normalizeLabel(name)
	// 不合格就不写申请。
	if err != nil {
		return OnboardingTicket{}, err
	}
	// 登录名规范化，和厂端认领同一规则。
	saLogin, err = normalizeLoginName(saLogin)
	// 不合格就不写申请。
	if err != nil {
		return OnboardingTicket{}, err
	}
	// 显示名规范化。
	saDisplay, err = normalizeLabel(saDisplay)
	// 不合格就不写申请。
	if err != nil {
		return OnboardingTicket{}, err
	}
	// 写入待批准申请。
	ticket, err := s.createOnboarding(ctx, OnboardingRequest{Kind: store.OnboardingEnroll, FactoryName: name, SALogin: saLogin, SADisplay: saDisplay, SourceAddr: source, ServiceVersionName: version})
	// 申请没写下就留拒绝审计。
	if err != nil {
		// 匿名入口被拒也要留痕。
		_ = s.audit(ctx, nil, &saLogin, nil, "apply_onboarding", name, audit.Deny)
		return OnboardingTicket{}, err
	}
	// 审计没写下则整次不算完成。
	if err := s.audit(ctx, nil, &saLogin, nil, "apply_onboarding", ticket.RequestID.String(), audit.Allow); err != nil {
		return OnboardingTicket{}, err
	}
	return ticket, nil
}

// OnboardingStatus 现场凭密钥查进度；不存在、过期或用掉都只回 expired。
func (s *Channel) OnboardingStatus(ctx context.Context, code string) (OnboardingProgress, error) {
	// 按哈希找申请，原文不落库。
	row, err := s.store.OnboardingBySecret(ctx, secret.TokenHash(code))
	// 不存在对现场来说就是失效。
	if errors.Is(err, domain.ErrNotFound) {
		return OnboardingProgress{Status: OnboardingExpired}, nil
	}
	// 库故障不能说成失效。
	if err != nil {
		return OnboardingProgress{}, err
	}
	// 用掉的找回单对现场就是失效；过期的待批准和找回单同理。
	if row.Status == store.OnboardingUsed || (!row.ExpiresAt.After(time.Now().UTC()) && (row.Status == store.OnboardingPending || row.Kind == store.OnboardingResetSA)) {
		return OnboardingProgress{Status: OnboardingExpired}, nil
	}
	return OnboardingProgress{Status: row.Status, FactoryName: row.FactoryName}, nil
}

// RequestSAReset 已认领的厂申请找回初始超管密码；厂名和登录名以云端名录为准。
func (s *Channel) RequestSAReset(ctx context.Context, factoryID uuid.UUID, version, source string) (OnboardingTicket, error) {
	// 停用或注销的厂不能找回。
	if err := s.guardEnrollable(ctx, factoryID); err != nil {
		// 被拒也要留痕。
		_ = s.audit(ctx, nil, nil, &factoryID, "request_sa_reset", factoryID.String(), audit.Deny)
		return OnboardingTicket{}, err
	}
	// 读名录上的厂名。
	fac, err := s.store.FactoryByID(ctx, factoryID)
	// 读不到就停。
	if err != nil {
		return OnboardingTicket{}, err
	}
	// 读交付时的初始超管。
	sa, err := s.store.InitialSuperAdmin(ctx, factoryID)
	// 读不到就停。
	if err != nil {
		return OnboardingTicket{}, err
	}
	// 写入待批准的找回单。
	ticket, err := s.createOnboarding(ctx, OnboardingRequest{Kind: store.OnboardingResetSA, FactoryID: &factoryID, FactoryName: fac.Name, SALogin: sa.LoginName, SADisplay: sa.DisplayName, SourceAddr: source, ServiceVersionName: version})
	// 申请没写下就留拒绝审计。
	if err != nil {
		// 被拒也要留痕。
		_ = s.audit(ctx, nil, nil, &factoryID, "request_sa_reset", factoryID.String(), audit.Deny)
		return OnboardingTicket{}, err
	}
	// 审计没写下则整次不算完成。
	if err := s.audit(ctx, nil, nil, &factoryID, "request_sa_reset", ticket.RequestID.String(), audit.Allow); err != nil {
		return OnboardingTicket{}, err
	}
	return ticket, nil
}

// ConsumeSAReset 厂端设新密码前用掉找回单，只成功一次。
func (s *Channel) ConsumeSAReset(ctx context.Context, factoryID uuid.UUID, code string) error {
	// 停用或注销的厂不能找回。
	if err := s.guardEnrollable(ctx, factoryID); err != nil {
		// 被拒也要留痕。
		_ = s.audit(ctx, nil, nil, &factoryID, "consume_sa_reset", factoryID.String(), audit.Deny)
		return err
	}
	// 条件更新用掉找回单，并发时只有一个能成。
	if err := s.store.ConsumeResetRequest(ctx, factoryID, secret.TokenHash(code)); err != nil {
		// 被拒也要留痕。
		_ = s.audit(ctx, nil, nil, &factoryID, "consume_sa_reset", factoryID.String(), audit.Deny)
		return err
	}
	// 审计没写下则整次不算完成。
	return s.audit(ctx, nil, nil, &factoryID, "consume_sa_reset", factoryID.String(), audit.Allow)
}

// PendingOnboarding 列出待接入和待找回的申请，只给 WAN 管理员。
func (s *Factories) PendingOnboarding(ctx context.Context, token string) (PendingOnboarding, error) {
	// 只有 WAN 管理员能读待接入列表。
	if _, err := s.RequireAdmin(ctx, token); err != nil {
		// 读被拒就留审计。
		_ = s.audit(ctx, nil, nil, nil, "read_onboarding", "wan", audit.Deny)
		return PendingOnboarding{}, err
	}
	// 只取还能批准的申请。
	rows, err := s.store.ListPendingOnboarding(ctx)
	// 读失败就停，不交残缺列表。
	if err != nil {
		return PendingOnboarding{}, err
	}
	// 列表随名录轮询，成功不记允许，免得撑大审计表。
	return PendingOnboarding{Requests: rows}, nil
}

// ApproveOnboarding 批准申请；新厂此时才写入名录，name 非空时用它修正厂名。
func (s *Factories) ApproveOnboarding(ctx context.Context, token string, requestID uuid.UUID, name string) (OnboardingRequest, error) {
	// 只有 WAN 管理员能批准。
	admin, err := s.RequireAdmin(ctx, token)
	// 无效就不能继续。
	if err != nil {
		// 批准被拒就留审计。
		_ = s.audit(ctx, nil, nil, nil, "approve_onboarding", requestID.String(), audit.Deny)
		return OnboardingRequest{}, err
	}
	// 管理员改了厂名才校验，没改就用现场填的。
	if strings.TrimSpace(name) != "" {
		// 厂名规范化，不合格就不批准。
		if name, err = normalizeLabel(name); err != nil {
			return OnboardingRequest{}, err
		}
	}
	// 先看申请是哪一类，走对应的批准。
	row, err := s.store.OnboardingByID(ctx, requestID)
	// 没有这条申请就停。
	if err != nil {
		return OnboardingRequest{}, err
	}
	// 准备接住批准结果。
	var out OnboardingRequest
	// 按申请种类批准。
	switch row.Kind {
	// 新厂接入：写名录并发短码。
	case store.OnboardingEnroll:
		// 申请密钥哈希直接成为这家厂的建厂码哈希。
		out, err = s.store.ApproveEnrollRequest(ctx, requestID, id.New(), id.New(), strings.TrimSpace(name))
	// 找回：厂必须仍然有效，批准后只留短时间。
	default:
		// 准备接住厂状态检查的结果。
		out, err = s.approveReset(ctx, row)
	}
	// 批准失败就留拒绝审计。
	if err != nil {
		// 批准被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, row.FactoryID, "approve_onboarding", requestID.String(), audit.Deny)
		return OnboardingRequest{}, err
	}
	// 审计没写下则整次不算完成。
	if err := s.audit(ctx, &admin.ID, nil, out.FactoryID, "approve_onboarding", row.Kind+" "+requestID.String(), audit.Allow); err != nil {
		return OnboardingRequest{}, err
	}
	return out, nil
}

// approveReset 批准找回前再确认厂仍然有效。
func (s *Factories) approveReset(ctx context.Context, row OnboardingRequest) (OnboardingRequest, error) {
	// 找回单一定绑着厂，没有就当已失效。
	if row.FactoryID == nil {
		return OnboardingRequest{}, domain.ErrOnboardingClosed
	}
	// 读名录看厂状态。
	fac, err := s.store.FactoryByID(ctx, *row.FactoryID)
	// 读不到就停。
	if err != nil {
		return OnboardingRequest{}, err
	}
	// 只有有效的厂能批准找回。
	if fac.Status != store.FactoryActive {
		return OnboardingRequest{}, domain.ErrFactoryDisabled
	}
	// 批准并把可用时间收短。
	return s.store.ApproveResetRequest(ctx, row.ID, time.Now().UTC().Add(resetApprovedTTL))
}

// RejectOnboarding 拒绝申请，现场可修改后重新申请。
func (s *Factories) RejectOnboarding(ctx context.Context, token string, requestID uuid.UUID) (OnboardingRequest, error) {
	// 只有 WAN 管理员能拒绝。
	admin, err := s.RequireAdmin(ctx, token)
	// 无效就不能继续。
	if err != nil {
		// 拒绝被拒就留审计。
		_ = s.audit(ctx, nil, nil, nil, "reject_onboarding", requestID.String(), audit.Deny)
		return OnboardingRequest{}, err
	}
	// 拒绝这条申请，已处理或已过期就停。
	out, err := s.store.RejectOnboarding(ctx, requestID)
	// 拒绝失败就留痕。
	if err != nil {
		// 拒绝失败也留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "reject_onboarding", requestID.String(), audit.Deny)
		return OnboardingRequest{}, err
	}
	// 审计没写下则整次不算完成。
	if err := s.audit(ctx, &admin.ID, nil, out.FactoryID, "reject_onboarding", out.Kind+" "+requestID.String(), audit.Allow); err != nil {
		return OnboardingRequest{}, err
	}
	return out, nil
}
