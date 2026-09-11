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

func memberFromAsset(a Asset) ClosureMember {
	cid := a.CreatorID
	return ClosureMember{
		ID: a.ID, Kind: a.Kind, Level: a.Level, Name: a.Name, Status: a.Status,
		Copyable: a.Copyable, Revision: a.Revision, Content: a.Content, Digest: a.Digest,
		Deps: a.Deps, CreatorID: &cid,
	}
}

func memberFromReplica(r AssetReplica) ClosureMember {
	return ClosureMember{
		ID: r.ID, Kind: r.Kind, Level: r.Level, Name: r.Name, Status: r.Status,
		Copyable: r.Copyable, Revision: r.Revision, Content: r.Content, Digest: r.Digest, Deps: r.Deps,
	}
}

func sealClosure(kind string, root ClosureMember, rest []ClosureMember, facID, clientID *uuid.UUID) ClosureSnapshot {
	members := make([]ClosureMember, 0, 1+len(rest))
	members = append(members, root)
	members = append(members, rest...)
	parts := make([]digest.Member, len(members))
	for i, m := range members {
		parts[i] = digest.Member{ID: m.ID, Revision: m.Revision, Digest: m.Digest, Content: m.Content}
	}
	return ClosureSnapshot{
		Kind: kind, AssetID: root.ID, Revision: root.Revision, Level: root.Level,
		Copyable: root.Copyable, Status: root.Status, TargetFactoryID: facID, TargetClientID: clientID,
		Members: members, Digest: digest.ClosureSum(parts),
	}
}

func validateClosure(snap ClosureSnapshot) error {
	if len(snap.Members) == 0 {
		return domain.ErrClosureIncomplete
	}
	for _, m := range snap.Members {
		if !digest.Match(m.Content, m.Digest) {
			return domain.ErrIntegrity
		}
	}
	parts := make([]digest.Member, len(snap.Members))
	for i, m := range snap.Members {
		parts[i] = digest.Member{ID: m.ID, Revision: m.Revision, Digest: m.Digest, Content: m.Content}
	}
	if !bytes.Equal(digest.ClosureSum(parts), snap.Digest) {
		return domain.ErrIntegrity
	}
	root := snap.Members[0]
	if snap.AssetID != root.ID || snap.Revision != root.Revision || snap.Kind != root.Kind {
		return domain.ErrClosureMismatch
	}
	if snap.Kind == KindProcess {
		if len(snap.Members) != 1 {
			return domain.ErrClosureMismatch
		}
		return nil
	}
	if snap.Kind != KindProject || root.Kind != KindProject {
		return domain.ErrClosureMismatch
	}
	if len(snap.Members) != 1+len(root.Deps) {
		if len(snap.Members) < 1+len(root.Deps) {
			return domain.ErrClosureIncomplete
		}
		return domain.ErrClosureMismatch
	}
	for i, d := range root.Deps {
		m := snap.Members[i+1]
		if m.ID != d.ID || m.Revision != d.Revision {
			return domain.ErrClosureMismatch
		}
		if !bytes.Equal(m.Digest, d.Digest) {
			return domain.ErrClosureMismatch
		}
	}
	return nil
}

func (s *Closure) loadDepMember(ctx context.Context, d AssetDep) (ClosureMember, error) {
	a, err := s.store.GovernedAssetByID(ctx, d.ID)
	if err == nil {
		if !digest.Match(a.Content, a.Digest) {
			return ClosureMember{}, domain.ErrIntegrity
		}
		if a.Kind != KindProcess {
			return ClosureMember{}, domain.ErrClosureMismatch
		}
		if a.Revision != d.Revision {
			return ClosureMember{}, domain.ErrClosureIncomplete
		}
		if !bytes.Equal(a.Digest, d.Digest) {
			return ClosureMember{}, domain.ErrClosureMismatch
		}
		return memberFromAsset(a), nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return ClosureMember{}, err
	}
	r, err := s.store.ReplicaByIDRev(ctx, d.ID, d.Revision)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return ClosureMember{}, domain.ErrClosureIncomplete
		}
		return ClosureMember{}, err
	}
	if !digest.Match(r.Content, r.Digest) {
		return ClosureMember{}, domain.ErrIntegrity
	}
	if r.Kind != KindProcess || r.Revision != d.Revision {
		return ClosureMember{}, domain.ErrClosureMismatch
	}
	if !bytes.Equal(r.Digest, d.Digest) {
		return ClosureMember{}, domain.ErrClosureMismatch
	}
	return memberFromReplica(r), nil
}

