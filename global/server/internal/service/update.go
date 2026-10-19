package service

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/audit"
	"wmesh/global/internal/platform/blob"
	"wmesh/global/internal/platform/digest"
	"wmesh/global/internal/platform/disk"
	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/store"
)

// SoftwareOffer 是带正文的一份软件包，只给已授权拉取方。
type SoftwareOffer struct {
	Kind        string `json:"kind"`        // wan_service / factory_service / client_apk
	Version     int64  `json:"version"`     // 单调整数
	VersionName string `json:"versionName"` // 给人看的版本名
	Digest      []byte `json:"digest"`      // 包文件 SHA-256
	Body        []byte `json:"body"`        // 包字节
}

// ApplyOutcome 决定确认后是否立刻记已装；测试夹具用，生产默认只落盘。
type ApplyOutcome int

const (
	ApplyDefer ApplyOutcome = iota // 只写待切换，不记已装
	ApplyOK                        // 夹具宣称落地成功
	ApplyFail                      // 夹具宣称落地失败
)

// Updates 管软件发布、厂自拉和本机云端确认，不替厂确认安装。
type Updates struct {
	*kernel // 软件发布共用的库与审计。
}

// SetApplyOutcome 测试注入落地结果；生产保持默认只落盘。
func (s *Updates) SetApplyOutcome(out ApplyOutcome) { s.applyOutcome = out }

// SetRunningSoftware 记下本进程云端服务版本，启动时由入口传入；不写库。
func (s *Updates) SetRunningSoftware(code int64, name string) {
	// 记下版本号，判断新旧时已装不低于它。
	s.runningCode = code
	// 记下版本名，库里没这版发布行时给人看。
	s.runningName = name
}

// installedBaseline 云端服务已装基线：库里已装与本进程版本取大。
func (s *Updates) installedBaseline(ctx context.Context) (int64, error) {
	// 读本机已经装上的版本，没有则是零。
	installed, err := s.store.InstalledSoftware(ctx, SoftwareWANService)
	// 读不到已装版本就不能判断新旧。
	if err != nil {
		return 0, err
	}
	// 首装没记已装，正在跑的版本也算装上了，免得提示同版或旧版。
	return max(installed, s.runningCode), nil
}

// MarkInstalled updater 探活通过后记已装；已是该版本则幂等。
func (s *Updates) MarkInstalled(ctx context.Context, kind string, version int64) error {
	// 用种类和版本当审计对象。
	target := softwareTarget(kind, version)
	// 写失败就保持原来的，不留半截。
	if err := s.store.PutInstalledSoftware(ctx, kind, version); err != nil {
		// 版本并不更新，当成已经装过，不再记失败。
		if errors.Is(err, domain.ErrStaleRevision) {
			return nil
		}
		// 落地被拒就留审计。
		_ = s.audit(ctx, nil, nil, nil, "apply_software", target, audit.Deny)
		return err
	}
	// 记成已装后才记允许。
	return s.audit(ctx, nil, nil, nil, "apply_software", target, audit.Allow)
}

// MarkApplyFailed updater 换包失败或已切回上一版时记一条落地拒绝；同一次结果只记一次。
// 最近一次确认之后已记过就跳过，重启和反复轮询都不重记；再确认后再失败另记一条。
func (s *Updates) MarkApplyFailed(ctx context.Context, kind string, version int64, reason string) error {
	// 缺种类或版本就对不上是哪次确认，不记。
	if kind == "" || version < 1 {
		return nil
	}
	// 确认审计的对象，用来找最近一次确认。
	confirmed := softwareTarget(kind, version)
	// updater 没给原因也写明不详，对象不留空尾巴。
	if strings.TrimSpace(reason) == "" {
		// 原因空着时用固定的不详字样。
		reason = "unknown"
	}
	// 查重和写入串起来，免得并发轮询各记一条。
	s.applyMu.Lock()
	// 记完或查失败都放开，下一次查进度才能进来。
	defer s.applyMu.Unlock()
	// 看最近一次确认之后是否已经记过这次失败。
	done, err := s.store.AuditDeniedSince(ctx, "apply_software", confirmed+":", "confirm_software", confirmed)
	// 查不了或已经记过都不再写，查不了留给下次再补。
	if err != nil || done {
		return err
	}
	// 本机落地不属哪家厂；对象带上原因，事后能查到为何没换上或已切回。
	return s.audit(ctx, nil, nil, nil, "apply_software", confirmed+": "+reason, audit.Deny)
}

// SettleStaged 进程起来后按 updater 结果补记：成功记已装，失败记落地失败。
// 没有结果或还在换都不记；补记没写上就交回错误，查进度时还会再补。
func (s *Updates) SettleStaged(ctx context.Context) error {
	// 没有进度来源就无从补记。
	if s.applyReporter == nil {
		return nil
	}
	// 读本机更换结果，种类和版本以 updater 记的为准。
	phase, kind, version, errMsg, err := s.applyReporter.Progress()
	// 结果读不了就交回，调用方只警告不拦启动。
	if err != nil {
		return err
	}
	// 缺种类或版本就对不上是哪一包，不记。
	if kind == "" || version < 1 {
		return nil
	}
	// 按成败分别补记，正在换或闲着都不动。
	switch phase {
	// 探活通过才记已装，已是该版本则幂等。
	case "ok":
		// 补记已装，晚到的就绪也能补上。
		return s.MarkInstalled(ctx, kind, version)
	// 换包失败或已切回上一版，补一条落地拒绝。
	case "fail":
		// 补记落地失败，同一次结果只记一条。
		return s.MarkApplyFailed(ctx, kind, version, errMsg)
	// 正在换或闲着，等出结果再说。
	default:
		return nil
	}
}

// validSoftwareKind 只认云端、厂服务和客户端三种包。
func validSoftwareKind(kind string) bool {
	return kind == SoftwareWANService || kind == SoftwareFactoryService || kind == SoftwareClientAPK
}

