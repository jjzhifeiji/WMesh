package service

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/audit"
	"wmesh/global/internal/platform/blob"
	"wmesh/global/internal/platform/digest"
	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/platform/nodekey"
	"wmesh/global/internal/platform/softwaresign"
	"wmesh/global/internal/store"
)

const (
	SoftwareFactoryService = store.SoftwareFactoryService // 厂端服务包
	SoftwareClientAPK      = store.SoftwareClientAPK      // 客户端 APK
)

// SoftwareSnapshot 是下到某厂的一份软件包；本圈可带正文供夹具送达。
type SoftwareSnapshot struct {
	Kind            string    `json:"kind"`            // factory_service / client_apk
	Version         int64     `json:"version"`         // 单调整数
	VersionName     string    `json:"versionName"`     // 给人看的版本名
	Digest          []byte    `json:"digest"`          // 包文件 SHA-256
	Signature       []byte    `json:"signature"`       // WAN 对目标厂的签名
	WANPublicKey    []byte    `json:"wanPublicKey"`    // 验签公钥
	TargetFactoryID uuid.UUID `json:"targetFactoryId"` // 目标厂
	Body            []byte    `json:"body"`            // 包字节；通道圈改为另拉
}

// Updates 管软件发布与按厂下发，不替厂确认安装。
type Updates struct{ *kernel }

// validSoftwareKind 只认厂服务包和客户端包。
func validSoftwareKind(kind string) bool {
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

// softwareTarget 审计对象：种类、版本和目标厂。
func softwareTarget(kind string, version int64, factoryID uuid.UUID) string {
	return kind + " v" + strconv.FormatInt(version, 10) + " factory=" + factoryID.String()
}

// ensureWANKey 没有签发钥则当场生成，私钥不外送。
func (s *Updates) ensureWANKey(ctx context.Context) (WANSigningKey, error) {
	k, err := s.store.WANSigningKey(ctx)
	if err == nil {
		return k, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return WANSigningKey{}, err
	}
	pub, priv, err := nodekey.Generate()
	if err != nil {
		return WANSigningKey{}, err
	}
	return s.store.PutWANSigningKey(ctx, pub, priv)
}

// SigningPublicKey 给出云端软件验签公钥；私钥不外送。
func (s *Updates) SigningPublicKey(ctx context.Context) ([]byte, error) {
	k, err := s.ensureWANKey(ctx)
	if err != nil {
		return nil, err
	}
	return k.PublicKey, nil
}

// PublishSoftware 由 WAN 管理员发布一条只向前的软件版本。
func (s *Updates) PublishSoftware(ctx context.Context, token, kind, versionName string, version int64, body []byte) (SoftwareRelease, error) {
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		return SoftwareRelease{}, err
	}
	target := kind + " v" + strconv.FormatInt(version, 10)
	if !validSoftwareKind(kind) || version < 1 {
		_ = s.audit(ctx, &admin.ID, nil, nil, "publish_software", target, audit.Deny)
		return SoftwareRelease{}, domain.ErrInvalidName
	}
	versionName, err = normalizeVersionName(versionName)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "publish_software", target, audit.Deny)
		return SoftwareRelease{}, err
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
	return row, nil
}