func (s *Closure) packFromMember(ctx context.Context, root ClosureMember) (ClosureSnapshot, error) {
	if root.Status != AssetAvailable {
		return ClosureSnapshot{}, domain.ErrAssetNotAvailable
	}
	rest := make([]ClosureMember, 0, len(root.Deps))
	for _, d := range root.Deps {
		m, err := s.loadDepMember(ctx, d)
		if err != nil {
			return ClosureSnapshot{}, err
		}
		rest = append(rest, m)
	}
	snap := sealClosure(root.Kind, root, rest, nil, nil)
	if err := validateClosure(snap); err != nil {
		return ClosureSnapshot{}, err
	}
	return snap, nil
}

func (s *Closure) loadRootForPack(ctx context.Context, assetID uuid.UUID) (ClosureMember, error) {
	a, err := s.store.GovernedAssetByID(ctx, assetID)
	if err == nil {
		if !digest.Match(a.Content, a.Digest) {
			return ClosureMember{}, domain.ErrIntegrity
		}
		return memberFromAsset(a), nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return ClosureMember{}, err
	}
	r, err := s.store.LatestReplica(ctx, assetID)
	if err != nil {
		return ClosureMember{}, err
	}
	if !digest.Match(r.Content, r.Digest) {
		return ClosureMember{}, domain.ErrIntegrity
	}
	return memberFromReplica(r), nil
}

func (s *Closure) auditPack(ctx context.Context, actor *uuid.UUID, snap ClosureSnapshot, result string) error {
	return s.audit(ctx, actor, nil, "pack_closure", closureTarget(snap), result)
}

func closureTarget(snap ClosureSnapshot) string {
	t := assetTarget(snap.AssetID, snap.Revision)
	for _, m := range snap.Members {
		if m.ID == snap.AssetID {
			continue
		}
		t += " " + assetTarget(m.ID, m.Revision)
	}
	if snap.TargetFactoryID != nil {
		t += " factory=" + snap.TargetFactoryID.String()
	}
	if snap.TargetClientID != nil {
		t += " client=" + snap.TargetClientID.String()
	}
	return t
}

// AssembleProject 从本厂当前行或已收副本组包；草稿/停用/缺失/串版拒绝。
func (s *Closure) AssembleProject(ctx context.Context, token string, assetID uuid.UUID) (ClosureSnapshot, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return ClosureSnapshot{}, err
	}
	root, err := s.loadRootForPack(ctx, assetID)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "pack_closure", assetID.String(), audit.Deny)
		return ClosureSnapshot{}, err
	}
	if err := s.canPack(ctx, acc, root); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "pack_closure", assetTarget(root.ID, root.Revision), audit.Deny)
		return ClosureSnapshot{}, err
	}
	snap, err := s.packFromMember(ctx, root)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "pack_closure", assetTarget(root.ID, root.Revision), audit.Deny)
		return ClosureSnapshot{}, err
	}
	if err := s.auditPack(ctx, &acc.ID, snap, audit.Allow); err != nil {
		return ClosureSnapshot{}, err
	}
	return snap, nil
}

func (s *Closure) canPack(ctx context.Context, acc Account, root ClosureMember) error {
	if root.Level == AssetLevelPersonal {
		if root.CreatorID == nil || *root.CreatorID != acc.ID {
			return domain.ErrForbidden
		}
		return nil
	}
	if root.Level == AssetLevelPlatform {
		if !s.isFactoryScopePE(ctx, acc) {
			return domain.ErrForbidden
		}
		return nil
	}
	var unit *uuid.UUID
	a, err := s.store.GovernedAssetByID(ctx, root.ID)
	if err == nil {
		unit = a.OrgUnitID
	}
	return s.canAuthorFactory(ctx, acc, unit)
}

func (s *Closure) isFactoryScopePE(ctx context.Context, acc Account) bool {
	grants, err := s.grantsOf(ctx, acc.ID)
	if err != nil {
		return false
	}
	for _, g := range withRoles(grants, RoleProcessEngineer) {
		if g.ScopeKind == ScopeFactory {
			return true
		}
	}
	return false
}

