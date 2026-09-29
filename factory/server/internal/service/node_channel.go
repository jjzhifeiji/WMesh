package service

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/clientmqtt"
	"wmesh/factory/internal/platform/contentcrypt"
	"wmesh/factory/internal/platform/domain"
)

// ClosureRef 是 inbox 里一份工程闭包的元数据，不含正文。
type ClosureRef struct {
	AssetID  uuid.UUID `json:"assetId"`  // 工艺或工程身份
	Revision int64     `json:"revision"` // 当前可拉修订
	Digest   []byte    `json:"digest"`   // 整包摘要；尚未组包可空
	Name     string    `json:"name"`     // 显示名
	Level    string    `json:"level"`    // factory / platform / personal
}

// ClientInbox 登录或回连后要对账的策略和获准闭包清单。
type ClientInbox struct {
	Policy           ClientPolicy `json:"policy"`           // 本厂现行策略
	Closures         []ClosureRef `json:"closures"`         // 可见资产；操作员 current 时为空
	SigningPublicKey []byte       `json:"signingPublicKey"` // 验下行 Intent
}

// TransitClosure 过站包：工艺成员是过站信封；工程根是明文，Wrap 为空。
type TransitClosure struct {
	Wrap     []byte          `json:"wrap,omitempty"` // 过站 DEK；工程明文不封
	Snapshot ClosureSnapshot `json:"snapshot"`       // 工艺为过站信封，工程根为明文
}

// AuthClientMQTT 厂网或本机 CONNECT：令牌有效；已登记 Client 须绑定未作废；未登记身份允许厂网登录人收听回连。
func (s *Node) AuthClientMQTT(ctx context.Context, clientID uuid.UUID, token string) (Account, error) {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return Account{}, err
	}
	// CONNECT 成功记下见到；在线由 MQTT 会话钩子另计。
	_ = s.store.NotePersonApp(ctx, acc.ID, 0, "")
	// 按身份读本厂这台设备
	cli, err := s.store.ClientByID(ctx, clientID)
	// 这台设备不在名录，拒绝
	if err != nil {
		// 设备尚未登记，允许厂网登录人收听回连
		if errors.Is(err, domain.ErrNotFound) {
			return acc, nil
		}
		return acc, err
	}
	// 不是有效绑定则拒绝签发、登录或对账
	if cli.Status != ClientStatusBound {
		return acc, domain.ErrBindingVoid
	}
	return acc, nil
}

// ClientInbox 登录人在这台上要对账的策略和获准闭包；不含正文。
func (s *Closure) ClientInbox(ctx context.Context, token string, clientID uuid.UUID) (ClientInbox, error) {
	// 当前登录人必须是这台的操作员
	acc, cli, err := s.requireClientOperator(ctx, token, clientID)
	// 没占这台操作员位则拒绝
	if err != nil {
		// 记下本机对账被拒绝，写失败不改变结果
		_ = s.audit(ctx, actorOf(acc), nil, "client_inbox", clientID.String(), audit.Deny)
		return ClientInbox{}, err
	}
	// 操作员身份已经核对过，这里只用策略和清单
	_ = cli
	// 读对本厂全部本机生效的策略
	pol, err := s.store.ClientPolicy(ctx)
	// 策略读失败，不能改或下发
	if err != nil {
		return ClientInbox{}, err
	}
	// 没有签发钥就生成一对，私钥不外送
	key, err := s.ensureSigningKey(ctx)
	// 签发钥备不齐，不能发运行凭证
	if err != nil {
		return ClientInbox{}, err
	}
	// 先装上策略和签发公钥，闭包清单随后再填
	out := ClientInbox{Policy: pol, Closures: []ClosureRef{}, SigningPublicKey: key.PublicKey}
	// 策略只要当前工程时，清单里不带其它份
	if pol.CacheScope == CacheScopeCurrent {
		// 本机对账成功后记审计，再把结果交回
		return out, s.audit(ctx, &acc.ID, nil, "client_inbox", clientID.String(), audit.Allow)
	}
	// 收集该机授权工程和本人个人工程
	refs, err := s.listAuthorizedRefs(ctx, acc, clientID)
	// 清单收不齐则不返回残缺对账
	if err != nil {
		return ClientInbox{}, err
	}
	// 填上获准闭包清单，里面没有正文
	out.Closures = refs
	// 本机对账成功后记审计，再把结果交回
	return out, s.audit(ctx, &acc.ID, nil, "client_inbox", clientID.String(), audit.Allow)
}

