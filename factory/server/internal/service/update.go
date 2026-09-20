package service

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/blob"
	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/disk"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/store"
)

const (
	SoftwareFactoryService = store.SoftwareFactoryService // 厂端服务包
	SoftwareClientAPK      = store.SoftwareClientAPK      // 客户端 APK
	SoftwareWANService     = "wan_service"                // 云端包，本厂不得收
)

// SoftwareMeta 是一份软件包的元数据，不含字节。
type SoftwareMeta struct {
	Kind        string `json:"kind"`        // factory_service / client_apk
	Version     int64  `json:"version"`     // 单调整数
	VersionName string `json:"versionName"` // 给人看的版本名
	Digest      []byte `json:"digest"`      // SHA-256
}

// SoftwareOffer 是带正文的一份软件包。
type SoftwareOffer struct {
	Kind        string `json:"kind"`        // factory_service / client_apk
	Version     int64  `json:"version"`     // 单调整数
	VersionName string `json:"versionName"` // 给人看的版本名
	Digest      []byte `json:"digest"`      // SHA-256
	Body        []byte `json:"body"`        // 包字节
}

// SoftwareSource 从已认领厂会话背后的 WAN 拉最高包。
type SoftwareSource interface {
	Latest(ctx context.Context, kind string) (SoftwareMeta, error)
	Pull(ctx context.Context, kind string, version int64) ([]byte, error)
}

// ApplyOutcome 决定确认后是否立刻记已装；测试夹具用，生产默认只落盘。
type ApplyOutcome int

const (
	ApplyDefer ApplyOutcome = iota // 只写待切换，不记已装
	ApplyOK                        // 夹具宣称落地成功
	ApplyFail                      // 夹具宣称落地失败
)

// Updates 管本厂自拉软件包、超管确认厂服务、本机确认客户端。
type Updates struct{ *kernel }

// softwareTarget 审计对象：种类和版本。
func softwareTarget(kind string, version int64) string {
	return kind + " v" + strconv.FormatInt(version, 10)
}

// factoryPullKind 本厂只收厂服务包和客户端包。
func factoryPullKind(kind string) bool {
	return kind == SoftwareFactoryService || kind == SoftwareClientAPK
}

// SetApplyOutcome 测试注入落地结果；生产保持默认只落盘。
func (s *Updates) SetApplyOutcome(out ApplyOutcome) { s.applyOutcome = out }

// SetSoftwareSource 注入厂→WAN 拉包；测试用内存源，生产由通道挂上。
func (s *Updates) SetSoftwareSource(src SoftwareSource) { s.softwareSource = src }

// completeReplica 本厂已收且摘要对得上的那一份；不完整当没有。
func (s *Updates) completeReplica(ctx context.Context, kind string, version int64) (SoftwareReplica, bool, error) {
	row, err := s.store.SoftwareReplica(ctx, kind, version)
	if errors.Is(err, domain.ErrNotFound) {
		return SoftwareReplica{}, false, nil
	}
	if err != nil {
		return SoftwareReplica{}, false, err
	}
	body, err := s.blobs.Get(ctx, row.ObjectKey)
	if err != nil || !digest.Match(body, row.Digest) {
		return SoftwareReplica{}, false, nil
	}
	return row, true, nil
}

// EnsureSoftware 本厂没有完整副本才去 WAN 拉；同版本进行中不重下。
func (s *Updates) EnsureSoftware(ctx context.Context, kind string, version int64) error {
	if !factoryPullKind(kind) || version < 1 {
		if kind == SoftwareWANService {
			return domain.ErrForbidden
		}
		return domain.ErrInvalidName
	}
	if _, ok, err := s.completeReplica(ctx, kind, version); err != nil || ok {
		return err
	}
	key := kind + "/" + strconv.FormatInt(version, 10)
	s.softwareMu.Lock()
	if s.softwareIn == nil {
		s.softwareIn = map[string]*softwareWait{}
	}
	if w, ok := s.softwareIn[key]; ok {
		s.softwareMu.Unlock()
		<-w.done
		return w.err
	}
	w := &softwareWait{done: make(chan struct{})}
	s.softwareIn[key] = w
	s.softwareMu.Unlock()
	err := s.pullSoftwareOnce(ctx, kind, version)
	w.err = err
	close(w.done)
	s.softwareMu.Lock()
	delete(s.softwareIn, key)
	s.softwareMu.Unlock()
	return err
}

