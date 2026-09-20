package service

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
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
type Updates struct{ *kernel }

// SetApplyOutcome 测试注入落地结果；生产保持默认只落盘。
func (s *Updates) SetApplyOutcome(out ApplyOutcome) { s.applyOutcome = out }

// MarkInstalled updater 探活通过后记已装；已是该版本则幂等。
func (s *Updates) MarkInstalled(ctx context.Context, kind string, version int64) error {
	target := softwareTarget(kind, version)
	if err := s.store.PutInstalledSoftware(ctx, kind, version); err != nil {
		if errors.Is(err, domain.ErrStaleRevision) {
			return nil
		}
		_ = s.audit(ctx, nil, nil, nil, "apply_software", target, audit.Deny)
		return err
	}
	return s.audit(ctx, nil, nil, nil, "apply_software", target, audit.Allow)
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
	name = strings.TrimSpace(name)
	n := utf8.RuneCountInString(name)
	if n < 1 || n > 80 {
		return "", domain.ErrInvalidName
	}
	return name, nil
}

// softwareTarget 审计对象：种类和版本。
func softwareTarget(kind string, version int64) string {
	return kind + " v" + strconv.FormatInt(version, 10)
}

// requireEnrolledFactory 已认领且未注销才能拉厂包。
func (s *Updates) requireEnrolledFactory(ctx context.Context, factoryID uuid.UUID) error {
	fac, err := s.store.FactoryByID(ctx, factoryID)
	if err != nil {
		return err
	}
	if fac.EnrolledAt == nil || fac.Status == FactoryRetired {
		return domain.ErrForbidden
	}
	return nil
}

// PublishSoftware 由 WAN 管理员发布一条只向前的软件版本。
func (s *Updates) PublishSoftware(ctx context.Context, token, kind, versionName string, version int64, body []byte) (SoftwareRelease, error) {
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		return SoftwareRelease{}, err
	}
	target := softwareTarget(kind, version)
	if !validSoftwareKind(kind) || version < 1 {
		_ = s.audit(ctx, &admin.ID, nil, nil, "publish_software", target, audit.Deny)
		return SoftwareRelease{}, domain.ErrInvalidName
	}
	versionName, err = normalizeVersionName(versionName)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "publish_software", target, audit.Deny)
		return SoftwareRelease{}, err
	}
	max, err := s.store.MaxSoftwareRelease(ctx, kind)
	if err != nil {
		return SoftwareRelease{}, err
	}
	// 先核对已有原件，避免不同摘要覆盖对象。
	existing, err := s.store.SoftwareRelease(ctx, kind, version)
	if err == nil {
		if !digest.Match(body, existing.Digest) {
			_ = s.audit(ctx, &admin.ID, nil, nil, "publish_software", target, audit.Deny)
			return SoftwareRelease{}, domain.ErrIntegrity
		}
		// 同摘要再发把对象补回，避免进程重启丢内存存根后确认失败。
		if err := s.blobs.Put(ctx, existing.ObjectKey, body); err != nil {
			_ = s.audit(ctx, &admin.ID, nil, nil, "publish_software", target, audit.Deny)
			return SoftwareRelease{}, err
		}
		if err := s.audit(ctx, &admin.ID, nil, nil, "publish_software", target, audit.Allow); err != nil {
			return SoftwareRelease{}, err
		}
		// 同摘要再发也通知一次，漏掉的厂还能补拉。
		s.notifyFactoryPack(ctx, existing)
		return existing, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return SoftwareRelease{}, err
	}
	if version < max {
		_ = s.audit(ctx, &admin.ID, nil, nil, "publish_software", target, audit.Deny)
		return SoftwareRelease{}, domain.ErrStaleRevision
	}
	if kind == SoftwareWANService {
		installed, err := s.store.InstalledSoftware(ctx, kind)
		if err != nil {
			return SoftwareRelease{}, err
		}
		if version < installed {
			_ = s.audit(ctx, &admin.ID, nil, nil, "publish_software", target, audit.Deny)
			return SoftwareRelease{}, domain.ErrStaleRevision
		}
	}
	sum := digest.Sum(body)
	key := store.SoftwareObjectKey(kind, version)
	// 包字节进对象存根，库只留摘要。
	if err := s.blobs.Put(ctx, key, body); err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "publish_software", target, audit.Deny)
		return SoftwareRelease{}, err
	}
	row, err := s.store.InsertSoftwareRelease(ctx, SoftwareRelease{
		Kind: kind, Version: version, VersionName: versionName, Digest: sum, ObjectKey: key,
	})
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "publish_software", target, audit.Deny)
		return SoftwareRelease{}, err
	}
	if err := s.audit(ctx, &admin.ID, nil, nil, "publish_software", target, audit.Allow); err != nil {
		return SoftwareRelease{}, err
	}
	// 有效厂立刻去拉厂服务包或 APK，正文仍走 HTTPS。
	s.notifyFactoryPack(ctx, row)
	return row, nil
}

