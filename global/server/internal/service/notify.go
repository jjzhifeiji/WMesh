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
	// 把小指令推给该厂，正文不走这里。
	Publish(factoryID uuid.UUID, payload []byte) error
	// 等该厂回执，超时则这次问询失败。
	Call(ctx context.Context, factoryID uuid.UUID, reqID string, payload []byte) ([]byte, error)
	// 断开时丢掉未完成问询，避免串厂。
	Drop(factoryID uuid.UUID)
}

// SetBus 挂上厂端指令总线；测试可不挂。
func (s *Service) SetBus(b CmdBus) {
	// 挂上厂端指令总线，测试可以不挂。
	s.kernel.bus = b
}

// VerifyFactoryProof 验厂钥对时间窗签名；停用/注销仍可通过，才能推治理状态。
func (s *Channel) VerifyFactoryProof(ctx context.Context, factoryID uuid.UUID, unix int64, sig []byte) error {
	// 取当前时间，失败就不能继续。
	now := time.Now().Unix()
	// 时间超出允许窗口则拒绝，防止过期签名重放。
	if d := now - unix; d > mqttProofSkew || d < -mqttProofSkew {
		return domain.ErrUnauthorized
	}
	// 按工厂名录处理。
	_, err := s.store.FactoryByID(ctx, factoryID)
	// 没有这家厂或状态不对就拒绝。
	if err != nil {
		// 没有这条就按不存在处理，不当成别的故障。
		if errors.Is(err, domain.ErrNotFound) {
			return domain.ErrUnauthorized
		}
		return err
	}
	// 按工厂名录处理。
	key, err := s.store.FactoryPublicKey(ctx, factoryID)
	// 没有这家厂或状态不对就拒绝。
	if err != nil {
		// 没有这条就按不存在处理，不当成别的故障。
		if errors.Is(err, domain.ErrNotFound) {
			return domain.ErrUnauthorized
		}
		return err
	}
	// 厂钥验签失败则拒绝连接，不当已认领。
	if !nodekey.Verify(key.PublicKey, nodekey.MQTTConnectPayload(factoryID, unix), sig) {
		return domain.ErrUnauthorized
	}
	return nil
}

// HandleFactoryUp 处理无问询号的上行：续内容租约，或收下厂端正在跑的版本。
func (s *Channel) HandleFactoryUp(ctx context.Context, factoryID uuid.UUID, payload []byte) {
	// 先留空，解开载荷成功再按类型处理。
	var cmd Cmd
	// 载荷解开失败则拒绝，不按坏包继续。
	if json.Unmarshal(payload, &cmd) != nil {
		return
	}
	// 按指令类型分发，不认识的直接丢掉。
	switch cmd.Typ {
	// 这是厂端自报版本，不带安装包正文。
	case CmdPresence:
		// 会话仍在线才刷新最近见到；离线行不动。
		_ = s.TouchChannel(ctx, factoryID)
		// 做完这一步再继续。
		_ = s.ReportFactoryRelease(ctx, factoryID, cmd.WebVersion, cmd.WebVersionName, cmd.ServiceVersion, cmd.ServiceVersionName)
	// 这是续租请求，窗口重新算一天。
	case CmdLeaseRenew:
		// 签发或续上这一份。
		lease, err := s.IssueContentLease(ctx, factoryID)
		// 失败则对方解不开或用不了。
		if err != nil {
			return
		}
		// 把变更推给这家厂。
		s.kernel.publishFactory(factoryID, leaseCmd(lease))
	}
}

