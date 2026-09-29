package service

import (
	"errors"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/store"
)

// CachedMember 是闭包里一条成员的核对材料，不含明文。
type CachedMember struct {
	ID       uuid.UUID  // 稳定身份
	Kind     string     // process / project
	Level    string     // platform / factory / personal
	Name     string     // 显示名
	Status   string     // 送达时状态
	WeldKind string     // 作业类型：single / multilayer / tbar
	Revision int64      // 钉死修订
	Digest   []byte     // 内容 SHA-256
	Deps     []AssetDep // 工艺必须空；工程钉死工艺
	OwnerID  *uuid.UUID // 个人级创建人；其余为空
}

// CachedClosure 是袋内一份工程闭包的元数据，正文只在信封里。
type CachedClosure struct {
	AssetID        uuid.UUID      // 根资产身份
	Revision       int64          // 根修订
	Kind           string         // 须为 project
	Level          string         // 与源相同
	Status         string         // 与源相同
	Name           string         // 显示名
	Digest         []byte         // 整包 SHA-256
	TargetClientID *uuid.UUID     // 只许本机激活
	Members        []CachedMember // 根在前，其余按 deps
}

// BindClient 钉本机 Client 身份，只拷他机信封时对不上。
func (p *Pouch) BindClient(clientID uuid.UUID) {
	// 钉上这台本机，闭包只对这台生效
	p.clientID = clientID
}

// SetPolicy 记下缓存上限和 scope；current 且已激活则丢掉其他工程份。
func (p *Pouch) SetPolicy(maxCached int, scope string) {
	// 上限没给或小于一则按两份，避免零上限锁死
	if maxCached < 1 {
		// 没给上限时按两份，避免把袋撑满
		maxCached = 2
	}
	// 套上缓存份数上限，多出来的工程不进袋
	p.maxCached = maxCached
	// 策略只要当前工程时，清单里不带其它份
	if scope != store.CacheScopeCurrent {
		// 没指定范围就缓存该人获准的全部工程
		scope = store.CacheScopeAll
	}
	// 记下缓存范围，只留当前时会丢掉其它份
	p.cacheScope = scope
	// 只留当前激活工程及其成员工艺
	p.pruneToActive()
}

// SetOnline 标记能否向厂端拉包；离线且袋内无该工程则激活拒绝。
func (p *Pouch) SetOnline(online bool) {
	// 按现场改在线标记，离线不再向厂端拉包
	p.online = online
}

// SetWelding 标记焊接中；焊接中不得切换激活、不得撤当前份。
func (p *Pouch) SetWelding(welding bool) {
	// 标上是否正在焊接，焊接中不能换激活份
	p.welding = welding
}

// HasClosure 袋内是否已有该工程闭包元数据。
func (p *Pouch) HasClosure(id uuid.UUID) bool {
	// 先看有没有这一份，没有就拒绝或留空
	_, ok := p.closures[id]
	return ok
}

// ActiveProject 当前激活工程；未激活为空。
func (p *Pouch) ActiveProject() *uuid.UUID {
	// 复制可空身份，避免和袋内共用指针，再交回调用方
	return cloneUUID(p.activeID)
}

// ActiveRevision 当前激活修订；无激活为 0。
func (p *Pouch) ActiveRevision() int64 {
	return p.activeRev
}

// ExportClosures 给出闭包元数据副本，不含明文。
func (p *Pouch) ExportClosures() []CachedClosure {
	// 按条数决定是空、超限还是继续
	out := make([]CachedClosure, 0, len(p.closures))
	// 逐份已缓存闭包处理，超范围的撤掉
	for _, c := range p.closures {
		// 复制闭包元数据，调用方改不到袋内
		out = append(out, cloneCached(c))
	}
	return out
}

// RestoreClosures 从磁盘元数据恢复闭包分组，不解正文。
func (p *Pouch) RestoreClosures(list []CachedClosure) {
	// 清空已缓存闭包，换人后按新授权再装
	p.closures = map[uuid.UUID]CachedClosure{}
	// 逐条处理，某一条失败不把整批悄悄算成功
	for _, c := range list {
		// 复制闭包元数据，调用方改不到袋内
		p.closures[c.AssetID] = cloneCached(c)
	}
}