// ListSoftwareReleases 给管理台列已发布版本；kind 空则三类都给。
func (s *Updates) ListSoftwareReleases(ctx context.Context, token, kind string) ([]SoftwareRelease, error) {
	if _, err := s.RequireAdmin(ctx, token); err != nil {
		return nil, err
	}
	if kind != "" && !validSoftwareKind(kind) {
		return nil, domain.ErrInvalidName
	}
	return s.store.ListSoftwareReleases(ctx, kind)
}

// LatestSoftware 已认领厂看该种类当前最高版本；不含字节，且不能拉云端包。
func (s *Updates) LatestSoftware(ctx context.Context, factoryID uuid.UUID, kind string) (SoftwareRelease, error) {
	if !factoryPullKind(kind) {
		return SoftwareRelease{}, domain.ErrForbidden
	}
	if err := s.requireEnrolledFactory(ctx, factoryID); err != nil {
		return SoftwareRelease{}, err
	}
	max, err := s.store.MaxSoftwareRelease(ctx, kind)
	if err != nil {
		return SoftwareRelease{}, err
	}
	if max < 1 {
		return SoftwareRelease{}, domain.ErrNotFound
	}
	return s.store.SoftwareRelease(ctx, kind, max)
}

// PullSoftware 已认领厂按版本拉包；只给当前最高，摘要不对不给。
func (s *Updates) PullSoftware(ctx context.Context, factoryID uuid.UUID, kind string, version int64) (SoftwareOffer, error) {
	target := softwareTarget(kind, version)
	if !factoryPullKind(kind) || version < 1 {
		_ = s.audit(ctx, nil, nil, &factoryID, "pull_software", target, audit.Deny)
		if version < 1 {
			return SoftwareOffer{}, domain.ErrInvalidName
		}
		return SoftwareOffer{}, domain.ErrForbidden
	}
	if err := s.requireEnrolledFactory(ctx, factoryID); err != nil {
		_ = s.audit(ctx, nil, nil, &factoryID, "pull_software", target, audit.Deny)
		return SoftwareOffer{}, err
	}
	max, err := s.store.MaxSoftwareRelease(ctx, kind)
	if err != nil {
		return SoftwareOffer{}, err
	}
	if version < max {
		_ = s.audit(ctx, nil, nil, &factoryID, "pull_software", target, audit.Deny)
		return SoftwareOffer{}, domain.ErrStaleRevision
	}
	rel, err := s.store.SoftwareRelease(ctx, kind, version)
	if err != nil {
		_ = s.audit(ctx, nil, nil, &factoryID, "pull_software", target, audit.Deny)
		return SoftwareOffer{}, err
	}
	body, err := s.blobs.Get(ctx, rel.ObjectKey)
	if err != nil {
		_ = s.audit(ctx, nil, nil, &factoryID, "pull_software", target, audit.Deny)
		if errors.Is(err, blob.ErrNotFound) {
			return SoftwareOffer{}, domain.ErrIntegrity
		}
		return SoftwareOffer{}, err
	}
	if !digest.Match(body, rel.Digest) {
		_ = s.audit(ctx, nil, nil, &factoryID, "pull_software", target, audit.Deny)
		return SoftwareOffer{}, domain.ErrIntegrity
	}
	if err := s.audit(ctx, nil, nil, &factoryID, "pull_software", target, audit.Allow); err != nil {
		return SoftwareOffer{}, err
	}
	return SoftwareOffer{
		Kind: rel.Kind, Version: rel.Version, VersionName: rel.VersionName,
		Digest: rel.Digest, Body: body,
	}, nil
}

