package service

import (
	"bytes"
	"context"
	"errors"
	"strconv"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/blob"
	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/nodekey"
	"wmesh/factory/internal/platform/softwaresign"
	"wmesh/factory/internal/store"
)

const (
	SoftwareFactoryService = store.SoftwareFactoryService // 厂端服务包
	SoftwareClientAPK      = store.SoftwareClientAPK      // 客户端 APK
)

// SoftwareSnapshot 是云端下到本厂的一份软件包；本圈可带正文。
type SoftwareSnapshot struct {
	Kind            string    `json:"kind"`            // factory_service / client_apk
	Version         int64     `json:"version"`         // 单调整数
	VersionName     string    `json:"versionName"`     // 给人看的版本名
	Digest          []byte    `json:"digest"`          // 包文件 SHA-256
	Signature       []byte    `json:"signature"`       // WAN 对目标本厂的签名
	WANPublicKey    []byte    `json:"wanPublicKey"`    // 须与本厂 wan_trust 一致
	TargetFactoryID uuid.UUID `json:"targetFactoryId"` // 目标厂
	Body            []byte    `json:"body"`            // 包字节
}

// ClientUpdateReady 是本厂签给某平板的客户端包就绪信封。
type ClientUpdateReady struct {
	Kind        string    `json:"kind"`        // 固定 client_apk
	Version     int64     `json:"version"`     // 版本
	VersionName string    `json:"versionName"` // 版本名
	Digest      []byte    `json:"digest"`      // SHA-256
	Signature   []byte    `json:"signature"`   // 本厂对目标 Client 的签名
	ClientID    uuid.UUID `json:"clientId"`    // 目标平板
	Body        []byte    `json:"body"`        // 包字节
}

// FactoryInstaller 确认后替换厂服务包；不得覆盖库与对象存储。
type FactoryInstaller interface {
	// Apply 安装该版本厂服务包；失败须保持旧进程可继续。
	Apply(ctx context.Context, version int64, body []byte) error
}

type okInstaller struct{}

// Apply 测试默认成功，不碰 Docker；生产由 hub 注入 docker load。
func (okInstaller) Apply(context.Context, int64, []byte) error { return nil }

// Updates 管本厂收软件包、超管确认厂服务、本机确认客户端。
type Updates struct{ *kernel }

// softwareTarget 审计对象：种类和版本。
func softwareTarget(kind string, version int64) string {
	return kind + " v" + strconv.FormatInt(version, 10)
}

// validSoftwareKind 只认厂服务包和客户端包。
func validSoftwareKind(kind string) bool {
	return kind == SoftwareFactoryService || kind == SoftwareClientAPK
}

// SetFactoryInstaller 测试注入失败；生产由 hub 注入 docker 安装器。
func (s *Updates) SetFactoryInstaller(in FactoryInstaller) {
	if in == nil {
		in = okInstaller{}
	}
	s.installer = in
}

// SetWANPublicKey 夹具登记 WAN 验签公钥；换钥拒绝。
func (s *Updates) SetWANPublicKey(ctx context.Context, publicKey []byte) error {
	return s.store.PutWANTrust(ctx, publicKey)
}