// 真正去源侧拉一份并落副本；调用方已占住单飞。
func (s *Updates) pullSoftwareOnce(ctx context.Context, kind string, version int64) error {
	if _, ok, err := s.completeReplica(ctx, kind, version); err != nil || ok {
		return err
	}
	if s.softwareSource == nil {
		return domain.ErrNotFound
	}
	var name string
	var want []byte
	if meta, err := s.softwareSource.Latest(ctx, kind); err == nil && meta.Version == version {
		name = meta.VersionName
		want = meta.Digest
	}
	body, err := s.softwareSource.Pull(ctx, kind, version)
	if err != nil {
		return err
	}
	if len(want) > 0 && !digest.Match(body, want) {
		return domain.ErrIntegrity
	}
	return s.IngestSoftware(ctx, SoftwareOffer{
		Kind: kind, Version: version, VersionName: name, Digest: digest.Sum(body), Body: body,
	})
}

// SetPendingSink 生产写入更新目录；测试可空。
func (s *Updates) SetPendingSink(sink PendingSink) { s.pendingSink = sink }

// MarkInstalled updater 探活通过后记已装；已是该版本则幂等。
func (s *Updates) MarkInstalled(ctx context.Context, kind string, version int64) error {
	target := softwareTarget(kind, version)
	if err := s.store.PutInstalledSoftware(ctx, kind, version); err != nil {
		if errors.Is(err, domain.ErrStaleRevision) {
			return nil
		}
		_ = s.audit(ctx, nil, nil, "apply_software", target, audit.Deny)
		return err
	}
	return s.audit(ctx, nil, nil, "apply_software", target, audit.Allow)
}

