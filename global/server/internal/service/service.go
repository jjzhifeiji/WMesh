// Package service 是 WAN 侧入口：唯一管理员、工厂名录和初始化交付。
// 不代建厂内普通账号、组织或角色，也不直连 SQL。
package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/audit"
	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/platform/secret"
)

const sessionTTL = 12 * time.Hour // 在线会话有效期；过期后必须重新登录

// FactoryBootstrap 在目标厂库写入待启用初始超管，并把激活口令只交给交付夹具。
type FactoryBootstrap interface {
	Bootstrap(ctx context.Context, factoryID uuid.UUID, saLogin, saDisplay string) (personID uuid.UUID, activationToken string, err error)
}

type Service struct {
	store *Store
	boot  FactoryBootstrap
}

// NewService 组装 WAN 应用服务；boot 只在目标厂库写初始超管。
func NewService(store *Store, boot FactoryBootstrap) *Service {
	return &Service{store: store, boot: boot}
}

// CreatedFactory 是创建工厂的交付结果；激活口令只给夹具，不写 WAN 库。
type CreatedFactory struct {
	Factory         Factory
	SuperAdminID    uuid.UUID
	ActivationToken string // 一次性激活口令原文，禁止写入审计
}

type Directory struct {
	Factories []Factory
	Initials  []InitialSuperAdmin
}

// BootstrapAdmin 写入唯一 WAN 管理员；已有管理员则拒绝。
func (s *Service) BootstrapAdmin(ctx context.Context, loginName, password string) error {
	hash, err := secret.HashPassword(password)
	if err != nil {
		return err
	}
	if _, err := s.store.CreateAdmin(ctx, loginName, hash); err != nil {
		_ = s.audit(ctx, nil, &loginName, nil, "bootstrap_wan_admin", loginName, audit.Deny)
		if errors.Is(err, domain.ErrWANAdminExists) {
			return err
		}
		return err
	}
	return s.audit(ctx, nil, &loginName, nil, "bootstrap_wan_admin", loginName, audit.Allow)
}

// Login 校验 WAN 口令并开会话；失败只记被声明登录名，不记口令。
func (s *Service) Login(ctx context.Context, loginName, password string) (string, error) {
	admin, err := s.store.AdminByLogin(ctx, loginName)
	if err != nil {
		_ = s.audit(ctx, nil, &loginName, nil, "login", "wan", audit.Deny)
		return "", domain.ErrInvalidCredentials
	}
	if !secret.VerifyPassword(admin.PasswordHash, password) {
		_ = s.audit(ctx, &admin.ID, &loginName, nil, "login", "wan", audit.Deny)
		return "", domain.ErrInvalidCredentials
	}
	token, err := secret.RandomToken()
	if err != nil {
		return "", err
	}
	if _, err := s.store.CreateSession(ctx, admin.ID, secret.TokenHash(token), time.Now().UTC().Add(sessionTTL)); err != nil {
		return "", err
	}
	if err := s.audit(ctx, &admin.ID, &loginName, nil, "login", "wan", audit.Allow); err != nil {
		_ = s.store.DeleteSessionByTokenHash(ctx, secret.TokenHash(token))
		return "", err
	}
	return token, nil
}

// Logout 结束当前 WAN 会话，不等于停用管理员。
func (s *Service) Logout(ctx context.Context, token string) error {
	sess, err := s.store.SessionByTokenHash(ctx, secret.TokenHash(token))
	if err != nil {
		_ = s.audit(ctx, nil, nil, nil, "logout", "wan", audit.Deny)
		return domain.ErrUnauthorized
	}
	if err := s.store.DeleteSessionByTokenHash(ctx, secret.TokenHash(token)); err != nil {
		return err
	}
	return s.audit(ctx, &sess.AdminID, nil, nil, "logout", "wan", audit.Allow)
}

// RequireAdmin 用会话取出当前 WAN 管理员；会话无效则拒绝。
func (s *Service) RequireAdmin(ctx context.Context, token string) (Admin, error) {
	sess, err := s.store.SessionByTokenHash(ctx, secret.TokenHash(token))
	if err != nil {
		return Admin{}, domain.ErrUnauthorized
	}
	return s.store.AdminByID(ctx, sess.AdminID)
}

// CreateFactory 建厂并只下发一名绑定该厂的初始超管；激活口令不落 WAN。
func (s *Service) CreateFactory(ctx context.Context, token, name, saLogin, saDisplay string) (CreatedFactory, error) {
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		_ = s.audit(ctx, nil, nil, nil, "create_factory", name, audit.Deny)
		return CreatedFactory{}, err
	}
	fac, err := s.store.CreateFactory(ctx, name)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "create_factory", name, audit.Deny)
		return CreatedFactory{}, err
	}
	personID, actToken, err := s.boot.Bootstrap(ctx, fac.ID, saLogin, saDisplay)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, &fac.ID, "create_factory", fac.ID.String(), audit.Deny)
		return CreatedFactory{}, err
	}
	if err := s.store.BindInitialSuperAdmin(ctx, fac.ID, personID, saLogin); err != nil {
		_ = s.audit(ctx, &admin.ID, nil, &fac.ID, "create_factory", fac.ID.String(), audit.Deny)
		return CreatedFactory{}, err
	}
	if err := s.audit(ctx, &admin.ID, nil, &fac.ID, "create_factory", fac.ID.String()+" "+personID.String(), audit.Allow); err != nil {
		return CreatedFactory{}, err
	}
	return CreatedFactory{Factory: fac, SuperAdminID: personID, ActivationToken: actToken}, nil
}

