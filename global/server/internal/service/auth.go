package service

import (
	"context"
	"time"

	"wmesh/global/internal/platform/audit"
	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/platform/secret"
)

const sessionTTL = 12 * time.Hour // 在线会话有效期；过期后必须重新登录

// BootstrapAdmin 写入唯一 WAN 管理员；已有管理员则拒绝并留审计。
func (s *Auth) BootstrapAdmin(ctx context.Context, loginName, password string) error {
	hash, err := secret.HashPassword(password)
	if err != nil {
		return err
	}
	if _, err := s.store.CreateAdmin(ctx, loginName, hash); err != nil {
		_ = s.audit(ctx, nil, &loginName, nil, "bootstrap_wan_admin", loginName, audit.Deny)
		return err
	}
	return s.audit(ctx, nil, &loginName, nil, "bootstrap_wan_admin", loginName, audit.Allow)
}

// Login 校验 WAN 口令并开会话；失败只记被声明登录名，不记口令。
func (s *Auth) Login(ctx context.Context, loginName, password string) (string, error) {
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
func (s *Auth) Logout(ctx context.Context, token string) error {
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
func (s *kernel) RequireAdmin(ctx context.Context, token string) (Admin, error) {
	sess, err := s.store.SessionByTokenHash(ctx, secret.TokenHash(token))
	if err != nil {
		return Admin{}, domain.ErrUnauthorized
	}
	return s.store.AdminByID(ctx, sess.AdminID)
}
