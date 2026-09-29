package service

import (
	"bytes"
	"context"
	"errors"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/store"
)

// memberFromAsset 把本厂原件收成组包成员。
func memberFromAsset(a Asset) ClosureMember {
	// 创建人抄出来，组包成员要带上是谁做的。
	cid := a.CreatorID
	return ClosureMember{
		ID: a.ID, Kind: a.Kind, Level: a.Level, Name: a.Name, Code: a.Code, Status: a.Status,
		Copyable: a.Copyable, WeldKind: a.WeldKind, Revision: a.Revision, Content: a.Content, Digest: a.Digest,
		Deps: a.Deps, CreatorID: &cid,
	}
}

// memberFromReplica 把已收副本当成组包成员。
func memberFromReplica(r AssetReplica) ClosureMember {
	return ClosureMember{
		ID: r.ID, Kind: r.Kind, Level: r.Level, Name: r.Name, Code: r.Code, Status: r.Status,
		Copyable: r.Copyable, WeldKind: r.WeldKind, Revision: r.Revision, Content: r.Content, Digest: r.Digest, Deps: r.Deps,
	}
}

// sealClosure 组包并算整包摘要。
func sealClosure(kind string, root ClosureMember, rest []ClosureMember, facID, clientID *uuid.UUID) ClosureSnapshot {
	// 根加依赖的位置先留好。
	members := make([]ClosureMember, 0, 1+len(rest))
	// 根放在第一位，校验时就认这个。
	members = append(members, root)
	// 依赖成员跟在根后面，顺序和钉死一致。
	members = append(members, rest...)
	// 按包内顺序准备摘要材料。
	parts := make([]digest.Member, len(members))
	// 按成员准备摘要材料，顺序和包内一致。
	for i, m := range members {
		// 这一位的身份、修订、摘要和正文都拿去算整包。
		parts[i] = digest.Member{ID: m.ID, Revision: m.Revision, Digest: m.Digest, Content: m.Content}
	}
	return ClosureSnapshot{
		Kind: kind, AssetID: root.ID, Revision: root.Revision, Level: root.Level,
		Copyable: root.Copyable, Status: root.Status, TargetFactoryID: facID, TargetClientID: clientID,
		Members: members, Digest: digest.ClosureSum(parts),
	}
}

// validateClosure 核对成员摘要、钉死依赖和整包摘要。
func validateClosure(snap ClosureSnapshot) error {
	// 一个成员都没有则包不完整，直接拒绝。
	if len(snap.Members) == 0 {
		return domain.ErrClosureIncomplete
	}
	// 逐个核对成员正文和摘要，对不上整包作废。
	for _, m := range snap.Members {
		// 摘要对不上当被篡改，拒绝继续使用。
		if !digest.Match(m.Content, m.Digest) {
			return domain.ErrIntegrity
		}
	}
	// 按成员顺序重算整包摘要，用来对账。
	parts := make([]digest.Member, len(snap.Members))
	// 按包内顺序重算整包摘要，用来对账。
	for i, m := range snap.Members {
		// 这一位的身份、修订、摘要和正文都拿去算整包。
		parts[i] = digest.Member{ID: m.ID, Revision: m.Revision, Digest: m.Digest, Content: m.Content}
	}
	// 重算的整包摘要对不上则当被篡改。
	if !bytes.Equal(digest.ClosureSum(parts), snap.Digest) {
		return domain.ErrIntegrity
	}
	// 第一位是根，后面用它核身份和依赖。
	root := snap.Members[0]
	// 包头必须和第一位根的身份、修订、种类一致。
	if snap.AssetID != root.ID || snap.Revision != root.Revision || snap.Kind != root.Kind {
		return domain.ErrClosureMismatch
	}
	// 工艺包只能有根自己，不能再夹成员。
	if snap.Kind == KindProcess {
		// 工艺包多了成员就和单工艺对不上。
		if len(snap.Members) != 1 {
			return domain.ErrClosureMismatch
		}
		return nil
	}
	// 不是工程包，或根不是工程，则拒绝。
	if snap.Kind != KindProject || root.Kind != KindProject {
		return domain.ErrClosureMismatch
	}
	// 成员个数必须刚好是根加全部依赖。
	if len(snap.Members) != 1+len(root.Deps) {
		// 成员比依赖少则算没收齐。
		if len(snap.Members) < 1+len(root.Deps) {
			return domain.ErrClosureIncomplete
		}
		return domain.ErrClosureMismatch
	}
	// 下一位成员必须对上这条钉死的工艺。
	for i, d := range root.Deps {
		// 依赖的下一位必须对上这条钉死。
		m := snap.Members[i+1]
		// 身份或修订和钉死的不一致则整包作废。
		if m.ID != d.ID || m.Revision != d.Revision {
			return domain.ErrClosureMismatch
		}
		// 摘要和钉死的不一致则整包作废。
		if !bytes.Equal(m.Digest, d.Digest) {
			return domain.ErrClosureMismatch
		}
	}
	return nil
}

