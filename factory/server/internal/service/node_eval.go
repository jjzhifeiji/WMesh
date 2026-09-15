package service

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/nodekey"
)

// NodeAction 是本机袋要判定的动作：新开受保护操作，或继续进行中的焊接。
type NodeAction int

const (
	NodeOpen     NodeAction = iota // 新开受保护操作
	NodeContinue                   // 进行中焊接是否继续
)

// NodeDecision 是本机袋判定结果。
type NodeDecision int

const (
	NodeDeny         NodeDecision = iota // 拒绝
	NodeAllow                            // 允许
	NodeContinueWeld                     // 过期或撤销时，进行中焊接继续
)

// NodeEval 是一次本地判定：结果与用了哪口钟。
type NodeEval struct {
	Decision   NodeDecision // 允许 / 拒绝 / 继续焊接
	TimeSource string       // server 或 local
}

// Clocks 由夹具注入：连通用 Server，断网用 Local。
type Clocks struct {
	Server time.Time // 服务器钟
	Local  time.Time // 本机钟
}

// RuntimeCred 是一张已签名的节点运行凭证，可放进本机袋。
type RuntimeCred struct {
	FactoryID    uuid.UUID // 签发工厂
	ClientID     uuid.UUID // 签给哪台 Client
	ClientPublic []byte    // 声明里的本机公钥
	CanRun       bool      // 本修订是否允许运行
	NotBefore    time.Time // 生效时间
	NotAfter     time.Time // 失效时间
	Revision     int64     // 节点授权修订
	Payload      []byte    // 被签名的声明原文
	Signature    []byte    // 厂钥签名
}

// Bag 是夹具里的本机持有物：密钥、信任材料、已接受凭证和连通/焊接标记。
type Bag struct {
	ClientID         uuid.UUID         // 本机稳定身份
	PublicKey        []byte            // 本机公钥
	PrivateKey       []byte            // 本机私钥；不进厂库
	FactoryID        uuid.UUID         // 当前认定所属工厂
	FactoryPublic    []byte            // 本厂签发公钥
	Connected        bool              // 已连网则跟服务器钟并接受新修订
	Welding          bool              // 进行中焊接：过期/撤销时仍继续
	AssetAllowed     bool              // 资产桩；离线受保护操作必须为真
	AcceptedRevision int64             // 已接受的最高节点修订
	Runtime          *RuntimeCred      // 当前生效的节点凭证
	OperatorID       *uuid.UUID        // 当前在本机操作的本厂账号；不是厂签发授权
	Closures         []ClosureSnapshot // 已缓存工程闭包，正文只在袋内
	ActiveID         *uuid.UUID        // 当前激活工程；空则未激活
	ActiveRevision   int64             // 当前激活修订；无激活为 0
	PendingFacts     []PendingFact     // 待汇聚运行事实，未成功前不进厂库
	PendingUploads   []PendingUpload   // 待汇聚点云/图片，正文只在袋内
	FailFlush        bool              // 夹具：本次汇聚失败，队列不动
	SoftwareVersion  int64             // 已确认安装的客户端包版本；0 表示未装
}

// PendingFact 是本机待汇聚的一条运行事实。
type PendingFact struct {
	ID        uuid.UUID  // 产生端稳定身份
	CreatorID uuid.UUID  // 创建账号
	OrgUnitID *uuid.UUID // 发生节点；直属为空
	OrgPath   []PathNode // 发生时路径快照
}

// PendingUpload 是本机待汇聚的一条点云或图片。
type PendingUpload struct {
	ID        uuid.UUID // 产生端稳定身份
	Kind      string    // point_cloud / image
	Content   []byte    // 正文，Flush 前只在袋内
	Digest    []byte    // SHA-256 摘要
	CreatorID uuid.UUID // 上传人
	ClientID  uuid.UUID // 来源 Client
}

type runtimeBody struct {
	FactoryID    string `json:"factoryId"`
	ClientID     string `json:"clientId"`
	ClientPublic string `json:"clientPublicKey"`
	CanRun       bool   `json:"canRun"`
	NotBefore    string `json:"notBefore"`
	NotAfter     string `json:"notAfter"`
	Revision     int64  `json:"revision"`
}

