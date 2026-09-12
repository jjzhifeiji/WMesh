package service

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/audit"
	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/platform/id"
	"wmesh/global/internal/platform/secret"
)

// CreatedFactory 是创建工厂的交付结果；建厂码只给持有者，不写 WAN 库原文。
type CreatedFactory struct {
	Factory         Factory   `json:"factory"`         // 刚写入名录的工厂
	SuperAdminID    uuid.UUID `json:"superAdminId"`    // 约定落在厂库的初始超管身份
	EnrollmentToken string    `json:"enrollmentToken"` // 一次性建厂码原文，禁止写入审计
}

type Directory struct {
	Factories []Factory           `json:"factories"` // WAN 工厂名录
	Initials  []InitialSuperAdmin `json:"initials"`  // 各厂初始超管身份，不含密码
}

// CreateFactory 只在 WAN 名录写下工厂与初始超管身份，发建厂码；不反打厂内网。
func (s *Factories) CreateFactory(ctx context.Context, token, name, saLogin, saDisplay string) (CreatedFactory, error) {
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		_ = s.audit(ctx, nil, nil, nil, "create_factory", name, audit.Deny)
		return CreatedFactory{}, err
	}
	code, err := secret.RandomToken()
	if err != nil {
		return CreatedFactory{}, err
	}
	fid := id.New()
	personID := id.New()
	// 名录与初始超管对账同一事务写入；厂端稍后用建厂码认领。
	fac, err := s.store.RegisterFactory(ctx, fid, name, personID, saLogin, saDisplay, secret.TokenHash(code))
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, &fid, "create_factory", name, audit.Deny)
		return CreatedFactory{}, err
	}
	if err := s.audit(ctx, &admin.ID, nil, &fac.ID, "create_factory", fac.ID.String()+" "+personID.String(), audit.Allow); err != nil {
		return CreatedFactory{}, err
	}
	return CreatedFactory{Factory: fac, SuperAdminID: personID, EnrollmentToken: code}, nil
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

// DisableFactory 停用工厂；厂端在线则立刻拒绝新登录，离线则回连后收敛。
func (s *Factories) DisableFactory(ctx context.Context, token string, factoryID uuid.UUID) (Factory, error) {
	return s.setStatus(ctx, token, factoryID, FactoryDisabled, "disable_factory")
}

// EnableFactory 重新启用已停用的工厂；已注销的不能启用。
func (s *Factories) EnableFactory(ctx context.Context, token string, factoryID uuid.UUID) (Factory, error) {
	return s.setStatus(ctx, token, factoryID, FactoryActive, "enable_factory")
}

// DeleteFactory 未认领则从名录拿掉；已认领只注销，历史与绑定保留。
func (s *Factories) DeleteFactory(ctx context.Context, token string, factoryID uuid.UUID) (*Factory, error) {
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		_ = s.audit(ctx, nil, nil, &factoryID, "delete_factory", factoryID.String(), audit.Deny)
		return nil, err
	}
	fac, err := s.store.FactoryByID(ctx, factoryID)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, &factoryID, "delete_factory", factoryID.String(), audit.Deny)
		return nil, err
	}
	if fac.EnrolledAt != nil || fac.Status == FactoryRetired {
		out, err := s.store.SetFactoryStatus(ctx, factoryID, FactoryRetired)
		if err != nil {
			_ = s.audit(ctx, &admin.ID, nil, &factoryID, "retire_factory", factoryID.String(), audit.Deny)
			return nil, err
		}
		if err := s.audit(ctx, &admin.ID, nil, &factoryID, "retire_factory", factoryID.String(), audit.Allow); err != nil {
			return nil, err
		}
		return &out, nil
	}
	if err := s.store.DeleteUnclaimedFactory(ctx, factoryID); err != nil {
		if errors.Is(err, domain.ErrReferenced) {
			out, err := s.store.SetFactoryStatus(ctx, factoryID, FactoryRetired)
			if err != nil {
				_ = s.audit(ctx, &admin.ID, nil, &factoryID, "retire_factory", factoryID.String(), audit.Deny)
				return nil, err
			}
			if err := s.audit(ctx, &admin.ID, nil, &factoryID, "retire_factory", factoryID.String(), audit.Allow); err != nil {
				return nil, err
			}
			return &out, nil
		}
		_ = s.audit(ctx, &admin.ID, nil, &factoryID, "delete_factory", factoryID.String(), audit.Deny)
		return nil, err
	}
	if err := s.audit(ctx, &admin.ID, nil, &factoryID, "delete_factory", factoryID.String(), audit.Allow); err != nil {
		return nil, err
	}
	return nil, nil
}

func (s *Factories) setStatus(ctx context.Context, token string, factoryID uuid.UUID, status, action string) (Factory, error) {
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		_ = s.audit(ctx, nil, nil, &factoryID, action, factoryID.String(), audit.Deny)
		return Factory{}, err
	}
	// 写入治理状态并升高修订；厂端只接受更高修订。
	out, err := s.store.SetFactoryStatus(ctx, factoryID, status)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, &factoryID, action, factoryID.String(), audit.Deny)
		return Factory{}, err
	}
	if err := s.audit(ctx, &admin.ID, nil, &factoryID, action, factoryID.String(), audit.Allow); err != nil {
		return Factory{}, err
	}
	return out, nil
}

// ListFactoryPeople 从 WAN 查厂内人员，一律拒绝。
func (s *Factories) ListFactoryPeople(ctx context.Context, token string, factoryID uuid.UUID) error {
	return s.denyFactoryManage(ctx, token, factoryID, "list_factory_people", factoryID.String())
}

// ReadAuthSecret 从 WAN 读任何认证秘密，一律拒绝。
func (s *Factories) ReadAuthSecret(ctx context.Context, token string, factoryID uuid.UUID) error {
	return s.denyFactoryManage(ctx, token, factoryID, "read_auth_secret", factoryID.String())
}