// PadClientInbox 厂网登录后拉与厂端列表同一份工艺/工程；不要求已绑设备。
func (s *Closure) PadClientInbox(ctx context.Context, token string) (ClientInbox, error) {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		// 记下厂网对账被拒绝，写失败不改变结果
		_ = s.audit(ctx, actorOf(acc), nil, "pad_inbox", "", audit.Deny)
		return ClientInbox{}, err
	}
	// 读对本厂全部本机生效的策略
	pol, err := s.store.ClientPolicy(ctx)
	// 策略读失败，不能改或下发
	if err != nil {
		return ClientInbox{}, err
	}
	// 没有签发钥就生成一对，私钥不外送
	key, err := s.ensureSigningKey(ctx)
	// 签发钥备不齐，不能发运行凭证
	if err != nil {
		return ClientInbox{}, err
	}
	// 与厂端工艺工程列表同一范围
	refs, err := s.listPadAssetRefs(ctx, acc)
	// 列表收不齐则厂网对账中止
	if err != nil {
		return ClientInbox{}, err
	}
	// 先装上策略和签发公钥，闭包清单随后再填
	out := ClientInbox{Policy: pol, Closures: refs, SigningPublicKey: key.PublicKey}
	// 厂网对账成功后记审计，再把结果交回
	return out, s.audit(ctx, &acc.ID, nil, "pad_inbox", acc.ID.String(), audit.Allow)
}

// PullClientClosure 组包后另造过站 DEK 封给这台已登录本机；厂库信封不原样拷。
func (s *Closure) PullClientClosure(ctx context.Context, token string, clientID, projectID uuid.UUID) (TransitClosure, error) {
	// 当前登录人必须是这台的操作员
	acc, _, err := s.requireClientOperator(ctx, token, clientID)
	// 收成文本身份，供审计或对账对准
	target := projectID.String() + " client=" + clientID.String()
	// 没占这台操作员位则拒绝
	if err != nil {
		// 记下本机拉包被拒绝，写失败不改变结果
		_ = s.audit(ctx, actorOf(acc), nil, "pull_closure", target, audit.Deny)
		return TransitClosure{}, err
	}
	// 拉不到这份就拒绝组包
	if err := s.assertPullable(ctx, acc, clientID, projectID); err != nil {
		// 记下本机拉包被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "pull_closure", target, audit.Deny)
		return TransitClosure{}, err
	}
	// 装上要组包的工程根
	root, err := s.loadRootForPack(ctx, projectID)
	// 根读不到则不能组包
	if err != nil {
		// 记下本机拉包被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "pull_closure", target, audit.Deny)
		return TransitClosure{}, err
	}
	// 按根组出钉死修订的闭包
	snap, err := s.packFromMember(ctx, root)
	// 组包失败则拒绝下发
	if err != nil {
		// 记下本机拉包被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "pull_closure", target, audit.Deny)
		return TransitClosure{}, err
	}
	// 另拷一份设备身份再取址，避免指向会被改的变量
	cid := clientID
	// 钉到这台本机，别的设备拉不到
	snap.TargetClientID = &cid
	// 过站 DEK 用登录人钥封，不用焊机钥。
	who, err := s.store.EnsurePersonUnwrapKey(ctx, acc.ID)
	// 解封钥读不到或长度不对，拒绝继续
	if err != nil || len(who.UnwrapKey) != contentcrypt.KeySize {
		// 解封钥读不到或长度不对，记下本机拉包被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "pull_closure", target, audit.Deny)
		// 读钥失败则交回错误，长度不对另按无效钥拒绝
		if err != nil {
			return TransitClosure{}, err
		}
		return TransitClosure{}, domain.ErrForbidden
	}
	// 取出本厂稳定身份，封包和审计都要用
	out, err := sealTransit(s.store.FactoryID(), clientID, who.UnwrapKey, snap)
	// 读钥失败则交回错误，长度不对另按无效钥拒绝
	if err != nil {
		// 记下本机拉包被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "pull_closure", target, audit.Deny)
		return TransitClosure{}, err
	}
	// 本机拉包成功后记审计，再把结果交回
	return out, s.audit(ctx, &acc.ID, nil, "pull_closure", target, audit.Allow)
}