// IngestSoftware 收下已由本厂会话拉回的包；只核摘要，不验签名。
func (s *Updates) IngestSoftware(ctx context.Context, offer SoftwareOffer) error {
	target := softwareTarget(offer.Kind, offer.Version)
	if !factoryPullKind(offer.Kind) || offer.Version < 1 {
		_ = s.audit(ctx, nil, nil, "pull_software", target, audit.Deny)
		if offer.Kind == SoftwareWANService {
			return domain.ErrForbidden
		}
		return domain.ErrInvalidName
	}
	if !digest.Match(offer.Body, offer.Digest) {
		_ = s.audit(ctx, nil, nil, "pull_software", target, audit.Deny)
		return domain.ErrIntegrity
	}
	// 已有副本先对摘要，避免不同正文覆盖对象。
	got, err := s.store.SoftwareReplica(ctx, offer.Kind, offer.Version)
	if err == nil {
		if !digest.Match(offer.Body, got.Digest) {
			_ = s.audit(ctx, nil, nil, "pull_software", target, audit.Deny)
			return domain.ErrIntegrity
		}
		// 同摘要再收把对象补回，避免进程重启丢内存存根后确认失败。
		if err := s.blobs.Put(ctx, got.ObjectKey, offer.Body); err != nil {
			_ = s.audit(ctx, nil, nil, "pull_software", target, audit.Deny)
			return err
		}
		return s.audit(ctx, nil, nil, "pull_software", target, audit.Allow)
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	if offer.Kind == SoftwareFactoryService {
		installed, err := s.store.InstalledSoftware(ctx, offer.Kind)
		if err != nil {
			return err
		}
		if offer.Version < installed {
			_ = s.audit(ctx, nil, nil, "pull_software", target, audit.Deny)
			return domain.ErrStaleRevision
		}
	}
	max, err := s.store.MaxSoftwareReplica(ctx, offer.Kind)
	if err != nil {
		return err
	}
	if offer.Version < max {
		_ = s.audit(ctx, nil, nil, "pull_software", target, audit.Deny)
		return domain.ErrStaleRevision
	}
	key := store.SoftwareObjectKey(offer.Kind, offer.Version)
	if err := s.blobs.Put(ctx, key, offer.Body); err != nil {
		_ = s.audit(ctx, nil, nil, "pull_software", target, audit.Deny)
		return err
	}
	if _, err := s.store.InsertSoftwareReplica(ctx, SoftwareReplica{
		Kind: offer.Kind, Version: offer.Version, VersionName: offer.VersionName,
		Digest: offer.Digest, ObjectKey: key,
	}); err != nil {
		_ = s.audit(ctx, nil, nil, "pull_software", target, audit.Deny)
		return err
	}
	return s.audit(ctx, nil, nil, "pull_software", target, audit.Allow)
}

// SyncFactorySoftware 超管触发问最高版并静默拉；已有完整副本则不再下。
func (s *Updates) SyncFactorySoftware(ctx context.Context, token, kind string) (SoftwareReplica, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return SoftwareReplica{}, err
	}
	target := softwareTarget(kind, 0)
	if err := s.can(ctx, acc, permManageAccount, nil); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "pull_software", target, audit.Deny)
		return SoftwareReplica{}, err
	}
	if !factoryPullKind(kind) {
		_ = s.audit(ctx, &acc.ID, nil, "pull_software", kind, audit.Deny)
		return SoftwareReplica{}, domain.ErrForbidden
	}
	if s.softwareSource == nil {
		_ = s.audit(ctx, &acc.ID, nil, "pull_software", kind, audit.Deny)
		return SoftwareReplica{}, domain.ErrNotFound
	}
	meta, err := s.softwareSource.Latest(ctx, kind)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "pull_software", kind, audit.Deny)
		return SoftwareReplica{}, err
	}
	// 只问最高版；正文有完整副本就不再下。
	if err := s.EnsureSoftware(ctx, meta.Kind, meta.Version); err != nil {
		return SoftwareReplica{}, err
	}
	row, ok, err := s.completeReplica(ctx, kind, meta.Version)
	if err != nil {
		return SoftwareReplica{}, err
	}
	if !ok {
		return SoftwareReplica{}, domain.ErrIntegrity
	}
	return row, nil
}

// CurrentFactorySoftware 本厂已确认安装的厂服务版本；未装过则版本为 0。
func (s *Updates) CurrentFactorySoftware(ctx context.Context, token string) (SoftwareReplica, error) {
	if _, err := s.RequireActive(ctx, token); err != nil {
		return SoftwareReplica{}, err
	}
	installed, err := s.store.InstalledSoftware(ctx, SoftwareFactoryService)
	if err != nil {
		return SoftwareReplica{}, err
	}
	if installed < 1 {
		return SoftwareReplica{Kind: SoftwareFactoryService}, nil
	}
	row, err := s.store.SoftwareReplica(ctx, SoftwareFactoryService, installed)
	if errors.Is(err, domain.ErrNotFound) {
		return SoftwareReplica{Kind: SoftwareFactoryService, Version: installed}, nil
	}
	return row, err
}

// PendingFactorySoftware 超管看本厂已收完整、高于已装的厂服务包；没有则空。
func (s *Updates) PendingFactorySoftware(ctx context.Context, token string) (*SoftwareReplica, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return nil, err
	}
	if err := s.can(ctx, acc, permManageAccount, nil); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "pending_software", SoftwareFactoryService, audit.Deny)
		return nil, err
	}
	row, ok, err := s.pendingReplica(ctx, SoftwareFactoryService, 0)
	if err != nil || !ok {
		return nil, err
	}
	return &row, nil
}

// pendingReplica 高于已装版本的最高已收副本；没有则空。
func (s *Updates) pendingReplica(ctx context.Context, kind string, installed int64) (SoftwareReplica, bool, error) {
	if kind == SoftwareFactoryService && installed == 0 {
		var err error
		installed, err = s.store.InstalledSoftware(ctx, kind)
		if err != nil {
			return SoftwareReplica{}, false, err
		}
	}
	max, err := s.store.MaxSoftwareReplica(ctx, kind)
	if err != nil {
		return SoftwareReplica{}, false, err
	}
	if max <= installed {
		return SoftwareReplica{}, false, nil
	}
	// 提示只认本厂已收完整副本，不完整当没有。
	return s.completeReplica(ctx, kind, max)
}