// loadDepMember 按钉死修订取工艺成员；原件没有再看副本。
func (s *Closure) loadDepMember(ctx context.Context, d AssetDep) (ClosureMember, error) {
	// 先看本厂原件，没有再按钉死修订取副本。
	a, err := s.store.GovernedAssetByID(ctx, d.ID)
	// 本厂原件在就收成成员，没有再按修订取副本。
	if err == nil {
		// 摘要对不上当被篡改，拒绝继续使用。
		if !digest.Match(a.Content, a.Digest) {
			return ClosureMember{}, domain.ErrIntegrity
		}
		// 依赖必须是工艺，工程不能嵌进包里。
		if a.Kind != KindProcess {
			return ClosureMember{}, domain.ErrClosureMismatch
		}
		// 原件修订和钉死的不一致则包没收齐。
		if a.Revision != d.Revision {
			return ClosureMember{}, domain.ErrClosureIncomplete
		}
		// 原件摘要和钉死的不一致则拒绝组包。
		if !bytes.Equal(a.Digest, d.Digest) {
			return ClosureMember{}, domain.ErrClosureMismatch
		}
		// 把本厂原件收成组包成员。
		return memberFromAsset(a), nil
	}
	// 不是找不到则把库错误抛回，避免当成没有。
	if !errors.Is(err, domain.ErrNotFound) {
		return ClosureMember{}, err
	}
	// 按钉死的修订取副本正文。
	r, err := s.store.ReplicaByIDRev(ctx, d.ID, d.Revision)
	// 这一修订没有或读失败则闭包不齐。
	if err != nil {
		// 找不到就换成业务上的缺失，不当成库故障。
		if errors.Is(err, domain.ErrNotFound) {
			return ClosureMember{}, domain.ErrClosureIncomplete
		}
		return ClosureMember{}, err
	}
	// 摘要对不上当被篡改，拒绝继续使用。
	if !digest.Match(r.Content, r.Digest) {
		return ClosureMember{}, domain.ErrIntegrity
	}
	// 副本必须是同一修订的工艺，否则对不上。
	if r.Kind != KindProcess || r.Revision != d.Revision {
		return ClosureMember{}, domain.ErrClosureMismatch
	}
	// 副本摘要和钉死的不一致则拒绝组包。
	if !bytes.Equal(r.Digest, d.Digest) {
		return ClosureMember{}, domain.ErrClosureMismatch
	}
	// 把已收副本收成组包成员。
	return memberFromReplica(r), nil
}

// packFromMember 根须可用，依赖按钉死修订收齐。
func (s *Closure) packFromMember(ctx context.Context, root ClosureMember) (ClosureSnapshot, error) {
	// 根不是可用则拒绝组包，草稿和停用都不行。
	if root.Status != AssetAvailable {
		return ClosureSnapshot{}, domain.ErrAssetNotAvailable
	}
	// 按依赖条数准备成员，收不齐就失败。
	rest := make([]ClosureMember, 0, len(root.Deps))
	// 按钉死修订把工艺收进包。
	for _, d := range root.Deps {
		// 按钉死的修订取工艺，原件没有再看副本。
		m, err := s.loadDepMember(ctx, d)
		// 这条依赖收不齐则整包失败。
		if err != nil {
			return ClosureSnapshot{}, err
		}
		// 这条工艺已按钉死修订收进包。
		rest = append(rest, m)
	}
	// 成员收齐后算整包摘要。
	snap := sealClosure(root.Kind, root, rest, nil, nil)
	// 闭包对不上则拒绝装入或激活。
	if err := validateClosure(snap); err != nil {
		return ClosureSnapshot{}, err
	}
	return snap, nil
}