// RestoreActive 恢复上次激活标记；袋内没有该闭包则忽略。
func (p *Pouch) RestoreActive(id *uuid.UUID, rev int64) {
	// 没有要恢复的激活工程，清空标记
	if id == nil {
		// 清掉当前激活工程，避免退出后仍解开它
		p.activeID = nil
		// 激活修订一并清零，下次必须重新激活
		p.activeRev = 0
		return
	}
	// 没有取到则按缺失处理
	if _, ok := p.closures[*id]; !ok {
		// 清掉当前激活工程，避免退出后仍解开它
		p.activeID = nil
		// 激活修订一并清零，下次必须重新激活
		p.activeRev = 0
		return
	}
	// 复制可空身份，避免和袋内共用指针
	p.activeID = cloneUUID(id)
	// 钉住这次激活的修订，旧修订不再当当前
	p.activeRev = rev
}

// CacheClosure 校验完整闭包后重封进袋；新工程份超上限拒绝，已有份整份不写。
func (p *Pouch) CacheClosure(snap ClosureSnapshot) error {
	// 内存里没有解封钥，拒绝装袋或解开
	if !p.loggedIn() {
		return domain.ErrUnauthorized
	}
	// 不完整则拒绝装进袋
	if err := validateClosure(snap); err != nil {
		return err
	}
	// 不是工程则拒绝装袋或授权
	if snap.Kind != KindProject {
		return domain.ErrForbidden
	}
	// 没钉本机或包不是给这台的，拒绝装袋
	if snap.TargetClientID == nil || p.clientID == uuid.Nil || *snap.TargetClientID != p.clientID {
		return domain.ErrForbidden
	}
	// 袋里已有同等或更高修订，不覆盖成更旧的
	if cur, ok := p.closures[snap.AssetID]; ok && cur.Revision >= snap.Revision {
		return nil
	}
	// 记下替换前的那一份，用来撤掉不再引用的信封
	old := p.closures[snap.AssetID]
	// 逐个成员解封或重封，缺一个就整包失败
	for _, m := range snap.Members {
		// 未登录则拒绝封入
		if err := p.PutPlain(m.ID, m.Level, m.Name, m.Revision, m.CreatorID, m.Content); err != nil {
			return err
		}
	}
	// 去掉正文只留核对材料
	p.closures[snap.AssetID] = metaFromSnap(snap)
	// 撤掉不再被任何闭包引用的信封
	p.dropUnreferenced(old.Members)
	return nil
}

// Activate 本机选定恰好一份已缓存工程；缺成员、串版、只拷、离线无信封、焊接中换份都拒绝。
func (p *Pouch) Activate(projectID uuid.UUID) error {
	// 内存里没有解封钥，拒绝装袋或解开
	if !p.loggedIn() {
		return domain.ErrUnauthorized
	}
	// 没有取到则按缺失处理
	if _, ok := p.closures[projectID]; !ok {
		// 策略只要当前工程时，清单里不带其它份
		if p.cacheScope == store.CacheScopeCurrent && !p.online {
			return domain.ErrNotFound
		}
		return domain.ErrNotFound
	}
	// 正在激活的这份不能撤，其它份可以撤
	if p.welding && p.activeID != nil && *p.activeID != projectID {
		return domain.ErrForbidden
	}
	// 按元数据重组成快照，缺信封当缺成员
	snap, err := p.snapshotFromDisk(projectID)
	// 缺信封则当闭包不完整拒绝
	if err != nil {
		return err
	}
	// 不完整则拒绝装进袋
	if err := validateClosure(snap); err != nil {
		return err
	}
	// 没钉本机或包不是给这台的，拒绝装袋
	if snap.TargetClientID == nil || p.clientID == uuid.Nil || *snap.TargetClientID != p.clientID {
		return domain.ErrForbidden
	}
	// 工程根在第一位，用来核对种类和创建人
	root := snap.Members[0]
	// 个人级只给创建人，平台级按副本规则
	if root.Level == AssetLevelPersonal {
		// 不是创建人则个人级拒绝，或改看授权
		if root.CreatorID == nil || p.person == nil || *root.CreatorID != *p.person {
			return domain.ErrForbidden
		}
	}
	// 不是可用则不能下发、授权或依赖
	if root.Status != AssetAvailable {
		return domain.ErrAssetNotAvailable
	}
	// 另拷一份身份再取址，避免下一轮把指针改掉
	id := snap.AssetID
	// 钉上当前激活工程，同一时刻只留这一份
	p.activeID = &id
	// 钉住装入时的修订，防止串版
	p.activeRev = snap.Revision
	// 只留当前激活工程及其成员工艺
	p.pruneToActive()
	return nil
}

