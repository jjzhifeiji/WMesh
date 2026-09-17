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
	p.clientID = clientID
}

// SetPolicy 记下缓存上限和 scope；current 且已激活则丢掉其他工程份。
func (p *Pouch) SetPolicy(maxCached int, scope string) {
	if maxCached < 1 {
		maxCached = 2
	}
	p.maxCached = maxCached
	if scope != store.CacheScopeCurrent {
		scope = store.CacheScopeAll
	}
	p.cacheScope = scope
	p.pruneToActive()
}

// SetOnline 标记能否向厂端拉包；离线且袋内无该工程则激活拒绝。
func (p *Pouch) SetOnline(online bool) {
	p.online = online
}

// SetWelding 标记焊接中；焊接中不得切换激活、不得撤当前份。
func (p *Pouch) SetWelding(welding bool) {
	p.welding = welding
}

// HasClosure 袋内是否已有该工程闭包元数据。
func (p *Pouch) HasClosure(id uuid.UUID) bool {
	_, ok := p.closures[id]
	return ok
}

// ActiveProject 当前激活工程；未激活为空。
func (p *Pouch) ActiveProject() *uuid.UUID {
	return cloneUUID(p.activeID)
}

// ActiveRevision 当前激活修订；无激活为 0。
func (p *Pouch) ActiveRevision() int64 {
	return p.activeRev
}

// ExportClosures 给出闭包元数据副本，不含明文。
func (p *Pouch) ExportClosures() []CachedClosure {
	out := make([]CachedClosure, 0, len(p.closures))
	for _, c := range p.closures {
		out = append(out, cloneCached(c))
	}
	return out
}

// RestoreClosures 从磁盘元数据恢复闭包分组，不解正文。
func (p *Pouch) RestoreClosures(list []CachedClosure) {
	p.closures = map[uuid.UUID]CachedClosure{}
	for _, c := range list {
		p.closures[c.AssetID] = cloneCached(c)
	}
}

// RestoreActive 恢复上次激活标记；袋内没有该闭包则忽略。
func (p *Pouch) RestoreActive(id *uuid.UUID, rev int64) {
	if id == nil {
		p.activeID = nil
		p.activeRev = 0
		return
	}
	if _, ok := p.closures[*id]; !ok {
		p.activeID = nil
		p.activeRev = 0
		return
	}
	p.activeID = cloneUUID(id)
	p.activeRev = rev
}

// CacheClosure 校验完整闭包后重封进袋；新工程份超上限拒绝，已有份整份不写。
func (p *Pouch) CacheClosure(snap ClosureSnapshot) error {
	if !p.loggedIn() {
		return domain.ErrUnauthorized
	}
	if err := validateClosure(snap); err != nil {
		return err
	}
	if snap.Kind != KindProject {
		return domain.ErrForbidden
	}
	if snap.TargetClientID == nil || p.clientID == uuid.Nil || *snap.TargetClientID != p.clientID {
		return domain.ErrForbidden
	}
	if cur, ok := p.closures[snap.AssetID]; ok && cur.Revision >= snap.Revision {
		return nil
	}
	old := p.closures[snap.AssetID]
	for _, m := range snap.Members {
		if err := p.PutPlain(m.ID, m.Level, m.Name, m.Revision, m.CreatorID, m.Content); err != nil {
			return err
		}
	}
	p.closures[snap.AssetID] = metaFromSnap(snap)
	p.dropUnreferenced(old.Members)
	return nil
}

// Activate 本机选定恰好一份已缓存工程；缺成员、串版、只拷、离线无信封、焊接中换份都拒绝。
func (p *Pouch) Activate(projectID uuid.UUID) error {
	if !p.loggedIn() {
		return domain.ErrUnauthorized
	}
	if _, ok := p.closures[projectID]; !ok {
		if p.cacheScope == store.CacheScopeCurrent && !p.online {
			return domain.ErrNotFound
		}
		return domain.ErrNotFound
	}
	if p.welding && p.activeID != nil && *p.activeID != projectID {
		return domain.ErrForbidden
	}
	snap, err := p.snapshotFromDisk(projectID)
	if err != nil {
		return err
	}
	if err := validateClosure(snap); err != nil {
		return err
	}
	if snap.TargetClientID == nil || p.clientID == uuid.Nil || *snap.TargetClientID != p.clientID {
		return domain.ErrForbidden
	}
	root := snap.Members[0]
	if root.Level == AssetLevelPersonal {
		if root.CreatorID == nil || p.person == nil || *root.CreatorID != *p.person {
			return domain.ErrForbidden
		}
	}
	if root.Status != AssetAvailable {
		return domain.ErrAssetNotAvailable
	}
	id := snap.AssetID
	p.activeID = &id
	p.activeRev = snap.Revision
	p.pruneToActive()
	return nil
}

