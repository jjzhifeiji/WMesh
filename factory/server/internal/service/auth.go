package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/secret"
)

const sessionTTL = 12 * time.Hour // 厂内在线会话有效期

// BootstrapInitial 在本厂写入待启用初始超管和预置厂级超管角色，激活口令只返回给交付方。
func (s *Auth) BootstrapInitial(ctx context.Context, saLogin, saDisplay string) (Account, string, error) {
	p, err := s.store.CreatePerson(ctx, saLogin, saDisplay, true)
	if err != nil {
		return Account{}, "", err
	}
	token, err := secret.RandomToken()
	if err != nil {
		return Account{}, "", err
	}
	if err := s.store.SetActivationHash(ctx, p.ID, secret.TokenHash(token)); err != nil {
		return Account{}, "", err
	}
	if _, err := s.store.GrantRole(ctx, p.ID, RoleFactorySuperAdmin, ScopeFactory, nil); err != nil {
		return Account{}, "", err
	}
	if err := s.audit(ctx, nil, &saLogin, "bootstrap_initial_sa", p.ID.String(), audit.Allow); err != nil {
		return Account{}, "", err
	}
	return accountOf(p), token, nil
}

// Activate 由持有者自己设日常口令，账号转为有效；WAN 不得代设。
func (s *Auth) Activate(ctx context.Context, loginName, activationToken, password string) error {
	p, err := s.store.PersonByLogin(ctx, loginName)
	if err != nil {
		_ = s.audit(ctx, nil, &loginName, "activate", loginName, audit.Deny)
		return domain.ErrInvalidActivation
	}
	if p.Status != StatusPending {
		_ = s.audit(ctx, &p.ID, &loginName, "activate", p.ID.String(), audit.Deny)
		return domain.ErrAlreadyActivated
	}
	if p.ActivationTokenHash == nil || !activationOK(*p.ActivationTokenHash, activationToken) {
		_ = s.audit(ctx, &p.ID, &loginName, "activate", p.ID.String(), audit.Deny)
		return domain.ErrInvalidActivation
	}
	hash, err := secret.HashPassword(password)
	if err != nil {
		return err
	}
	if err := s.store.ActivatePerson(ctx, p.ID, hash); err != nil {
		_ = s.audit(ctx, &p.ID, &loginName, "activate", p.ID.String(), audit.Deny)
		return err
	}
	return s.audit(ctx, &p.ID, &loginName, "activate", p.ID.String(), audit.Allow)
}

// activationOK 恒定时间比对激活口令哈希，不给按位猜测的机会。
func activationOK(storedHash, token string) bool {
	return secret.Equal(storedHash, secret.TokenHash(token))
}

// Login 只接受本厂有效账号；待启用、已停用或口令错误都拒绝，审计不记秘密。
func (s *Auth) Login(ctx context.Context, loginName, password string) (string, error) {
	p, err := s.store.PersonByLogin(ctx, loginName)
	if err != nil {
		_ = s.audit(ctx, nil, &loginName, "login", s.store.FactoryID().String(), audit.Deny)
		return "", domain.ErrInvalidCredentials
	}
	// 待启用可预写角色，但角色不生效，也不能登录。
	if p.Status == StatusPending {
		_ = s.audit(ctx, &p.ID, &loginName, "login", s.store.FactoryID().String(), audit.Deny)
		return "", domain.ErrAccountPending
	}
	if p.Status == StatusDisabled {
		_ = s.audit(ctx, &p.ID, &loginName, "login", s.store.FactoryID().String(), audit.Deny)
		return "", domain.ErrAccountDisabled
	}
	if p.PasswordHash == nil || !secret.VerifyPassword(*p.PasswordHash, password) {
		_ = s.audit(ctx, &p.ID, &loginName, "login", s.store.FactoryID().String(), audit.Deny)
		return "", domain.ErrInvalidCredentials
	}
	token, err := secret.RandomToken()
	if err != nil {
		return "", err
	}
	if _, err := s.store.CreateSession(ctx, p.ID, secret.TokenHash(token), time.Now().UTC().Add(sessionTTL)); err != nil {
		return "", err
	}
	if err := s.audit(ctx, &p.ID, &loginName, "login", s.store.FactoryID().String(), audit.Allow); err != nil {
		_ = s.store.DeleteSessionByTokenHash(ctx, secret.TokenHash(token))
		return "", err
	}
	return token, nil
}