func (s *Closure) putCached(ctx context.Context, bag *Bag, snap ClosureSnapshot) error {
	if err := validateClosure(snap); err != nil {
		return err
	}
	if snap.Kind != KindProject {
		return domain.ErrForbidden
	}
	if snap.TargetClientID == nil || *snap.TargetClientID != bag.ClientID {
		return domain.ErrForbidden
	}
	limit, err := s.store.CacheLimit(ctx)
	if err != nil {
		return err
	}
	if _, ok := bag.findClosure(snap.AssetID); !ok && bag.projectCount() >= limit {
		return domain.ErrClientCacheFull
	}
	bag.putClosure(snap)
	return nil
}

func (b *Bag) findClosure(id uuid.UUID) (ClosureSnapshot, bool) {
	for _, c := range b.Closures {
		if c.AssetID == id {
			return c, true
		}
	}
	return ClosureSnapshot{}, false
}

func (b *Bag) putClosure(snap ClosureSnapshot) {
	for i, c := range b.Closures {
		if c.AssetID == snap.AssetID {
			b.Closures[i] = snap
			return
		}
	}
	b.Closures = append(b.Closures, snap)
}

func (b *Bag) removeClosure(id uuid.UUID) {
	out := b.Closures[:0]
	for _, c := range b.Closures {
		if c.AssetID != id {
			out = append(out, c)
		}
	}
	b.Closures = out
}

func (b *Bag) projectCount() int {
	seen := map[uuid.UUID]struct{}{}
	for _, c := range b.Closures {
		if c.Kind == KindProject {
			seen[c.AssetID] = struct{}{}
		}
	}
	return len(seen)
}

// AcceptCachedClosure 夹具把已组好的工程闭包放进本机袋；成员不对则拒绝。
func (s *Closure) AcceptCachedClosure(ctx context.Context, bag *Bag, snap ClosureSnapshot) error {
	if err := s.putCached(ctx, bag, snap); err != nil {
		_ = s.audit(ctx, nil, nil, "pack_closure", closureTarget(snap), audit.Deny)
		return err
	}
	return s.audit(ctx, nil, nil, "cache_closure", closureTarget(snap), audit.Allow)
}

// UncacheProject 撤掉一份未激活缓存；焊接中或正在激活的拒绝。
func (s *Closure) UncacheProject(ctx context.Context, bag *Bag, projectID uuid.UUID) error {
	if bag.Welding {
		_ = s.audit(ctx, nil, nil, "uncache_closure", projectID.String(), audit.Deny)
		return domain.ErrForbidden
	}
	if bag.ActiveID != nil && *bag.ActiveID == projectID {
		_ = s.audit(ctx, nil, nil, "uncache_closure", projectID.String(), audit.Deny)
		return domain.ErrForbidden
	}
	if _, ok := bag.findClosure(projectID); !ok {
		_ = s.audit(ctx, nil, nil, "uncache_closure", projectID.String(), audit.Deny)
		return domain.ErrNotFound
	}
	bag.removeClosure(projectID)
	return s.audit(ctx, nil, nil, "uncache_closure", projectID.String(), audit.Allow)
}