// packForPad 平板登录与厂端列表同一范围：草稿也拉；工艺单独成包；工程明文只带根和工艺 Id。
func (s *Closure) packForPad(ctx context.Context, assetID uuid.UUID) (ClosureSnapshot, error) {
	// 组包根优先用本厂原件，否则用副本。
	root, err := s.loadRootForPack(ctx, assetID)
	// 根读不到则不能组包。
	if err != nil {
		return ClosureSnapshot{}, err
	}
	// 复制一份依赖，避免改到调用方手里的切片。
	root.Deps = copyDeps(root.Deps)
	// 工艺单独成包，不带工程依赖。
	if root.Kind == KindProcess {
		// 成员收齐后算整包摘要。
		snap := sealClosure(KindProcess, root, nil, nil, nil)
		// 闭包对不上则拒绝装入或激活。
		if err := validateClosure(snap); err != nil {
			return ClosureSnapshot{}, err
		}
		// 把当前目录挂点写进闭包，不参与摘要。
		s.attachFSPaths(ctx, &snap)
		return snap, nil
	}
	// 既不是工艺也不是工程则拒绝组给平板。
	if root.Kind != KindProject {
		return ClosureSnapshot{}, domain.ErrForbidden
	}
	// 工程不把工艺正文打进包，焊道只引用工艺 Id。
	parts := []digest.Member{{ID: root.ID, Revision: root.Revision, Digest: root.Digest, Content: root.Content}}
	// 工程包只带根，工艺正文不打进去。
	snap := ClosureSnapshot{
		Kind: KindProject, AssetID: root.ID, Revision: root.Revision, Level: root.Level,
		Copyable: root.Copyable, Status: root.Status, Members: []ClosureMember{root},
		Digest: digest.ClosureSum(parts),
	}
	// 把当前目录挂点写进闭包，不参与摘要。
	s.attachFSPaths(ctx, &snap)
	return snap, nil
}

// attachFSPaths 把当前目录挂点写进闭包，不进摘要。
func (s *Closure) attachFSPaths(ctx context.Context, snap *ClosureSnapshot) {
	// 逐个补目录挂点，读不到的跳过。
	for i := range snap.Members {
		// 读这个成员现在挂在哪条目录上。
		parent, folders, err := s.store.FSLineage(ctx, snap.Members[i].ID)
		// 这个成员的目录读不到就跳过，不挡住组包。
		if err != nil {
			continue
		}
		// 挂上当前父文件夹，这块不进摘要。
		snap.Members[i].FSParentID = parent
		// 把目录层级写上，激活时能对上位置。
		snap.Members[i].FSPath = folders
	}
}

// loadRootForPack 组包根：本厂原件优先，否则已收副本。
func (s *Closure) loadRootForPack(ctx context.Context, assetID uuid.UUID) (ClosureMember, error) {
	// 按身份读本厂原件正文。
	a, err := s.store.GovernedAssetByID(ctx, assetID)
	// 本厂原件在就用它当根，没有再看副本。
	if err == nil {
		// 摘要对不上当被篡改，拒绝继续使用。
		if !digest.Match(a.Content, a.Digest) {
			return ClosureMember{}, domain.ErrIntegrity
		}
		// 把本厂原件收成组包成员。
		return memberFromAsset(a), nil
	}
	// 不是找不到则把库错误抛回，避免当成没有。
	if !errors.Is(err, domain.ErrNotFound) {
		return ClosureMember{}, err
	}
	// 取该身份已收的最高修订副本。
	r, err := s.store.LatestReplica(ctx, assetID)
	// 副本读失败则不能当组包根。
	if err != nil {
		return ClosureMember{}, err
	}
	// 摘要对不上当被篡改，拒绝继续使用。
	if !digest.Match(r.Content, r.Digest) {
		return ClosureMember{}, domain.ErrIntegrity
	}
	// 把已收副本收成组包成员。
	return memberFromReplica(r), nil
}