// Logout 立刻结束当前会话，账号仍保持原状态。
func (s *Auth) Logout(ctx context.Context, token string) error {
	sess, err := s.store.SessionByTokenHash(ctx, secret.TokenHash(token))
	if err != nil {
		_ = s.audit(ctx, nil, nil, "logout", s.store.FactoryID().String(), audit.Deny)
		return domain.ErrUnauthorized
	}
	if err := s.store.DeleteSessionByTokenHash(ctx, secret.TokenHash(token)); err != nil {
		return err
	}
	return s.audit(ctx, &sess.PersonID, nil, "logout", s.store.FactoryID().String(), audit.Allow)
}

// RequireActive 校验会话后重查账号状态；停用或待启用的旧会话也不能再做新操作。
func (s *kernel) RequireActive(ctx context.Context, token string) (Account, error) {
	sess, err := s.store.SessionByTokenHash(ctx, secret.TokenHash(token))
	if err != nil {
		if errors.Is(err, domain.ErrSessionExpired) {
			_ = s.audit(ctx, nil, nil, "protect", s.store.FactoryID().String(), audit.Deny)
			return Account{}, domain.ErrUnauthorized
		}
		_ = s.audit(ctx, nil, nil, "protect", s.store.FactoryID().String(), audit.Deny)
		return Account{}, domain.ErrUnauthorized
	}
	p, err := s.store.PersonByID(ctx, sess.PersonID)
	if err != nil {
		return Account{}, err
	}
	if p.Status == StatusPending {
		_ = s.audit(ctx, &p.ID, &p.LoginName, "protect", p.ID.String(), audit.Deny)
		return Account{}, domain.ErrAccountPending
	}
	if p.Status != StatusActive {
		_ = s.audit(ctx, &p.ID, &p.LoginName, "protect", p.ID.String(), audit.Deny)
		return Account{}, domain.ErrAccountDisabled
	}
	return accountOf(p), nil
}

// ChangePassword 改日常口令，稳定身份不变，审计不写口令原文。
func (s *Auth) ChangePassword(ctx context.Context, token, newPassword string) error {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return err
	}
	hash, err := secret.HashPassword(newPassword)
	if err != nil {
		return err
	}
	if err := s.store.SetPasswordHash(ctx, acc.ID, hash); err != nil {
		return err
	}
	return s.audit(ctx, &acc.ID, &acc.LoginName, "change_password", acc.ID.String(), audit.Allow)
}

// Rename 改显示名或登录名，不改稳定身份。
func (s *Auth) Rename(ctx context.Context, token, displayName, loginName string) error {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return err
	}
	if err := s.store.RenamePerson(ctx, acc.ID, displayName, loginName); err != nil {
		_ = s.audit(ctx, &acc.ID, &loginName, "rename", acc.ID.String(), audit.Deny)
		return err
	}
	return s.audit(ctx, &acc.ID, &loginName, "rename", acc.ID.String(), audit.Allow)
}

// DisableAccount 停用本厂账号；若会去掉最后一名有效厂级超管则拒绝。
func (s *Auth) DisableAccount(ctx context.Context, token string, targetID uuid.UUID) error {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return err
	}
	if err := s.can(ctx, acc, permManageAccount, nil); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "disable_account", targetID.String(), audit.Deny)
		return err
	}
	// 激活后必须至少留一个「有效账号 + 厂级超管角色」。
	if err := s.guardLastAdminOnDisable(ctx, targetID); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "disable_account", targetID.String(), audit.Deny)
		return err
	}
	if err := s.store.SetPersonStatus(ctx, targetID, StatusDisabled); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "disable_account", targetID.String(), audit.Deny)
		return err
	}
	return s.audit(ctx, &acc.ID, nil, "disable_account", targetID.String(), audit.Allow)
}