// ConfirmFactoryUpdate 仅本厂超管确认后才写待切换；落地成功才记已装。
func (s *Updates) ConfirmFactoryUpdate(ctx context.Context, token, kind string, version int64) error {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return err
	}
	target := softwareTarget(kind, version)
	if err := s.can(ctx, acc, permManageAccount, nil); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "confirm_software", target, audit.Deny)
		return err
	}
	if kind != SoftwareFactoryService {
		_ = s.audit(ctx, &acc.ID, nil, "confirm_software", target, audit.Deny)
		return domain.ErrForbidden
	}
	pending, ok, err := s.pendingReplica(ctx, kind, 0)
	if err != nil {
		return err
	}
	if !ok || pending.Version != version {
		_ = s.audit(ctx, &acc.ID, nil, "confirm_software", target, audit.Deny)
		if !ok {
			return domain.ErrNotFound
		}
		return domain.ErrStaleRevision
	}
	body, err := s.blobs.Get(ctx, pending.ObjectKey)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "confirm_software", target, audit.Deny)
		if errors.Is(err, blob.ErrNotFound) {
			return domain.ErrIntegrity
		}
		return err
	}
	if !digest.Match(body, pending.Digest) {
		_ = s.audit(ctx, &acc.ID, nil, "confirm_software", target, audit.Deny)
		return domain.ErrIntegrity
	}
	if s.pendingSink != nil {
		if err := s.pendingSink.Stage(kind, version, pending.Digest, body); err != nil {
			_ = s.audit(ctx, &acc.ID, nil, "confirm_software", target, audit.Deny)
			return err
		}
	}
	return s.finishApply(ctx, &acc.ID, kind, version, target)
}

// ApplyProgress 超管看本机更换进度；就绪则补记已装。
type ApplyProgress struct {
	Phase   string `json:"phase"`           // idle / applying / ok / fail
	Kind    string `json:"kind"`            // 待切换或已落地种类
	Version int64  `json:"version"`         // 对应版本；idle 为 0
	Error   string `json:"error,omitempty"` // fail 时 updater 原因
}

// ApplyProgress 读盘上进度；探活通过才记已装。
func (s *Updates) ApplyProgress(ctx context.Context, token string) (ApplyProgress, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return ApplyProgress{}, err
	}
	if err := s.can(ctx, acc, permManageAccount, nil); err != nil {
		return ApplyProgress{}, err
	}
	if s.applyReporter == nil {
		return ApplyProgress{Phase: "idle"}, nil
	}
	phase, kind, version, errMsg, err := s.applyReporter.Progress()
	if err != nil {
		return ApplyProgress{}, err
	}
	if phase == "ok" && kind != "" && version > 0 {
		// updater 写就绪可能晚于新进程启动，这里补记已装。
		if err := s.MarkInstalled(ctx, kind, version); err != nil {
			return ApplyProgress{}, err
		}
	}
	return ApplyProgress{Phase: phase, Kind: kind, Version: version, Error: errMsg}, nil
}

// finishApply 按夹具结果记已装或保持待切换。
func (s *Updates) finishApply(ctx context.Context, actor *uuid.UUID, kind string, version int64, target string) error {
	switch s.applyOutcome {
	case ApplyFail:
		_ = s.audit(ctx, actor, nil, "confirm_software", target, audit.Deny)
		return domain.ErrSoftwareInstallFailed
	case ApplyOK:
		if err := s.store.PutInstalledSoftware(ctx, kind, version); err != nil {
			_ = s.audit(ctx, actor, nil, "confirm_software", target, audit.Deny)
			return err
		}
		return s.audit(ctx, actor, nil, "confirm_software", target, audit.Allow)
	default:
		return s.audit(ctx, actor, nil, "confirm_software", target, audit.Allow)
	}
}

