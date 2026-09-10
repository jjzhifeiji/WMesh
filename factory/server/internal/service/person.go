package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/nodekey"
)

// IssuePersonOfflineGrant 由本厂有效超管给本厂有效账号签发绑定某 Client 的人员离线授权。
func (s *Service) IssuePersonOfflineGrant(ctx context.Context, token string, personID, clientID uuid.UUID, notBefore, notAfter time.Time) (PersonCred, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return PersonCred{}, err
	}
	target := personID.String() + " " + clientID.String()
	if err := s.can(ctx, acc, permManageAccount, nil); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "issue_person_offline", target, audit.Deny)
		return PersonCred{}, err
	}
	p, err := s.store.PersonByID(ctx, personID)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "issue_person_offline", target, audit.Deny)
		return PersonCred{}, err
	}
	if p.Status == StatusPending {
		_ = s.audit(ctx, &acc.ID, nil, "issue_person_offline", target, audit.Deny)
		return PersonCred{}, domain.ErrAccountPending
	}
	if p.Status != StatusActive || p.PasswordHash == nil {
		_ = s.audit(ctx, &acc.ID, nil, "issue_person_offline", target, audit.Deny)
		return PersonCred{}, domain.ErrAccountDisabled
	}
	cl, err := s.store.ClientByID(ctx, clientID)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "issue_person_offline", target, audit.Deny)
		return PersonCred{}, err
	}
	if cl.Status != ClientStatusBound {
		_ = s.audit(ctx, &acc.ID, nil, "issue_person_offline", target, audit.Deny)
		return PersonCred{}, domain.ErrBindingVoid
	}
	key, err := s.ensureSigningKey(ctx)
	if err != nil {
		return PersonCred{}, err
	}
	roles, err := s.store.ActiveGrants(ctx, personID)
	if err != nil {
		return PersonCred{}, err
	}
	snap := make([]RoleSnapshot, 0, len(roles))
	allowDirect := false
	for _, g := range roles {
		snap = append(snap, RoleSnapshot{Role: g.Role, ScopeKind: g.ScopeKind, OrgUnitID: g.OrgUnitID})
		if g.ScopeKind == ScopeFactory {
			allowDirect = true
		}
	}
	assigns, err := s.store.ActiveAssignments(ctx, personID)
	if err != nil {
		return PersonCred{}, err
	}
	orgs := make([]OrgOption, 0, len(assigns))
	for _, a := range assigns {
		path, err := s.store.PathSnapshot(ctx, a.OrgUnitID)
		if err != nil {
			return PersonCred{}, err
		}
		orgs = append(orgs, OrgOption{OrgUnitID: a.OrgUnitID, Path: path})
	}
	rev := int64(1)
	if latest, err := s.store.LatestPersonOfflineGrant(ctx, personID, clientID); err == nil {
		rev = latest.Revision + 1
	} else if !errors.Is(err, domain.ErrNotFound) {
		return PersonCred{}, err
	}
	cred := PersonCred{
		FactoryID:     s.store.FactoryID(),
		ClientID:      clientID,
		ClientPublic:  cl.PublicKey,
		PersonID:      personID,
		LoginName:     p.LoginName,
		PasswordHash:  *p.PasswordHash,
		AllowDirect:   allowDirect,
		OrgSnapshot:   orgs,
		RolesSnapshot: snap,
		NotBefore:     notBefore.UTC(),
		NotAfter:      notAfter.UTC(),
		Revision:      rev,
	}
	payload, err := encodePerson(cred)
	if err != nil {
		return PersonCred{}, err
	}
	cred.Payload = payload
	cred.Signature = nodekey.Sign(key.PrivateKey, payload)
	if _, err := s.store.InsertPersonOfflineGrant(ctx, PersonOfflineGrant{
		PersonID:      personID,
		ClientID:      clientID,
		Revision:      rev,
		LoginName:     cred.LoginName,
		PasswordHash:  cred.PasswordHash,
		AllowDirect:   cred.AllowDirect,
		OrgSnapshot:   cred.OrgSnapshot,
		RolesSnapshot: cred.RolesSnapshot,
		NotBefore:     cred.NotBefore,
		NotAfter:      cred.NotAfter,
		Payload:       payload,
		Signature:     cred.Signature,
	}); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "issue_person_offline", target, audit.Deny)
		return PersonCred{}, err
	}
	if err := s.audit(ctx, &acc.ID, nil, "issue_person_offline", target, audit.Allow); err != nil {
		return PersonCred{}, err
	}
	return cred, nil
}

// LoginOffline 只凭本机袋核验人员授权与口令；缺一不可。
func (s *Service) LoginOffline(ctx context.Context, bag Bag, clocks Clocks, loginName, password string) (NodeEval, error) {
	ev := LoginOfflineEval(bag, clocks, loginName, password)
	var actor *uuid.UUID
	if bag.Person != nil {
		id := bag.Person.PersonID
		actor = &id
	}
	result := audit.Deny
	if ev.Decision == NodeAllow {
		result = audit.Allow
	}
	if err := s.auditTimed(ctx, actor, &loginName, "person_login", bag.ClientID.String(), result, ev.TimeSource); err != nil {
		return ev, err
	}
	return ev, nil
}

// EvaluateOfflineOp 离线新开受保护操作：人员、节点、资产桩都允许才允许。
func (s *Service) EvaluateOfflineOp(ctx context.Context, bag Bag, clocks Clocks, loginName, password string) (NodeEval, error) {
	ev := EvaluateOffline(bag, clocks, loginName, password)
	var actor *uuid.UUID
	if bag.Person != nil {
		id := bag.Person.PersonID
		actor = &id
	}
	result := audit.Deny
	if ev.Decision == NodeAllow {
		result = audit.Allow
	}
	if err := s.auditTimed(ctx, actor, &loginName, "person_open", bag.ClientID.String(), result, ev.TimeSource); err != nil {
		return ev, err
	}
	return ev, nil
}