// CallFactory 经 MQTT 问该厂升档清单或快照。
func (s *Service) CallFactory(ctx context.Context, factoryID uuid.UUID, cmd Cmd) (Cmd, error) {
	// 没挂指令总线就跳过推送，测试可以不挂。
	if s.bus == nil {
		return Cmd{}, domain.ErrFactoryOffline
	}
	// 按工厂名录处理。
	fac, err := s.store.FactoryByID(ctx, factoryID)
	// 没有这家厂或状态不对就拒绝。
	if err != nil {
		return Cmd{}, err
	}
	// 已注销的厂不能再启用或认领。
	if fac.Status == FactoryRetired {
		return Cmd{}, domain.ErrFactoryRetired
	}
	// 停用的厂拒绝新操作，只能再被启用。
	if fac.Status == FactoryDisabled {
		return Cmd{}, domain.ErrFactoryDisabled
	}
	// 厂端不在线就不能做这件要通道的事。
	if !fac.ChannelOnline {
		return Cmd{}, domain.ErrFactoryOffline
	}
	// 空和有值走不同路，避免把空白写进名录。
	if cmd.ReqID == "" {
		// 编译用来认缝隙目录的匹配式。
		cmd.ReqID = uuid.NewString()
	}
	// 编成字节再送出。
	raw, err := json.Marshal(cmd)
	// 编不出就拒绝，不发送半截。
	if err != nil {
		return Cmd{}, err
	}
	// 等这家厂回执，失败就不能继续。
	reply, err := s.bus.Call(ctx, factoryID, cmd.ReqID, raw)
	// 超时则这次问询失败。
	if err != nil {
		return Cmd{}, domain.ErrFactoryOffline
	}
	// 先留空指令，认准类型再填载荷。
	var out Cmd
	// 载荷解开失败则拒绝，不按坏包继续。
	if err := json.Unmarshal(reply, &out); err != nil {
		return Cmd{}, domain.ErrFactoryOffline
	}
	// 回执带了错误就按失败，不当成拉到了。
	if out.Error != "" {
		// 把厂端错误收成拒绝原因。
		return Cmd{}, mapFactoryUpErr(out.Error)
	}
	return out, nil
}

// DropFactorySession 踢掉该厂 MQTT 会话。
func (s *Service) DropFactorySession(factoryID uuid.UUID) {
	// 没挂指令总线就跳过推送，测试可以不挂。
	if s.bus != nil {
		// 丢掉这家厂未完成的问询。
		s.bus.Drop(factoryID)
	}
}