// factoryPullKind 厂会话只能拉厂服务包和客户端包。
func factoryPullKind(kind string) bool {
	return kind == SoftwareFactoryService || kind == SoftwareClientAPK
}

// normalizeVersionName 去掉首尾空白，须 1～80 字。
func normalizeVersionName(name string) (string, error) {
	// 去掉多余空白或前后缀。
	name = strings.TrimSpace(name)
	// 按字数看长度，失败就不能继续。
	n := utf8.RuneCountInString(name)
	// 字数不在允许范围就拒绝，避免空名或超长。
	if n < 1 || n > 80 {
		return "", domain.ErrInvalidName
	}
	return name, nil
}

// softwareTarget 审计对象：种类和版本。
func softwareTarget(kind string, version int64) string {
	// 把数字收成文本。
	return kind + " v" + strconv.FormatInt(version, 10)
}

// requireEnrolledFactory 已认领且未注销才能拉厂包。
func (s *Updates) requireEnrolledFactory(ctx context.Context, factoryID uuid.UUID) error {
	// 按工厂名录处理。
	fac, err := s.store.FactoryByID(ctx, factoryID)
	// 没有这家厂或状态不对就拒绝。
	if err != nil {
		return err
	}
	// 还没认领或已经注销，就不能继续这步。
	if fac.EnrolledAt == nil || fac.Status == FactoryRetired {
		return domain.ErrForbidden
	}
	return nil
}

// PublishSoftware 由 WAN 管理员发布一条只向前的软件版本。
func (s *Updates) PublishSoftware(ctx context.Context, token, kind, versionName string, version int64, body []byte) (SoftwareRelease, error) {
	// 核对当前管理员会话。
	admin, err := s.RequireAdmin(ctx, token)
	// 无效就不能继续，不当已经登录。
	if err != nil {
		return SoftwareRelease{}, err
	}
	// 用种类和版本当审计对象。
	target := softwareTarget(kind, version)
	// 不是三种安装包之一则拒绝发布或拉取。
	if !validSoftwareKind(kind) || version < 1 {
		// 发布安装包被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "publish_software", target, audit.Deny)
		return SoftwareRelease{}, domain.ErrInvalidName
	}
	// 收成合法值，空或太长不要。
	versionName, err = normalizeVersionName(versionName)
	// 不合法就拒绝，避免脏数据入库。
	if err != nil {
		// 发布安装包被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "publish_software", target, audit.Deny)
		return SoftwareRelease{}, err
	}
	// 取当前最高的一版。
	max, err := s.store.MaxSoftwareRelease(ctx, kind)
	// 取不到就拒绝，避免发错一版。
	if err != nil {
		return SoftwareRelease{}, err
	}
	// 先核对已有原件，避免不同摘要覆盖对象。
	existing, err := s.store.SoftwareRelease(ctx, kind, version)
	// 没有错误才继续，有错留在后面的分支。
	if err == nil {
		// 摘要对不上则按损坏拒绝，不能入库或下发。
		if !digest.Match(body, existing.Digest) {
			// 发布安装包被拒就留审计。
			_ = s.audit(ctx, &admin.ID, nil, nil, "publish_software", target, audit.Deny)
			return SoftwareRelease{}, domain.ErrIntegrity
		}
		// 同摘要再发把对象补回，避免进程重启丢内存存根后确认失败。
		if err := s.blobs.Put(ctx, existing.ObjectKey, body); err != nil {
			// 发布安装包被拒就留审计。
			_ = s.audit(ctx, &admin.ID, nil, nil, "publish_software", target, audit.Deny)
			return SoftwareRelease{}, err
		}
		// 刚写进去的就是这份摘要，厂来拉时不必再整包核。
		s.rememberBlob(existing.ObjectKey, existing.Digest, int64(len(body)))
		// 审计没记下则中止，不当这次已经成功。
		if err := s.audit(ctx, &admin.ID, nil, nil, "publish_software", target, audit.Allow); err != nil {
			return SoftwareRelease{}, err
		}
		// 同摘要再发也通知一次，漏掉的厂还能补拉。
		s.notifyFactoryPack(ctx, existing)
		return existing, nil
	}
	// 不是没有这条，就当真正的故障返回。
	if !errors.Is(err, domain.ErrNotFound) {
		return SoftwareRelease{}, err
	}
	// 和已有版本比过再决定能不能清、拉或覆盖。
	if version < max {
		// 发布安装包被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "publish_software", target, audit.Deny)
		return SoftwareRelease{}, domain.ErrStaleRevision
	}
	// 云端包和厂端包分开，拿错种类就拒绝。
	if kind == SoftwareWANService {
		// 读已装基线，库里已装与本进程版本取大。
		installed, err := s.installedBaseline(ctx)
		// 读不到已装版本就不许清理或覆盖。
		if err != nil {
			return SoftwareRelease{}, err
		}
		// 和已有版本比过再决定能不能清、拉或覆盖。
		if version < installed {
			// 发布安装包被拒就留审计。
			_ = s.audit(ctx, &admin.ID, nil, nil, "publish_software", target, audit.Deny)
			return SoftwareRelease{}, domain.ErrStaleRevision
		}
	}
	// 按内容算摘要，失败就不能继续。
	sum := digest.Sum(body)
	// 处理软件包的版本或对象。
	key := store.SoftwareObjectKey(kind, version)
	// 包字节进对象存根，库只留摘要。
	if err := s.blobs.Put(ctx, key, body); err != nil {
		// 发布安装包被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "publish_software", target, audit.Deny)
		return SoftwareRelease{}, err
	}
	// 刚写进去的就是这份摘要，厂来拉时不必再整包核。
	s.rememberBlob(key, sum, int64(len(body)))
	// 写入这一条，失败就不能继续。
	row, err := s.store.InsertSoftwareRelease(ctx, SoftwareRelease{
		Kind: kind, Version: version, VersionName: versionName, Digest: sum, ObjectKey: key,
	})
	// 这一步失败就停，避免留下半截。
	if err != nil {
		// 发布安装包被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "publish_software", target, audit.Deny)
		return SoftwareRelease{}, err
	}
	// 审计没记下则中止，不当这次已经成功。
	if err := s.audit(ctx, &admin.ID, nil, nil, "publish_software", target, audit.Allow); err != nil {
		return SoftwareRelease{}, err
	}
	// 有效厂立刻去拉厂服务包或 APK，正文仍走 HTTPS。
	s.notifyFactoryPack(ctx, row)
	return row, nil
}

