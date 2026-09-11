package service

import (
	"context"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/audit"
	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/platform/id"
)

// CreatedFactory 是创建工厂的交付结果；激活口令只给夹具，不写 WAN 库。
type CreatedFactory struct {
	Factory         Factory   `json:"factory"`         // 刚写入名录的工厂
	SuperAdminID    uuid.UUID `json:"superAdminId"`    // 厂库里的初始超管身份
	ActivationToken string    `json:"activationToken"` // 一次性激活口令原文，禁止写入审计
}

type Directory struct {
	Factories []Factory           `json:"factories"` // WAN 工厂名录
	Initials  []InitialSuperAdmin `json:"initials"`  // 各厂初始超管身份，不含口令
}

// CreateFactory 建厂并只下发一名绑定该厂的初始超管；激活口令不落 WAN。
func (s *Factories) CreateFactory(ctx context.Context, token, name, saLogin, saDisplay string) (CreatedFactory, error) {
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		_ = s.audit(ctx, nil, nil, nil, "create_factory", name, audit.Deny)
		return CreatedFactory{}, err
	}
	// 先发身份、先在厂库落初始超管，成功后才进 WAN 名录；厂端失败时名录里不留没有超管的空厂。
	fid := id.New()
	personID, actToken, err := s.boot.Bootstrap(ctx, fid, saLogin, saDisplay)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, &fid, "create_factory", name, audit.Deny)
		return CreatedFactory{}, err
	}
	// 名录与初始超管对账同一事务写入，避免只有其一。
	fac, err := s.store.RegisterFactory(ctx, fid, name, personID, saLogin)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, &fid, "create_factory", name, audit.Deny)
		return CreatedFactory{}, err
	}
	if err := s.audit(ctx, &admin.ID, nil, &fac.ID, "create_factory", fac.ID.String()+" "+personID.String(), audit.Allow); err != nil {
		return CreatedFactory{}, err
	}
	return CreatedFactory{Factory: fac, SuperAdminID: personID, ActivationToken: actToken}, nil
}

// IssueInitialSuperAdmin 拒绝再给已有工厂下发第二名初始超管。
func (s *Factories) IssueInitialSuperAdmin(ctx context.Context, token string, factoryID uuid.UUID) error {
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		_ = s.audit(ctx, nil, nil, &factoryID, "issue_initial_sa", factoryID.String(), audit.Deny)
		return err
	}
	_ = s.audit(ctx, &admin.ID, nil, &factoryID, "issue_initial_sa", factoryID.String(), audit.Deny)
	return domain.ErrInitialSAExists
}

// InviteWANAdmin 拒绝任何第二 WAN 用户或授权。
func (s *Factories) InviteWANAdmin(ctx context.Context, token, targetLogin string) error {
	var actor *uuid.UUID
	if admin, err := s.RequireAdmin(ctx, token); err == nil {
		actor = &admin.ID
	}
	_ = s.audit(ctx, actor, &targetLogin, nil, "invite_wan_admin", targetLogin, audit.Deny)
	return domain.ErrForbidden
}

// CreateFactoryPerson 拒绝 WAN 代建厂内人员。
func (s *Factories) CreateFactoryPerson(ctx context.Context, token string, factoryID uuid.UUID, loginName string) error {
	return s.denyFactoryManage(ctx, token, factoryID, "create_factory_person", loginName)
}

// CreateFactoryOrg 拒绝 WAN 代建厂内组织。
func (s *Factories) CreateFactoryOrg(ctx context.Context, token string, factoryID uuid.UUID, name string) error {
	return s.denyFactoryManage(ctx, token, factoryID, "create_factory_org", name)
}

// GrantFactoryRole 拒绝 WAN 代授厂内角色。
func (s *Factories) GrantFactoryRole(ctx context.Context, token string, factoryID uuid.UUID, target string) error {
	return s.denyFactoryManage(ctx, token, factoryID, "grant_factory_role", target)
}

func (s *kernel) denyFactoryManage(ctx context.Context, token string, factoryID uuid.UUID, action, target string) error {
	var actor *uuid.UUID
	if admin, err := s.RequireAdmin(ctx, token); err == nil {
		actor = &admin.ID
	}
	_ = s.audit(ctx, actor, nil, &factoryID, action, target, audit.Deny)
	return domain.ErrForbidden
}

// Directory 只返回工厂名录和初始超管身份，不含厂内扩展明细。
func (s *Factories) Directory(ctx context.Context, token string) (Directory, error) {
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		_ = s.audit(ctx, nil, nil, nil, "read_directory", "wan", audit.Deny)
		return Directory{}, err
	}
	facs, err := s.store.ListFactories(ctx)
	if err != nil {
		return Directory{}, err
	}
	initials, err := s.store.ListInitialSuperAdmins(ctx)
	if err != nil {
		return Directory{}, err
	}
	if err := s.audit(ctx, &admin.ID, nil, nil, "read_directory", "wan", audit.Allow); err != nil {
		return Directory{}, err
	}
	return Directory{Factories: facs, Initials: initials}, nil
}

// ListFactoryPeople 从 WAN 查厂内人员，一律拒绝。
func (s *Factories) ListFactoryPeople(ctx context.Context, token string, factoryID uuid.UUID) error {
	return s.denyFactoryManage(ctx, token, factoryID, "list_factory_people", factoryID.String())
}

// ReadAuthSecret 从 WAN 读任何认证秘密，一律拒绝。
func (s *Factories) ReadAuthSecret(ctx context.Context, token string, factoryID uuid.UUID) error {
	return s.denyFactoryManage(ctx, token, factoryID, "read_auth_secret", factoryID.String())
}