// PadPullClientClosure 厂网登录后拉包：工程明文，工艺用登录人钥封过站 DEK。
func (s *Closure) PadPullClientClosure(ctx context.Context, token string, assetID uuid.UUID) (TransitClosure, error) {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 收成文本身份，供审计或对账对准
	target := assetID.String()
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		// 记下厂网拉包被拒绝，写失败不改变结果
		_ = s.audit(ctx, actorOf(acc), nil, "pad_pull", target, audit.Deny)
		return TransitClosure{}, err
	}
	// 不在可见列表则拒绝拉包
	if err := s.assertPadPullable(ctx, acc, assetID); err != nil {
		// 记下厂网拉包被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "pad_pull", target, audit.Deny)
		return TransitClosure{}, err
	}
	// 厂网拉包组出工程，工艺另封
	snap, err := s.packForPad(ctx, assetID)
	// 组包失败则这次拉包拒绝
	if err != nil {
		// 记下厂网拉包被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "pad_pull", target, audit.Deny)
		return TransitClosure{}, err
	}
	// 不是工程则拒绝装袋或授权
	if snap.Kind == KindProject {
		// 工程明文过站，只引用工艺 Id，不封过站 DEK。
		return TransitClosure{Snapshot: snap}, s.audit(ctx, &acc.ID, nil, "pad_pull", target, audit.Allow)
	}
	// 过站 DEK 用登录人钥封，AAD 绑登录人，不绑焊机。
	who, err := s.store.EnsurePersonUnwrapKey(ctx, acc.ID)
	// 解封钥读不到或长度不对，拒绝继续
	if err != nil || len(who.UnwrapKey) != contentcrypt.KeySize {
		// 解封钥读不到或长度不对，记下厂网拉包被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "pad_pull", target, audit.Deny)
		// 读钥失败则交回错误，长度不对另按无效钥拒绝
		if err != nil {
			return TransitClosure{}, err
		}
		return TransitClosure{}, domain.ErrForbidden
	}
	// 取出本厂稳定身份，封包和审计都要用
	out, err := sealTransit(s.store.FactoryID(), acc.ID, who.UnwrapKey, snap)
	// 读钥失败则交回错误，长度不对另按无效钥拒绝
	if err != nil {
		// 记下厂网拉包被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "pad_pull", target, audit.Deny)
		return TransitClosure{}, err
	}
	// 厂网拉包成功后记审计，再把结果交回
	return out, s.audit(ctx, &acc.ID, nil, "pad_pull", target, audit.Allow)
}

// HandleClientUp 收下本机回执；身份以 MQTT 会话为准，载荷里自报的不管。
func (s *Closure) HandleClientUp(ctx context.Context, clientID uuid.UUID, payload []byte) {
	// 带正文的上行交给闭包回执，这里只认在场
	if clientmqtt.HasBody(payload) {
		// 记下本机回执被拒绝，写失败不改变结果
		_ = s.audit(ctx, nil, nil, "client_ack", clientID.String(), audit.Deny)
		return
	}
	// 准备接本机回执，种类不对就忽略
	var rec clientmqtt.Receipt
	// 种类不对则忽略，不当成在场或回执
	if json.Unmarshal(payload, &rec) != nil || rec.Typ != clientmqtt.TypAck {
		return
	}
	// 收成文本身份，供审计或对账对准
	target := clientID.String()
	// 回执带了资产才补进审计对象，空的忽略
	if rec.AssetID != "" {
		// 审计对象补上回执里的资产，方便对上哪一包
		target += " " + rec.AssetID
	}
	// 记下本机回执已允许，写失败不回滚已完成的事
	_ = s.audit(ctx, nil, nil, "client_ack", target, audit.Allow)
}

// 当前登录人必须是这台已绑定本机的操作员。
func (s *kernel) requireClientOperator(ctx context.Context, token string, clientID uuid.UUID) (Account, Client, error) {
	// 厂网登录人对未作废设备对账
	acc, cli, err := s.requireBoundClient(ctx, token, clientID)
	// 设备已作废或未绑定则拒绝
	if err != nil {
		return acc, cli, err
	}
	// 这台没有使用人或使用人不是当前登录者
	if cli.OperatorID == nil || *cli.OperatorID != acc.ID {
		return acc, cli, domain.ErrForbidden
	}
	return acc, cli, nil
}

// 厂网登录人可对账本厂未作废设备；不要求已占操作员位。
func (s *kernel) requireBoundClient(ctx context.Context, token string, clientID uuid.UUID) (Account, Client, error) {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return Account{}, Client{}, err
	}
	// 按身份读本厂这台设备
	cli, err := s.store.ClientByID(ctx, clientID)
	// 这台设备不在名录，拒绝
	if err != nil {
		return acc, Client{}, err
	}
	// 不是有效绑定则拒绝签发、登录或对账
	if cli.Status != ClientStatusBound {
		return acc, cli, domain.ErrBindingVoid
	}
	return acc, cli, nil
}