// ListSoftwareReleases 给管理台列已发布版本；kind 空则三类都给。
func (s *Updates) ListSoftwareReleases(ctx context.Context, token, kind string) ([]SoftwareRelease, error) {
	// 会话有效才做这一步，无效就当没登录。
	if _, err := s.RequireAdmin(ctx, token); err != nil {
		return nil, err
	}
	// 不是三种安装包之一则拒绝发布或拉取。
	if kind != "" && !validSoftwareKind(kind) {
		return nil, domain.ErrInvalidName
	}
	// 列出这一批供后面筛选。
	return s.store.ListSoftwareReleases(ctx, kind)
}

// LatestSoftware 已认领厂看该种类当前最高版本；不含字节，且不能拉云端包。
func (s *Updates) LatestSoftware(ctx context.Context, factoryID uuid.UUID, kind string) (SoftwareRelease, error) {
	// 厂端不能拉云端包，种类不对就拒绝。
	if !factoryPullKind(kind) {
		return SoftwareRelease{}, domain.ErrForbidden
	}
	// 不够资格就拒绝，不能继续。
	if err := s.requireEnrolledFactory(ctx, factoryID); err != nil {
		return SoftwareRelease{}, err
	}
	// 取当前最高的一版。
	max, err := s.store.MaxSoftwareRelease(ctx, kind)
	// 取不到就拒绝，避免发错一版。
	if err != nil {
		return SoftwareRelease{}, err
	}
	// 条件不满足则拒绝，避免把错状态写进去。
	if max < 1 {
		return SoftwareRelease{}, domain.ErrNotFound
	}
	// 处理软件包的版本或对象。
	return s.store.SoftwareRelease(ctx, kind, max)
}

// 厂拉包前的资格核对：种类、认领和是否当前最高；被拒都留审计。
func (s *Updates) pullableRelease(ctx context.Context, factoryID uuid.UUID, kind string, version int64, target string) (SoftwareRelease, error) {
	// 厂端不能拉云端包，种类不对就拒绝。
	if !factoryPullKind(kind) || version < 1 {
		// 拉包被拒就留审计。
		_ = s.audit(ctx, nil, nil, &factoryID, "pull_software", target, audit.Deny)
		// 字数不在允许范围就拒绝，避免空名或超长。
		if version < 1 {
			return SoftwareRelease{}, domain.ErrInvalidName
		}
		return SoftwareRelease{}, domain.ErrForbidden
	}
	// 不够资格就拒绝，不能继续。
	if err := s.requireEnrolledFactory(ctx, factoryID); err != nil {
		// 拉包被拒就留审计。
		_ = s.audit(ctx, nil, nil, &factoryID, "pull_software", target, audit.Deny)
		return SoftwareRelease{}, err
	}
	// 取当前最高的一版。
	max, err := s.store.MaxSoftwareRelease(ctx, kind)
	// 取不到就拒绝，避免发错一版。
	if err != nil {
		return SoftwareRelease{}, err
	}
	// 和已有版本比过再决定能不能清、拉或覆盖。
	if version < max {
		// 拉包被拒就留审计。
		_ = s.audit(ctx, nil, nil, &factoryID, "pull_software", target, audit.Deny)
		return SoftwareRelease{}, domain.ErrStaleRevision
	}
	// 处理软件包的版本或对象。
	rel, err := s.store.SoftwareRelease(ctx, kind, version)
	// 种类不对或没有这一版就拒绝。
	if err != nil {
		// 拉包被拒就留审计。
		_ = s.audit(ctx, nil, nil, &factoryID, "pull_software", target, audit.Deny)
		return SoftwareRelease{}, err
	}
	return rel, nil
}

// PullSoftware 已认领厂按版本拉包；只给当前最高，摘要不对不给。旧厂端走这条 JSON。
func (s *Updates) PullSoftware(ctx context.Context, factoryID uuid.UUID, kind string, version int64) (SoftwareOffer, error) {
	// 用种类和版本当审计对象。
	target := softwareTarget(kind, version)
	// 先过种类、认领和最高版的核对，被拒已留审计。
	rel, err := s.pullableRelease(ctx, factoryID, kind, version, target)
	// 没过核对就不读包字节。
	if err != nil {
		return SoftwareOffer{}, err
	}
	// 读出这一条再往下用。
	body, err := s.blobs.Get(ctx, rel.ObjectKey)
	// 读不到就拒绝，避免按空数据继续。
	if err != nil {
		// 拉包被拒就留审计。
		_ = s.audit(ctx, nil, nil, &factoryID, "pull_software", target, audit.Deny)
		// 对象不存在就当没有这份包，不当成别的故障。
		if errors.Is(err, blob.ErrNotFound) {
			return SoftwareOffer{}, domain.ErrIntegrity
		}
		return SoftwareOffer{}, err
	}
	// 摘要对不上则按损坏拒绝，不能入库或下发。
	if !digest.Match(body, rel.Digest) {
		// 拉包被拒就留审计。
		_ = s.audit(ctx, nil, nil, &factoryID, "pull_software", target, audit.Deny)
		return SoftwareOffer{}, domain.ErrIntegrity
	}
	// 审计没记下则中止，不当这次已经成功。
	if err := s.audit(ctx, nil, nil, &factoryID, "pull_software", target, audit.Allow); err != nil {
		return SoftwareOffer{}, err
	}
	return SoftwareOffer{
		Kind: rel.Kind, Version: rel.Version, VersionName: rel.VersionName,
		Digest: rel.Digest, Body: body,
	}, nil
}

