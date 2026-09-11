package service

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/nodekey"
	"wmesh/factory/internal/platform/secret"
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
	ClientID               uuid.UUID         // 本机稳定身份
	PublicKey              []byte            // 本机公钥
	PrivateKey             []byte            // 本机私钥；不进厂库
	FactoryID              uuid.UUID         // 当前认定所属工厂
	FactoryPublic          []byte            // 本厂签发公钥
	Connected              bool              // 已连网则跟服务器钟并接受新修订
	Welding                bool              // 进行中焊接：过期/撤销时仍继续
	AssetAllowed           bool              // 资产桩；离线受保护操作必须为真
	AcceptedRevision       int64             // 已接受的最高节点修订
	Runtime                *RuntimeCred      // 当前生效的节点凭证
	AcceptedPersonRevision int64             // 已接受的最高人员授权修订
	Person                 *PersonCred       // 当前生效的人员离线授权
	Closures               []ClosureSnapshot // 已缓存工程闭包，正文只在袋内
	ActiveID               *uuid.UUID        // 当前激活工程；空则未激活
	ActiveRevision         int64             // 当前激活修订；无激活为 0
}

// PersonCred 是签给本机某账号的人员离线授权快照。
type PersonCred struct {
	FactoryID     uuid.UUID      // 签发工厂
	ClientID      uuid.UUID      // 绑定 Client
	ClientPublic  []byte         // 声明里的本机公钥
	PersonID      uuid.UUID      // 本厂账号
	LoginName     string         // 签发时登录名
	PasswordHash  string         // 该人口令验证材料；不进审计
	AllowDirect   bool           // 是否允许 Factory 直属
	OrgSnapshot   []OrgOption    // 当时可选节点及路径
	RolesSnapshot []RoleSnapshot // 当时角色与作用域
	Active        bool           // 签发时账号是否有效；停用快照为假
	NotBefore     time.Time      // 生效时间
	NotAfter      time.Time      // 失效时间
	Revision      int64          // 人员授权修订
	Payload       []byte         // 被签名的声明原文
	Signature     []byte         // 厂钥签名
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
		if action == NodeContinue && bag.Welding {
			ev.Decision = NodeContinueWeld
		}
		return ev
	}
	ev.Decision = NodeAllow
	return ev
}

func bagNow(bag Bag, clocks Clocks) (time.Time, string) {
	if bag.Connected {
		return clocks.Server, audit.Server
	}
	return clocks.Local, audit.Local
}

type personBody struct {
	FactoryID     string         `json:"factoryId"`
	ClientID      string         `json:"clientId"`
	ClientPublic  string         `json:"clientPublicKey"`
	PersonID      string         `json:"personId"`
	LoginName     string         `json:"loginName"`
	PasswordHash  string         `json:"passwordHash"`
	AllowDirect   bool           `json:"allowDirect"`
	OrgSnapshot   []OrgOption    `json:"orgSnapshot"`
	RolesSnapshot []RoleSnapshot `json:"rolesSnapshot"`
	Active        *bool          `json:"active,omitempty"`
	NotBefore     string         `json:"notBefore"`
	NotAfter      string         `json:"notAfter"`
	Revision      int64          `json:"revision"`
}

func encodePerson(c PersonCred) ([]byte, error) {
	if c.OrgSnapshot == nil {
		c.OrgSnapshot = []OrgOption{}
	}
	if c.RolesSnapshot == nil {
		c.RolesSnapshot = []RoleSnapshot{}
	}
	active := c.Active
	return json.Marshal(personBody{
		FactoryID:     c.FactoryID.String(),
		ClientID:      c.ClientID.String(),
		ClientPublic:  base64.RawStdEncoding.EncodeToString(c.ClientPublic),
		PersonID:      c.PersonID.String(),
		LoginName:     c.LoginName,
		PasswordHash:  c.PasswordHash,
		AllowDirect:   c.AllowDirect,
		OrgSnapshot:   c.OrgSnapshot,
		RolesSnapshot: c.RolesSnapshot,
		Active:        &active,
		NotBefore:     c.NotBefore.UTC().Format(time.RFC3339Nano),
		NotAfter:      c.NotAfter.UTC().Format(time.RFC3339Nano),
		Revision:      c.Revision,
	})
}

