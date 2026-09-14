package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/secret"
)

const sessionTTL = 12 * time.Hour // 厂内在线会话有效期

// BootstrapInitial 在本厂写入待启用初始超管和预置厂级超管角色，激活码只返回给交付方。
func (s *Auth) BootstrapInitial(ctx context.Context, saLogin, saDisplay string) (Account, string, error) {
	// 写入待启用初始超管，尚无日常密码。
	p, err := s.store.CreatePerson(ctx, saLogin, saDisplay, true)
	if err != nil {
		return Account{}, "", err
	}
	// 激活码只给交付方，库里只存哈希。
	token, err := secret.ActivationCode()
	if err != nil {
		return Account{}, "", err
	}
	if err := s.store.SetActivationHash(ctx, p.ID, secret.TokenHash(token)); err != nil {
		return Account{}, "", err
	}
	// 预置厂级超管角色，待启用时还不生效。
	if _, err := s.store.GrantRole(ctx, p.ID, RoleFactorySuperAdmin, ScopeFactory, nil); err != nil {
		return Account{}, "", err
	}
	if err := s.audit(ctx, nil, &saLogin, "bootstrap_initial_sa", p.ID.String(), audit.Allow); err != nil {
		return Account{}, "", err
	}
	return accountOf(p), token, nil
}

// AcceptEnrollment 按 WAN 约定身份写入初始超管并当场自设密码，完成激活。
func (s *Auth) AcceptEnrollment(ctx context.Context, personID uuid.UUID, loginName, displayName, password string) (Account, error) {
	if password == "" {
		return Account{}, domain.ErrInvalidActivation
	}
	// 身份须对上；尚无档则按约定写入。失败一律记拒绝。
	p, err := s.store.PersonByID(ctx, personID)
	if errors.Is(err, domain.ErrNotFound) {
		p, err = s.store.CreatePersonAt(ctx, personID, loginName, displayName, true)
		if err != nil {
			_ = s.audit(ctx, nil, &loginName, "enroll_factory", personID.String(), audit.Deny)
			return Account{}, err
		}
	} else if err != nil {
		return Account{}, err
	} else if !p.IsInitialSuperAdmin || p.LoginName != loginName {
		_ = s.audit(ctx, &p.ID, &loginName, "enroll_factory", personID.String(), audit.Deny)
		return Account{}, domain.ErrInvalidEnrollment
	}
	// 幂等补厂级超管角色；已有则跳过。
	if _, err := s.store.GrantRole(ctx, p.ID, RoleFactorySuperAdmin, ScopeFactory, nil); err != nil && !errors.Is(err, domain.ErrDuplicateRoleGrant) {
		_ = s.audit(ctx, &p.ID, &loginName, "enroll_factory", p.ID.String(), audit.Deny)
		return Account{}, err
	}
	if p.Status == StatusActive {
		return accountOf(p), nil
	}
	// 当场自设日常密码，只存哈希。
	hash, err := secret.HashPassword(password)
	if err != nil {
		return Account{}, err
	}
	if err := s.store.ActivatePerson(ctx, p.ID, hash); err != nil {
		_ = s.audit(ctx, &p.ID, &loginName, "activate", p.ID.String(), audit.Deny)
		return Account{}, err
	}
	if err := s.audit(ctx, &p.ID, &loginName, "enroll_factory", p.ID.String(), audit.Allow); err != nil {
		return Account{}, err
	}
	return accountOf(p), s.audit(ctx, &p.ID, &loginName, "activate", p.ID.String(), audit.Allow)
}

