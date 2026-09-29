package service

import (
	"github.com/google/uuid"

	"wmesh/factory/internal/platform/contentcrypt"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/store"
)

// CachedEnvelope 是本机袋里一份资产信封，正文只以 WM2 落盘。
type CachedEnvelope struct {
	ID       uuid.UUID  // 资产身份
	Level    string     // platform / factory / personal
	OwnerID  *uuid.UUID // 个人级创建人；其余为空
	Name     string     // 显示名，不解正文
	Revision int64      // 钉死修订
	Blob     []byte     // WM2 信封
}

// Pouch 是本机袋：解封钥只在登录内存；C3 起按闭包激活恰好一份工程。
type Pouch struct {
	persist       bool                         // 为真才把解封材料留在包装里
	person        *uuid.UUID                   // 当前登录人，退出后清空
	persistPerson *uuid.UUID                   // 仅 persist 时记下登录人，不是秘密
	unwrap        []byte                       // 只放内存的解封钥，不进审计
	items         map[uuid.UUID]CachedEnvelope // 袋内资产信封，正文只留密文
	material      []byte                       // 仅 persist=true 时保留包装材料，不是工艺明文
	clientID      uuid.UUID                    // 这台本机，空则还没钉设备
	welding       bool                         // 正在焊接时断网也允许做完
	online        bool                         // 连着厂网才按在线策略拉包
	maxCached     int                          // 工程缓存份数上限，超出拒绝
	cacheScope    string                       // 全部获准或只留当前激活工程
	closures      map[uuid.UUID]CachedClosure  // 已缓存的工程闭包，按身份索引
	activeID      *uuid.UUID                   // 当前激活的那一份工程
	activeRev     int64                        // 激活时钉死的修订，旧的不算
}

// NewPouch 空袋，未登录；默认上限 2、scope=all、在线。
func NewPouch() *Pouch {
	return &Pouch{
		items:      map[uuid.UUID]CachedEnvelope{},
		closures:   map[uuid.UUID]CachedClosure{},
		maxCached:  2,
		cacheScope: store.CacheScopeAll,
		online:     true,
	}
}

// Login 把解封钥放进内存；默认不把钥落到包装材料。
func (p *Pouch) Login(unwrap []byte, person uuid.UUID, persist bool) error {
	// 钥长不对或没有登录人，拒绝把钥放进袋
	if len(unwrap) != contentcrypt.KeySize || person == uuid.Nil {
		return domain.ErrForbidden
	}
	// 立刻清掉内存钥、激活和袋内容
	p.Logout()
	// 复制一份字节，避免和原来的钥或正文共用底层
	p.unwrap = append([]byte(nil), unwrap...)
	// 另拷一份身份再取址，避免下一轮把指针改掉
	id := person
	// 记下当前登录人，退出前袋都归这个人
	p.person = &id
	// 记下这次是否允许把解封材料留在包装里
	p.persist = persist
	// 不允许落盘则清掉包装里的登录人
	if persist {
		// 复制一份字节，避免和原来的钥或正文共用底层
		p.material = append([]byte(nil), unwrap...)
		// 只在允许落盘时记下登录人，钥仍不进明文
		p.persistPerson = &id
		// 其余情况走这里，避免前面的分支漏判
	} else {
		// 用完立刻清掉钥，避免留在内存
		contentcrypt.Zero(p.material)
		// 不留包装材料，退出后磁盘上没有解封钥
		p.material = nil
		// 不持久化登录人，避免下次误当成已登录
		p.persistPerson = nil
	}
	return nil
}

// Logout 立刻清内存钥、激活、焊接标记和本机袋；换人等于清库。
func (p *Pouch) Logout() {
	// 用完立刻清掉钥，避免留在内存
	contentcrypt.Zero(p.unwrap)
	// 退出时清掉内存里的解封钥
	p.unwrap = nil
	// 退出后不再记登录人
	p.person = nil
	// 标上是否正在焊接，焊接中不能换激活份
	p.welding = false
	// 清掉当前激活工程，避免退出后仍解开它
	p.activeID = nil
	// 激活修订一并清零，下次必须重新激活
	p.activeRev = 0
	// 清空袋内信封，避免下一个人看到上一份
	p.items = map[uuid.UUID]CachedEnvelope{}
	// 清空已缓存闭包，换人后按新授权再装
	p.closures = map[uuid.UUID]CachedClosure{}
	// 不允许落盘则清掉包装里的登录人
	if !p.persist {
		// 用完立刻清掉钥，避免留在内存
		contentcrypt.Zero(p.material)
		// 不留包装材料，退出后磁盘上没有解封钥
		p.material = nil
		// 不持久化登录人，避免下次误当成已登录
		p.persistPerson = nil
	}
}