// 记下这次组包的允许或拒绝。
func (s *Closure) auditPack(ctx context.Context, actor *uuid.UUID, snap ClosureSnapshot, result string) error {
	// 按这次结果记组包审计，写失败则不算组完。
	return s.audit(ctx, actor, nil, "pack_closure", closureTarget(snap), result)
}

// 审计对象：根、成员加目标厂或 Client。
func closureTarget(snap ClosureSnapshot) string {
	// 拼根的身份和修订，再附上其余成员。
	t := assetTarget(snap.AssetID, snap.Revision)
	// 根以外的成员也写进审计对象。
	for _, m := range snap.Members {
		// 根已经写过，成员里再碰到就跳过。
		if m.ID == snap.AssetID {
			continue
		}
		// 拼根的身份和修订，再附上其余成员。
		t += " " + assetTarget(m.ID, m.Revision)
	}
	// 有目标厂就写进审计，方便对上下发对象。
	if snap.TargetFactoryID != nil {
		// 有目标厂就写进审计，方便对上下发对象。
		t += " factory=" + snap.TargetFactoryID.String()
	}
	// 有目标终端就写进审计。
	if snap.TargetClientID != nil {
		// 有目标终端就写进审计。
		t += " client=" + snap.TargetClientID.String()
	}
	return t
}

// AssembleProject 从本厂当前行或已收副本组包；草稿/停用/缺失/串版拒绝。
func (s *Closure) AssembleProject(ctx context.Context, token string, assetID uuid.UUID) (ClosureSnapshot, error) {
	// 核对登录仍有效，后面的操作都靠这次会话。
	acc, err := s.RequireActive(ctx, token)
	// 登录已失效则拒绝，避免未登录的人继续。
	if err != nil {
		return ClosureSnapshot{}, err
	}
	// 有权才能组包。失败一律记拒绝。
	root, err := s.loadRootForPack(ctx, assetID)
	// 失败则记拒绝并停下，不继续往下改。
	if err != nil {
		// 组包或装袋被拒时补记审计。
		_ = s.audit(ctx, &acc.ID, nil, "pack_closure", assetID.String(), audit.Deny)
		return ClosureSnapshot{}, err
	}
	// 无权组包则记拒绝，不把包发出去。
	if err := s.canPack(ctx, acc, root); err != nil {
		// 组包或装袋被拒时补记审计。
		_ = s.audit(ctx, &acc.ID, nil, "pack_closure", assetTarget(root.ID, root.Revision), audit.Deny)
		return ClosureSnapshot{}, err
	}
	// 根须可用，依赖按钉死修订收齐再封包。
	snap, err := s.packFromMember(ctx, root)
	// 组包失败则记拒绝。
	if err != nil {
		// 组包或装袋被拒时补记审计。
		_ = s.audit(ctx, &acc.ID, nil, "pack_closure", assetTarget(root.ID, root.Revision), audit.Deny)
		return ClosureSnapshot{}, err
	}
	// 成功必须记上审计，没记上则本次不算做成。
	if err := s.auditPack(ctx, &acc.ID, snap, audit.Allow); err != nil {
		return ClosureSnapshot{}, err
	}
	return snap, nil
}

