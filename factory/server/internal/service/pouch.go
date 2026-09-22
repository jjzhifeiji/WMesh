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
	persist       bool
	person        *uuid.UUID
	persistPerson *uuid.UUID // 仅 persist 时记下登录人，不是秘密
	unwrap        []byte
	items         map[uuid.UUID]CachedEnvelope
	material      []byte // 仅 persist=true 时保留包装材料，不是工艺明文
	clientID      uuid.UUID
	welding       bool
	online        bool
	maxCached     int
	cacheScope    string
	closures      map[uuid.UUID]CachedClosure
	activeID      *uuid.UUID
	activeRev     int64
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
	if len(unwrap) != contentcrypt.KeySize || person == uuid.Nil {
		return domain.ErrForbidden
	}
	p.Logout()
	p.unwrap = append([]byte(nil), unwrap...)
	id := person
	p.person = &id
	p.persist = persist
	if persist {
		p.material = append([]byte(nil), unwrap...)
		p.persistPerson = &id
	} else {
		contentcrypt.Zero(p.material)
		p.material = nil
		p.persistPerson = nil
	}
	return nil
}

// Logout 立刻清内存钥、激活、焊接标记和本机袋；换人等于清库。
func (p *Pouch) Logout() {
	contentcrypt.Zero(p.unwrap)
	p.unwrap = nil
	p.person = nil
	p.welding = false
	p.activeID = nil
	p.activeRev = 0
	p.items = map[uuid.UUID]CachedEnvelope{}
	p.closures = map[uuid.UUID]CachedClosure{}
	if !p.persist {
		contentcrypt.Zero(p.material)
		p.material = nil
		p.persistPerson = nil
	}
}

// PutPlain 登录后把明文重封进袋；未登录拒绝。
func (p *Pouch) PutPlain(id uuid.UUID, level, name string, rev int64, owner *uuid.UUID, plain []byte) error {
	if len(p.unwrap) != contentcrypt.KeySize || p.person == nil {
		return domain.ErrUnauthorized
	}
	if level == store.AssetLevelPersonal {
		if owner == nil || *owner != *p.person {
			return domain.ErrForbidden
		}
	}
	blob, err := contentcrypt.Seal(p.unwrap, plain, contentcrypt.AssetAAD(id, rev, "pouch"))
	if err != nil {
		return err
	}
	env := CachedEnvelope{ID: id, Level: level, Name: name, Revision: rev, Blob: blob}
	if owner != nil {
		oid := *owner
		env.OwnerID = &oid
	}
	p.items[id] = env
	return nil
}

// PutPlainIfNewer 只在更高修订时覆盖；同修订或更低不改袋。
func (p *Pouch) PutPlainIfNewer(id uuid.UUID, level, name string, rev int64, owner *uuid.UUID, plain []byte) (bool, error) {
	if env, ok := p.items[id]; ok && env.Revision >= rev {
		return false, nil
	}
	if err := p.PutPlain(id, level, name, rev, owner, plain); err != nil {
		return false, err
	}
	return true, nil
}

// RevisionOf 袋内该身份当前修订；没有则 0。
func (p *Pouch) RevisionOf(id uuid.UUID) int64 {
	if env, ok := p.items[id]; ok {
		return env.Revision
	}
	return 0
}

// Open 解开一份信封；未登录、个人级非创建人都拒绝。
func (p *Pouch) Open(id uuid.UUID) ([]byte, error) {
	if len(p.unwrap) != contentcrypt.KeySize || p.person == nil {
		return nil, domain.ErrUnauthorized
	}
	env, ok := p.items[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	if env.Level == store.AssetLevelPersonal {
		if env.OwnerID == nil || p.person == nil || *env.OwnerID != *p.person {
			return nil, domain.ErrForbidden
		}
	}
	return contentcrypt.Open(p.unwrap, env.Blob, contentcrypt.AssetAAD(env.ID, env.Revision, "pouch"))
}

// DiskSnapshot 只给出信封和元数据，不含解封钥。
func (p *Pouch) DiskSnapshot() []CachedEnvelope {
	out := make([]CachedEnvelope, 0, len(p.items))
	for _, env := range p.items {
		cp := env
		cp.Blob = append([]byte(nil), env.Blob...)
		if env.OwnerID != nil {
			oid := *env.OwnerID
			cp.OwnerID = &oid
		}
		out = append(out, cp)
	}
	return out
}

// HasUnwrapKey 内存里是否还持有解封钥。
func (p *Pouch) HasUnwrapKey() bool {
	return len(p.unwrap) == contentcrypt.KeySize
}

// RelockFromMaterial 仅在允许落盘包装材料且材料仍在时恢复内存钥。
func (p *Pouch) RelockFromMaterial() error {
	if !p.persist || len(p.material) != contentcrypt.KeySize {
		return domain.ErrUnauthorized
	}
	p.unwrap = append([]byte(nil), p.material...)
	if p.persistPerson != nil {
		id := *p.persistPerson
		p.person = &id
	}
	return nil
}