// encodeRuntime 把声明收成稳定 JSON，供签名。
func encodeRuntime(c RuntimeCred) ([]byte, error) {
	return json.Marshal(runtimeBody{
		FactoryID:    c.FactoryID.String(),
		ClientID:     c.ClientID.String(),
		ClientPublic: base64.RawStdEncoding.EncodeToString(c.ClientPublic),
		CanRun:       c.CanRun,
		NotBefore:    c.NotBefore.UTC().Format(time.RFC3339Nano),
		NotAfter:     c.NotAfter.UTC().Format(time.RFC3339Nano),
		Revision:     c.Revision,
	})
}

// decodeRuntime 从声明原文还原凭证字段。
func decodeRuntime(payload []byte) (RuntimeCred, error) {
	var body runtimeBody
	if err := json.Unmarshal(payload, &body); err != nil {
		return RuntimeCred{}, err
	}
	fid, err := uuid.Parse(body.FactoryID)
	if err != nil {
		return RuntimeCred{}, err
	}
	cid, err := uuid.Parse(body.ClientID)
	if err != nil {
		return RuntimeCred{}, err
	}
	pub, err := base64.RawStdEncoding.DecodeString(body.ClientPublic)
	if err != nil {
		return RuntimeCred{}, err
	}
	nb, err := time.Parse(time.RFC3339Nano, body.NotBefore)
	if err != nil {
		return RuntimeCred{}, err
	}
	na, err := time.Parse(time.RFC3339Nano, body.NotAfter)
	if err != nil {
		return RuntimeCred{}, err
	}
	return RuntimeCred{
		FactoryID:    fid,
		ClientID:     cid,
		ClientPublic: pub,
		CanRun:       body.CanRun,
		NotBefore:    nb.UTC(),
		NotAfter:     na.UTC(),
		Revision:     body.Revision,
		Payload:      payload,
	}, nil
}

// credHolds 本机钥、身份和厂钥签名都对才算持有。
func credHolds(bag Bag, cred RuntimeCred) bool {
	if !nodekey.Match(bag.PrivateKey, bag.PublicKey) {
		return false
	}
	if cred.ClientID != bag.ClientID || cred.FactoryID != bag.FactoryID {
		return false
	}
	if !bytes.Equal(cred.ClientPublic, bag.PublicKey) {
		return false
	}
	return nodekey.Verify(bag.FactoryPublic, cred.Payload, cred.Signature)
}

// ApplyRuntime 只接受更高修订且持有方/签发方匹配的凭证；旧修订不得覆盖新意图。
func (b *Bag) ApplyRuntime(cred RuntimeCred) {
	if cred.Revision <= b.AcceptedRevision {
		return
	}
	if !credHolds(*b, cred) {
		return
	}
	cp := cred
	b.Runtime = &cp
	b.AcceptedRevision = cred.Revision
}

// EvaluateRuntime 断网只查本机袋：签发方、本机、本厂、未过期、未见撤销。
func EvaluateRuntime(bag Bag, clocks Clocks, action NodeAction) NodeEval {
	src := audit.Local
	now := clocks.Local
	if bag.Connected {
		src = audit.Server
		now = clocks.Server
	}
	// 默认拒绝；无有效持有则直接返回。
	ev := NodeEval{Decision: NodeDeny, TimeSource: src}
	if bag.Runtime == nil || !credHolds(bag, *bag.Runtime) {
		return ev
	}
	cred, err := decodeRuntime(bag.Runtime.Payload)
	if err != nil {
		return ev
	}
	cred.Signature = bag.Runtime.Signature
	if !credHolds(bag, cred) {
		return ev
	}
	blocked := now.Before(cred.NotBefore) || now.After(cred.NotAfter) || !cred.CanRun
	if blocked {
		// 过期或撤销时，进行中焊接继续。
		if action == NodeContinue && bag.Welding {
			ev.Decision = NodeContinueWeld
		}
		return ev
	}
	ev.Decision = NodeAllow
	return ev
}

// bagNow 连网跟服务器钟，断网用本机钟。
func bagNow(bag Bag, clocks Clocks) (time.Time, string) {
	if bag.Connected {
		return clocks.Server, audit.Server
	}
	return clocks.Local, audit.Local
}