// canPack 个人级仅创建人；平台级须整厂作用域操作员；厂级看操作员作用域。
func (s *Closure) canPack(ctx context.Context, acc Account, root ClosureMember) error {
	// 个人级组包只允许创建人。
	if root.Level == AssetLevelPersonal {
		// 不是创建人则拒绝组这份个人级包。
		if root.CreatorID == nil || *root.CreatorID != acc.ID {
			return domain.ErrForbidden
		}
		return nil
	}
	// 平台级必须是整厂范围的操作员才能组。
	if root.Level == AssetLevelPlatform {
		// 没有整厂操作员资格则拒绝组平台级。
		if !s.isFactoryScopeOperator(ctx, acc) {
			return domain.ErrForbidden
		}
		return nil
	}
	// 厂级默认不挂节点，读到原件再填创建位置。
	var unit *uuid.UUID
	// 厂级按创建时节点看操作员作用域。
	a, err := s.store.GovernedAssetMetaByID(ctx, root.ID)
	// 厂级原件读到了就按其创建节点看作用域。
	if err == nil {
		// 按创建时的节点看操作员作用域。
		unit = a.OrgUnitID
	}
	// 按操作员作用域看是否盖住该节点。
	return s.opCovers(ctx, acc, unit)
}

// isFactoryScopeOperator 是否持有本厂作用域的操作员角色。
func (s *Closure) isFactoryScopeOperator(ctx context.Context, acc Account) bool {
	// 读出当前人的授权，再判断角色。
	grants, err := s.grantsOf(ctx, acc.ID)
	// 授权读不到则拒绝，不当成没权限放过。
	if err != nil {
		return false
	}
	// 只看操作员授权里有没有整厂范围。
	for _, g := range withRoles(grants, RoleOperator) {
		// 有整厂操作员就可以组平台级。
		if g.ScopeKind == ScopeFactory {
			return true
		}
	}
	return false
}

// putCached 校验后写入本机袋。
func (s *Closure) putCached(ctx context.Context, bag *Bag, snap ClosureSnapshot) error {
	// 闭包对不上则拒绝装入或激活。
	if err := validateClosure(snap); err != nil {
		return err
	}
	// 袋里只缓存工程，工艺包拒绝放入。
	if snap.Kind != KindProject {
		return domain.ErrForbidden
	}
	// 不是发给这台终端的包拒绝放进袋。
	if snap.TargetClientID == nil || *snap.TargetClientID != bag.ClientID {
		return domain.ErrForbidden
	}
	// 同身份覆盖旧缓存，没有就追加。
	bag.putClosure(snap)
	return nil
}

// findClosure 按工程身份取袋内缓存。
func (b *Bag) findClosure(id uuid.UUID) (ClosureSnapshot, bool) {
	// 按工程身份在袋里找那一份缓存。
	for _, c := range b.Closures {
		// 身份对上就取出这份缓存。
		if c.AssetID == id {
			return c, true
		}
	}
	return ClosureSnapshot{}, false
}

// putClosure 同身份覆盖，否则追加。
func (b *Bag) putClosure(snap ClosureSnapshot) {
	// 同身份就覆盖，避免袋里留两份。
	for i, c := range b.Closures {
		// 同身份用新包覆盖，避免袋里留两份。
		if c.AssetID == snap.AssetID {
			// 同身份用新包覆盖旧缓存。
			b.Closures[i] = snap
			return
		}
	}
	// 袋里没有同身份时追加这一份。
	b.Closures = append(b.Closures, snap)
}

// removeClosure 撤掉一份缓存，不管激活标记。
func (b *Bag) removeClosure(id uuid.UUID) {
	// 在原底层上重收，撤掉的那份不留下。
	out := b.Closures[:0]
	// 把要撤的那份拿掉，其余留在袋里。
	for _, c := range b.Closures {
		// 不是要撤的那份就留在袋里。
		if c.AssetID != id {
			// 不是要撤的那份就留在袋里。
			out = append(out, c)
		}
	}
	// 袋里只留没被撤掉的缓存。
	b.Closures = out
}

// projectCount 袋内不重复工程份数。
func (b *Bag) projectCount() int {
	// 记下已经数过或走过的身份，避免重复。
	seen := map[uuid.UUID]struct{}{}
	// 只数不重复的工程，工艺不计入。
	for _, c := range b.Closures {
		// 只数工程，工艺不计入袋里的份数。
		if c.Kind == KindProject {
			// 这份工程已经数过。
			seen[c.AssetID] = struct{}{}
		}
	}
	// 返回袋里不重复的工程份数。
	return len(seen)
}