// ActivateProject 本机激活恰好一份已缓存工程；双重许可与完整性都过才允许。
func (s *Closure) ActivateProject(ctx context.Context, bag *Bag, clocks Clocks, projectID uuid.UUID) error {
	snap, ok := bag.findClosure(projectID)
	if !ok {
		_ = s.auditTimed(ctx, nil, nil, "activate_closure", projectID.String(), audit.Deny, bagTimeSource(*bag))
		return domain.ErrNotFound
	}
	src := bagTimeSource(*bag)
	if err := validateClosure(snap); err != nil {
		_ = s.auditTimed(ctx, personActor(bag), nil, "activate_closure", closureTarget(snap), audit.Deny, src)
		return err
	}
	if snap.TargetClientID == nil || *snap.TargetClientID != bag.ClientID {
		_ = s.auditTimed(ctx, personActor(bag), nil, "activate_closure", closureTarget(snap), audit.Deny, src)
		return domain.ErrForbidden
	}
	node := EvaluateRuntime(*bag, clocks, NodeOpen)
	if node.Decision != NodeAllow {
		_ = s.auditTimed(ctx, personActor(bag), nil, "activate_closure", closureTarget(snap), audit.Deny, node.TimeSource)
		return domain.ErrForbidden
	}
	cred, ok := currentPerson(*bag)
	now, src := bagNow(*bag, clocks)
	if !ok || !cred.Active || now.Before(cred.NotBefore) || now.After(cred.NotAfter) || !personCanOperate(cred.RolesSnapshot) {
		_ = s.auditTimed(ctx, personActor(bag), nil, "activate_closure", closureTarget(snap), audit.Deny, src)
		return domain.ErrForbidden
	}
	root := snap.Members[0]
	if root.Level == AssetLevelPersonal {
		if root.CreatorID == nil || *root.CreatorID != cred.PersonID {
			_ = s.auditTimed(ctx, &cred.PersonID, nil, "activate_closure", closureTarget(snap), audit.Deny, src)
			return domain.ErrForbidden
		}
	}
	if bag.Welding && bag.ActiveID != nil && *bag.ActiveID != projectID {
		_ = s.auditTimed(ctx, &cred.PersonID, nil, "activate_closure", closureTarget(snap), audit.Deny, src)
		return domain.ErrForbidden
	}
	if bag.Connected {
		if err := s.assertSourceAvailable(ctx, root); err != nil {
			_ = s.auditTimed(ctx, &cred.PersonID, nil, "activate_closure", closureTarget(snap), audit.Deny, src)
			return err
		}
		if root.Level != AssetLevelPersonal {
			g, err := s.store.ClientGrant(ctx, projectID, bag.ClientID)
			if err != nil || !g.Active {
				_ = s.auditTimed(ctx, &cred.PersonID, nil, "activate_closure", closureTarget(snap), audit.Deny, src)
				if err != nil && !errors.Is(err, domain.ErrNotFound) {
					return err
				}
				return domain.ErrForbidden
			}
		}
	} else if root.Status != AssetAvailable {
		_ = s.auditTimed(ctx, &cred.PersonID, nil, "activate_closure", closureTarget(snap), audit.Deny, src)
		return domain.ErrAssetNotAvailable
	}
	id := snap.AssetID
	bag.ActiveID = &id
	bag.ActiveRevision = snap.Revision
	return s.auditTimed(ctx, &cred.PersonID, nil, "activate_closure", closureTarget(snap), audit.Allow, src)
}

func (s *Closure) assertSourceAvailable(ctx context.Context, root ClosureMember) error {
	if root.Level == AssetLevelPlatform {
		r, err := s.store.ReplicaByIDRev(ctx, root.ID, root.Revision)
		if err != nil {
			return err
		}
		if r.Status != AssetAvailable {
			return domain.ErrAssetNotAvailable
		}
		return nil
	}
	a, err := s.loadChecked(ctx, root.ID)
	if err != nil {
		return err
	}
	if a.Status != AssetAvailable {
		return domain.ErrAssetNotAvailable
	}
	return nil
}

func bagTimeSource(bag Bag) string {
	if bag.Connected {
		return audit.Server
	}
	return audit.Local
}

func personActor(bag *Bag) *uuid.UUID {
	if bag.Person == nil {
		return nil
	}
	id := bag.Person.PersonID
	return &id
}

func copyDeps(deps []AssetDep) []AssetDep {
	out := make([]AssetDep, len(deps))
	copy(out, deps)
	return out
}

func recordMembers(snap ClosureSnapshot) []AssetDep {
	out := make([]AssetDep, 0, len(snap.Members))
	for _, m := range snap.Members {
		out = append(out, AssetDep{ID: m.ID, Revision: m.Revision, Digest: m.Digest})
	}
	return out
}

// SetProjectDeps 显式改工程依赖并升高修订；钉死必须仍能读到。
func (s *Closure) SetProjectDeps(ctx context.Context, token string, assetID uuid.UUID, expected int64, deps []AssetDep) (Asset, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return Asset{}, err
	}
	return s.mutateAsset(ctx, acc, assetID, expected, "update_asset", func(cur Asset) (store.AssetWrite, error) {
		if cur.Kind != KindProject {
			return store.AssetWrite{}, domain.ErrAssetDependency
		}
		if cur.Status == AssetDisabled {
			return store.AssetWrite{}, domain.ErrAssetNotAvailable
		}
		if cur.Level == AssetLevelPersonal {
			if err := s.assertPersonalProjectDeps(ctx, acc, deps); err != nil {
				return store.AssetWrite{}, err
			}
		} else if err := s.assertFactoryProcessDeps(ctx, deps); err != nil {
			return store.AssetWrite{}, err
		}
		return store.AssetWrite{Name: cur.Name, Content: cur.Content, Digest: cur.Digest, Copyable: cur.Copyable, Status: cur.Status, Deps: copyDeps(deps)}, nil
	})
}
