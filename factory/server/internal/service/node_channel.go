package service

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/clientmqtt"
	"wmesh/factory/internal/platform/contentcrypt"
	"wmesh/factory/internal/platform/domain"
)

// ClosureRef 是 inbox 里一份工程闭包的元数据，不含正文。
type ClosureRef struct {
	AssetID  uuid.UUID `json:"assetId"`  // 工程身份
	Revision int64     `json:"revision"` // 当前可拉修订
	Digest   []byte    `json:"digest"`   // 整包摘要；尚未组包可空
	Name     string    `json:"name"`     // 显示名
	Level    string    `json:"level"`    // factory / platform / personal
}

// ClientInbox 登录或回连后要对账的策略和获准闭包清单。
type ClientInbox struct {
	Policy             ClientPolicy `json:"policy"`             // 本厂现行策略
	Closures           []ClosureRef `json:"closures"`           // 该人获准工程；current 时为空
	SigningPublicKey   []byte       `json:"signingPublicKey"`   // 验下行 Intent
}

// TransitClosure 过站闭包：成员正文是一次性 DEK 信封，不是厂库信封。
type TransitClosure struct {
	Wrap     []byte          `json:"wrap"`     // 用本机解封钥包着的过站 DEK
	Snapshot ClosureSnapshot `json:"snapshot"` // 成员 Content 为过站信封
}

// AuthClientMQTT 本机 MQTT CONNECT：会话令牌对得上当前登录人且绑定有效。
func (s *Node) AuthClientMQTT(ctx context.Context, clientID uuid.UUID, token string) error {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return err
	}
	cli, err := s.store.ClientByID(ctx, clientID)
	if err != nil {
		return err
	}
	if cli.Status != ClientStatusBound {
		return domain.ErrBindingVoid
	}
	if cli.OperatorID == nil || *cli.OperatorID != acc.ID {
		return domain.ErrForbidden
	}
	return nil
}

// ClientInbox 登录人在这台上要对账的策略和获准闭包；不含正文。
func (s *Closure) ClientInbox(ctx context.Context, token string, clientID uuid.UUID) (ClientInbox, error) {
	acc, cli, err := s.requireClientOperator(ctx, token, clientID)
	if err != nil {
		_ = s.audit(ctx, actorOf(acc), nil, "client_inbox", clientID.String(), audit.Deny)
		return ClientInbox{}, err
	}
	_ = cli
	pol, err := s.store.ClientPolicy(ctx)
	if err != nil {
		return ClientInbox{}, err
	}
	key, err := s.ensureSigningKey(ctx)
	if err != nil {
		return ClientInbox{}, err
	}
	out := ClientInbox{Policy: pol, Closures: []ClosureRef{}, SigningPublicKey: key.PublicKey}
	if pol.CacheScope == CacheScopeCurrent {
		return out, s.audit(ctx, &acc.ID, nil, "client_inbox", clientID.String(), audit.Allow)
	}
	refs, err := s.listAuthorizedRefs(ctx, acc, clientID)
	if err != nil {
		return ClientInbox{}, err
	}
	out.Closures = refs
	return out, s.audit(ctx, &acc.ID, nil, "client_inbox", clientID.String(), audit.Allow)
}

// PadClientInbox 厂网登录后按设备对账获准闭包；不要求已占操作员位。
func (s *Closure) PadClientInbox(ctx context.Context, token string, clientID uuid.UUID) (ClientInbox, error) {
	acc, _, err := s.requireBoundClient(ctx, token, clientID)
	if err != nil {
		_ = s.audit(ctx, actorOf(acc), nil, "pad_inbox", clientID.String(), audit.Deny)
		return ClientInbox{}, err
	}
	pol, err := s.store.ClientPolicy(ctx)
	if err != nil {
		return ClientInbox{}, err
	}
	key, err := s.ensureSigningKey(ctx)
	if err != nil {
		return ClientInbox{}, err
	}
	out := ClientInbox{Policy: pol, Closures: []ClosureRef{}, SigningPublicKey: key.PublicKey}
	if pol.CacheScope == CacheScopeCurrent {
		return out, s.audit(ctx, &acc.ID, nil, "pad_inbox", clientID.String(), audit.Allow)
	}
	refs, err := s.listAuthorizedRefs(ctx, acc, clientID)
	if err != nil {
		return ClientInbox{}, err
	}
	out.Closures = refs
	return out, s.audit(ctx, &acc.ID, nil, "pad_inbox", clientID.String(), audit.Allow)
}