// IssueInitialSuperAdmin 拒绝再给已有工厂下发第二名初始超管。
func (s *Service) IssueInitialSuperAdmin(ctx context.Context, token string, factoryID uuid.UUID) error {
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		_ = s.audit(ctx, nil, nil, &factoryID, "issue_initial_sa", factoryID.String(), audit.Deny)
		return err
	}
	_ = s.audit(ctx, &admin.ID, nil, &factoryID, "issue_initial_sa", factoryID.String(), audit.Deny)
	return domain.ErrInitialSAExists
}

// InviteWANAdmin 拒绝任何第二 WAN 用户或授权。
func (s *Service) InviteWANAdmin(ctx context.Context, token, targetLogin string) error {
	var actor *uuid.UUID
	if admin, err := s.RequireAdmin(ctx, token); err == nil {
		actor = &admin.ID
	}
	_ = s.audit(ctx, actor, &targetLogin, nil, "invite_wan_admin", targetLogin, audit.Deny)
	return domain.ErrForbidden
}

// CreateFactoryPerson 拒绝 WAN 代建厂内人员。
func (s *Service) CreateFactoryPerson(ctx context.Context, token string, factoryID uuid.UUID, loginName string) error {
	return s.denyFactoryManage(ctx, token, factoryID, "create_factory_person", loginName)
}

// CreateFactoryOrg 拒绝 WAN 代建厂内组织。
func (s *Service) CreateFactoryOrg(ctx context.Context, token string, factoryID uuid.UUID, name string) error {
	return s.denyFactoryManage(ctx, token, factoryID, "create_factory_org", name)
}

// GrantFactoryRole 拒绝 WAN 代授厂内角色。
func (s *Service) GrantFactoryRole(ctx context.Context, token string, factoryID uuid.UUID, target string) error {
	return s.denyFactoryManage(ctx, token, factoryID, "grant_factory_role", target)
}

func (s *Service) denyFactoryManage(ctx context.Context, token string, factoryID uuid.UUID, action, target string) error {
	var actor *uuid.UUID
	if admin, err := s.RequireAdmin(ctx, token); err == nil {
		actor = &admin.ID
	}
	_ = s.audit(ctx, actor, nil, &factoryID, action, target, audit.Deny)
	return domain.ErrForbidden
}

// Directory 只返回工厂名录和初始超管身份，不含厂内扩展明细。
func (s *Service) Directory(ctx context.Context, token string) (Directory, error) {
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		_ = s.audit(ctx, nil, nil, nil, "read_directory", "wan", audit.Deny)
		return Directory{}, err
	}
	facs, err := s.store.ListFactories(ctx)
	if err != nil {
		return Directory{}, err
	}
	initials := make([]InitialSuperAdmin, 0, len(facs))
	for _, f := range facs {
		row, err := s.store.InitialSuperAdmin(ctx, f.ID)
		if err != nil {
			return Directory{}, err
		}
		initials = append(initials, row)
	}
	if err := s.audit(ctx, &admin.ID, nil, nil, "read_directory", "wan", audit.Allow); err != nil {
		return Directory{}, err
	}
	return Directory{Factories: facs, Initials: initials}, nil
}

// ListFactoryPeople 从 WAN 查厂内人员，一律拒绝。
func (s *Service) ListFactoryPeople(ctx context.Context, token string, factoryID uuid.UUID) error {
	return s.denyFactoryManage(ctx, token, factoryID, "list_factory_people", factoryID.String())
}

// ReadAuthSecret 从 WAN 读任何认证秘密，一律拒绝。
func (s *Service) ReadAuthSecret(ctx context.Context, token string, factoryID uuid.UUID) error {
	return s.denyFactoryManage(ctx, token, factoryID, "read_auth_secret", factoryID.String())
}

func (s *Service) ListAudit(ctx context.Context) ([]audit.Row, error) {
	return s.store.ListAudit(ctx)
}

func (s *Service) HasTable(ctx context.Context, name string) (bool, error) {
	return s.store.HasTable(ctx, name)
}

func (s *Service) audit(ctx context.Context, actor *uuid.UUID, claimed *string, factoryID *uuid.UUID, action, target, result string) error {
	return s.store.AppendAudit(ctx, audit.Event{
		ActorID:      actor,
		ClaimedLogin: claimed,
		FactoryID:    factoryID,
		Action:       action,
		Target:       target,
		Result:       result,
		TimeSource:   audit.Server,
	})
}