func decodePerson(payload []byte) (PersonCred, error) {
	var body personBody
	if err := json.Unmarshal(payload, &body); err != nil {
		return PersonCred{}, err
	}
	fid, err := uuid.Parse(body.FactoryID)
	if err != nil {
		return PersonCred{}, err
	}
	cid, err := uuid.Parse(body.ClientID)
	if err != nil {
		return PersonCred{}, err
	}
	pid, err := uuid.Parse(body.PersonID)
	if err != nil {
		return PersonCred{}, err
	}
	pub, err := base64.RawStdEncoding.DecodeString(body.ClientPublic)
	if err != nil {
		return PersonCred{}, err
	}
	nb, err := time.Parse(time.RFC3339Nano, body.NotBefore)
	if err != nil {
		return PersonCred{}, err
	}
	na, err := time.Parse(time.RFC3339Nano, body.NotAfter)
	if err != nil {
		return PersonCred{}, err
	}
	orgs := body.OrgSnapshot
	if orgs == nil {
		orgs = []OrgOption{}
	}
	roles := body.RolesSnapshot
	if roles == nil {
		roles = []RoleSnapshot{}
	}
	active := true // 旧声明无此字段时按有效账号
	if body.Active != nil {
		active = *body.Active
	}
	return PersonCred{
		FactoryID:     fid,
		ClientID:      cid,
		ClientPublic:  pub,
		PersonID:      pid,
		LoginName:     body.LoginName,
		PasswordHash:  body.PasswordHash,
		AllowDirect:   body.AllowDirect,
		OrgSnapshot:   orgs,
		RolesSnapshot: roles,
		Active:        active,
		NotBefore:     nb.UTC(),
		NotAfter:      na.UTC(),
		Revision:      body.Revision,
		Payload:       payload,
	}, nil
}

func personHolds(bag Bag, cred PersonCred) bool {
	if !nodekey.Match(bag.PrivateKey, bag.PublicKey) {
		return false
	}
	if cred.ClientID != bag.ClientID || cred.FactoryID != bag.FactoryID || cred.PersonID == uuid.Nil {
		return false
	}
	if !bytes.Equal(cred.ClientPublic, bag.PublicKey) {
		return false
	}
	return nodekey.Verify(bag.FactoryPublic, cred.Payload, cred.Signature)
}

// ApplyPerson 只接受更高修订且绑在本机本厂的人员授权。
func (b *Bag) ApplyPerson(cred PersonCred) {
	if cred.Revision <= b.AcceptedPersonRevision {
		return
	}
	if !personHolds(*b, cred) {
		return
	}
	cp := cred
	b.Person = &cp
	b.AcceptedPersonRevision = cred.Revision
}

func currentPerson(bag Bag) (PersonCred, bool) {
	if bag.Person == nil || !personHolds(bag, *bag.Person) {
		return PersonCred{}, false
	}
	cred, err := decodePerson(bag.Person.Payload)
	if err != nil {
		return PersonCred{}, false
	}
	cred.Signature = bag.Person.Signature
	if !personHolds(bag, cred) {
		return PersonCred{}, false
	}
	return cred, true
}

func personCanOperate(roles []RoleSnapshot) bool {
	for _, r := range roles {
		if r.Role == RoleOperator || r.Role == RoleProcessEngineer {
			return true
		}
	}
	return false
}

// LoginOfflineEval 核验本机袋里的人员授权与口令，不查厂库账号表。
func LoginOfflineEval(bag Bag, clocks Clocks, loginName, password string) NodeEval {
	now, src := bagNow(bag, clocks)
	ev := NodeEval{Decision: NodeDeny, TimeSource: src}
	cred, ok := currentPerson(bag)
	if !ok {
		return ev
	}
	if now.Before(cred.NotBefore) || now.After(cred.NotAfter) {
		return ev
	}
	if !cred.Active {
		return ev // 停用快照不得再登录
	}
	if cred.LoginName != loginName || !secret.VerifyPassword(cred.PasswordHash, password) {
		return ev
	}
	ev.Decision = NodeAllow
	return ev
}

// EvaluateOffline 离线受保护操作：人员授权、节点凭证、资产桩都允许才允许。
func EvaluateOffline(bag Bag, clocks Clocks, loginName, password string) NodeEval {
	ev := LoginOfflineEval(bag, clocks, loginName, password)
	if ev.Decision != NodeAllow {
		return ev
	}
	node := EvaluateRuntime(bag, clocks, NodeOpen)
	if node.Decision != NodeAllow {
		ev.Decision = NodeDeny
		ev.TimeSource = node.TimeSource
		return ev
	}
	cred, ok := currentPerson(bag)
	if !ok || !personCanOperate(cred.RolesSnapshot) || !bag.AssetAllowed {
		ev.Decision = NodeDeny
		return ev
	}
	return ev
}