// CurrentWANSoftware 本机已落地的云端服务版本；未装过则版本为 0。
func (s *Updates) CurrentWANSoftware(ctx context.Context, token string) (SoftwareRelease, error) {
	if _, err := s.RequireAdmin(ctx, token); err != nil {
		return SoftwareRelease{}, err
	}
	installed, err := s.store.InstalledSoftware(ctx, SoftwareWANService)
	if err != nil {
		return SoftwareRelease{}, err
	}
	if installed < 1 {
		return SoftwareRelease{Kind: SoftwareWANService}, nil
	}
	row, err := s.store.SoftwareRelease(ctx, SoftwareWANService, installed)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return SoftwareRelease{Kind: SoftwareWANService, Version: installed}, nil
		}
		return SoftwareRelease{}, err
	}
	return row, nil
}

// PendingWANSoftware 管理员看待确认的云端服务包；没有则空。
func (s *Updates) PendingWANSoftware(ctx context.Context, token string) (*SoftwareRelease, error) {
	if _, err := s.RequireAdmin(ctx, token); err != nil {
		return nil, err
	}
	installed, err := s.store.InstalledSoftware(ctx, SoftwareWANService)
	if err != nil {
		return nil, err
	}
	max, err := s.store.MaxSoftwareRelease(ctx, SoftwareWANService)
	if err != nil {
		return nil, err
	}
	if max <= installed {
		return nil, nil
	}
	row, err := s.store.SoftwareRelease(ctx, SoftwareWANService, max)
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// ConfirmWANUpdate 仅 WAN 管理员确认后才写待切换；落地成功才记已装。
func (s *Updates) ConfirmWANUpdate(ctx context.Context, token, kind string, version int64) error {
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		return err
	}
	target := softwareTarget(kind, version)
	if kind != SoftwareWANService {
		_ = s.audit(ctx, &admin.ID, nil, nil, "confirm_software", target, audit.Deny)
		return domain.ErrForbidden
	}
	pending, err := s.PendingWANSoftware(ctx, token)
	if err != nil {
		return err
	}
	if pending == nil || pending.Version != version {
		_ = s.audit(ctx, &admin.ID, nil, nil, "confirm_software", target, audit.Deny)
		if pending == nil {
			return domain.ErrNotFound
		}
		return domain.ErrStaleRevision
	}
	body, err := s.blobs.Get(ctx, pending.ObjectKey)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "confirm_software", target, audit.Deny)
		if errors.Is(err, blob.ErrNotFound) {
			return domain.ErrIntegrity
		}
		return err
	}
	if !digest.Match(body, pending.Digest) {
		_ = s.audit(ctx, &admin.ID, nil, nil, "confirm_software", target, audit.Deny)
		return domain.ErrIntegrity
	}
	if s.pendingSink != nil {
		if err := s.pendingSink.Stage(kind, version, pending.Digest, body); err != nil {
			_ = s.audit(ctx, &admin.ID, nil, nil, "confirm_software", target, audit.Deny)
			return err
		}
	}
	return s.finishApply(ctx, &admin.ID, kind, version, target)
}

// ApplyProgress 管理员看本机更换进度；就绪则补记已装。
type ApplyProgress struct {
	Phase   string `json:"phase"`           // idle / applying / ok / fail
	Kind    string `json:"kind"`            // 待切换或已落地种类
	Version int64  `json:"version"`         // 对应版本；idle 为 0
	Error   string `json:"error,omitempty"` // fail 时 updater 原因
}

// ApplyProgress 读盘上进度；探活通过才记已装。
func (s *Updates) ApplyProgress(ctx context.Context, token string) (ApplyProgress, error) {
	if _, err := s.RequireAdmin(ctx, token); err != nil {
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
		_ = s.audit(ctx, actor, nil, nil, "confirm_software", target, audit.Deny)
		return domain.ErrSoftwareInstallFailed
	case ApplyOK:
		if err := s.store.PutInstalledSoftware(ctx, kind, version); err != nil {
			_ = s.audit(ctx, actor, nil, nil, "confirm_software", target, audit.Deny)
			return err
		}
		return s.audit(ctx, actor, nil, nil, "confirm_software", target, audit.Allow)
	default:
		return s.audit(ctx, actor, nil, nil, "confirm_software", target, audit.Allow)
	}
}

// PullSoftwareForFactory 给通道 HTTPS 拉当前最高厂包；语义与 PullSoftware 相同。
func (s *Updates) PullSoftwareForFactory(ctx context.Context, factoryID uuid.UUID, kind string, version int64) (SoftwareOffer, error) {
	return s.PullSoftware(ctx, factoryID, kind, version)
}

const (
	SoftwareKeepLatest    = "latest"    // 该种类当前最高，厂还要来拉
	SoftwareKeepInstalled = "installed" // 本机正在跑
)