// AcceptCachedClosure 夹具把已组好的工程闭包放进本机袋；成员不对则拒绝。
func (s *Closure) AcceptCachedClosure(ctx context.Context, bag *Bag, snap ClosureSnapshot) error {
	// 校验不过则记拒绝，袋里不放。
	if err := s.putCached(ctx, bag, snap); err != nil {
		// 组包或装袋被拒时补记审计。
		_ = s.audit(ctx, nil, nil, "pack_closure", closureTarget(snap), audit.Deny)
		return err
	}
	// 做成后记审计；没记上则调用方当失败。
	return s.audit(ctx, nil, nil, "cache_closure", closureTarget(snap), audit.Allow)
}

// UncacheProject 撤掉一份未激活缓存；焊接中或正在激活的拒绝。
func (s *Closure) UncacheProject(ctx context.Context, bag *Bag, projectID uuid.UUID) error {
	// 正在焊接则拒绝撤缓存，避免作业丢包。
	if bag.Welding {
		// 撤缓存被拒时补记审计，袋里先留着。
		_ = s.audit(ctx, nil, nil, "uncache_closure", projectID.String(), audit.Deny)
		return domain.ErrForbidden
	}
	// 正在激活的这份拒绝撤掉。
	if bag.ActiveID != nil && *bag.ActiveID == projectID {
		// 撤缓存被拒时补记审计，袋里先留着。
		_ = s.audit(ctx, nil, nil, "uncache_closure", projectID.String(), audit.Deny)
		return domain.ErrForbidden
	}
	// 袋里没有这份则按找不到拒绝。
	if _, ok := bag.findClosure(projectID); !ok {
		// 撤缓存被拒时补记审计，袋里先留着。
		_ = s.audit(ctx, nil, nil, "uncache_closure", projectID.String(), audit.Deny)
		return domain.ErrNotFound
	}
	// 撤掉这份缓存，不管它是否曾经激活。
	bag.removeClosure(projectID)
	// 做成后记审计；没记上则调用方当失败。
	return s.audit(ctx, nil, nil, "uncache_closure", projectID.String(), audit.Allow)
}

