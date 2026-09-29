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

// 运行凭证的声明正文，签名盖的是这些字段
type runtimeBody struct {
	FactoryID    string `json:"factoryId"`       // 签给哪一家厂，避免留下不一致的结果
	ClientID     string `json:"clientId"`        // 本机身份，用来对上这台设备
	ClientPublic string `json:"clientPublicKey"` // 这台本机的公钥，用来核对持有
	CanRun       bool   `json:"canRun"`          // 这一修订是否允许运行
	NotBefore    string `json:"notBefore"`       // 凭证生效时间，未到则拒绝
	NotAfter     string `json:"notAfter"`        // 凭证失效时间，过了则拒绝
	Revision     int64  `json:"revision"`        // 凭证修订，只接受更高的一版
}

// encodeRuntime 把声明收成稳定 JSON，供签名。
func encodeRuntime(c RuntimeCred) ([]byte, error) {
	// 收成稳定正文，再交回调用方
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
	// 准备接声明正文，还原失败则凭证无效
	var body runtimeBody
	// 解析失败则按无效拒绝或忽略
	if err := json.Unmarshal(payload, &body); err != nil {
		return RuntimeCred{}, err
	}
	// 解析本机身份，避免留下不一致的结果
	fid, err := uuid.Parse(body.FactoryID)
	// 解析失败就不把现场钉到设备
	if err != nil {
		return RuntimeCred{}, err
	}
	// 解析本机身份，避免留下不一致的结果
	cid, err := uuid.Parse(body.ClientID)
	// 解析失败就不把现场钉到设备
	if err != nil {
		return RuntimeCred{}, err
	}
	// 还原编码后的钥或声明
	pub, err := base64.RawStdEncoding.DecodeString(body.ClientPublic)
	// 解码失败则当钥无效拒绝
	if err != nil {
		return RuntimeCred{}, err
	}
	// 解析本机身份，避免留下不一致的结果
	nb, err := time.Parse(time.RFC3339Nano, body.NotBefore)
	// 解析失败就不把现场钉到设备
	if err != nil {
		return RuntimeCred{}, err
	}
	// 解析本机身份，避免留下不一致的结果
	na, err := time.Parse(time.RFC3339Nano, body.NotAfter)
	// 解析失败就不把现场钉到设备
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
	// 条件不成立则拒绝或跳过，不往下改数据
	if !nodekey.Match(bag.PrivateKey, bag.PublicKey) {
		return false
	}
	// 凭证上的厂或本机对不上这只袋，不当持有
	if cred.ClientID != bag.ClientID || cred.FactoryID != bag.FactoryID {
		return false
	}
	// 条件不成立则拒绝或跳过，不往下改数据
	if !bytes.Equal(cred.ClientPublic, bag.PublicKey) {
		return false
	}
	// 用厂钥验声明签名，再交回调用方
	return nodekey.Verify(bag.FactoryPublic, cred.Payload, cred.Signature)
}

// ApplyRuntime 只接受更高修订且持有方/签发方匹配的凭证；旧修订不得覆盖新意图。
func (b *Bag) ApplyRuntime(cred RuntimeCred) {
	// 修订没有更高，拒绝覆盖已接受的凭证
	if cred.Revision <= b.AcceptedRevision {
		return
	}
	// 钥、身份或签名对不上则不当持有
	if !credHolds(*b, cred) {
		return
	}
	// 先复制再改，避免改到袋内或入参原文
	cp := cred
	// 袋内换成这份更高修订的凭证
	b.Runtime = &cp
	// 记下已接受的修订，更旧的不能覆盖
	b.AcceptedRevision = cred.Revision
}

// EvaluateRuntime 断网只查本机袋：签发方、本机、本厂、未过期、未见撤销。
func EvaluateRuntime(bag Bag, clocks Clocks, action NodeAction) NodeEval {
	// 先按本机钟，连着厂服再改用服务器钟
	src := audit.Local
	// 断网用本机钟判断凭证是否过期
	now := clocks.Local
	// 连着厂服就改用服务器钟
	if bag.Connected {
		// 连着厂服就用服务器钟，避免本机钟被拨
		src = audit.Server
		// 连网改用服务器钟，过期与否跟厂服一致
		now = clocks.Server
	}
	// 默认拒绝；无有效持有则直接返回。
	ev := NodeEval{Decision: NodeDeny, TimeSource: src}
	// 钥、身份或签名对不上则不当持有
	if bag.Runtime == nil || !credHolds(bag, *bag.Runtime) {
		return ev
	}
	// 从声明原文还原凭证字段
	cred, err := decodeRuntime(bag.Runtime.Payload)
	// 声明还原失败，不当作有效凭证
	if err != nil {
		return ev
	}
	// 带上签名，对不上就当没持有这张凭证
	cred.Signature = bag.Runtime.Signature
	// 钥、身份或签名对不上则不当持有
	if !credHolds(bag, cred) {
		return ev
	}
	// 看当前是否已经晚于失效时刻
	blocked := now.Before(cred.NotBefore) || now.After(cred.NotAfter) || !cred.CanRun
	// 未生效、已过期或已撤销则挡住
	if blocked {
		// 过期或撤销时，进行中焊接继续。
		if action == NodeContinue && bag.Welding {
			// 过期或撤销时，进行中的焊接仍允许做完
			ev.Decision = NodeContinueWeld
		}
		return ev
	}
	// 所需项都过了，改为允许
	ev.Decision = NodeAllow
	return ev
}

// bagNow 连网跟服务器钟，断网用本机钟。
func bagNow(bag Bag, clocks Clocks) (time.Time, string) {
	// 连着厂服就改用服务器钟
	if bag.Connected {
		return clocks.Server, audit.Server
	}
	return clocks.Local, audit.Local
}
