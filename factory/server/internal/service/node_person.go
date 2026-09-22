package service

import (
	"context"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/secret"
)

// loginPerson 核对本厂有效账号和密码；不签发授权，他厂袋直接拒绝。
func (s *kernel) loginPerson(ctx context.Context, bag Bag, clocks Clocks, loginName, password string) (Person, NodeEval) {
	_, src := bagNow(bag, clocks)
	ev := NodeEval{Decision: NodeDeny, TimeSource: src}
	if bag.FactoryID != s.store.FactoryID() {
		return Person{}, ev
	}
	p, err := s.store.PersonByLogin(ctx, loginName)
	if err != nil {
		return Person{}, ev
	}
	if p.Status != StatusActive || p.PasswordHash == nil || *p.PasswordHash == "" {
		return Person{}, ev
	}
	if !secret.VerifyPassword(*p.PasswordHash, password) {
		return Person{}, ev
	}
	ev.Decision = NodeAllow
	return p, ev
}

// canOperateAs 须有操作员角色，分配不够。
func (s *kernel) canOperateAs(ctx context.Context, personID uuid.UUID) (bool, error) {
	grants, err := s.store.ActiveGrants(ctx, personID)
	if err != nil {
		return false, err
	}
	for _, g := range grants {
		if g.Role == RoleOperator {
			return true, nil
		}
	}
	return false, nil
}

// operatorAccount 袋上当前使用人须仍是本厂有效账号。
func (s *kernel) operatorAccount(ctx context.Context, bag Bag) (Account, error) {
	if bag.OperatorID == nil {
		return Account{}, domain.ErrForbidden
	}
	p, err := s.store.PersonByID(ctx, *bag.OperatorID)
	if err != nil {
		return Account{}, err
	}
	if p.Status != StatusActive {
		return Account{}, domain.ErrForbidden
	}
	return accountOf(p), nil
}

// 袋上当前使用人，给审计当操作者。
func personActor(bag *Bag) *uuid.UUID {
	if bag == nil || bag.OperatorID == nil {
		return nil
	}
	id := *bag.OperatorID
	return &id
}

// 登录通过才把人员记成操作者。
func actorID(p Person, ev NodeEval) *uuid.UUID {
	if ev.Decision != NodeAllow {
		return nil
	}
	id := p.ID
	return &id
}

// LoginOffline 用本厂有效账号+密码登录本厂设备；不另签发人员授权。
func (s *Node) LoginOffline(ctx context.Context, bag Bag, clocks Clocks, loginName, password string) (NodeEval, error) {
	p, ev := s.loginPerson(ctx, bag, clocks, loginName, password)
	result := audit.Deny
	if ev.Decision == NodeAllow {
		result = audit.Allow
		if err := s.rememberOperator(ctx, bag.ClientID, p.ID); err != nil {
			return ev, err
		}
	}
	if err := s.auditTimed(ctx, actorID(p, ev), &loginName, "person_login", bag.ClientID.String(), result, ev.TimeSource); err != nil {
		return ev, err
	}
	return ev, nil
}

// EvaluateOfflineOp 本厂有效操作员/工程师 + 节点凭证 + 资产桩，三者都过才允许。
func (s *Node) EvaluateOfflineOp(ctx context.Context, bag Bag, clocks Clocks, loginName, password string) (NodeEval, error) {
	p, ev := s.evalOffline(ctx, bag, clocks, loginName, password)
	result := audit.Deny
	if ev.Decision == NodeAllow {
		result = audit.Allow
	}
	if err := s.auditTimed(ctx, actorID(p, ev), &loginName, "person_open", bag.ClientID.String(), result, ev.TimeSource); err != nil {
		return ev, err
	}
	return ev, nil
}

// evalOffline 账号、节点凭证、资产桩都过才允许。
func (s *kernel) evalOffline(ctx context.Context, bag Bag, clocks Clocks, loginName, password string) (Person, NodeEval) {
	p, ev := s.loginPerson(ctx, bag, clocks, loginName, password)
	if ev.Decision != NodeAllow {
		return p, ev
	}
	node := EvaluateRuntime(bag, clocks, NodeOpen)
	if node.Decision != NodeAllow {
		ev.Decision = NodeDeny
		ev.TimeSource = node.TimeSource
		return p, ev
	}
	ok, err := s.canOperateAs(ctx, p.ID)
	if err != nil || !ok || !bag.AssetAllowed {
		ev.Decision = NodeDeny
		return p, ev
	}
	if err := s.rememberOperator(ctx, bag.ClientID, p.ID); err != nil {
		ev.Decision = NodeDeny
		return p, ev
	}
	return p, ev
}

// rememberOperator 把当前使用人落到该 Client，便于管理端查看。
func (s *kernel) rememberOperator(ctx context.Context, clientID, personID uuid.UUID) error {
	return s.store.SetClientOperator(ctx, clientID, personID)
}

// CreateOfflineFact 用当前厂库分配选定上下文并冻结发生时路径；账号停用或无分配即拒绝。
func (s *Node) CreateOfflineFact(ctx context.Context, bag Bag, clocks Clocks, loginName, password string, wc WorkContext) (FactStub, error) {
	p, ev := s.evalOffline(ctx, bag, clocks, loginName, password)
	actor := actorID(p, ev)
	if bag.FactoryID != s.store.FactoryID() || ev.Decision != NodeAllow {
		_ = s.auditAtSrc(ctx, actor, "create_fact", "fact", audit.Deny, ev.TimeSource, nil, nil)
		return FactStub{}, domain.ErrForbidden
	}
	unitID, path, err := s.resolveWorkContext(ctx, accountOf(p), wc)
	if err != nil {
		_ = s.auditAtSrc(ctx, actor, "create_fact", "fact", audit.Deny, ev.TimeSource, unitID, path)
		return FactStub{}, err
	}
	// 冻结发生时路径，写入事实桩。
	row, err := s.store.InsertFact(ctx, p.ID, unitID, path)
	if err != nil {
		_ = s.auditAtSrc(ctx, actor, "create_fact", "fact", audit.Deny, ev.TimeSource, unitID, path)
		return FactStub{}, err
	}
	return row, s.auditAtSrc(ctx, actor, "create_fact", row.ID.String(), audit.Allow, ev.TimeSource, unitID, path)
}
