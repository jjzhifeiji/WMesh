package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/platform/nodekey"
)

const mqttProofSkew = 300 // CONNECT/HTTPS 签名允许的秒差

// CmdBus 把小指令交给 MQTT Broker；正文仍走 HTTPS。
type CmdBus interface {
	Publish(factoryID uuid.UUID, payload []byte) error
	Call(ctx context.Context, factoryID uuid.UUID, reqID string, payload []byte) ([]byte, error)
	Drop(factoryID uuid.UUID)
}

// SetBus 挂上厂端指令总线；测试可不挂。
func (s *Service) SetBus(b CmdBus) {
	s.kernel.bus = b
}

// VerifyFactoryProof 验厂钥对时间窗签名；停用/注销仍可通过，才能推治理状态。
func (s *Channel) VerifyFactoryProof(ctx context.Context, factoryID uuid.UUID, unix int64, sig []byte) error {
	now := time.Now().Unix()
	if d := now - unix; d > mqttProofSkew || d < -mqttProofSkew {
		return domain.ErrUnauthorized
	}
	_, err := s.store.FactoryByID(ctx, factoryID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.ErrUnauthorized
		}
		return err
	}
	key, err := s.store.FactoryPublicKey(ctx, factoryID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.ErrUnauthorized
		}
		return err
	}
	if !nodekey.Verify(key.PublicKey, nodekey.MQTTConnectPayload(factoryID, unix), sig) {
		return domain.ErrUnauthorized
	}
	return nil
}

// HandleFactoryUp 处理无问询号的上行：续内容租约，或收下厂端正在跑的版本。
func (s *Channel) HandleFactoryUp(ctx context.Context, factoryID uuid.UUID, payload []byte) {
	var cmd Cmd
	if json.Unmarshal(payload, &cmd) != nil {
		return
	}
	switch cmd.Typ {
	case CmdPresence:
		// 会话仍在线才刷新最近见到；离线行不动。
		_ = s.TouchChannel(ctx, factoryID)
		_ = s.ReportFactoryRelease(ctx, factoryID, cmd.WebVersion, cmd.WebVersionName, cmd.ServiceVersion, cmd.ServiceVersionName)
	case CmdLeaseRenew:
		lease, err := s.IssueContentLease(ctx, factoryID)
		if err != nil {
			return
		}
		s.kernel.publishFactory(factoryID, leaseCmd(lease))
	}
}

// CallFactory 经 MQTT 问该厂升档清单或快照。
func (s *Service) CallFactory(ctx context.Context, factoryID uuid.UUID, cmd Cmd) (Cmd, error) {
	if s.bus == nil {
		return Cmd{}, domain.ErrFactoryOffline
	}
	fac, err := s.store.FactoryByID(ctx, factoryID)
	if err != nil {
		return Cmd{}, err
	}
	if fac.Status == FactoryRetired {
		return Cmd{}, domain.ErrFactoryRetired
	}
	if fac.Status == FactoryDisabled {
		return Cmd{}, domain.ErrFactoryDisabled
	}
	if !fac.ChannelOnline {
		return Cmd{}, domain.ErrFactoryOffline
	}
	if cmd.ReqID == "" {
		cmd.ReqID = uuid.NewString()
	}
	raw, err := json.Marshal(cmd)
	if err != nil {
		return Cmd{}, err
	}
	reply, err := s.bus.Call(ctx, factoryID, cmd.ReqID, raw)
	if err != nil {
		return Cmd{}, domain.ErrFactoryOffline
	}
	var out Cmd
	if err := json.Unmarshal(reply, &out); err != nil {
		return Cmd{}, domain.ErrFactoryOffline
	}
	if out.Error != "" {
		return Cmd{}, mapFactoryUpErr(out.Error)
	}
	return out, nil
}

// DropFactorySession 踢掉该厂 MQTT 会话。
func (s *Service) DropFactorySession(factoryID uuid.UUID) {
	if s.bus != nil {
		s.bus.Drop(factoryID)
	}
}

// 把厂端英文错误收回本侧业务错误。
func mapFactoryUpErr(msg string) error {
	switch msg {
	case domain.ErrNotFound.Error():
		return domain.ErrNotFound
	case domain.ErrForbidden.Error():
		return domain.ErrForbidden
	case domain.ErrAssetNotAvailable.Error():
		return domain.ErrAssetNotAvailable
	case domain.ErrAssetNotCopyable.Error():
		return domain.ErrAssetNotCopyable
	case domain.ErrIntegrity.Error():
		return domain.ErrIntegrity
	case domain.ErrAssetDependency.Error():
		return domain.ErrAssetDependency
	default:
		return domain.ErrFactoryOffline
	}
}

// 收成租约指令。
func leaseCmd(lease ContentLease) Cmd {
	return Cmd{
		Typ:      CmdLease,
		Lease:    lease.Key,
		NotAfter: lease.NotAfter.UTC().Format(time.RFC3339Nano),
	}
}