// actorOf 没有账号时审计不记人。
func actorOf(acc Account) *uuid.UUID {
	// 会话上还没有人，对账清单按空处理
	if acc.ID == uuid.Nil {
		return nil
	}
	// 另拷一份身份再取址，避免下一轮把指针改掉
	id := acc.ID
	return &id
}

// 厂级/平台级须有有效授权；个人级仅创建人。
func (s *Closure) assertPullable(ctx context.Context, acc Account, clientID, projectID uuid.UUID) error {
	// 装上要组包的工程根
	root, err := s.loadRootForPack(ctx, projectID)
	// 根读不到则不能组包
	if err != nil {
		return err
	}
	// 不是工程则拒绝装袋或授权
	if root.Kind != KindProject {
		return domain.ErrForbidden
	}
	// 个人级只给创建人，平台级按副本规则
	if root.Level == AssetLevelPersonal {
		// 不是创建人则个人级拒绝，或改看授权
		if root.CreatorID == nil || *root.CreatorID != acc.ID {
			return domain.ErrForbidden
		}
		return nil
	}
	// 读这台对该工程是否仍有授权
	grant, err := s.store.ClientGrant(ctx, projectID, clientID)
	// 授权读不到或已收回则拒绝下发
	if err != nil {
		return err
	}
	// 授权已收回则这台不能再收这份工程
	if !grant.Active {
		return domain.ErrForbidden
	}
	return nil
}

// 厂网登录可拉厂端列表里同一份资产，不另走下发授权。
func (s *Closure) assertPadPullable(ctx context.Context, acc Account, assetID uuid.UUID) error {
	// 只留下调用方能看的资产
	rows, err := s.listVisibleAssets(ctx, acc, "")
	// 裁剪失败则不交越权行
	if err != nil {
		return err
	}
	// 逐行按调用方作用域裁，看不到的丢掉
	for _, a := range rows {
		// 就是要找的那一条，命中后采用
		if a.ID == assetID {
			return nil
		}
	}
	return domain.ErrNotFound
}

// listPadAssetRefs 与厂端工艺/工程列表同一范围。
func (s *Closure) listPadAssetRefs(ctx context.Context, acc Account) ([]ClosureRef, error) {
	// 只留下调用方能看的资产
	rows, err := s.listVisibleAssets(ctx, acc, "")
	// 裁剪失败则不交越权行
	if err != nil {
		return nil, err
	}
	// 按条数决定是空、超限还是继续
	out := make([]ClosureRef, 0, len(rows))
	// 逐行按调用方作用域裁，看不到的丢掉
	for _, a := range rows {
		// 厂网对账用的修订和摘要
		ref, err := s.refForPad(ctx, a.ID)
		// 组不出来则这条不进对账清单
		if err != nil {
			continue
		}
		// 把这一条收进结果，漏了清单就不齐
		out = append(out, ref)
	}
	return out, nil
}

// refForPad 组包得到修订和摘要；组包失败仍给出名称。
func (s *Closure) refForPad(ctx context.Context, assetID uuid.UUID) (ClosureRef, error) {
	// 装上要组包的工程根
	root, err := s.loadRootForPack(ctx, assetID)
	// 根读不到则不能组包
	if err != nil {
		return ClosureRef{}, err
	}
	// 厂网拉包组出工程，工艺另封
	snap, err := s.packForPad(ctx, assetID)
	// 组包失败则这次拉包拒绝
	if err != nil {
		return ClosureRef{AssetID: root.ID, Revision: root.Revision, Name: root.Name, Level: root.Level}, nil
	}
	return ClosureRef{AssetID: snap.AssetID, Revision: snap.Revision, Digest: snap.Digest, Name: root.Name, Level: snap.Level}, nil
}