// PullClientClosure 组包后另造过站 DEK 封给这台已登录本机；厂库信封不原样拷。
func (s *Closure) PullClientClosure(ctx context.Context, token string, clientID, projectID uuid.UUID) (TransitClosure, error) {
	acc, cli, err := s.requireClientOperator(ctx, token, clientID)
	target := projectID.String() + " client=" + clientID.String()
	if err != nil {
		_ = s.audit(ctx, actorOf(acc), nil, "pull_closure", target, audit.Deny)
		return TransitClosure{}, err
	}
	if err := s.assertPullable(ctx, acc, clientID, projectID); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "pull_closure", target, audit.Deny)
		return TransitClosure{}, err
	}
	root, err := s.loadRootForPack(ctx, projectID)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "pull_closure", target, audit.Deny)
		return TransitClosure{}, err
	}
	snap, err := s.packFromMember(ctx, root)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "pull_closure", target, audit.Deny)
		return TransitClosure{}, err
	}
	cid := clientID
	snap.TargetClientID = &cid
	out, err := sealTransit(s.store.FactoryID(), cli, snap)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "pull_closure", target, audit.Deny)
		return TransitClosure{}, err
	}
	return out, s.audit(ctx, &acc.ID, nil, "pull_closure", target, audit.Allow)
}

// PadPullClientClosure 厂网登录后按设备拉过站密文；不要求已占操作员位。
func (s *Closure) PadPullClientClosure(ctx context.Context, token string, clientID, projectID uuid.UUID) (TransitClosure, error) {
	acc, cli, err := s.requireBoundClient(ctx, token, clientID)
	target := projectID.String() + " client=" + clientID.String()
	if err != nil {
		_ = s.audit(ctx, actorOf(acc), nil, "pad_pull", target, audit.Deny)
		return TransitClosure{}, err
	}
	if err := s.assertPullable(ctx, acc, clientID, projectID); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "pad_pull", target, audit.Deny)
		return TransitClosure{}, err
	}
	root, err := s.loadRootForPack(ctx, projectID)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "pad_pull", target, audit.Deny)
		return TransitClosure{}, err
	}
	snap, err := s.packFromMember(ctx, root)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "pad_pull", target, audit.Deny)
		return TransitClosure{}, err
	}
	cid := clientID
	snap.TargetClientID = &cid
	out, err := sealTransit(s.store.FactoryID(), cli, snap)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "pad_pull", target, audit.Deny)
		return TransitClosure{}, err
	}
	return out, s.audit(ctx, &acc.ID, nil, "pad_pull", target, audit.Allow)
}

// HandleClientUp 收下本机回执；身份以 MQTT 会话为准，载荷里自报的不管。
func (s *Closure) HandleClientUp(ctx context.Context, clientID uuid.UUID, payload []byte) {
	if clientmqtt.HasBody(payload) {
		_ = s.audit(ctx, nil, nil, "client_ack", clientID.String(), audit.Deny)
		return
	}
	var rec clientmqtt.Receipt
	if json.Unmarshal(payload, &rec) != nil || rec.Typ != clientmqtt.TypAck {
		return
	}
	target := clientID.String()
	if rec.AssetID != "" {
		target += " " + rec.AssetID
	}
	_ = s.audit(ctx, nil, nil, "client_ack", target, audit.Allow)
}