// ActivateProject 本机激活恰好一份已缓存工程；双重许可与完整性都过才允许。
func (s *Closure) ActivateProject(ctx context.Context, bag *Bag, clocks Clocks, projectID uuid.UUID) error {
	// 按工程身份在袋里找缓存。
	snap, ok := bag.findClosure(projectID)
	// 袋里没有这份工程则拒绝激活。
	if !ok {
		// 激活被拒时补记审计，不把工程标成在用。
		_ = s.auditTimed(ctx, nil, nil, "activate_closure", projectID.String(), audit.Deny, bagTimeSource(*bag))
		return domain.ErrNotFound
	}
	// 在线记服务器时间，离线记本机时间。
	src := bagTimeSource(*bag)
	// 闭包对不上则拒绝，并补记审计。
	if err := validateClosure(snap); err != nil {
		// 激活被拒时补记审计，不把工程标成在用。
		_ = s.auditTimed(ctx, personActor(bag), nil, "activate_closure", closureTarget(snap), audit.Deny, src)
		return err
	}
	// 不是发给这台的包拒绝激活。
	if snap.TargetClientID == nil || *snap.TargetClientID != bag.ClientID {
		// 激活被拒时补记审计，不把工程标成在用。
		_ = s.auditTimed(ctx, personActor(bag), nil, "activate_closure", closureTarget(snap), audit.Deny, src)
		return domain.ErrForbidden
	}
	// 按本机时钟和状态看现在允不允许操作。
	node := EvaluateRuntime(*bag, clocks, NodeOpen)
	// 运行状态不允许时拒绝激活。
	if node.Decision != NodeAllow {
		// 激活被拒时补记审计，不把工程标成在用。
		_ = s.auditTimed(ctx, personActor(bag), nil, "activate_closure", closureTarget(snap), audit.Deny, node.TimeSource)
		return domain.ErrForbidden
	}
	// 把袋里的登录者对上本厂账号。
	acc, err := s.operatorAccount(ctx, *bag)
	// 对不上本厂账号则拒绝，并补记审计。
	if err != nil {
		// 激活被拒时补记审计，不把工程标成在用。
		_ = s.auditTimed(ctx, personActor(bag), nil, "activate_closure", closureTarget(snap), audit.Deny, src)
		return domain.ErrForbidden
	}
	// 确认当前登录者还能当操作员。
	canOp, err := s.canOperateAs(ctx, acc.ID)
	// 查不到操作员资格或并不具备则拒绝激活。
	if err != nil || !canOp {
		// 激活被拒时补记审计，不把工程标成在用。
		_ = s.auditTimed(ctx, &acc.ID, nil, "activate_closure", closureTarget(snap), audit.Deny, src)
		return domain.ErrForbidden
	}
	// 第一位是根，后面用它核身份和依赖。
	root := snap.Members[0]
	// 个人级工程只允许创建人激活。
	if root.Level == AssetLevelPersonal {
		// 不是创建人则拒绝激活这份个人工程。
		if root.CreatorID == nil || *root.CreatorID != acc.ID {
			// 激活被拒时补记审计，不把工程标成在用。
			_ = s.auditTimed(ctx, &acc.ID, nil, "activate_closure", closureTarget(snap), audit.Deny, src)
			return domain.ErrForbidden
		}
	}
	// 焊接中不能换成另一份工程。
	if bag.Welding && bag.ActiveID != nil && *bag.ActiveID != projectID {
		// 激活被拒时补记审计，不把工程标成在用。
		_ = s.auditTimed(ctx, &acc.ID, nil, "activate_closure", closureTarget(snap), audit.Deny, src)
		return domain.ErrForbidden
	}
	// 连着厂网时还要核源仍可用，离线则看包内状态。
	if bag.Connected {
		// 源已不可用则拒绝，并补记审计。
		if err := s.assertSourceAvailable(ctx, root); err != nil {
			// 激活被拒时补记审计，不把工程标成在用。
			_ = s.auditTimed(ctx, &acc.ID, nil, "activate_closure", closureTarget(snap), audit.Deny, src)
			return err
		}
		// 连网时厂级和平台级还要有发给这台的授权。
		if root.Level != AssetLevelPersonal {
			// 连网时厂级须仍有有效下发授权。
			g, err := s.store.ClientGrant(ctx, projectID, bag.ClientID)
			// 没有有效下发授权则拒绝激活这份厂级工程。
			if err != nil || !g.Active {
				// 激活被拒时补记审计，不把工程标成在用。
				_ = s.auditTimed(ctx, &acc.ID, nil, "activate_closure", closureTarget(snap), audit.Deny, src)
				// 这一步失败则停下，避免带着残缺结果继续。
				if err != nil && !errors.Is(err, domain.ErrNotFound) {
					return err
				}
				return domain.ErrForbidden
			}
		}
		// 没连上网时，源不是可用则拒绝激活。
	} else if root.Status != AssetAvailable {
		// 激活被拒时补记审计，不把工程标成在用。
		_ = s.auditTimed(ctx, &acc.ID, nil, "activate_closure", closureTarget(snap), audit.Deny, src)
		return domain.ErrAssetNotAvailable
	}
	// 激活的是这份工程的身份。
	id := snap.AssetID
	// 标成当前激活，焊接时就用这一份。
	bag.ActiveID = &id
	// 记下激活时的修订，避免串到别的版。
	bag.ActiveRevision = snap.Revision
	// 做成后记审计；没记上则调用方当失败。
	return s.auditTimed(ctx, &acc.ID, nil, "activate_closure", closureTarget(snap), audit.Allow, src)
}

