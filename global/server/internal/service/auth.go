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

// Login 校验 WAN 密码并开会话；失败只记被声明登录名，不记密码。
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

// ChangePassword 改自己的日常密码，稳定身份不变，审计不写密码原文。
func (s *Auth) ChangePassword(ctx context.Context, token, newPassword string) error {
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		return err
	}
	hash, err := secret.HashPassword(newPassword)
	if err != nil {
		return err
	}
	if err := s.store.SetAdminPassword(ctx, admin.ID, hash); err != nil {
		return err
	}
	return s.audit(ctx, &admin.ID, &admin.LoginName, nil, "change_password", admin.ID.String(), audit.Allow)
}

// ResetAdminPassword 按登录名覆盖唯一管理员密码哈希，并作废其全部会话。
func (s *Auth) ResetAdminPassword(ctx context.Context, loginName, password string) error {
	admin, err := s.store.AdminByLogin(ctx, loginName)
	if err != nil {
		_ = s.audit(ctx, nil, &loginName, nil, "reset_wan_admin", loginName, audit.Deny)
		return err
	}
	hash, err := secret.HashPassword(password)
	if err != nil {
		return err
	}
	if err := s.store.SetAdminPassword(ctx, admin.ID, hash); err != nil {
		return err
	}
	if err := s.store.DeleteSessionsForAdmin(ctx, admin.ID); err != nil {
		return err
	}
	return s.audit(ctx, &admin.ID, &loginName, nil, "reset_wan_admin", admin.ID.String(), audit.Allow)
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