// DistributeSoftware 把已发布版本下到已认领工厂；低版本拒绝，同版本幂等。
func (s *Updates) DistributeSoftware(ctx context.Context, token, kind string, version int64, factoryID uuid.UUID) (SoftwareSnapshot, error) {
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		return SoftwareSnapshot{}, err
	}
	target := softwareTarget(kind, version, factoryID)
	if !validSoftwareKind(kind) || version < 1 {
		_ = s.audit(ctx, &admin.ID, nil, &factoryID, "distribute_software", target, audit.Deny)
		return SoftwareSnapshot{}, domain.ErrInvalidName
	}
	rel, err := s.store.SoftwareRelease(ctx, kind, version)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, &factoryID, "distribute_software", target, audit.Deny)
		return SoftwareSnapshot{}, err
	}
	fac, err := s.store.FactoryByID(ctx, factoryID)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, &factoryID, "distribute_software", target, audit.Deny)
		return SoftwareSnapshot{}, err
	}
	// 未认领或已注销不得下发。
	if fac.EnrolledAt == nil || fac.Status == FactoryRetired {
		_ = s.audit(ctx, &admin.ID, nil, &factoryID, "distribute_software", target, audit.Deny)
		return SoftwareSnapshot{}, domain.ErrForbidden
	}
	max, err := s.store.MaxSoftwareDistributed(ctx, kind, factoryID)
	if err != nil {
		return SoftwareSnapshot{}, err
	}
	if version < max {
		_ = s.audit(ctx, &admin.ID, nil, &factoryID, "distribute_software", target, audit.Deny)
		return SoftwareSnapshot{}, domain.ErrStaleRevision
	}
	body, err := s.blobs.Get(ctx, rel.ObjectKey)
	if err != nil {
		if errors.Is(err, blob.ErrNotFound) {
			_ = s.audit(ctx, &admin.ID, nil, &factoryID, "distribute_software", target, audit.Deny)
			return SoftwareSnapshot{}, domain.ErrIntegrity
		}
		return SoftwareSnapshot{}, err
	}
	if !digest.Match(body, rel.Digest) {
		_ = s.audit(ctx, &admin.ID, nil, &factoryID, "distribute_software", target, audit.Deny)
		return SoftwareSnapshot{}, domain.ErrIntegrity
	}
	key, err := s.ensureWANKey(ctx)
	if err != nil {
		return SoftwareSnapshot{}, err
	}
	sig := nodekey.Sign(key.PrivateKey, softwaresign.Message(kind, version, rel.Digest, factoryID))
	dist, err := s.store.InsertSoftwareDistribution(ctx, SoftwareDistribution{
		Kind: kind, Version: version, FactoryID: factoryID, Signature: sig,
	})
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, &factoryID, "distribute_software", target, audit.Deny)
		return SoftwareSnapshot{}, err
	}
	if err := s.audit(ctx, &admin.ID, nil, &factoryID, "distribute_software", target, audit.Allow); err != nil {
		return SoftwareSnapshot{}, err
	}
	s.notifySoftware(factoryID, kind, version)
	return SoftwareSnapshot{
		Kind: kind, Version: version, VersionName: rel.VersionName, Digest: rel.Digest,
		Signature: dist.Signature, WANPublicKey: key.PublicKey, TargetFactoryID: factoryID, Body: body,
	}, nil
}

// ListSoftwareReleases 给管理台列已发布版本；kind 空则两类都给。
func (s *Updates) ListSoftwareReleases(ctx context.Context, token, kind string) ([]SoftwareRelease, error) {
	if _, err := s.RequireAdmin(ctx, token); err != nil {
		return nil, err
	}
	if kind != "" && !validSoftwareKind(kind) {
		return nil, domain.ErrInvalidName
	}
	// 列出已发布元数据，不含包字节。
	return s.store.ListSoftwareReleases(ctx, kind)
}

// SnapshotsForFactory 按已下发记录重建快照，供通道回连补送。
func (s *Updates) SnapshotsForFactory(ctx context.Context, factoryID uuid.UUID) ([]SoftwareSnapshot, error) {
	// 取该厂已下发记录，再配发布元数据和对象存根。
	dists, err := s.store.ListSoftwareDistributions(ctx, factoryID)
	if err != nil || len(dists) == 0 {
		return nil, err
	}
	key, err := s.store.WANSigningKey(ctx)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]SoftwareSnapshot, 0, len(dists))
	for _, d := range dists {
		rel, err := s.store.SoftwareRelease(ctx, d.Kind, d.Version)
		if err != nil {
			continue
		}
		// 从对象存根取出包字节。
		body, err := s.blobs.Get(ctx, rel.ObjectKey)
		if err != nil {
			continue
		}
		out = append(out, SoftwareSnapshot{
			Kind: d.Kind, Version: d.Version, VersionName: rel.VersionName, Digest: rel.Digest,
			Signature: d.Signature, WANPublicKey: key.PublicKey, TargetFactoryID: factoryID, Body: body,
		})
	}
	return out, nil
}