// assertSourceAvailable 连网时核对源仍可用；平台级看副本状态。
func (s *Closure) assertSourceAvailable(ctx context.Context, root ClosureMember) error {
	// 平台级看副本是否仍可用，不看本厂原件。
	if root.Level == AssetLevelPlatform {
		// 按钉死的修订取副本正文。
		r, err := s.store.ReplicaByIDRev(ctx, root.ID, root.Revision)
		// 这一修订没有或读失败则闭包不齐。
		if err != nil {
			return err
		}
		// 平台副本不是可用则拒绝激活。
		if r.Status != AssetAvailable {
			return domain.ErrAssetNotAvailable
		}
		return nil
	}
	// 读本厂原件并核对摘要，不符就当坏的。
	a, err := s.loadChecked(ctx, root.ID)
	// 原件读不到或摘要不符则拒绝。
	if err != nil {
		return err
	}
	// 本厂原件不是可用则拒绝激活。
	if a.Status != AssetAvailable {
		return domain.ErrAssetNotAvailable
	}
	return nil
}

// 在线用服务器时，离线用本机时。
func bagTimeSource(bag Bag) string {
	// 在线用服务器时间，离线改用本机时间。
	if bag.Connected {
		return audit.Server
	}
	return audit.Local
}

// 复制依赖切片，避免改到调用方。
func copyDeps(deps []AssetDep) []AssetDep {
	// 按原样复制依赖，避免改到调用方。
	out := make([]AssetDep, len(deps))
	// 拷走一份，后面改切片不会碰到原件。
	copy(out, deps)
	return out
}

// 把闭包成员收成身份、修订和摘要。
func recordMembers(snap ClosureSnapshot) []AssetDep {
	// 按成员数准备身份、修订和摘要。
	out := make([]AssetDep, 0, len(snap.Members))
	// 每个成员收成身份、修订和摘要。
	for _, m := range snap.Members {
		// 这个成员收成一条钉死依赖。
		out = append(out, AssetDep{ID: m.ID, Revision: m.Revision, Digest: m.Digest})
	}
	return out
}

// SetProjectDeps 显式改工程依赖并升高修订；钉死必须仍能读到。
func (s *Closure) SetProjectDeps(ctx context.Context, token string, assetID uuid.UUID, expected int64, deps []AssetDep) (Asset, error) {
	// 核对登录仍有效，后面的操作都靠这次会话。
	acc, err := s.RequireActive(ctx, token)
	// 登录已失效则拒绝，避免未登录的人继续。
	if err != nil {
		return Asset{}, err
	}
	// 只改工程依赖；停用拒绝，引用必须仍能读到。
	return s.mutateAsset(ctx, acc, assetID, expected, "update_asset", func(cur Asset) (store.AssetWrite, error) {
		// 只有工程能改依赖，工艺拒绝。
		if cur.Kind != KindProject {
			return store.AssetWrite{}, domain.ErrAssetDependency
		}
		// 停用的工程不能改依赖。
		if cur.Status == AssetDisabled {
			return store.AssetWrite{}, domain.ErrAssetNotAvailable
		}
		// 按身份重钉当前可用修订，跟上工艺升版。
		deps, err := s.resolveProjectDeps(ctx, acc, cur.Level, deps)
		// 有一条重钉失败则整份依赖不改。
		if err != nil {
			return store.AssetWrite{}, err
		}
		// 焊道类型对不上则拒绝。
		if err := s.assertDepsWeldKind(ctx, cur.WeldKind, deps); err != nil {
			return store.AssetWrite{}, err
		}
		// 改依赖后，当前模版下的旧引用仍须落在新 deps 里。
		opened, err := s.loadChecked(ctx, cur.ID)
		// 原件读不到或摘要不符则拒绝。
		if err != nil {
			return store.AssetWrite{}, err
		}
		// 有引用对不上依赖则拒绝保存。
		if err := s.assertProjectProcessIDs(ctx, opened.Content, deps); err != nil {
			return store.AssetWrite{}, err
		}
		// 拼出本次要写入的内容，没通过的字段不改。
		return store.AssetWrite{Name: cur.Name, Digest: cur.Digest, Copyable: cur.Copyable, Status: cur.Status, Deps: copyDeps(deps), KeepContent: true}, nil
	})
}