// SoftwareStream 是按版本打开的一份软件包流，只给已授权拉取方；用完要关。
type SoftwareStream struct {
	Kind        string      // factory_service / client_apk
	Version     int64       // 单调整数
	VersionName string      // 给人看的版本名
	Digest      []byte      // 包文件 SHA-256
	Body        blob.Object // 包字节流，可定位续传，调用方负责关
}

// blobCheck 是一份对象已核过的摘要和长度，长度变了就要重核。
type blobCheck struct {
	digest []byte // 核对通过时的摘要
	size   int64  // 核对时的对象总长
}

// blobVerifyWait 是首次整包核摘要的时限，存储卡住也不会一直挂着。
const blobVerifyWait = 10 * time.Minute

// OpenSoftware 已认领厂按版本打开包流；只给当前最高，摘要每个对象只核一次。
// resume 为真是续传请求，不再记允许审计。
func (s *Updates) OpenSoftware(ctx context.Context, factoryID uuid.UUID, kind string, version int64, resume bool) (SoftwareStream, error) {
	// 用种类和版本当审计对象。
	target := softwareTarget(kind, version)
	// 先过种类、认领和最高版的核对，被拒已留审计。
	rel, err := s.pullableRelease(ctx, factoryID, kind, version, target)
	// 没过核对就不打开对象。
	if err != nil {
		return SoftwareStream{}, err
	}
	// 打开对象流，只问长度，不整包读进内存。
	obj, err := s.blobs.Open(ctx, rel.ObjectKey)
	// 打不开就拒绝，避免给出空包。
	if err != nil {
		// 拉包被拒就留审计。
		_ = s.audit(ctx, nil, nil, &factoryID, "pull_software", target, audit.Deny)
		// 对象不存在就当包已损坏，不当成别的故障。
		if errors.Is(err, blob.ErrNotFound) {
			return SoftwareStream{}, domain.ErrIntegrity
		}
		return SoftwareStream{}, err
	}
	// 摘要没核过就先整包核一遍，坏对象不发出去。
	if err := s.checkBlob(ctx, rel.ObjectKey, rel.Digest, obj.Size()); err != nil {
		// 不发了就关上对象流。
		_ = obj.Close()
		// 拉包被拒就留审计。
		_ = s.audit(ctx, nil, nil, &factoryID, "pull_software", target, audit.Deny)
		return SoftwareStream{}, err
	}
	// 从头拉才记允许，续传不重复记。
	if !resume {
		// 审计没记下则中止，不当这次已经成功。
		if err := s.audit(ctx, nil, nil, &factoryID, "pull_software", target, audit.Allow); err != nil {
			// 不发了就关上对象流。
			_ = obj.Close()
			return SoftwareStream{}, err
		}
	}
	return SoftwareStream{
		Kind: rel.Kind, Version: rel.Version, VersionName: rel.VersionName,
		Digest: rel.Digest, Body: obj,
	}, nil
}

// 核对象摘要：记过且长度没变就放行，否则按对象键单飞整包核一次。
func (k *kernel) checkBlob(ctx context.Context, key string, want []byte, size int64) error {
	// 已核过且长度没变就不再整包读。
	if k.blobVerified(key, want, size) {
		return nil
	}
	// 同一对象只核一路，别的拉包等它的结果。
	ch := k.blobFly.DoChan(key, func() (any, error) {
		// 排队期间上一路已核完就不再整包读。
		if k.blobVerified(key, want, size) {
			return nil, nil
		}
		// 领头的请求断了也核完，等着的人还要结果。
		return nil, k.hashBlob(context.WithoutCancel(ctx), key, want)
	})
	// 核完或本请求先断，谁先到走谁。
	select {
	// 核完就交回结果，坏对象是完整性错误。
	case res := <-ch:
		return res.Err
	// 本请求断了就先走，核对照样跑完。
	case <-ctx.Done():
		// 交回本请求的取消原因。
		return ctx.Err()
	}
}

// 整包流式算一遍摘要，对得上才记下；坏对象交回完整性错误。
func (k *kernel) hashBlob(ctx context.Context, key string, want []byte) error {
	// 给核对定个时限，存储卡住也会收尾。
	ctx, cancel := context.WithTimeout(ctx, blobVerifyWait)
	// 核完就取消，含提前返回。
	defer cancel()
	// 另开一路流来核，不动拉包那一路的偏移。
	obj, err := k.blobs.Open(ctx, key)
	// 对象没了就当包已损坏。
	if errors.Is(err, blob.ErrNotFound) {
		return domain.ErrIntegrity
	}
	// 别的打不开交回原因，下次再核。
	if err != nil {
		return err
	}
	// 核完就关上对象流。
	defer obj.Close()
	// 边读边算摘要，不整包进内存。
	h := sha256.New()
	// 整份读一遍喂给摘要。
	n, err := io.Copy(h, obj)
	// 读一半断了就交回，不记核对结果。
	if err != nil {
		return err
	}
	// 摘要对不上就是坏对象，不能发出去。
	if subtle.ConstantTimeCompare(h.Sum(nil), want) != 1 {
		return domain.ErrIntegrity
	}
	// 核对通过就记下，下次直接放行。
	k.rememberBlob(key, want, n)
	return nil
}

// 看这份对象是否已按这份摘要和长度核过。
func (k *kernel) blobVerified(key string, want []byte, size int64) bool {
	// 读表前先占住，避免和记下交错。
	k.blobMu.Lock()
	// 读完就放开。
	defer k.blobMu.Unlock()
	// 取出这份对象的核对记录。
	c, ok := k.blobChecked[key]
	// 摘要和长度都对上才算核过。
	return ok && c.size == size && subtle.ConstantTimeCompare(c.digest, want) == 1
}