// OpenProcess 只从当前激活闭包的成员工艺按 processId 解明文。
func (p *Pouch) OpenProcess(processID uuid.UUID) ([]byte, error) {
	// 内存里没有解封钥，拒绝装袋或解开
	if !p.loggedIn() {
		return nil, domain.ErrUnauthorized
	}
	// 还没激活工程，不能开工艺或只留当前份
	if p.activeID == nil {
		return nil, domain.ErrNotFound
	}
	// 取出这一条并看是否存在，没有则留空
	cl, ok := p.closures[*p.activeID]
	// 袋里没有这一份，拒绝解开或激活
	if !ok {
		return nil, domain.ErrNotFound
	}
	// 先空着，找到当前激活成员再填
	var member *CachedMember
	// 逐个成员解封或重封，缺一个就整包失败
	for i := range cl.Members {
		// 指到当前闭包里的这个成员再核对
		m := &cl.Members[i]
		// 就是当前要开的那条工艺才解开
		if m.ID == processID {
			// 命中当前激活闭包里的这条工艺
			member = m
			break
		}
	}
	// 不是工程则拒绝装袋或授权
	if member == nil || member.Kind != KindProcess {
		return nil, domain.ErrNotFound
	}
	// 解开这一份，未登录或非创建人拒绝
	return p.Open(processID)
}

// Uncache 撤掉一份未激活缓存；焊接中或正在激活的拒绝。
func (p *Pouch) Uncache(projectID uuid.UUID) error {
	// 内存里没有解封钥，拒绝装袋或解开
	if !p.loggedIn() {
		return domain.ErrUnauthorized
	}
	// 正在焊接才允许在过期后把这一道做完
	if p.welding {
		return domain.ErrForbidden
	}
	// 正在激活的这份不能撤，其它份可以撤
	if p.activeID != nil && *p.activeID == projectID {
		return domain.ErrForbidden
	}
	// 取出这一条并看是否存在，没有则留空
	old, ok := p.closures[projectID]
	// 袋里没有这一份，拒绝解开或激活
	if !ok {
		return domain.ErrNotFound
	}
	// 从集合里撤掉这一份
	delete(p.closures, projectID)
	// 撤掉不再被任何闭包引用的信封
	p.dropUnreferenced(old.Members)
	return nil
}

// loggedIn 是否已把解封钥放进内存。
func (p *Pouch) loggedIn() bool {
	// 看内存里是否还持有解封钥，再交回调用方
	return p.HasUnwrapKey() && p.person != nil
}

// projectCount 袋内不重复工程份数。
func (p *Pouch) projectCount() int {
	// 先数袋里有几份不重复工程
	n := 0
	// 逐份已缓存闭包处理，超范围的撤掉
	for _, c := range p.closures {
		// 不是工程则拒绝装袋或授权
		if c.Kind == KindProject {
			// 又一份工程，用来和缓存上限比较
			n++
		}
	}
	return n
}

// pruneToActive current 时只留当前激活工程及其成员工艺。
func (p *Pouch) pruneToActive() {
	// 还没激活工程，不能开工艺或只留当前份
	if p.cacheScope != store.CacheScopeCurrent || p.activeID == nil {
		return
	}
	// 记下要留下的激活工程，其余份撤掉
	keep := *p.activeID
	// 逐份已缓存闭包处理，超范围的撤掉
	for id, cl := range p.closures {
		// 当前激活的这份留下，其它工程从袋里撤掉
		if id == keep {
			continue
		}
		// 从集合里撤掉这一份
		delete(p.closures, id)
		// 撤掉不再被任何闭包引用的信封
		p.dropUnreferenced(cl.Members)
	}
}

// dropUnreferenced 撤掉不再被任何闭包引用的信封。
func (p *Pouch) dropUnreferenced(old []CachedMember) {
	// 算出仍被闭包引用的资产
	ref := p.referencedIDs()
	// 逐条处理，某一条失败不把整批悄悄算成功
	for _, m := range old {
		// 没有取到则按缺失处理
		if _, ok := ref[m.ID]; !ok {
			// 从集合里撤掉这一份
			delete(p.items, m.ID)
		}
	}
}

// referencedIDs 仍被闭包引用的资产。
func (p *Pouch) referencedIDs() map[uuid.UUID]struct{} {
	out := map[uuid.UUID]struct{}{}
	for _, c := range p.closures {
		// 逐个成员解封或重封，缺一个就整包失败
		for _, m := range c.Members {
			// 记下仍被引用或仍在线的身份
			out[m.ID] = struct{}{}
		}
	}
	return out
}