// 当前登录人必须是这台已绑定本机的操作员。
func (s *kernel) requireClientOperator(ctx context.Context, token string, clientID uuid.UUID) (Account, Client, error) {
	acc, cli, err := s.requireBoundClient(ctx, token, clientID)
	if err != nil {
		return acc, cli, err
	}
	if cli.OperatorID == nil || *cli.OperatorID != acc.ID {
		return acc, cli, domain.ErrForbidden
	}
	return acc, cli, nil
}

// 厂网登录人可对账本厂未作废设备；不要求已占操作员位。
func (s *kernel) requireBoundClient(ctx context.Context, token string, clientID uuid.UUID) (Account, Client, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return Account{}, Client{}, err
	}
	cli, err := s.store.ClientByID(ctx, clientID)
	if err != nil {
		return acc, Client{}, err
	}
	if cli.Status != ClientStatusBound {
		return acc, cli, domain.ErrBindingVoid
	}
	return acc, cli, nil
}

// actorOf 没有账号时审计不记人。
func actorOf(acc Account) *uuid.UUID {
	if acc.ID == uuid.Nil {
		return nil
	}
	id := acc.ID
	return &id
}

// 厂级/平台级须有有效授权；个人级仅创建人。
func (s *Closure) assertPullable(ctx context.Context, acc Account, clientID, projectID uuid.UUID) error {
	root, err := s.loadRootForPack(ctx, projectID)
	if err != nil {
		return err
	}
	if root.Kind != KindProject {
		return domain.ErrForbidden
	}
	if root.Level == AssetLevelPersonal {
		if root.CreatorID == nil || *root.CreatorID != acc.ID {
			return domain.ErrForbidden
		}
		return nil
	}
	grant, err := s.store.ClientGrant(ctx, projectID, clientID)
	if err != nil {
		return err
	}
	if !grant.Active {
		return domain.ErrForbidden
	}
	return nil
}

// listAuthorizedRefs 收集该机授权工程和登录人个人级可用工程。
func (s *Closure) listAuthorizedRefs(ctx context.Context, acc Account, clientID uuid.UUID) ([]ClosureRef, error) {
	grants, err := s.store.ListActiveGrantsForClient(ctx, clientID)
	if err != nil {
		return nil, err
	}
	out := make([]ClosureRef, 0, len(grants))
	seen := map[uuid.UUID]struct{}{}
	for _, g := range grants {
		ref, err := s.refForProject(ctx, g.ProjectID)
		if err != nil {
			continue
		}
		out = append(out, ref)
		seen[g.ProjectID] = struct{}{}
	}
	rows, err := s.store.ListGovernedAssets(ctx)
	if err != nil {
		return nil, err
	}
	for _, a := range rows {
		if a.Kind != KindProject || a.Level != AssetLevelPersonal || a.CreatorID != acc.ID || a.Status != AssetAvailable {
			continue
		}
		if _, ok := seen[a.ID]; ok {
			continue
		}
		ref, err := s.refForProject(ctx, a.ID)
		if err != nil {
			continue
		}
		out = append(out, ref)
	}
	return out, nil
}

// refForProject 组包得到修订和摘要；组包失败仍给出名称。
func (s *Closure) refForProject(ctx context.Context, projectID uuid.UUID) (ClosureRef, error) {
	root, err := s.loadRootForPack(ctx, projectID)
	if err != nil {
		return ClosureRef{}, err
	}
	snap, err := s.packFromMember(ctx, root)
	if err != nil {
		return ClosureRef{AssetID: root.ID, Revision: root.Revision, Name: root.Name, Level: root.Level}, nil
	}
	return ClosureRef{AssetID: snap.AssetID, Revision: snap.Revision, Digest: snap.Digest, Name: root.Name, Level: snap.Level}, nil
}