// listAuthorizedRefs 收集该机授权工程和登录人个人级可用工程。
func (s *Closure) listAuthorizedRefs(ctx context.Context, acc Account, clientID uuid.UUID) ([]ClosureRef, error) {
	// 取这台仍有效的工程授权
	grants, err := s.store.ListActiveGrantsForClient(ctx, clientID)
	// 授权读失败，不给残缺对账清单
	if err != nil {
		return nil, err
	}
	// 按条数决定是空、超限还是继续
	out := make([]ClosureRef, 0, len(grants))
	// 用来挡重复，同一份只收一次
	seen := map[uuid.UUID]struct{}{}
	// 逐条有效授权看是否盖住目标
	for _, g := range grants {
		// 组包得到修订和摘要，失败仍留名称
		ref, err := s.refForProject(ctx, g.ProjectID)
		// 连名称都组不出来则这条作废
		if err != nil {
			continue
		}
		// 把这一条收进结果，漏了清单就不齐
		out = append(out, ref)
		// 记下已经收过，后面的重复直接跳过
		seen[g.ProjectID] = struct{}{}
	}
	// 列出本厂正在治理的资产
	rows, err := s.store.ListGovernedAssets(ctx)
	// 列表读失败则不返回残缺
	if err != nil {
		return nil, err
	}
	// 逐行按调用方作用域裁，看不到的丢掉
	for _, a := range rows {
		// 这一条不符合就跳过，其它条继续
		if a.Kind != KindProject || a.Level != AssetLevelPersonal || a.CreatorID != acc.ID || a.Status != AssetAvailable {
			continue
		}
		// 已经收过则跳过，保证一份只出现一次
		if _, ok := seen[a.ID]; ok {
			continue
		}
		// 组包得到修订和摘要，失败仍留名称
		ref, err := s.refForProject(ctx, a.ID)
		// 连名称都组不出来则这条作废
		if err != nil {
			continue
		}
		// 把这一条收进结果，漏了清单就不齐
		out = append(out, ref)
	}
	return out, nil
}

// refForProject 组包得到修订和摘要；组包失败仍给出名称。
func (s *Closure) refForProject(ctx context.Context, projectID uuid.UUID) (ClosureRef, error) {
	// 装上要组包的工程根
	root, err := s.loadRootForPack(ctx, projectID)
	// 根读不到则不能组包
	if err != nil {
		return ClosureRef{}, err
	}
	// 按根组出钉死修订的闭包
	snap, err := s.packFromMember(ctx, root)
	// 组包失败则拒绝下发
	if err != nil {
		return ClosureRef{AssetID: root.ID, Revision: root.Revision, Name: root.Name, Level: root.Level}, nil
	}
	return ClosureRef{AssetID: snap.AssetID, Revision: snap.Revision, Digest: snap.Digest, Name: root.Name, Level: snap.Level}, nil
}

// sealTransit 另造过站 DEK；boundID 是登录人或本机，到站用登录人钥解开后再重封进袋。
func sealTransit(factoryID, boundID uuid.UUID, unwrap []byte, snap ClosureSnapshot) (TransitClosure, error) {
	// 解封钥长度不对则拒绝，避免用坏钥去解
	if len(unwrap) != contentcrypt.KeySize {
		return TransitClosure{}, domain.ErrForbidden
	}
	// 生成过站或解封用的随机钥
	dek, err := contentcrypt.RandomKey()
	// 读钥失败则交回错误，长度不对另按无效钥拒绝
	if err != nil {
		return TransitClosure{}, err
	}
	// 离开时清掉钥或放锁，避免秘密残留或堵住回连
	defer contentcrypt.Zero(dek)
	// 按条数决定是空、超限还是继续
	members := make([]ClosureMember, len(snap.Members))
	// 逐个成员解封或重封，缺一个就整包失败
	for i, m := range snap.Members {
		// 复制一份字节，避免和原来的钥或正文共用底层
		plain := append([]byte(nil), m.Content...)
		// 绑上本机身份再封过站包
		env, err := contentcrypt.Seal(dek, plain, contentcrypt.ClientTransitAAD(factoryID, boundID, m.ID, m.Revision))
		// 用完立刻清掉钥，避免留在内存
		contentcrypt.Zero(plain)
		// 本机附加数据拼不齐，拒绝下发
		if err != nil {
			return TransitClosure{}, err
		}
		// 成员换成过站信封，厂库密文不原样下发
		m.Content = env
		// 把处理好的成员放回这一位
		members[i] = m
	}
	// 成员列表换成这一版，下发按它走
	snap.Members = members
	// 绑上过站钥的附加数据
	wrap, err := contentcrypt.Seal(unwrap, dek, contentcrypt.ClientTransitDEKAAD(factoryID, boundID))
	// 附加数据拼不齐，解不开过站钥
	if err != nil {
		return TransitClosure{}, err
	}
	return TransitClosure{Wrap: wrap, Snapshot: snap}, nil
}