// SoftwareItem 是带保留原因的已发布行，给管理台分列。
type SoftwareItem struct {
	SoftwareRelease
	Keep string `json:"keep"` // latest / installed / 空则可清
}

// ImagePrune 是本机清镜像进度。
type ImagePrune struct {
	Ready     bool   `json:"ready"`               // updater 已写结果
	OK        bool   `json:"ok"`                  // 清完
	Reclaimed string `json:"reclaimed,omitempty"` // 回报空间
}

// ListSoftwareItems 按种类列出已发布版本并标出不能删的行。
func (s *Updates) ListSoftwareItems(ctx context.Context, token, kind string) ([]SoftwareItem, error) {
	rows, err := s.ListSoftwareReleases(ctx, token, kind)
	if err != nil {
		return nil, err
	}
	out := make([]SoftwareItem, 0, len(rows))
	for _, row := range rows {
		keep, err := s.softwareKeep(ctx, row.Kind, row.Version)
		if err != nil {
			return nil, err
		}
		out = append(out, SoftwareItem{SoftwareRelease: row, Keep: keep})
	}
	return out, nil
}

// 已装和当前最高必须留着。
func (s *Updates) softwareKeep(ctx context.Context, kind string, version int64) (string, error) {
	max, err := s.store.MaxSoftwareRelease(ctx, kind)
	if err != nil {
		return "", err
	}
	installed := int64(0)
	if kind == SoftwareWANService {
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

// DeleteSoftware 清掉一份不是最高也不是已装的旧包。
func (s *Updates) DeleteSoftware(ctx context.Context, token, kind string, version int64) error {
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		return err
	}
	target := softwareTarget(kind, version)
	if !validSoftwareKind(kind) || version < 1 {
		_ = s.audit(ctx, &admin.ID, nil, nil, "prune_software", target, audit.Deny)
		return domain.ErrInvalidName
	}
	keep, err := s.softwareKeep(ctx, kind, version)
	if err != nil {
		return err
	}
	if keep != "" {
		_ = s.audit(ctx, &admin.ID, nil, nil, "prune_software", target, audit.Deny)
		return domain.ErrReferenced
	}
	row, err := s.store.SoftwareRelease(ctx, kind, version)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "prune_software", target, audit.Deny)
		return err
	}
	if err := s.blobs.Delete(ctx, row.ObjectKey); err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "prune_software", target, audit.Deny)
		return err
	}
	if err := s.store.DeleteSoftwareRelease(ctx, kind, version); err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "prune_software", target, audit.Deny)
		return err
	}
	return s.audit(ctx, &admin.ID, nil, nil, "prune_software", target, audit.Allow)
}

// PruneSoftware 清掉该种类所有可删旧包；kind 空则三类都清。
func (s *Updates) PruneSoftware(ctx context.Context, token, kind string) (int, error) {
	items, err := s.ListSoftwareItems(ctx, token, kind)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, item := range items {
		if item.Keep != "" {
			continue
		}
		if err := s.DeleteSoftware(ctx, token, item.Kind, item.Version); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// RequestImagePrune 请本机 updater 清无用 app 镜像；ref 空则全部可清。
func (s *Updates) RequestImagePrune(ctx context.Context, token, ref string) error {
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		return err
	}
	target := imagePruneAuditTarget(ref)
	if err := s.imagePruneTarget(ref); err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "prune_images", target, audit.Deny)
		return err
	}
	if s.imageJanitor == nil {
		return s.audit(ctx, &admin.ID, nil, nil, "prune_images", target, audit.Allow)
	}
	if err := s.imageJanitor.RequestPrune(ref); err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "prune_images", target, audit.Deny)
		return err
	}
	return s.audit(ctx, &admin.ID, nil, nil, "prune_images", target, audit.Allow)
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

// ImagePruneProgress 看 updater 清镜像结果。
func (s *Updates) ImagePruneProgress(ctx context.Context, token string) (ImagePrune, error) {
	if _, err := s.RequireAdmin(ctx, token); err != nil {
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
	Database int64      `json:"database"` // 当前库字节
	Images   ImageUsage `json:"images"`   // 本机 app 镜像
}

// StorageUsage 超管看本机磁盘余量、对象存储已用、库占用和本机 app 镜像。
func (s *Updates) StorageUsage(ctx context.Context, token string) (StorageUsage, error) {
	if _, err := s.RequireAdmin(ctx, token); err != nil {
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