// 记下这份对象已核过的摘要和长度，之后拉包不再整包重算。
func (k *kernel) rememberBlob(key string, sum []byte, size int64) {
	// 写表前先占住，避免并发写坏。
	k.blobMu.Lock()
	// 写完就放开。
	defer k.blobMu.Unlock()
	// 第一次记才建表。
	if k.blobChecked == nil {
		// 建一张空表，按对象键记。
		k.blobChecked = map[string]blobCheck{}
	}
	// 拷一份摘要记下，调用方事后改不到。
	k.blobChecked[key] = blobCheck{digest: append([]byte(nil), sum...), size: size}
}

// 忘掉这份对象的核对记录，删包后同键再发要重核。
func (k *kernel) forgetBlob(key string) {
	// 改表前先占住，避免和读交错。
	k.blobMu.Lock()
	// 改完就放开。
	defer k.blobMu.Unlock()
	// 删掉这一条，没有也不出错。
	delete(k.blobChecked, key)
}

// 清空全部核对记录，换了对象存储后都要重核。
func (k *kernel) forgetBlobs() {
	// 改表前先占住，避免和读交错。
	k.blobMu.Lock()
	// 改完就放开。
	defer k.blobMu.Unlock()
	// 换一张空表，旧记录不再算数。
	k.blobChecked = nil
}

// CurrentWANSoftware 本机云端服务已装基线；库里没记且本进程没报版本则为 0。
func (s *Updates) CurrentWANSoftware(ctx context.Context, token string) (SoftwareRelease, error) {
	// 会话有效才做这一步，无效就当没登录。
	if _, err := s.RequireAdmin(ctx, token); err != nil {
		return SoftwareRelease{}, err
	}
	// 读已装基线，库里已装与本进程版本取大。
	installed, err := s.installedBaseline(ctx)
	// 读不到已装版本就不许清理或覆盖。
	if err != nil {
		return SoftwareRelease{}, err
	}
	// 和已有版本比过再决定能不能清、拉或覆盖。
	if installed < 1 {
		return SoftwareRelease{Kind: SoftwareWANService}, nil
	}
	// 处理软件包的版本或对象。
	row, err := s.store.SoftwareRelease(ctx, SoftwareWANService, installed)
	// 种类不对或没有这一版就拒绝。
	if err != nil {
		// 没有这条就按不存在处理，不当成别的故障。
		if errors.Is(err, domain.ErrNotFound) {
			// 没有发布行时只给版本号，版本名先空着。
			cur := SoftwareRelease{Kind: SoftwareWANService, Version: installed}
			// 基线正是本进程版本，就用本进程的版本名给人看。
			if installed == s.runningCode {
				// 填上本进程版本名，管理台不显示空名。
				cur.VersionName = s.runningName
			}
			return cur, nil
		}
		return SoftwareRelease{}, err
	}
	return row, nil
}