// OpenProcess 只从当前激活闭包的成员工艺按 processId 解明文。
func (p *Pouch) OpenProcess(processID uuid.UUID) ([]byte, error) {
	if !p.loggedIn() {
		return nil, domain.ErrUnauthorized
	}
	if p.activeID == nil {
		return nil, domain.ErrNotFound
	}
	cl, ok := p.closures[*p.activeID]
	if !ok {
		return nil, domain.ErrNotFound
	}
	var member *CachedMember
	for i := range cl.Members {
		m := &cl.Members[i]
		if m.ID == processID {
			member = m
			break
		}
	}
	if member == nil || member.Kind != KindProcess {
		return nil, domain.ErrNotFound
	}
	return p.Open(processID)
}

// Uncache 撤掉一份未激活缓存；焊接中或正在激活的拒绝。
func (p *Pouch) Uncache(projectID uuid.UUID) error {
	if !p.loggedIn() {
		return domain.ErrUnauthorized
	}
	if p.welding {
		return domain.ErrForbidden
	}
	if p.activeID != nil && *p.activeID == projectID {
		return domain.ErrForbidden
	}
	old, ok := p.closures[projectID]
	if !ok {
		return domain.ErrNotFound
	}
	delete(p.closures, projectID)
	p.dropUnreferenced(old.Members)
	return nil
}

// loggedIn 是否已把解封钥放进内存。
func (p *Pouch) loggedIn() bool {
	return p.HasUnwrapKey() && p.person != nil
}

// projectCount 袋内不重复工程份数。
func (p *Pouch) projectCount() int {
	n := 0
	for _, c := range p.closures {
		if c.Kind == KindProject {
			n++
		}
	}
	return n
}

// pruneToActive current 时只留当前激活工程及其成员工艺。
func (p *Pouch) pruneToActive() {
	if p.cacheScope != store.CacheScopeCurrent || p.activeID == nil {
		return
	}
	keep := *p.activeID
	for id, cl := range p.closures {
		if id == keep {
			continue
		}
		delete(p.closures, id)
		p.dropUnreferenced(cl.Members)
	}
}

// dropUnreferenced 撤掉不再被任何闭包引用的信封。
func (p *Pouch) dropUnreferenced(old []CachedMember) {
	ref := p.referencedIDs()
	for _, m := range old {
		if _, ok := ref[m.ID]; !ok {
			delete(p.items, m.ID)
		}
	}
}

// referencedIDs 仍被闭包引用的资产。
func (p *Pouch) referencedIDs() map[uuid.UUID]struct{} {
	out := map[uuid.UUID]struct{}{}
	for _, c := range p.closures {
		for _, m := range c.Members {
			out[m.ID] = struct{}{}
		}
	}
	return out
}

// snapshotFromDisk 解开成员后按元数据重组成快照，缺信封当缺成员。
func (p *Pouch) snapshotFromDisk(id uuid.UUID) (ClosureSnapshot, error) {
	meta, ok := p.closures[id]
	if !ok {
		return ClosureSnapshot{}, domain.ErrNotFound
	}
	members := make([]ClosureMember, len(meta.Members))
	for i, m := range meta.Members {
		plain, err := p.Open(m.ID)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return ClosureSnapshot{}, domain.ErrClosureIncomplete
			}
			return ClosureSnapshot{}, err
		}
		members[i] = ClosureMember{
			ID: m.ID, Kind: m.Kind, Level: m.Level, Name: m.Name, Status: m.Status,
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
	members := make([]CachedMember, len(snap.Members))
	name := ""
	for i, m := range snap.Members {
		if i == 0 {
			name = m.Name
		}
		members[i] = CachedMember{
			ID: m.ID, Kind: m.Kind, Level: m.Level, Name: m.Name, Status: m.Status,
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
	members := make([]CachedMember, len(c.Members))
	for i, m := range c.Members {
		members[i] = CachedMember{
			ID: m.ID, Kind: m.Kind, Level: m.Level, Name: m.Name, Status: m.Status,
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
	if p == nil {
		return nil
	}
	v := *p
	return &v
}
