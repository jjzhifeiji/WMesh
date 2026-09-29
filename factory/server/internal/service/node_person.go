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
	// 连网用服务器钟，断网用本机钟
	_, src := bagNow(bag, clocks)
	// 先按拒绝，账号和凭证都过才改成允许
	ev := NodeEval{Decision: NodeDeny, TimeSource: src}
	// 袋不是这台或不是本厂，直接拒绝
	if bag.FactoryID != s.store.FactoryID() {
		return Person{}, ev
	}
	// 按登录名找本厂人员
	p, err := s.store.PersonByLogin(ctx, loginName)
	// 登录名对不上人，按拒绝停住
	if err != nil {
		return Person{}, ev
	}
	// 不是有效状态则拒绝，待启用和停用都不能做
	if p.Status != StatusActive || p.PasswordHash == nil || *p.PasswordHash == "" {
		return Person{}, ev
	}
	// 还没有口令或口令不对，拒绝登录
	if !secret.VerifyPassword(*p.PasswordHash, password) {
		return Person{}, ev
	}
	// 所需项都过了，改为允许
	ev.Decision = NodeAllow
	return p, ev
}

// canOperateAs 须有操作员角色，分配不够。
func (s *kernel) canOperateAs(ctx context.Context, personID uuid.UUID) (bool, error) {
	// 只取仍有效的角色
	grants, err := s.store.ActiveGrants(ctx, personID)
	// 角色读失败，不能判定许可
	if err != nil {
		return false, err
	}
	// 逐条有效授权看是否盖住目标
	for _, g := range grants {
		// 只认操作员角色，别的角色不能开这道操作
		if g.Role == RoleOperator {
			return true, nil
		}
	}
	return false, nil
}

// operatorAccount 袋上当前使用人须仍是本厂有效账号。
func (s *kernel) operatorAccount(ctx context.Context, bag Bag) (Account, error) {
	// 这台没有使用人或使用人不是当前登录者
	if bag.OperatorID == nil {
		return Account{}, domain.ErrForbidden
	}
	// 按稳定身份读人员，改名也不变
	p, err := s.store.PersonByID(ctx, *bag.OperatorID)
	// 人员不存在，拒绝这次变更
	if err != nil {
		return Account{}, err
	}
	// 不是有效状态则拒绝，待启用和停用都不能做
	if p.Status != StatusActive {
		return Account{}, domain.ErrForbidden
	}
	// 收成不含口令的对外账号，再交回调用方
	return accountOf(p), nil
}

// 袋上当前使用人，给审计当操作者。
func personActor(bag *Bag) *uuid.UUID {
	// 这台没有使用人或使用人不是当前登录者
	if bag == nil || bag.OperatorID == nil {
		return nil
	}
	// 另拷一份身份再取址，避免下一轮把指针改掉
	id := *bag.OperatorID
	return &id
}

// 登录通过才把人员记成操作者。
func actorID(p Person, ev NodeEval) *uuid.UUID {
	// 判定不是允许则拒绝操作或改记审计
	if ev.Decision != NodeAllow {
		return nil
	}
	// 另拷一份身份再取址，避免下一轮把指针改掉
	id := p.ID
	return &id
}

// LoginOffline 用本厂有效账号+密码登录本厂设备；不另签发人员授权。
func (s *Node) LoginOffline(ctx context.Context, bag Bag, clocks Clocks, loginName, password string) (NodeEval, error) {
	// 核对本厂有效账号和口令
	p, ev := s.loginPerson(ctx, bag, clocks, loginName, password)
	// 默认记拒绝，通过后再改成允许
	result := audit.Deny
	// 判定不是允许则拒绝操作或改记审计
	if ev.Decision == NodeAllow {
		// 判定通过，审计按允许落
		result = audit.Allow
		// 写不进去则管理端仍显示旧人
		if err := s.rememberOperator(ctx, bag.ClientID, p.ID); err != nil {
			return ev, err
		}
	}
	// 审计没写下则整次不算完成
	if err := s.auditTimed(ctx, actorID(p, ev), &loginName, "person_login", bag.ClientID.String(), result, ev.TimeSource); err != nil {
		return ev, err
	}
	return ev, nil
}