// sealTransit 另造过站 DEK，到站用本机解封钥解开后再重封进袋。
func sealTransit(factoryID uuid.UUID, cli Client, snap ClosureSnapshot) (TransitClosure, error) {
	if len(cli.UnwrapKey) != contentcrypt.KeySize {
		return TransitClosure{}, domain.ErrForbidden
	}
	dek, err := contentcrypt.RandomKey()
	if err != nil {
		return TransitClosure{}, err
	}
	defer contentcrypt.Zero(dek)
	members := make([]ClosureMember, len(snap.Members))
	for i, m := range snap.Members {
		plain := append([]byte(nil), m.Content...)
		env, err := contentcrypt.Seal(dek, plain, contentcrypt.ClientTransitAAD(factoryID, cli.ID, m.ID, m.Revision))
		contentcrypt.Zero(plain)
		if err != nil {
			return TransitClosure{}, err
		}
		m.Content = env
		members[i] = m
	}
	snap.Members = members
	wrap, err := contentcrypt.Seal(cli.UnwrapKey, dek, contentcrypt.ClientTransitDEKAAD(factoryID, cli.ID))
	if err != nil {
		return TransitClosure{}, err
	}
	return TransitClosure{Wrap: wrap, Snapshot: snap}, nil
}

// fanoutPolicy 把更高修订策略推给本厂已绑定 Client，正文不进 MQTT。
func (s *Closure) fanoutPolicy(ctx context.Context, pol ClientPolicy) {
	if s.clientDown == nil {
		return
	}
	key, err := s.ensureSigningKey(ctx)
	if err != nil {
		return
	}
	rows, err := s.store.ListClients(ctx)
	if err != nil {
		return
	}
	in := clientmqtt.Intent{
		Typ:               clientmqtt.TypPolicy,
		Revision:          pol.Revision,
		MaxCachedProjects: pol.MaxCachedProjects,
		CacheScope:        pol.CacheScope,
		PersistUnwrapKey:  pol.PersistUnwrapKey,
		KeyTTLSeconds:     pol.KeyTTLSeconds,
	}
	fid := s.store.FactoryID()
	for _, cl := range rows {
		if cl.Status != ClientStatusBound {
			continue
		}
		raw, err := clientmqtt.Sign(key.PrivateKey, fid, cl.ID, in)
		if err != nil || clientmqtt.HasBody(raw) {
			continue
		}
		s.clientDown.PublishDown(fid, cl.ID, raw)
	}
}

// publishClosureReady 只推身份、修订和摘要，不推成员正文。
func (s *Closure) publishClosureReady(ctx context.Context, clientID uuid.UUID, snap ClosureSnapshot) {
	if s.clientDown == nil {
		return
	}
	key, err := s.ensureSigningKey(ctx)
	if err != nil {
		return
	}
	in := clientmqtt.Intent{
		Typ:      clientmqtt.TypClosure,
		Revision: snap.Revision,
		AssetID:  snap.AssetID.String(),
		Digest:   snap.Digest,
	}
	fid := s.store.FactoryID()
	raw, err := clientmqtt.Sign(key.PrivateKey, fid, clientID, in)
	if err != nil || clientmqtt.HasBody(raw) {
		return
	}
	s.clientDown.PublishDown(fid, clientID, raw)
}

// OpenTransit 用本机解封钥解开过站 DEK，再解成员；给夹具和本机袋验收。
func OpenTransit(factoryID uuid.UUID, cli Client, t TransitClosure) (ClosureSnapshot, error) {
	dek, err := contentcrypt.Open(cli.UnwrapKey, t.Wrap, contentcrypt.ClientTransitDEKAAD(factoryID, cli.ID))
	if err != nil {
		return ClosureSnapshot{}, err
	}
	defer contentcrypt.Zero(dek)
	snap := t.Snapshot
	members := make([]ClosureMember, len(snap.Members))
	for i, m := range snap.Members {
		plain, err := contentcrypt.Open(dek, m.Content, contentcrypt.ClientTransitAAD(factoryID, cli.ID, m.ID, m.Revision))
		if err != nil {
			return ClosureSnapshot{}, err
		}
		m.Content = plain
		members[i] = m
	}
	snap.Members = members
	return snap, nil
}