// AcceptSoftwareDelivery 先验 WAN 签名和摘要，再留下只读副本。
func (s *Updates) AcceptSoftwareDelivery(ctx context.Context, snap SoftwareSnapshot) error {
	target := softwareTarget(snap.Kind, snap.Version)
	if !validSoftwareKind(snap.Kind) || snap.Version < 1 {
		_ = s.audit(ctx, nil, nil, "accept_software", target, audit.Deny)
		return domain.ErrInvalidName
	}
	fid := s.store.FactoryID()
	if snap.TargetFactoryID != fid {
		_ = s.audit(ctx, nil, nil, "accept_software", target, audit.Deny)
		return domain.ErrForbidden
	}
	trust, err := s.store.WANTrust(ctx)
	if errors.Is(err, domain.ErrNotFound) {
		// 通道首次送达记下快照公钥；换钥仍拒绝。
		if err := s.store.PutWANTrust(ctx, snap.WANPublicKey); err != nil {
			_ = s.audit(ctx, nil, nil, "accept_software", target, audit.Deny)
			return err
		}
		trust, err = s.store.WANTrust(ctx)
	}
	if err != nil {
		_ = s.audit(ctx, nil, nil, "accept_software", target, audit.Deny)
		return err
	}
	if !bytes.Equal(trust.PublicKey, snap.WANPublicKey) {
		_ = s.audit(ctx, nil, nil, "accept_software", target, audit.Deny)
		return domain.ErrIntegrity
	}
	if !digest.Match(snap.Body, snap.Digest) {
		_ = s.audit(ctx, nil, nil, "accept_software", target, audit.Deny)
		return domain.ErrIntegrity
	}
	msg := softwaresign.Message(snap.Kind, snap.Version, snap.Digest, fid)
	if !nodekey.Verify(trust.PublicKey, msg, snap.Signature) {
		_ = s.audit(ctx, nil, nil, "accept_software", target, audit.Deny)
		return domain.ErrIntegrity
	}
	installed, err := s.store.InstalledSoftware(ctx, snap.Kind)
	if err != nil {
		return err
	}
	if snap.Kind == SoftwareFactoryService && snap.Version < installed {
		_ = s.audit(ctx, nil, nil, "accept_software", target, audit.Deny)
		return domain.ErrStaleRevision
	}
	max, err := s.store.MaxSoftwareReplica(ctx, snap.Kind)
	if err != nil {
		return err
	}
	if snap.Version < max {
		_ = s.audit(ctx, nil, nil, "accept_software", target, audit.Deny)
		return domain.ErrStaleRevision
	}
	key := store.SoftwareObjectKey(snap.Kind, snap.Version)
	if err := s.blobs.Put(ctx, key, snap.Body); err != nil {
		_ = s.audit(ctx, nil, nil, "accept_software", target, audit.Deny)
		return err
	}
	if _, err := s.store.InsertSoftwareReplica(ctx, SoftwareReplica{
		Kind: snap.Kind, Version: snap.Version, VersionName: snap.VersionName,
		Digest: snap.Digest, ObjectKey: key, Signature: snap.Signature,
	}); err != nil {
		_ = s.audit(ctx, nil, nil, "accept_software", target, audit.Deny)
		return err
	}
	return s.audit(ctx, nil, nil, "accept_software", target, audit.Allow)
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

// PendingFactorySoftware 超管看待确认的厂服务包；没有则空。
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
	row, err := s.store.SoftwareReplica(ctx, kind, max)
	if err != nil {
		return SoftwareReplica{}, false, err
	}
	return row, true, nil
}

// ConfirmFactoryUpdate 仅本厂超管确认后才替换厂服务；安装失败保持旧版本。
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
	if err := s.installer.Apply(ctx, version, body); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "confirm_software", target, audit.Deny)
		return domain.ErrSoftwareInstallFailed
	}
	if err := s.store.PutInstalledSoftware(ctx, kind, version); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "confirm_software", target, audit.Deny)
		return err
	}
	return s.audit(ctx, &acc.ID, nil, "confirm_software", target, audit.Allow)
}