// ConfirmClientUpdate 当前登录者确认后才记下本机已装版本；焊接中拒绝。
func (s *Updates) ConfirmClientUpdate(ctx context.Context, bag *Bag, clocks Clocks, version int64) error {
	target := softwareTarget(SoftwareClientAPK, version)
	src := bagTimeSource(*bag)
	if bag.OperatorID == nil {
		_ = s.auditTimed(ctx, nil, nil, "confirm_software", target, audit.Deny, src)
		return domain.ErrForbidden
	}
	if bag.Welding {
		_ = s.auditTimed(ctx, personActor(bag), nil, "confirm_software", target, audit.Deny, src)
		return domain.ErrForbidden
	}
	node := EvaluateRuntime(*bag, clocks, NodeOpen)
	if node.Decision != NodeAllow {
		_ = s.auditTimed(ctx, personActor(bag), nil, "confirm_software", target, audit.Deny, node.TimeSource)
		return domain.ErrForbidden
	}
	if _, err := s.operatorAccount(ctx, *bag); err != nil {
		_ = s.auditTimed(ctx, personActor(bag), nil, "confirm_software", target, audit.Deny, src)
		return err
	}
	if version <= bag.SoftwareVersion {
		_ = s.auditTimed(ctx, personActor(bag), nil, "confirm_software", target, audit.Deny, src)
		return domain.ErrStaleRevision
	}
	pending, ok, err := s.pendingReplica(ctx, SoftwareClientAPK, bag.SoftwareVersion)
	if err != nil {
		return err
	}
	if !ok || pending.Version != version {
		_ = s.auditTimed(ctx, personActor(bag), nil, "confirm_software", target, audit.Deny, src)
		if !ok {
			return domain.ErrNotFound
		}
		return domain.ErrStaleRevision
	}
	bag.SoftwareVersion = version
	return s.auditTimed(ctx, personActor(bag), nil, "confirm_software", target, audit.Allow, src)
}

// PadClientSoftware 登录者看本厂已收最高客户端包元数据，不含字节。
func (s *Updates) PadClientSoftware(ctx context.Context, token string) (*SoftwareReplica, error) {
	if _, err := s.RequireActive(ctx, token); err != nil {
		return nil, err
	}
	max, err := s.store.MaxSoftwareReplica(ctx, SoftwareClientAPK)
	if err != nil {
		return nil, err
	}
	if max < 1 {
		return nil, nil
	}
	row, ok, err := s.completeReplica(ctx, SoftwareClientAPK, max)
	if err != nil || !ok {
		return nil, err
	}
	return &row, nil
}

// PullPadClientSoftware 登录者按版本拉客户端包；摘要不对不给。
func (s *Updates) PullPadClientSoftware(ctx context.Context, token string, version int64) ([]byte, error) {
	acc, err := s.RequireActive(ctx, token)
	target := softwareTarget(SoftwareClientAPK, version)
	if err != nil {
		_ = s.audit(ctx, nil, nil, "pull_software", target, audit.Deny)
		return nil, err
	}
	if version < 1 {
		_ = s.audit(ctx, &acc.ID, nil, "pull_software", target, audit.Deny)
		return nil, domain.ErrInvalidName
	}
	row, err := s.store.SoftwareReplica(ctx, SoftwareClientAPK, version)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "pull_software", target, audit.Deny)
		return nil, err
	}
	body, err := s.blobs.Get(ctx, row.ObjectKey)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "pull_software", target, audit.Deny)
		if errors.Is(err, blob.ErrNotFound) {
			return nil, domain.ErrIntegrity
		}
		return nil, err
	}
	if !digest.Match(body, row.Digest) {
		_ = s.audit(ctx, &acc.ID, nil, "pull_software", target, audit.Deny)
		return nil, domain.ErrIntegrity
	}
	if err := s.audit(ctx, &acc.ID, nil, "pull_software", target, audit.Allow); err != nil {
		return nil, err
	}
	return body, nil
}