// 向这一家厂发一条指令。
func (k *kernel) publishFactory(factoryID uuid.UUID, cmd Cmd) {
	if k.bus == nil {
		return
	}
	raw, err := json.Marshal(cmd)
	if err != nil {
		return
	}
	_ = k.bus.Publish(factoryID, raw)
}

// 向所有有效厂发同一条指令。
func (k *kernel) publishActive(ctx context.Context, cmd Cmd) {
	facs, err := k.store.ListFactories(ctx)
	if err != nil {
		return
	}
	for _, fac := range facs {
		if fac.Status != FactoryActive {
			continue
		}
		k.publishFactory(fac.ID, cmd)
	}
}

// 可用或停用修订才通知厂端去拉。
func (k *kernel) notifyAsset(ctx context.Context, a Asset) {
	if a.Status != AssetAvailable && a.Status != AssetDisabled {
		return
	}
	k.publishActive(ctx, Cmd{Typ: CmdClosure, AssetID: a.ID.String(), Revision: a.Revision, Kind: a.Kind})
}

// notifyPlatformFS 把当前平台目录推给有效厂，路径变更不必再升修订。
func (k *kernel) notifyPlatformFS(ctx context.Context, kind string) {
	if kind != KindProcess && kind != KindProject {
		return
	}
	layout, err := k.store.PlatformFSLayout(ctx, kind)
	if err != nil {
		return
	}
	raw, err := json.Marshal(layout)
	if err != nil {
		return
	}
	k.publishActive(ctx, Cmd{Typ: CmdFSApply, Kind: kind, Snapshot: raw})
}

// 删除后通知厂端撤回展示。
func (k *kernel) notifyRetract(ctx context.Context, assetID uuid.UUID) {
	k.publishActive(ctx, Cmd{Typ: CmdRetract, AssetID: assetID.String()})
}

// 模版修订升高只通知这一份。
func (k *kernel) notifyTemplate(ctx context.Context, id uuid.UUID, kind string, revision int64) {
	k.publishActive(ctx, Cmd{Typ: CmdTemplate, TemplateID: id.String(), Kind: kind, Revision: revision})
}

// 删除后把仍在的模版再通知一遍。
func (k *kernel) notifyRemainingTemplates(ctx context.Context) {
	proc, err := k.ensureTemplate(ctx, KindProcess)
	if err == nil {
		k.notifyTemplate(ctx, proc.ID, proc.Kind, proc.Revision)
	}
	projects, err := k.ensureProjectItems(ctx)
	if err != nil {
		return
	}
	for _, t := range projects {
		k.notifyTemplate(ctx, t.ID, t.Kind, t.Revision)
	}
}

// 厂服务包或客户端包发布后通知已认领有效厂去拉，不含字节。
func (k *kernel) notifyFactoryPack(ctx context.Context, row SoftwareRelease) {
	if row.Version < 1 || (row.Kind != SoftwareClientAPK && row.Kind != SoftwareFactoryService) {
		return
	}
	facs, err := k.store.ListFactories(ctx)
	if err != nil {
		return
	}
	cmd := Cmd{Typ: CmdSoftware, Kind: row.Kind, Version: row.Version, VersionName: row.VersionName}
	for _, fac := range facs {
		// 未认领或已停用/注销的厂没有通道，不必通知。
		if fac.Status != FactoryActive || fac.EnrolledAt == nil {
			continue
		}
		k.publishFactory(fac.ID, cmd)
	}
}

// 治理状态变更立刻推给该厂。
func (k *kernel) notifyLifecycle(fac Factory) {
	k.publishFactory(fac.ID, Cmd{
		Typ: CmdFactoryState, Status: fac.Status, Revision: fac.LifecycleRevision, ShortCode: fac.ShortCode, FactoryName: fac.Name,
	})
}

// 注销或除名时踢掉 MQTT 会话。
func (k *kernel) dropFactory(factoryID uuid.UUID) {
	if k.bus != nil {
		k.bus.Drop(factoryID)
	}
}

// 改分时先通知旧厂作废，再通知新厂绑定。
func (k *kernel) notifyClient(row Client, oldFactory *uuid.UUID) {
	if oldFactory != nil && (row.FactoryID == nil || *oldFactory != *row.FactoryID) {
		k.publishFactory(*oldFactory, Cmd{
			Typ: CmdClientVoid, ClientID: row.ID.String(), BindingRevision: row.BindingRevision,
		})
	}
	if row.FactoryID != nil {
		k.publishFactory(*row.FactoryID, Cmd{
			Typ: CmdClientBind, ClientID: row.ID.String(), ClientName: row.Name,
			ClientShortCode: row.ShortCode, DeviceSerial: row.DeviceSerial,
			PublicKey: row.PublicKey, BindingRevision: row.BindingRevision,
		})
	}
}