// ReadyClientUpdate 给已绑定平板签一份当前最高客户端包就绪信封。
func (s *Updates) ReadyClientUpdate(ctx context.Context, clientID uuid.UUID) (ClientUpdateReady, error) {
	cl, err := s.store.ClientByID(ctx, clientID)
	if err != nil {
		_ = s.audit(ctx, nil, nil, "ready_software", clientID.String(), audit.Deny)
		return ClientUpdateReady{}, err
	}
	if cl.Status != ClientStatusBound {
		_ = s.audit(ctx, nil, nil, "ready_software", clientID.String(), audit.Deny)
		return ClientUpdateReady{}, domain.ErrBindingVoid
	}
	max, err := s.store.MaxSoftwareReplica(ctx, SoftwareClientAPK)
	if err != nil {
		return ClientUpdateReady{}, err
	}
	if max < 1 {
		_ = s.audit(ctx, nil, nil, "ready_software", clientID.String(), audit.Deny)
		return ClientUpdateReady{}, domain.ErrNotFound
	}
	rep, err := s.store.SoftwareReplica(ctx, SoftwareClientAPK, max)
	if err != nil {
		_ = s.audit(ctx, nil, nil, "ready_software", clientID.String(), audit.Deny)
		return ClientUpdateReady{}, err
	}
	body, err := s.blobs.Get(ctx, rep.ObjectKey)
	if err != nil {
		_ = s.audit(ctx, nil, nil, "ready_software", clientID.String(), audit.Deny)
		if errors.Is(err, blob.ErrNotFound) {
			return ClientUpdateReady{}, domain.ErrIntegrity
		}
		return ClientUpdateReady{}, err
	}
	key, err := s.ensureSigningKey(ctx)
	if err != nil {
		return ClientUpdateReady{}, err
	}
	sig := nodekey.Sign(key.PrivateKey, softwaresign.Message(rep.Kind, rep.Version, rep.Digest, clientID))
	if err := s.audit(ctx, nil, nil, "ready_software", softwareTarget(rep.Kind, rep.Version)+" client="+clientID.String(), audit.Allow); err != nil {
		return ClientUpdateReady{}, err
	}
	return ClientUpdateReady{
		Kind: rep.Kind, Version: rep.Version, VersionName: rep.VersionName,
		Digest: rep.Digest, Signature: sig, ClientID: clientID, Body: body,
	}, nil
}

// ConfirmClientUpdate 当前登录者确认后才记下本机已装版本；焊接中拒绝。
func (s *Updates) ConfirmClientUpdate(ctx context.Context, bag *Bag, clocks Clocks, ready ClientUpdateReady) error {
	target := softwareTarget(ready.Kind, ready.Version)
	src := bagTimeSource(*bag)
	if bag.OperatorID == nil {
		_ = s.auditTimed(ctx, nil, nil, "confirm_software", target, audit.Deny, src)
		return domain.ErrForbidden
	}
	if ready.Kind != SoftwareClientAPK || ready.ClientID != bag.ClientID {
		_ = s.auditTimed(ctx, personActor(bag), nil, "confirm_software", target, audit.Deny, src)
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
	if ready.Version <= bag.SoftwareVersion {
		_ = s.auditTimed(ctx, personActor(bag), nil, "confirm_software", target, audit.Deny, src)
		return domain.ErrStaleRevision
	}
	pending, ok, err := s.pendingReplica(ctx, SoftwareClientAPK, bag.SoftwareVersion)
	if err != nil {
		return err
	}
	if !ok || pending.Version != ready.Version {
		_ = s.auditTimed(ctx, personActor(bag), nil, "confirm_software", target, audit.Deny, src)
		if !ok {
			return domain.ErrNotFound
		}
		return domain.ErrStaleRevision
	}
	if !digest.Match(ready.Body, ready.Digest) || !bytes.Equal(ready.Digest, pending.Digest) {
		_ = s.auditTimed(ctx, personActor(bag), nil, "confirm_software", target, audit.Deny, src)
		return domain.ErrIntegrity
	}
	msg := softwaresign.Message(ready.Kind, ready.Version, ready.Digest, bag.ClientID)
	if !nodekey.Verify(bag.FactoryPublic, msg, ready.Signature) {
		_ = s.auditTimed(ctx, personActor(bag), nil, "confirm_software", target, audit.Deny, src)
		return domain.ErrIntegrity
	}
	bag.SoftwareVersion = ready.Version
	return s.auditTimed(ctx, personActor(bag), nil, "confirm_software", target, audit.Allow, src)
}