const (
	SoftwareKeepLatest    = "latest"    // 该种类当前最高，提示或平板还要用
	SoftwareKeepInstalled = "installed" // 本厂正在跑
)

// SoftwareItem 是带保留原因的本厂副本行。
type SoftwareItem struct {
	SoftwareReplica
	Keep string `json:"keep"` // latest / installed / 空则可清
}

// ImagePrune 是本机清镜像进度。
type ImagePrune struct {
	Ready     bool   `json:"ready"`               // updater 已写结果
	OK        bool   `json:"ok"`                  // 清完
	Reclaimed string `json:"reclaimed,omitempty"` // 回报空间
}

// ListFactorySoftware 超管看本厂已收副本，按种类分列。
func (s *Updates) ListFactorySoftware(ctx context.Context, token, kind string) ([]SoftwareItem, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return nil, err
	}
	if err := s.can(ctx, acc, permManageAccount, nil); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "list_software", kind, audit.Deny)
		return nil, err
	}
	if kind != "" && !factoryPullKind(kind) {
		return nil, domain.ErrInvalidName
	}
	rows, err := s.store.ListSoftwareReplicas(ctx, kind)
	if err != nil {
		return nil, err
	}
	out := make([]SoftwareItem, 0, len(rows))
	for _, row := range rows {
		keep, err := s.softwareKeep(ctx, row.Kind, row.Version)
		if err != nil {
			return nil, err
		}
		out = append(out, SoftwareItem{SoftwareReplica: row, Keep: keep})
	}
	return out, nil
}

// 已装和当前最高必须留着。
func (s *Updates) softwareKeep(ctx context.Context, kind string, version int64) (string, error) {
	max, err := s.store.MaxSoftwareReplica(ctx, kind)
	if err != nil {
		return "", err
	}
	installed := int64(0)
	if kind == SoftwareFactoryService {
		installed, err = s.store.InstalledSoftware(ctx, kind)
		if err != nil {
			return "", err
		}
	}
	if version == installed && installed > 0 {
		return SoftwareKeepInstalled, nil
	}
	if version == max && max > 0 {
		return SoftwareKeepLatest, nil
	}
	return "", nil
}

// DeleteFactorySoftware 清掉一份不是最高也不是已装的本厂旧副本。
func (s *Updates) DeleteFactorySoftware(ctx context.Context, token, kind string, version int64) error {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return err
	}
	target := softwareTarget(kind, version)
	if err := s.can(ctx, acc, permManageAccount, nil); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "prune_software", target, audit.Deny)
		return err
	}
	if !factoryPullKind(kind) || version < 1 {
		_ = s.audit(ctx, &acc.ID, nil, "prune_software", target, audit.Deny)
		return domain.ErrInvalidName
	}
	keep, err := s.softwareKeep(ctx, kind, version)
	if err != nil {
		return err
	}
	if keep != "" {
		_ = s.audit(ctx, &acc.ID, nil, "prune_software", target, audit.Deny)
		return domain.ErrReferenced
	}
	row, err := s.store.SoftwareReplica(ctx, kind, version)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "prune_software", target, audit.Deny)
		return err
	}
	if err := s.blobs.Delete(ctx, row.ObjectKey); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "prune_software", target, audit.Deny)
		return err
	}
	if err := s.store.DeleteSoftwareReplica(ctx, kind, version); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "prune_software", target, audit.Deny)
		return err
	}
	return s.audit(ctx, &acc.ID, nil, "prune_software", target, audit.Allow)
}