// EvaluateOfflineOp 本厂有效操作员/工程师 + 节点凭证 + 资产桩，三者都过才允许。
func (s *Node) EvaluateOfflineOp(ctx context.Context, bag Bag, clocks Clocks, loginName, password string) (NodeEval, error) {
	// 账号、凭证、资产桩都过才允许
	p, ev := s.evalOffline(ctx, bag, clocks, loginName, password)
	// 默认记拒绝，通过后再改成允许
	result := audit.Deny
	// 判定不是允许则拒绝操作或改记审计
	if ev.Decision == NodeAllow {
		// 判定通过，审计按允许落
		result = audit.Allow
	}
	// 审计没写下则整次不算完成
	if err := s.auditTimed(ctx, actorID(p, ev), &loginName, "person_open", bag.ClientID.String(), result, ev.TimeSource); err != nil {
		return ev, err
	}
	return ev, nil
}

// evalOffline 账号、节点凭证、资产桩都过才允许。
func (s *kernel) evalOffline(ctx context.Context, bag Bag, clocks Clocks, loginName, password string) (Person, NodeEval) {
	// 核对本厂有效账号和口令
	p, ev := s.loginPerson(ctx, bag, clocks, loginName, password)
	// 判定不是允许则拒绝操作或改记审计
	if ev.Decision != NodeAllow {
		return p, ev
	}
	// 断网只查本机袋里的凭证
	node := EvaluateRuntime(bag, clocks, NodeOpen)
	// 判定不是允许则拒绝操作或改记审计
	if node.Decision != NodeAllow {
		// 这一项没过，整次判定改为拒绝
		ev.Decision = NodeDeny
		// 审计改用这一支判定所用的钟
		ev.TimeSource = node.TimeSource
		return p, ev
	}
	// 核对是否仍有操作员角色
	ok, err := s.canOperateAs(ctx, p.ID)
	// 没有操作员角色，光有分配也不够
	if err != nil || !ok || !bag.AssetAllowed {
		// 这一项没过，整次判定改为拒绝
		ev.Decision = NodeDeny
		return p, ev
	}
	// 写不进去则管理端仍显示旧人
	if err := s.rememberOperator(ctx, bag.ClientID, p.ID); err != nil {
		// 这一项没过，整次判定改为拒绝
		ev.Decision = NodeDeny
		return p, ev
	}
	return p, ev
}

// rememberOperator 把当前使用人落到该 Client，便于管理端查看。
func (s *kernel) rememberOperator(ctx context.Context, clientID, personID uuid.UUID) error {
	// 记下这台当前使用人，方便管理端看，再交回调用方
	return s.store.SetClientOperator(ctx, clientID, personID)
}

// CreateOfflineFact 用当前厂库分配选定上下文并冻结发生时路径；账号停用或无分配即拒绝。
func (s *Node) CreateOfflineFact(ctx context.Context, bag Bag, clocks Clocks, loginName, password string, wc WorkContext) (FactStub, error) {
	// 账号、凭证、资产桩都过才允许
	p, ev := s.evalOffline(ctx, bag, clocks, loginName, password)
	// 登录通过才把人员记成操作者
	actor := actorID(p, ev)
	// 判定不是允许则拒绝操作或改记审计
	if bag.FactoryID != s.store.FactoryID() || ev.Decision != NodeAllow {
		// 记下写运行事实被拒绝，写失败不改变结果
		_ = s.auditAtSrc(ctx, actor, "create_fact", "fact", audit.Deny, ev.TimeSource, nil, nil)
		return FactStub{}, domain.ErrForbidden
	}
	// 收成不含口令的对外账号
	unitID, path, err := s.resolveWorkContext(ctx, accountOf(p), wc)
	// 账号视图收不成就不交回
	if err != nil {
		// 记下写运行事实被拒绝，写失败不改变结果
		_ = s.auditAtSrc(ctx, actor, "create_fact", "fact", audit.Deny, ev.TimeSource, unitID, path)
		return FactStub{}, err
	}
	// 冻结发生时路径，写入事实桩。
	row, err := s.store.InsertFact(ctx, p.ID, unitID, path)
	// 写入失败则停住，避免留下半截状态
	if err != nil {
		// 记下写运行事实被拒绝，写失败不改变结果
		_ = s.auditAtSrc(ctx, actor, "create_fact", "fact", audit.Deny, ev.TimeSource, unitID, path)
		return FactStub{}, err
	}
	// 写运行事实成功后记审计，再把结果交回
	return row, s.auditAtSrc(ctx, actor, "create_fact", row.ID.String(), audit.Allow, ev.TimeSource, unitID, path)
}