// 把厂端英文错误收回本侧业务错误。
func mapFactoryUpErr(msg string) error {
	// 按回执内容决定成功还是拒绝。
	switch msg {
	// 没有这条记录，按不存在拒绝。
	case domain.ErrNotFound.Error():
		return domain.ErrNotFound
	// 没有这份许可，按拒绝回给调用方。
	case domain.ErrForbidden.Error():
		return domain.ErrForbidden
	// 不是可用资产，不能当已发布使用。
	case domain.ErrAssetNotAvailable.Error():
		return domain.ErrAssetNotAvailable
	// 不可复制不得升档，在这里挡住。
	case domain.ErrAssetNotCopyable.Error():
		return domain.ErrAssetNotCopyable
	// 摘要对不上，按损坏拒绝。
	case domain.ErrIntegrity.Error():
		return domain.ErrIntegrity
	// 依赖缺失或错配，整次按这个拒绝。
	case domain.ErrAssetDependency.Error():
		return domain.ErrAssetDependency
	// 其余情况走这里，避免漏掉一种状态。
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
	// 没挂指令总线就跳过推送，测试可以不挂。
	if k.bus == nil {
		return
	}
	// 编成字节再送出。
	raw, err := json.Marshal(cmd)
	// 编不出就拒绝，不发送半截。
	if err != nil {
		return
	}
	// 发布给该收到的一方。
	_ = k.bus.Publish(factoryID, raw)
}

// 向所有有效厂发同一条指令。
func (k *kernel) publishActive(ctx context.Context, cmd Cmd) {
	// 列出这一批供后面筛选。
	facs, err := k.store.ListFactories(ctx)
	// 列出失败就拒绝，避免交出不完整结果。
	if err != nil {
		return
	}
	// 逐厂处理，某一厂失败不改其他厂。
	for _, fac := range facs {
		// 不是有效厂就只给状态，不再发业务指令。
		if fac.Status != FactoryActive {
			continue
		}
		// 把变更推给这家厂。
		k.publishFactory(fac.ID, cmd)
	}
}

// 可用或停用修订才通知厂端去拉。
func (k *kernel) notifyAsset(ctx context.Context, a Asset) {
	// 已停用则拒绝变更，正文和依赖都锁住。
	if a.Status != AssetAvailable && a.Status != AssetDisabled {
		return
	}
	// 只推给仍有效的厂。
	k.publishActive(ctx, Cmd{Typ: CmdClosure, AssetID: a.ID.String(), Revision: a.Revision, Kind: a.Kind})
}

// notifyPlatformFS 把当前平台目录推给有效厂，路径变更不必再升修订。
func (k *kernel) notifyPlatformFS(ctx context.Context, kind string) {
	// 按是不是工程决定要不要核对焊道和依赖。
	if kind != KindProcess && kind != KindProject {
		return
	}
	// 读平台级的这一份。
	layout, err := k.store.PlatformFSLayout(ctx, kind)
	// 厂级数据不在这里查。
	if err != nil {
		return
	}
	// 编成字节再送出。
	raw, err := json.Marshal(layout)
	// 编不出就拒绝，不发送半截。
	if err != nil {
		return
	}
	// 只推给仍有效的厂。
	k.publishActive(ctx, Cmd{Typ: CmdFSApply, Kind: kind, Snapshot: raw})
}

// 删除后通知厂端撤回展示。
func (k *kernel) notifyRetract(ctx context.Context, assetID uuid.UUID) {
	// 只推给仍有效的厂。
	k.publishActive(ctx, Cmd{Typ: CmdRetract, AssetID: assetID.String()})
}

// 模版修订升高只通知这一份。
func (k *kernel) notifyTemplate(ctx context.Context, id uuid.UUID, kind string, revision int64) {
	// 只推给仍有效的厂。
	k.publishActive(ctx, Cmd{Typ: CmdTemplate, TemplateID: id.String(), Kind: kind, Revision: revision})
}

// 删除后把仍在的模版再通知一遍。
func (k *kernel) notifyRemainingTemplates(ctx context.Context) {
	// 没有这份模版就补上种子。
	proc, err := k.ensureTemplate(ctx, KindProcess)
	// 没有错误才继续，有错留在后面的分支。
	if err == nil {
		// 通知厂端去拉模版。
		k.notifyTemplate(ctx, proc.ID, proc.Kind, proc.Revision)
	}
	// 工程项不齐就按种子补。
	projects, err := k.ensureProjectItems(ctx)
	// 补失败就拒绝，模版不能缺层。
	if err != nil {
		return
	}
	// 逐个工程套上当前模版，套不上就拒绝。
	for _, t := range projects {
		// 通知厂端去拉模版。
		k.notifyTemplate(ctx, t.ID, t.Kind, t.Revision)
	}
}

// 厂服务包或客户端包发布后通知已认领有效厂去拉，不含字节。
func (k *kernel) notifyFactoryPack(ctx context.Context, row SoftwareRelease) {
	// 字数不在允许范围就拒绝，避免空名或超长。
	if row.Version < 1 || (row.Kind != SoftwareClientAPK && row.Kind != SoftwareFactoryService) {
		return
	}
	// 列出这一批供后面筛选。
	facs, err := k.store.ListFactories(ctx)
	// 列出失败就拒绝，避免交出不完整结果。
	if err != nil {
		return
	}
	// 组一条软件包指令，正文仍走拉取。
	cmd := Cmd{Typ: CmdSoftware, Kind: row.Kind, Version: row.Version, VersionName: row.VersionName}
	// 逐厂处理，某一厂失败不改其他厂。
	for _, fac := range facs {
		// 未认领或已停用/注销的厂没有通道，不必通知。
		if fac.Status != FactoryActive || fac.EnrolledAt == nil {
			continue
		}
		// 把变更推给这家厂。
		k.publishFactory(fac.ID, cmd)
	}
}

// 治理状态变更立刻推给该厂。
func (k *kernel) notifyLifecycle(fac Factory) {
	// 把变更推给这家厂。
	k.publishFactory(fac.ID, Cmd{
		Typ: CmdFactoryState, Status: fac.Status, Revision: fac.LifecycleRevision, ShortCode: fac.ShortCode, FactoryName: fac.Name,
	})
}

// 注销或除名时踢掉 MQTT 会话。
func (k *kernel) dropFactory(factoryID uuid.UUID) {
	// 没挂指令总线就跳过推送，测试可以不挂。
	if k.bus != nil {
		// 丢掉这家厂未完成的问询。
		k.bus.Drop(factoryID)
	}
}

// 改分时先通知旧厂作废，再通知新厂绑定。
func (k *kernel) notifyClient(row Client, oldFactory *uuid.UUID) {
	// 还没有准备好就停，避免空着往下用。
	if oldFactory != nil && (row.FactoryID == nil || *oldFactory != *row.FactoryID) {
		// 把变更推给这家厂。
		k.publishFactory(*oldFactory, Cmd{
			Typ: CmdClientVoid, ClientID: row.ID.String(), BindingRevision: row.BindingRevision,
		})
	}
	// 还没有准备好就停，避免空着往下用。
	if row.FactoryID != nil {
		// 把变更推给这家厂。
		k.publishFactory(*row.FactoryID, Cmd{
			Typ: CmdClientBind, ClientID: row.ID.String(), ClientName: row.Name,
			ClientShortCode: row.ShortCode, DeviceSerial: row.DeviceSerial,
			PublicKey: row.PublicKey, BindingRevision: row.BindingRevision,
		})
	}
}
