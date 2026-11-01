package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

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

// TransitClosure 过站包：工程根和工艺成员都是过站信封，Wrap 封着过站 DEK。
type TransitClosure struct {
	Wrap     []byte          `json:"wrap,omitempty"` // 过站 DEK，用登录人或本机钥封
	Snapshot ClosureSnapshot `json:"snapshot"`       // 成员正文都是过站信封
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

// PadClientInbox 厂网登录拉本人可用的工艺/工程清单，别人的个人级不给；不要求已绑设备。
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
	// 按登录人裁过：别人的个人级不给，钉住的也不例外
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

// PadPullClientClosure 厂网登录后拉包：工程和工艺都用登录人钥封过站 DEK；列表外钉住的平台工艺按钉死修订给。
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
	// 不在本人示教器清单里则拒绝拉包；列表外钉住的平台工艺带回钉死修订
	pin, err := s.assertPadPullable(ctx, acc, assetID)
	// 清单里没有这份则记拒绝并停下
	if err != nil {
		// 记下厂网拉包被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "pad_pull", target, audit.Deny)
		return TransitClosure{}, err
	}
	// 可见资产照旧按身份取当前版组包
	var snap ClosureSnapshot
	// 列表外钉住的平台工艺按钉死修订单独成包，不拿最新版顶
	if pin != nil {
		// 按钉死修订组单工艺包，读不到钉死修订就拒
		snap, err = s.packPinnedForPad(ctx, *pin)
		// 列表里有的照旧组包
	} else {
		// 组出工程根或单工艺包，正文随后统一封过站
		snap, err = s.packForPad(ctx, assetID)
	}
	// 组包失败则这次拉包拒绝
	if err != nil {
		// 记下厂网拉包被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "pad_pull", target, audit.Deny)
		return TransitClosure{}, err
	}
	// 工程根和工艺同样封：过站 DEK 用登录人钥封，AAD 绑登录人，不绑焊机。
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
func (s *Closure) HandleClientUp(ctx context.Context, personID, clientID uuid.UUID, payload []byte) {
	// 带正文或解不开的上行只计数，不逐条写审计
	if clientmqtt.HasBody(payload) {
		// 累加被拒条数，按条数节流记日志
		s.noteUpReject(personID, clientID)
		return
	}
	// 准备接本机回执，种类不对就忽略
	var rec clientmqtt.Receipt
	// 字段类型对不上的上行同样只计数
	if json.Unmarshal(payload, &rec) != nil {
		// 累加被拒条数，按条数节流记日志
		s.noteUpReject(personID, clientID)
		return
	}
	// 种类不对则忽略，不当成在场或回执
	if rec.Typ != clientmqtt.TypAck {
		return
	}
	// 收成文本身份，供审计或对账对准
	target := clientID.String()
	// 资产能按身份解析才补进审计对象，乱填的不拼
	if asset, err := uuid.Parse(rec.AssetID); err == nil {
		// 补上规范写法的资产身份，长度固定
		target += " " + asset.String()
	}
	// 操作者记会话里鉴权过的登录人，空身份不记人
	var actor *uuid.UUID
	// 会话里有登录人才记到人头上
	if personID != uuid.Nil {
		// 取登录人的地址作操作者
		actor = &personID
	}
	// 记下本机回执已允许，写失败不回滚已完成的事
	_ = s.audit(ctx, actor, nil, "client_ack", target, audit.Allow)
}

// noteUpReject 累加被拒的本机上行，头一条和每满一批才记日志。
func (s *Closure) noteUpReject(personID, clientID uuid.UUID) {
	// 累加本厂被拒的上行条数
	n := s.upRejects.Add(1)
	// 头一条和每满一批才打日志，免得刷屏
	if n == 1 || n%upRejectLogEvery == 0 {
		// 记下哪台哪个人送了坏上行，便于排查
		slog.Warn("client mqtt up rejected", "client", clientID, "person", personID, "rejected", n)
	}
}

// upRejectLogEvery 被拒的本机上行每满这么多条再记一条日志。
const upRejectLogEvery = 100

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

// 厂网登录只能拉本人清单里的资产，与清单同一套裁剪；不另走下发授权。列表外钉住的平台工艺交回钉死依赖。
func (s *Closure) assertPadPullable(ctx context.Context, acc Account, assetID uuid.UUID) (*AssetDep, error) {
	// 按登录人裁过：别人的个人级不给
	rows, err := s.padVisibleAssets(ctx, acc)
	// 裁剪失败则不交越权行
	if err != nil {
		return nil, err
	}
	// 逐行按调用方作用域裁，看不到的丢掉
	for _, a := range rows {
		// 就是要找的那一条，命中后按当前版拉
		if a.ID == assetID {
			return nil, nil
		}
	}
	// 列表里没有，再看可见工程钉住的平台工艺
	pins, err := s.padPinnedProcesses(ctx, rows)
	// 钉住的收不齐则不放行
	if err != nil {
		return nil, err
	}
	// 逐份钉住的平台工艺找要拉的那一份
	for _, p := range pins {
		// 身份对上就按钉死修订放行
		if p.Dep.ID == assetID {
			// 另拷一份钉死依赖再取址，避免指向循环变量
			d := p.Dep
			return &d, nil
		}
	}
	return nil, domain.ErrNotFound
}

// listPadAssetRefs 示教器清单，按登录人裁过：别人的个人级不给。
func (s *Closure) listPadAssetRefs(ctx context.Context, acc Account) ([]ClosureRef, error) {
	// 按登录人裁过：别人的个人级不给
	rows, err := s.padVisibleAssets(ctx, acc)
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
	// 再补列表外、被可见工程钉住的平台工艺，免得示教器对账把它删掉
	pins, err := s.padPinnedProcesses(ctx, rows)
	// 钉住的收不齐则不交残缺清单
	if err != nil {
		return nil, err
	}
	// 逐份钉住的平台工艺补进清单
	for _, p := range pins {
		// 修订填钉死修订，示教器照常逐条拉
		out = append(out, s.refForPinned(ctx, p))
	}
	return out, nil
}

// padPin 可见工程钉住、但列表里已没有的一份平台工艺。
type padPin struct {
	Dep  AssetDep // 钉死的身份、修订和摘要
	Name string   // 钉死那一修订的显示名
}

// padPinnedProcesses 收出可见工程钉住、列表外但副本表能按修订读到的平台工艺；同身份取最高钉死修订。
func (s *Closure) padPinnedProcesses(ctx context.Context, rows []Asset) ([]padPin, error) {
	// 记下列表里已有的身份，这些照旧按身份给当前版
	listed := make(map[uuid.UUID]struct{}, len(rows))
	// 逐条记下可见资产的身份
	for _, a := range rows {
		// 这个身份已在清单里，不再另补
		listed[a.ID] = struct{}{}
	}
	// 同一身份只留最高的钉死修订
	best := map[uuid.UUID]padPin{}
	// 按首次碰到的先后排，清单顺序稳定
	order := []uuid.UUID{}
	// 逐份可见资产看它钉了哪些工艺
	for _, a := range rows {
		// 只有工程才有钉死依赖，工艺跳过
		if a.Kind != KindProject {
			continue
		}
		// 逐条看钉死依赖，列表里有的不再补
		for _, d := range a.Deps {
			// 列表里已有这份工艺，照旧给当前版
			if _, ok := listed[d.ID]; ok {
				continue
			}
			// 看这个身份是否已收过钉死修订
			prev, ok := best[d.ID]
			// 已有不低于这一条的修订，不再重读
			if ok && prev.Dep.Revision >= d.Revision {
				continue
			}
			// 按钉死修订读副本元数据，不解正文
			r, err := s.store.ReplicaMetaByIDRev(ctx, d.ID, d.Revision)
			// 副本表里没有这一修订就不补，库错误则整份失败
			if err != nil {
				// 找不到说明这条不是已收的平台工艺，跳过即可
				if errors.Is(err, domain.ErrNotFound) {
					continue
				}
				return nil, err
			}
			// 只补平台级工艺，别的种类不走这条口子
			if r.Kind != KindProcess || r.Level != AssetLevelPlatform {
				continue
			}
			// 头一回碰到这个身份就记下先后
			if !ok {
				// 按首次出现的位置排进清单
				order = append(order, d.ID)
			}
			// 留下这条更高的钉死修订和它的名字
			best[d.ID] = padPin{Dep: d, Name: r.Name}
		}
	}
	// 按记下的先后备好结果
	out := make([]padPin, 0, len(order))
	// 按先后把每个身份的最高钉死修订收进来
	for _, pid := range order {
		// 收进这份钉住的平台工艺
		out = append(out, best[pid])
	}
	return out, nil
}

// refForPinned 钉住的平台工艺按钉死修订组包；组包失败仍给出身份、修订和名称。
func (s *Closure) refForPinned(ctx context.Context, p padPin) ClosureRef {
	// 按钉死修订组单工艺包，拿整包摘要
	snap, err := s.packPinnedForPad(ctx, p.Dep)
	// 组不出来仍按钉死修订列出，和可见资产组包失败同一口径
	if err != nil {
		return ClosureRef{AssetID: p.Dep.ID, Revision: p.Dep.Revision, Name: p.Name, Level: AssetLevelPlatform}
	}
	return ClosureRef{AssetID: snap.AssetID, Revision: snap.Revision, Digest: snap.Digest, Name: p.Name, Level: snap.Level}
}

// padVisibleAssets 示教器可拉范围：厂端列表去掉别人的个人级，钉住的也不给。
func (s *Closure) padVisibleAssets(ctx context.Context, acc Account) ([]Asset, error) {
	// 先取厂端列表那一份，再按登录人裁
	rows, err := s.listVisibleAssets(ctx, acc, "")
	// 列表读不出来则整份失败，不回半截
	if err != nil {
		return nil, err
	}
	// 按列表条数备好容量，顺序和厂端列表一致
	out := make([]Asset, 0, len(rows))
	// 逐条看归属，别人的个人级不进示教器
	for _, a := range rows {
		// 个人级只给创建人，管理员和被钉住的都不例外
		if a.Level == AssetLevelPersonal && a.CreatorID != acc.ID {
			continue
		}
		// 本人个人级、厂级和平台级照旧收进来
		out = append(out, a)
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