// PendingWANSoftware 管理员看待确认的云端服务包；没有则空。
func (s *Updates) PendingWANSoftware(ctx context.Context, token string) (*SoftwareRelease, error) {
	// 会话有效才做这一步，无效就当没登录。
	if _, err := s.RequireAdmin(ctx, token); err != nil {
		return nil, err
	}
	// 读已装基线，库里已装与本进程版本取大。
	installed, err := s.installedBaseline(ctx)
	// 读不到已装版本就不许清理或覆盖。
	if err != nil {
		return nil, err
	}
	// 取当前最高的一版。
	max, err := s.store.MaxSoftwareRelease(ctx, SoftwareWANService)
	// 取不到就拒绝，避免发错一版。
	if err != nil {
		return nil, err
	}
	// 和已有版本比过再决定能不能清、拉或覆盖。
	if max <= installed {
		return nil, nil
	}
	// 处理软件包的版本或对象。
	row, err := s.store.SoftwareRelease(ctx, SoftwareWANService, max)
	// 种类不对或没有这一版就拒绝。
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// ConfirmWANUpdate 仅 WAN 管理员确认后才写待切换；落地成功才记已装。
func (s *Updates) ConfirmWANUpdate(ctx context.Context, token, kind string, version int64) error {
	// 核对当前管理员会话。
	admin, err := s.RequireAdmin(ctx, token)
	// 无效就不能继续，不当已经登录。
	if err != nil {
		return err
	}
	// 用种类和版本当审计对象。
	target := softwareTarget(kind, version)
	// 云端包和厂端包分开，拿错种类就拒绝。
	if kind != SoftwareWANService {
		// 确认安装被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "confirm_software", target, audit.Deny)
		return domain.ErrForbidden
	}
	// 读还等着切换的包。
	pending, err := s.PendingWANSoftware(ctx, token)
	// 没有就不用换，不能凭空确认。
	if err != nil {
		return err
	}
	// 没有待切换或版本不对，就不能确认这一版。
	if pending == nil || pending.Version != version {
		// 确认安装被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "confirm_software", target, audit.Deny)
		// 没有待切换或版本不对，就不能确认这一版。
		if pending == nil {
			return domain.ErrNotFound
		}
		return domain.ErrStaleRevision
	}
	// 读出这一条再往下用。
	body, err := s.blobs.Get(ctx, pending.ObjectKey)
	// 读不到就拒绝，避免按空数据继续。
	if err != nil {
		// 确认安装被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "confirm_software", target, audit.Deny)
		// 对象不存在就当没有这份包，不当成别的故障。
		if errors.Is(err, blob.ErrNotFound) {
			return domain.ErrIntegrity
		}
		return err
	}
	// 摘要对不上则按损坏拒绝，不能入库或下发。
	if !digest.Match(body, pending.Digest) {
		// 确认安装被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "confirm_software", target, audit.Deny)
		return domain.ErrIntegrity
	}
	// 没挂落盘就跳过，测试可以只走夹具。
	if s.pendingSink != nil {
		// 没挂落盘就跳过，测试可以只走夹具。
		if err := s.pendingSink.Stage(kind, version, pending.Digest, body); err != nil {
			// 确认安装被拒就留审计。
			_ = s.audit(ctx, &admin.ID, nil, nil, "confirm_software", target, audit.Deny)
			return err
		}
	}
	// 按确认结果记已装或保持旧版。
	return s.finishApply(ctx, &admin.ID, kind, version, target)
}

// ApplyProgress 管理员看本机更换进度；就绪则补记已装。
type ApplyProgress struct {
	Phase   string `json:"phase"`           // idle / applying / ok / fail
	Kind    string `json:"kind"`            // 待切换或已落地种类
	Version int64  `json:"version"`         // 对应版本；idle 为 0
	Error   string `json:"error,omitempty"` // fail 时 updater 原因
}

// ApplyProgress 读盘上进度；探活通过才记已装，失败补记一条落地拒绝。
func (s *Updates) ApplyProgress(ctx context.Context, token string) (ApplyProgress, error) {
	// 会话有效才做这一步，无效就当没登录。
	if _, err := s.RequireAdmin(ctx, token); err != nil {
		return ApplyProgress{}, err
	}
	// 没有进度读取器就不确认，避免凭空记已装。
	if s.applyReporter == nil {
		return ApplyProgress{Phase: "idle"}, nil
	}
	// 读更新器走到哪一步。
	phase, kind, version, errMsg, err := s.applyReporter.Progress()
	// 读不到就不能当成已经装完。
	if err != nil {
		return ApplyProgress{}, err
	}
	// 更新器还没报成功，就不能记成已装。
	if phase == "ok" && kind != "" && version > 0 {
		// updater 写就绪可能晚于新进程启动，这里补记已装。
		if err := s.MarkInstalled(ctx, kind, version); err != nil {
			return ApplyProgress{}, err
		}
	}
	// 换包失败或已切回上一版就补一条落地拒绝，同一次结果只记一条。
	if phase == "fail" {
		// 审计没写上不挡页面显示失败，下次轮询再补。
		_ = s.MarkApplyFailed(ctx, kind, version, errMsg)
	}
	return ApplyProgress{Phase: phase, Kind: kind, Version: version, Error: errMsg}, nil
}

// finishApply 按夹具结果记已装或保持待切换。
func (s *Updates) finishApply(ctx context.Context, actor *uuid.UUID, kind string, version int64, target string) error {
	// 按确认结果决定记已装还是保持旧版。
	switch s.applyOutcome {
	// 夹具宣称落地失败，记拒绝并保持旧版本。
	case ApplyFail:
		// 确认安装被拒就留审计。
		_ = s.audit(ctx, actor, nil, nil, "confirm_software", target, audit.Deny)
		return domain.ErrSoftwareInstallFailed
	// 夹具宣称已经落地，才把该版本记成已装。
	case ApplyOK:
		// 写失败就保持原来的，不留半截。
		if err := s.store.PutInstalledSoftware(ctx, kind, version); err != nil {
			// 确认安装被拒就留审计。
			_ = s.audit(ctx, actor, nil, nil, "confirm_software", target, audit.Deny)
			return err
		}
		// 旧版本继续跑，不能记成已换。
		return s.audit(ctx, actor, nil, nil, "confirm_software", target, audit.Allow)
	// 其余情况走这里，避免漏掉一种状态。
	default:
		// 旧版本继续跑，不能记成已换。
		return s.audit(ctx, actor, nil, nil, "confirm_software", target, audit.Allow)
	}
}

// PullSoftwareForFactory 给通道 HTTPS 拉当前最高厂包；语义与 PullSoftware 相同。
func (s *Updates) PullSoftwareForFactory(ctx context.Context, factoryID uuid.UUID, kind string, version int64) (SoftwareOffer, error) {
	// 把已授权的内容拉出来。
	return s.PullSoftware(ctx, factoryID, kind, version)
}

const (
	SoftwareKeepLatest    = "latest"    // 该种类当前最高，厂还要来拉
	SoftwareKeepInstalled = "installed" // 本机正在跑
)

// SoftwareItem 是带保留原因的已发布行，给管理台分列。
type SoftwareItem struct {
	SoftwareRelease        // 已发布的版本行，再标能否删除。
	Keep            string `json:"keep"` // latest / installed / 空则可清
}

// ImagePrune 是本机清镜像进度。
type ImagePrune struct {
	Ready     bool   `json:"ready"`               // updater 已写结果
	OK        bool   `json:"ok"`                  // 清完
	Reclaimed string `json:"reclaimed,omitempty"` // 回报空间
}

// ListSoftwareItems 按种类列出已发布版本并标出不能删的行。
func (s *Updates) ListSoftwareItems(ctx context.Context, token, kind string) ([]SoftwareItem, error) {
	// 列出这一批供后面筛选。
	rows, err := s.ListSoftwareReleases(ctx, token, kind)
	// 列出失败就拒绝，避免交出不完整结果。
	if err != nil {
		return nil, err
	}
	// 按数量先准备容器。
	out := make([]SoftwareItem, 0, len(rows))
	// 逐行整理，坏的一行就整批拒绝。
	for _, row := range rows {
		// 标出最高版和正在跑的。
		keep, err := s.softwareKeep(ctx, row.Kind, row.Version)
		// 这两类不能删，删了厂端会断。
		if err != nil {
			return nil, err
		}
		// 把这一项接进结果。
		out = append(out, SoftwareItem{SoftwareRelease: row, Keep: keep})
	}
	return out, nil
}

// 已装和当前最高必须留着。
func (s *Updates) softwareKeep(ctx context.Context, kind string, version int64) (string, error) {
	// 取当前最高的一版。
	max, err := s.store.MaxSoftwareRelease(ctx, kind)
	// 取不到就拒绝，避免发错一版。
	if err != nil {
		return "", err
	}
	// 收成整数，失败就不能继续。
	installed := int64(0)
	// 云端包和厂端包分开，拿错种类就拒绝。
	if kind == SoftwareWANService {
		// 读已装基线，库里已装与本进程版本取大。
		installed, err = s.installedBaseline(ctx)
		// 读不到已装版本就不许清理或覆盖。
		if err != nil {
			return "", err
		}
	}
	// 和已有版本比过再决定能不能清、拉或覆盖。
	if version == installed && installed > 0 {
		return SoftwareKeepInstalled, nil
	}
	// 和已有版本比过再决定能不能清、拉或覆盖。
	if version == max && max > 0 {
		return SoftwareKeepLatest, nil
	}
	return "", nil
}

// DeleteSoftware 清掉一份不是最高也不是已装的旧包。
func (s *Updates) DeleteSoftware(ctx context.Context, token, kind string, version int64) error {
	// 核对当前管理员会话。
	admin, err := s.RequireAdmin(ctx, token)
	// 无效就不能继续，不当已经登录。
	if err != nil {
		return err
	}
	// 用种类和版本当审计对象。
	target := softwareTarget(kind, version)
	// 不是三种安装包之一则拒绝发布或拉取。
	if !validSoftwareKind(kind) || version < 1 {
		// 清理被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "prune_software", target, audit.Deny)
		return domain.ErrInvalidName
	}
	// 标出最高版和正在跑的。
	keep, err := s.softwareKeep(ctx, kind, version)
	// 这两类不能删，删了厂端会断。
	if err != nil {
		return err
	}
	// 空和有值走不同路，避免把空白写进名录。
	if keep != "" {
		// 清理被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "prune_software", target, audit.Deny)
		return domain.ErrReferenced
	}
	// 处理软件包的版本或对象。
	row, err := s.store.SoftwareRelease(ctx, kind, version)
	// 种类不对或没有这一版就拒绝。
	if err != nil {
		// 清理被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "prune_software", target, audit.Deny)
		return err
	}
	// 删除失败就停，避免库里留下残行。
	if err := s.blobs.Delete(ctx, row.ObjectKey); err != nil {
		// 清理被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "prune_software", target, audit.Deny)
		return err
	}
	// 对象已删，核对记录跟着作废。
	s.forgetBlob(row.ObjectKey)
	// 删除失败就停，避免库里留下残行。
	if err := s.store.DeleteSoftwareRelease(ctx, kind, version); err != nil {
		// 清理被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "prune_software", target, audit.Deny)
		return err
	}
	// 正在用的不能删。
	return s.audit(ctx, &admin.ID, nil, nil, "prune_software", target, audit.Allow)
}

// PruneSoftware 清掉该种类所有可删旧包；kind 空则三类都清。
func (s *Updates) PruneSoftware(ctx context.Context, token, kind string) (int, error) {
	// 列出这一批供后面筛选。
	items, err := s.ListSoftwareItems(ctx, token, kind)
	// 列出失败就拒绝，避免交出不完整结果。
	if err != nil {
		return 0, err
	}
	// 先从零开始数，只计真正删掉的旧包。
	n := 0
	// 逐项检查，不合法就整份拒绝。
	for _, item := range items {
		// 标了保留的不能删，最高版和正在跑的都算。
		if item.Keep != "" {
			continue
		}
		// 删除失败就停，避免库里留下残行。
		if err := s.DeleteSoftware(ctx, token, item.Kind, item.Version); err != nil {
			return n, err
		}
		// 这一份删掉了，可清数量加一。
		n++
	}
	return n, nil
}

// RequestImagePrune 请本机 updater 清无用 app 镜像；ref 空则全部可清。
func (s *Updates) RequestImagePrune(ctx context.Context, token, ref string) error {
	// 核对当前管理员会话。
	admin, err := s.RequireAdmin(ctx, token)
	// 无效就不能继续，不当已经登录。
	if err != nil {
		return err
	}
	// 处理本机镜像的清理对象。
	target := imagePruneAuditTarget(ref)
	// 引用不合法就拒绝。
	if err := s.imagePruneTarget(ref); err != nil {
		// 清镜像被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "prune_images", target, audit.Deny)
		return err
	}
	// 没挂这项就跳过，测试夹具可以是空的。
	if s.imageJanitor == nil {
		// 更新器收下后才记允许。
		return s.audit(ctx, &admin.ID, nil, nil, "prune_images", target, audit.Allow)
	}
	// 没挂这项就跳过，测试夹具可以是空的。
	if err := s.imageJanitor.RequestPrune(ref); err != nil {
		// 清镜像被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "prune_images", target, audit.Deny)
		return err
	}
	// 更新器收下后才记允许。
	return s.audit(ctx, &admin.ID, nil, nil, "prune_images", target, audit.Allow)
}

// 点名清某一个标签时，current / previous 拒绝。
func (s *Updates) imagePruneTarget(ref string) error {
	// 空和有值走不同路，避免把空白写进名录。
	if ref == "" {
		return nil
	}
	// 镜像引用含空格或管道则拒绝，避免被当成命令。
	if !validImageRef(ref) {
		return domain.ErrInvalidName
	}
	// 没挂这项就跳过，测试夹具可以是空的。
	if s.imageJanitor == nil {
		return nil
	}
	// 读本机镜像清单。
	raw, err := s.imageJanitor.ImagesJSON()
	// 读失败就拒绝，这里不碰 docker。
	if err != nil {
		return err
	}
	// 空的就按没有处理，避免交出空壳当成功。
	if len(raw) == 0 {
		return domain.ErrNotFound
	}
	// 先留空镜像占用，读到清单再填。
	var usage ImageUsage
	// 载荷解开失败则拒绝，不按坏包继续。
	if err := json.Unmarshal(raw, &usage); err != nil {
		return err
	}
	// 逐个镜像看占用，这里不删除容器。
	for _, it := range usage.Items {
		// 条件不满足则拒绝，避免把错状态写进去。
		if it.Ref != ref {
			continue
		}
		// 标了保留的不能删，最高版和正在跑的都算。
		if it.Keep != "" {
			return domain.ErrReferenced
		}
		return nil
	}
	return domain.ErrNotFound
}

// 镜像标签只允许仓库名加版本，挡住命令字符。
func validImageRef(ref string) bool {
	// 空和有值走不同路，避免把空白写进名录。
	if ref == "" {
		return true
	}
	// 镜像引用含空格或管道则拒绝，避免被当成命令。
	if len(ref) > 256 || strings.ContainsAny(ref, " \t\n\r;|&$`'\"\\<>") {
		return false
	}
	// 做完这一步再继续。
	host, tag, ok := strings.Cut(ref, ":")
	// 看是否包含这段，用来区分类型。
	return ok && host != "" && tag != "" && !strings.Contains(tag, ":")
}

// 审计对象：点名用标签，一键清用 docker。
func imagePruneAuditTarget(ref string) string {
	// 空和有值走不同路，避免把空白写进名录。
	if ref == "" {
		return "docker"
	}
	return ref
}

// ImagePruneProgress 看 updater 清镜像结果。
func (s *Updates) ImagePruneProgress(ctx context.Context, token string) (ImagePrune, error) {
	// 会话有效才做这一步，无效就当没登录。
	if _, err := s.RequireAdmin(ctx, token); err != nil {
		return ImagePrune{}, err
	}
	// 没挂这项就跳过，测试夹具可以是空的。
	if s.imageJanitor == nil {
		return ImagePrune{Ready: true, OK: true, Reclaimed: "0B"}, nil
	}
	// 读清理结果，失败就不能继续。
	reclaimed, ok, present, err := s.imageJanitor.PruneResult()
	// 还没有结果就不能当成已经清完。
	if err != nil {
		return ImagePrune{}, err
	}
	return ImagePrune{Ready: present, OK: ok, Reclaimed: reclaimed}, nil
}

// BlobUsage 是对象存储已用，没有配额。
type BlobUsage struct {
	Used    int64  `json:"used"`             // 对象字节合计
	Objects int64  `json:"objects"`          // 对象个数
	Bucket  string `json:"bucket,omitempty"` // 桶名；内存存根为空
}

// ImageItem 是本机一条 app 镜像标签。
type ImageItem struct {
	Ref  string `json:"ref"`  // repository:tag
	ID   string `json:"id"`   // docker 镜像短号
	Size int64  `json:"size"` // 该标签字节
	Keep string `json:"keep"` // current / previous / 空则可清
}

// ImageUsage 是本机 app 镜像占用，由 updater 写入。
type ImageUsage struct {
	Kind  string      `json:"kind"`  // 本机服务种类
	Used  int64       `json:"used"`  // 去重后字节
	Count int         `json:"count"` // 去重后个数
	Items []ImageItem `json:"items"` // 标签列表
}

// StorageUsage 是本侧磁盘、对象存储、库和本机 app 镜像占用。
type StorageUsage struct {
	Disk     disk.Space `json:"disk"`     // 本机根盘
	OSS      BlobUsage  `json:"oss"`      // 本侧桶内对象
	Database int64      `json:"database"` // 当前库字节
	Images   ImageUsage `json:"images"`   // 本机 app 镜像
}

// StorageUsage 超管看本机磁盘余量、对象存储已用、库占用和本机 app 镜像。
func (s *Updates) StorageUsage(ctx context.Context, token string) (StorageUsage, error) {
	// 会话有效才做这一步，无效就当没登录。
	if _, err := s.RequireAdmin(ctx, token); err != nil {
		return StorageUsage{}, err
	}
	// 先留空占用，读到哪一项再填哪一项。
	out := StorageUsage{}
	// 读到磁盘余量才填上，失败就先留空。
	if space, err := disk.Of("/"); err == nil {
		// 填上根盘余量，读不到就保持空白。
		out.Disk = space
	}
	// 没挂对象存储就跳过，测试用内存夹具。
	if s.blobs != nil {
		// 统计已经占用的字节和个数。
		used, objects, err := s.blobs.Usage(ctx)
		// 统计失败就拒绝，避免报出假数。
		if err != nil {
			return StorageUsage{}, err
		}
		// 填上桶里已用字节和对象个数。
		out.OSS = BlobUsage{Used: used, Objects: objects}
		if named, ok := s.blobs.(interface{ Bucket() string }); ok { // 只有对象存储才有桶名。
			out.OSS.Bucket = named.Bucket()
		}
	}
	// 读当前库占用了多少字节。
	n, err := s.store.DatabaseSize(ctx)
	// 读失败就拒绝，避免报成零。
	if err != nil {
		return StorageUsage{}, err
	}
	// 填上当前库占用的字节数。
	out.Database = n
	// 没挂这项就跳过，测试夹具可以是空的。
	if s.imageJanitor != nil {
		// 读本机镜像清单。
		raw, err := s.imageJanitor.ImagesJSON()
		// 读失败就拒绝，这里不碰 docker。
		if err != nil {
			return StorageUsage{}, err
		}
		// 有内容才继续解析，空字节不当坏包。
		if len(raw) > 0 {
			// 清单是空就换成空列表，避免前端见到空值。
			if err := json.Unmarshal(raw, &out.Images); err != nil {
				return StorageUsage{}, err
			}
		}
	}
	// 清单是空就换成空列表，避免前端见到空值。
	if out.Images.Items == nil {
		// 没有镜像清单时给空列表，避免前端见到空。
		out.Images.Items = []ImageItem{}
	}
	return out, nil
}