// snapshotFromDisk 解开成员后按元数据重组成快照，缺信封当缺成员。
func (p *Pouch) snapshotFromDisk(id uuid.UUID) (ClosureSnapshot, error) {
	// 取出这一条并看是否存在，没有则留空
	meta, ok := p.closures[id]
	// 袋里没有这一份，拒绝解开或激活
	if !ok {
		return ClosureSnapshot{}, domain.ErrNotFound
	}
	// 按条数决定是空、超限还是继续
	members := make([]ClosureMember, len(meta.Members))
	// 逐个成员解封或重封，缺一个就整包失败
	for i, m := range meta.Members {
		// 解开这一份，未登录或非创建人拒绝
		plain, err := p.Open(m.ID)
		// 解不开则拒绝，不当成明文
		if err != nil {
			// 没有这条记录，按缺失跳过或拒绝
			if errors.Is(err, domain.ErrNotFound) {
				return ClosureSnapshot{}, domain.ErrClosureIncomplete
			}
			return ClosureSnapshot{}, err
		}
		// 把处理好的成员放回这一位
		members[i] = ClosureMember{
			ID: m.ID, Kind: m.Kind, Level: m.Level, Name: m.Name, Status: m.Status, WeldKind: m.WeldKind,
			Revision: m.Revision, Content: plain, Digest: append([]byte(nil), m.Digest...),
			Deps: copyDeps(m.Deps), CreatorID: cloneUUID(m.OwnerID),
		}
	}
	return ClosureSnapshot{
		Kind: meta.Kind, AssetID: meta.AssetID, Revision: meta.Revision, Level: meta.Level,
		Status: meta.Status, TargetClientID: cloneUUID(meta.TargetClientID),
		Members: members, Digest: append([]byte(nil), meta.Digest...),
	}, nil
}

// metaFromSnap 去掉正文只留核对材料。
func metaFromSnap(snap ClosureSnapshot) CachedClosure {
	// 按条数决定是空、超限还是继续
	members := make([]CachedMember, len(snap.Members))
	// 名字先空着，成员带了显示名再填上
	name := ""
	for i, m := range snap.Members {
		// 第一位当工程根，其余当成员工艺
		if i == 0 {
			// 用成员自己的显示名，没有就保持空
			name = m.Name
		}
		// 把处理好的成员放回这一位
		members[i] = CachedMember{
			ID: m.ID, Kind: m.Kind, Level: m.Level, Name: m.Name, Status: m.Status, WeldKind: m.WeldKind,
			Revision: m.Revision, Digest: append([]byte(nil), m.Digest...),
			Deps: copyDeps(m.Deps), OwnerID: cloneUUID(m.CreatorID),
		}
	}
	return CachedClosure{
		AssetID: snap.AssetID, Revision: snap.Revision, Kind: snap.Kind, Level: snap.Level,
		Status: snap.Status, Name: name, Digest: append([]byte(nil), snap.Digest...),
		TargetClientID: cloneUUID(snap.TargetClientID), Members: members,
	}
}

// cloneCached 复制闭包元数据，避免调用方改袋内切片。
func cloneCached(c CachedClosure) CachedClosure {
	// 按条数决定是空、超限还是继续
	members := make([]CachedMember, len(c.Members))
	// 逐个成员解封或重封，缺一个就整包失败
	for i, m := range c.Members {
		// 把处理好的成员放回这一位
		members[i] = CachedMember{
			ID: m.ID, Kind: m.Kind, Level: m.Level, Name: m.Name, Status: m.Status, WeldKind: m.WeldKind,
			Revision: m.Revision, Digest: append([]byte(nil), m.Digest...),
			Deps: copyDeps(m.Deps), OwnerID: cloneUUID(m.OwnerID),
		}
	}
	return CachedClosure{
		AssetID: c.AssetID, Revision: c.Revision, Kind: c.Kind, Level: c.Level,
		Status: c.Status, Name: c.Name, Digest: append([]byte(nil), c.Digest...),
		TargetClientID: cloneUUID(c.TargetClientID), Members: members,
	}
}

// cloneUUID 复制可空身份。
func cloneUUID(p *uuid.UUID) *uuid.UUID {
	// 没有袋则无法复制身份，调用方得到空
	if p == nil {
		return nil
	}
	// 整袋复制一份再改，避免改到调用方手里的那只
	v := *p
	return &v
}