// PutPlain 登录后把明文重封进袋；未登录拒绝。
func (p *Pouch) PutPlain(id uuid.UUID, level, name string, rev int64, owner *uuid.UUID, plain []byte) error {
	// 解封钥长度不对则拒绝，避免用坏钥去解
	if len(p.unwrap) != contentcrypt.KeySize || p.person == nil {
		return domain.ErrUnauthorized
	}
	// 个人级只给创建人，平台级按副本规则
	if level == store.AssetLevelPersonal {
		// 个人级只有创建人能放进自己的袋
		if owner == nil || *owner != *p.person {
			return domain.ErrForbidden
		}
	}
	// 绑上资产身份和修订再封装
	blob, err := contentcrypt.Seal(p.unwrap, plain, contentcrypt.AssetAAD(id, rev, "pouch"))
	// 附加数据拼不齐，拒绝封装
	if err != nil {
		return err
	}
	// 组一份只含密文的信封，明文不留在袋外
	env := CachedEnvelope{ID: id, Level: level, Name: name, Revision: rev, Blob: blob}
	// 有创建人或本机才拷走指针，空的保持空
	if owner != nil {
		// 另拷创建人再取址，避免和入参共用指针
		oid := *owner
		// 个人级记下创建人，其他人解开会被拒绝
		env.OwnerID = &oid
	}
	// 把重封后的信封放进袋，正文仍是密文
	p.items[id] = env
	return nil
}

// PutPlainIfNewer 只在更高修订时覆盖；同修订或更低不改袋。
func (p *Pouch) PutPlainIfNewer(id uuid.UUID, level, name string, rev int64, owner *uuid.UUID, plain []byte) (bool, error) {
	// 袋里已有同等或更高修订，不覆盖成更旧的
	if env, ok := p.items[id]; ok && env.Revision >= rev {
		return false, nil
	}
	// 未登录则拒绝封入
	if err := p.PutPlain(id, level, name, rev, owner, plain); err != nil {
		return false, err
	}
	return true, nil
}

// RevisionOf 袋内该身份当前修订；没有则 0。
func (p *Pouch) RevisionOf(id uuid.UUID) int64 {
	// 袋里已有这份才比较或解开，没有则按新的或拒绝
	if env, ok := p.items[id]; ok {
		return env.Revision
	}
	return 0
}

// Open 解开一份信封；未登录、个人级非创建人都拒绝。
func (p *Pouch) Open(id uuid.UUID) ([]byte, error) {
	// 解封钥长度不对则拒绝，避免用坏钥去解
	if len(p.unwrap) != contentcrypt.KeySize || p.person == nil {
		return nil, domain.ErrUnauthorized
	}
	// 取出这一条并看是否存在，没有则留空
	env, ok := p.items[id]
	// 袋里没有这一份，拒绝解开或激活
	if !ok {
		return nil, domain.ErrNotFound
	}
	// 个人级只给创建人，平台级按副本规则
	if env.Level == store.AssetLevelPersonal {
		// 不是创建人就拒绝解开这份个人级资产
		if env.OwnerID == nil || p.person == nil || *env.OwnerID != *p.person {
			return nil, domain.ErrForbidden
		}
	}
	// 绑上资产身份和修订再封装，再交回调用方
	return contentcrypt.Open(p.unwrap, env.Blob, contentcrypt.AssetAAD(env.ID, env.Revision, "pouch"))
}

// DiskSnapshot 只给出信封和元数据，不含解封钥。
func (p *Pouch) DiskSnapshot() []CachedEnvelope {
	// 按条数决定是空、超限还是继续
	out := make([]CachedEnvelope, 0, len(p.items))
	// 逐条待发或清单处理，失败的留下次
	for _, env := range p.items {
		// 先复制再改，避免改到袋内或入参原文
		cp := env
		// 复制一份字节，避免和原来的钥或正文共用底层
		cp.Blob = append([]byte(nil), env.Blob...)
		// 有创建人或本机才拷走指针，空的保持空
		if env.OwnerID != nil {
			// 另拷创建人再取址，避免和入参共用指针
			oid := *env.OwnerID
			// 个人级记下创建人，其他人解开会被拒绝
			cp.OwnerID = &oid
		}
		// 把这一条收进结果，漏了清单就不齐
		out = append(out, cp)
	}
	return out
}

// HasUnwrapKey 内存里是否还持有解封钥。
func (p *Pouch) HasUnwrapKey() bool {
	// 按条数决定是空、超限还是继续，再交回调用方
	return len(p.unwrap) == contentcrypt.KeySize
}

// RelockFromMaterial 仅在允许落盘包装材料且材料仍在时恢复内存钥。
func (p *Pouch) RelockFromMaterial() error {
	// 解封钥长度不对则拒绝，避免用坏钥去解
	if !p.persist || len(p.material) != contentcrypt.KeySize {
		return domain.ErrUnauthorized
	}
	// 复制一份字节，避免和原来的钥或正文共用底层
	p.unwrap = append([]byte(nil), p.material...)
	// 不允许落盘则清掉包装里的登录人
	if p.persistPerson != nil {
		// 另拷一份身份再取址，避免下一轮把指针改掉
		id := *p.persistPerson
		// 记下当前登录人，退出前袋都归这个人
		p.person = &id
	}
	return nil
}