// fanoutPolicy 把更高修订策略推给本厂已绑定 Client，正文不进 MQTT。
func (s *Closure) fanoutPolicy(ctx context.Context, pol ClientPolicy) {
	// 没有上送或下行通道则只在厂内记账
	if s.clientDown == nil {
		return
	}
	// 没有签发钥就生成一对，私钥不外送
	key, err := s.ensureSigningKey(ctx)
	// 签发钥备不齐，不能发运行凭证
	if err != nil {
		return
	}
	// 列出本厂已接受的设备，不含私钥
	rows, err := s.store.ListClients(ctx)
	// 设备名录读失败，不返回残缺列表
	if err != nil {
		return
	}
	// 只放身份、修订和摘要，正文不进消息
	in := clientmqtt.Intent{
		Typ:               clientmqtt.TypPolicy,
		Revision:          pol.Revision,
		MaxCachedProjects: pol.MaxCachedProjects,
		CacheScope:        pol.CacheScope,
		PersistUnwrapKey:  pol.PersistUnwrapKey,
		KeyTTLSeconds:     pol.KeyTTLSeconds,
		EncryptPouch:      pol.EncryptPouch,
	}
	// 取出本厂稳定身份，封包和审计都要用
	fid := s.store.FactoryID()
	// 逐行按调用方作用域裁，看不到的丢掉
	for _, cl := range rows {
		// 这一条不符合就跳过，其它条继续
		if cl.Status != ClientStatusBound {
			continue
		}
		// 用本厂私钥给运行声明签名
		raw, err := clientmqtt.Sign(key.PrivateKey, fid, cl.ID, in)
		// 签名失败则这张凭证不能发出
		if err != nil || clientmqtt.HasBody(raw) {
			continue
		}
		// 向这台本机投控制面信封
		s.clientDown.PublishDown(fid, cl.ID, raw)
	}
}

// publishClosureReady 只推身份、修订和摘要，不推成员正文。
func (s *Closure) publishClosureReady(ctx context.Context, clientID uuid.UUID, snap ClosureSnapshot) {
	// 没有上送或下行通道则只在厂内记账
	if s.clientDown == nil {
		return
	}
	// 没有签发钥就生成一对，私钥不外送
	key, err := s.ensureSigningKey(ctx)
	// 签发钥备不齐，不能发运行凭证
	if err != nil {
		return
	}
	// 只放身份、修订和摘要，正文不进消息
	in := clientmqtt.Intent{
		Typ:      clientmqtt.TypClosure,
		Revision: snap.Revision,
		AssetID:  snap.AssetID.String(),
		Digest:   snap.Digest,
	}
	// 取出本厂稳定身份，封包和审计都要用
	fid := s.store.FactoryID()
	// 用本厂私钥给运行声明签名
	raw, err := clientmqtt.Sign(key.PrivateKey, fid, clientID, in)
	// 签名失败则这张凭证不能发出
	if err != nil || clientmqtt.HasBody(raw) {
		return
	}
	// 向这台本机投控制面信封
	s.clientDown.PublishDown(fid, clientID, raw)
}

// OpenTransit 用登录人解封钥解开过站 DEK，再解成员；boundID 须与封包时相同。
func OpenTransit(factoryID, boundID uuid.UUID, unwrap []byte, t TransitClosure) (ClosureSnapshot, error) {
	// 绑上过站钥的附加数据
	dek, err := contentcrypt.Open(unwrap, t.Wrap, contentcrypt.ClientTransitDEKAAD(factoryID, boundID))
	// 附加数据拼不齐，解不开过站钥
	if err != nil {
		return ClosureSnapshot{}, err
	}
	// 离开时清掉钥或放锁，避免秘密残留或堵住回连
	defer contentcrypt.Zero(dek)
	// 从过站包取出快照，准备解开成员
	snap := t.Snapshot
	// 按条数决定是空、超限还是继续
	members := make([]ClosureMember, len(snap.Members))
	// 逐个成员解封或重封，缺一个就整包失败
	for i, m := range snap.Members {
		// 绑上本机身份再封过站包
		plain, err := contentcrypt.Open(dek, m.Content, contentcrypt.ClientTransitAAD(factoryID, boundID, m.ID, m.Revision))
		// 本机附加数据拼不齐，拒绝下发
		if err != nil {
			return ClosureSnapshot{}, err
		}
		// 换成解开的明文，调用方不再碰信封
		m.Content = plain
		// 把处理好的成员放回这一位
		members[i] = m
	}
	// 成员列表换成这一版，下发按它走
	snap.Members = members
	return snap, nil
}