// PruneFactorySoftware 清掉该种类所有可删旧副本；kind 空则两类都清。
func (s *Updates) PruneFactorySoftware(ctx context.Context, token, kind string) (int, error) {
	items, err := s.ListFactorySoftware(ctx, token, kind)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, item := range items {
		if item.Keep != "" {
			continue
		}
		if err := s.DeleteFactorySoftware(ctx, token, item.Kind, item.Version); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// RequestImagePrune 请本机 updater 清无用 app 镜像；ref 空则全部可清。
func (s *Updates) RequestImagePrune(ctx context.Context, token, ref string) error {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return err
	}
	target := imagePruneAuditTarget(ref)
	if err := s.can(ctx, acc, permManageAccount, nil); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "prune_images", target, audit.Deny)
		return err
	}
	if err := s.imagePruneTarget(ref); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "prune_images", target, audit.Deny)
		return err
	}
	if s.imageJanitor == nil {
		return s.audit(ctx, &acc.ID, nil, "prune_images", target, audit.Allow)
	}
	if err := s.imageJanitor.RequestPrune(ref); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "prune_images", target, audit.Deny)
		return err
	}
	return s.audit(ctx, &acc.ID, nil, "prune_images", target, audit.Allow)
}

// 点名清某一个标签时，current / previous 拒绝。
func (s *Updates) imagePruneTarget(ref string) error {
	if ref == "" {
		return nil
	}
	if !validImageRef(ref) {
		return domain.ErrInvalidName
	}
	if s.imageJanitor == nil {
		return nil
	}
	raw, err := s.imageJanitor.ImagesJSON()
	if err != nil {
		return err
	}
	if len(raw) == 0 {
		return domain.ErrNotFound
	}
	var usage ImageUsage
	if err := json.Unmarshal(raw, &usage); err != nil {
		return err
	}
	for _, it := range usage.Items {
		if it.Ref != ref {
			continue
		}
		if it.Keep != "" {
			return domain.ErrReferenced
		}
		return nil
	}
	return domain.ErrNotFound
}

// 镜像标签只允许仓库名加版本，挡住命令字符。
func validImageRef(ref string) bool {
	if ref == "" {
		return true
	}
	if len(ref) > 256 || strings.ContainsAny(ref, " \t\n\r;|&$`'\"\\<>") {
		return false
	}
	host, tag, ok := strings.Cut(ref, ":")
	return ok && host != "" && tag != "" && !strings.Contains(tag, ":")
}

// 审计对象：点名用标签，一键清用 docker。
func imagePruneAuditTarget(ref string) string {
	if ref == "" {
		return "docker"
	}
	return ref
}

// ImagePruneProgress 超管看 updater 清镜像结果。
func (s *Updates) ImagePruneProgress(ctx context.Context, token string) (ImagePrune, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return ImagePrune{}, err
	}
	if err := s.can(ctx, acc, permManageAccount, nil); err != nil {
		return ImagePrune{}, err
	}
	if s.imageJanitor == nil {
		return ImagePrune{Ready: true, OK: true, Reclaimed: "0B"}, nil
	}
	reclaimed, ok, present, err := s.imageJanitor.PruneResult()
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
	Database int64      `json:"database"` // 当前厂库字节
	Images   ImageUsage `json:"images"`   // 本机 app 镜像
}

// StorageUsage 超管看本机磁盘余量、对象存储已用、本厂库占用和本机 app 镜像。
func (s *Updates) StorageUsage(ctx context.Context, token string) (StorageUsage, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return StorageUsage{}, err
	}
	if err := s.can(ctx, acc, permManageAccount, nil); err != nil {
		return StorageUsage{}, err
	}
	out := StorageUsage{}
	if space, err := disk.Of("/"); err == nil {
		out.Disk = space
	}
	if s.blobs != nil {
		used, objects, err := s.blobs.Usage(ctx)
		if err != nil {
			return StorageUsage{}, err
		}
		out.OSS = BlobUsage{Used: used, Objects: objects}
		if named, ok := s.blobs.(interface{ Bucket() string }); ok {
			out.OSS.Bucket = named.Bucket()
		}
	}
	n, err := s.store.DatabaseSize(ctx)
	if err != nil {
		return StorageUsage{}, err
	}
	out.Database = n
	if s.imageJanitor != nil {
		raw, err := s.imageJanitor.ImagesJSON()
		if err != nil {
			return StorageUsage{}, err
		}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &out.Images); err != nil {
				return StorageUsage{}, err
			}
		}
	}
	if out.Images.Items == nil {
		out.Images.Items = []ImageItem{}
	}
	return out, nil
}