// Activate 由持有者自己设日常密码，账号转为有效；WAN 不得代设。
func (s *Auth) Activate(ctx context.Context, loginName, activationToken, password string) error {
	// 工厂须开着；码不对或已激活一律记拒绝。
	if err := s.requireFactoryOpen(ctx); err != nil {
		_ = s.audit(ctx, nil, &loginName, "activate", s.store.FactoryID().String(), audit.Deny)
		return err
	}
	activationToken = strings.TrimSpace(activationToken)
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
	// 日常密码只存哈希；激活后激活码作废。
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

// activationOK 恒定时间比对激活码哈希，不给按位猜测的机会。
func activationOK(storedHash, token string) bool {
	return secret.Equal(storedHash, secret.TokenHash(token))
}

// Login 只接受本厂有效账号；待启用、已停用或密码错误都拒绝，审计不记秘密。
func (s *Auth) Login(ctx context.Context, loginName, password string) (string, error) {
	// 只接受本厂有效账号；失败一律记拒绝，不记密码。
	if err := s.requireFactoryOpen(ctx); err != nil {
		_ = s.audit(ctx, nil, &loginName, "login", s.store.FactoryID().String(), audit.Deny)
		return "", err
	}
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
	// 发会话令牌，库只存哈希。
	token, err := secret.RandomToken()
	if err != nil {
		return "", err
	}
	if _, err := s.store.CreateSession(ctx, p.ID, secret.TokenHash(token), time.Now().UTC().Add(sessionTTL)); err != nil {
		return "", err
	}
	if err := s.audit(ctx, &p.ID, &loginName, "login", s.store.FactoryID().String(), audit.Allow); err != nil {
		// 审计失败则立刻作废刚开会话。
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
	// 立刻结束会话，账号状态不变。
	if err := s.store.DeleteSessionByTokenHash(ctx, secret.TokenHash(token)); err != nil {
		return err
	}
	return s.audit(ctx, &sess.PersonID, nil, "logout", s.store.FactoryID().String(), audit.Allow)
}

// RequireActive 校验会话后重查账号状态；停用或待启用的旧会话也不能再做新操作。
func (s *kernel) RequireActive(ctx context.Context, token string) (Account, error) {
	// 令牌只比哈希；过期、停用、待启用都拒绝，会话不当权限缓存。
	sess, err := s.store.SessionByTokenHash(ctx, secret.TokenHash(token))
	if err != nil {
		if errors.Is(err, domain.ErrSessionExpired) {
			_ = s.audit(ctx, nil, nil, "protect", s.store.FactoryID().String(), audit.Deny)
			return Account{}, domain.ErrUnauthorized
		}
		_ = s.audit(ctx, nil, nil, "protect", s.store.FactoryID().String(), audit.Deny)
		return Account{}, domain.ErrUnauthorized
	}
	if err := s.requireFactoryOpen(ctx); err != nil {
		_ = s.audit(ctx, &sess.PersonID, nil, "protect", s.store.FactoryID().String(), audit.Deny)
		return Account{}, err
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

// ChangePassword 改日常密码，稳定身份不变，审计不写密码原文。
func (s *Auth) ChangePassword(ctx context.Context, token, newPassword string) error {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return err
	}
	// 日常密码只存哈希，不写原文。
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
	// 改显示名或登录名，稳定身份不变。
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
	// 只有工厂超管能停用；最后一名有效超管入口不能停。失败一律记拒绝。
	if err := s.can(ctx, acc, permManageAccount, nil); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "disable_account", targetID.String(), audit.Deny)
		return err
	}
	// 激活后必须至少留一个「有效账号 + 厂级超管角色」。
	if err := s.guardLastAdminOnDisable(ctx, targetID); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "disable_account", targetID.String(), audit.Deny)
		return err
	}
	// 停用后新操作立刻按新状态判定。
	if err := s.store.SetPersonStatus(ctx, targetID, StatusDisabled); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "disable_account", targetID.String(), audit.Deny)
		return err
	}
	return s.audit(ctx, &acc.ID, nil, "disable_account", targetID.String(), audit.Allow)
}

// ResetPassword 超管把本厂其他人日常密码改回登录名+123456；旧密码立刻失效，会话作废。
func (s *Auth) ResetPassword(ctx context.Context, token string, targetID uuid.UUID) (Account, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return Account{}, err
	}
	// 只有工厂超管能重置他人密码。失败一律记拒绝。
	if err := s.can(ctx, acc, permManageAccount, nil); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "reset_password", targetID.String(), audit.Deny)
		return Account{}, err
	}
	// 自己忘了也得由另一名超管改；登录着的人走改密码。
	if acc.ID == targetID {
		_ = s.audit(ctx, &acc.ID, nil, "reset_password", targetID.String(), audit.Deny)
		return Account{}, domain.ErrForbidden
	}
	p, err := s.store.PersonByID(ctx, targetID)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "reset_password", targetID.String(), audit.Deny)
		return Account{}, err
	}
	hash, err := secret.HashPassword(secret.DefaultPersonPassword(p.LoginName))
	if err != nil {
		return Account{}, err
	}
	next := StatusActive
	if p.Status == StatusDisabled {
		next = StatusDisabled
	}
	// 覆盖哈希；停用账号保持停用。旧会话立刻作废。
	if err := s.store.ApplyPersonPassword(ctx, p.ID, hash, next); err != nil {
		_ = s.audit(ctx, &acc.ID, &p.LoginName, "reset_password", p.ID.String(), audit.Deny)
		return Account{}, err
	}
	if err := s.store.DeleteSessionsForPerson(ctx, p.ID); err != nil {
		return Account{}, err
	}
	p, err = s.store.PersonByID(ctx, p.ID)
	if err != nil {
		return Account{}, err
	}
	if err := s.audit(ctx, &acc.ID, &p.LoginName, "reset_password", p.ID.String(), audit.Allow); err != nil {
		return Account{}, err
	}
	return accountOf(p), nil
}

// EnableAccount 恢复已停用账号；有日常密码的回到有效，否则回到待启用。不删账号。
func (s *Auth) EnableAccount(ctx context.Context, token string, targetID uuid.UUID) error {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return err
	}
	// 只有工厂超管能恢复账号。失败一律记拒绝。
	if err := s.can(ctx, acc, permManageAccount, nil); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "enable_account", targetID.String(), audit.Deny)
		return err
	}
	p, err := s.store.PersonByID(ctx, targetID)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "enable_account", targetID.String(), audit.Deny)
		return err
	}
	if p.Status != StatusDisabled {
		return s.audit(ctx, &acc.ID, nil, "enable_account", targetID.String(), audit.Allow)
	}
	next := StatusPending
	if p.PasswordHash != nil && *p.PasswordHash != "" {
		next = StatusActive
	}
	// 有日常密码的回到有效，否则回到待启用。
	if err := s.store.SetPersonStatus(ctx, targetID, next); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "enable_account", targetID.String(), audit.Deny)
		return err
	}
	return s.audit(ctx, &acc.ID, nil, "enable_account", targetID.String(), audit.Allow)
}
