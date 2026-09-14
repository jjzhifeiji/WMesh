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
	// 只存哈希，不落密码原文。
	hash, err := secret.HashPassword(password)
	if err != nil {
		return err
	}
	// 写入唯一 WAN 管理员；已有则拒绝。
	if _, err := s.store.CreateAdmin(ctx, loginName, hash); err != nil {
		_ = s.audit(ctx, nil, &loginName, nil, "bootstrap_wan_admin", loginName, audit.Deny)
		return err
	}
	// 引导成功才记允许。
	return s.audit(ctx, nil, &loginName, nil, "bootstrap_wan_admin", loginName, audit.Allow)
}

// Login 校验 WAN 密码并开会话；失败只记被声明登录名，不记密码。
func (s *Auth) Login(ctx context.Context, loginName, password string) (string, error) {
	// 失败一律记拒绝；不存在也当口令错，不记密码。
	admin, err := s.store.AdminByLogin(ctx, loginName)
	if err != nil {
		_ = s.audit(ctx, nil, &loginName, nil, "login", "wan", audit.Deny)
		return "", domain.ErrInvalidCredentials
	}
	if !secret.VerifyPassword(admin.PasswordHash, password) {
		_ = s.audit(ctx, &admin.ID, &loginName, nil, "login", "wan", audit.Deny)
		return "", domain.ErrInvalidCredentials
	}
	// 发一次性会话令牌，只存哈希。
	token, err := secret.RandomToken()
	if err != nil {
		return "", err
	}
	if _, err := s.store.CreateSession(ctx, admin.ID, secret.TokenHash(token), time.Now().UTC().Add(sessionTTL)); err != nil {
		return "", err
	}
	// 登录成功才记允许。
	if err := s.audit(ctx, &admin.ID, &loginName, nil, "login", "wan", audit.Allow); err != nil {
		// 审计没记下则作废刚开的会话。
		_ = s.store.DeleteSessionByTokenHash(ctx, secret.TokenHash(token))
		return "", err
	}
	return token, nil
}

// ChangePassword 改自己的日常密码，稳定身份不变，审计不写密码原文。
func (s *Auth) ChangePassword(ctx context.Context, token, newPassword string) error {
	// 只能改自己的日常密码。
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		return err
	}
	// 只存新哈希，不落原文。
	hash, err := secret.HashPassword(newPassword)
	if err != nil {
		return err
	}
	if err := s.store.SetAdminPassword(ctx, admin.ID, hash); err != nil {
		return err
	}
	// 改密成功才记允许。
	return s.audit(ctx, &admin.ID, &admin.LoginName, nil, "change_password", admin.ID.String(), audit.Allow)
}

// ResetAdminPassword 按登录名覆盖唯一管理员密码哈希，并作废其全部会话。
func (s *Auth) ResetAdminPassword(ctx context.Context, loginName, password string) error {
	// 按登录名找唯一管理员。
	admin, err := s.store.AdminByLogin(ctx, loginName)
	if err != nil {
		_ = s.audit(ctx, nil, &loginName, nil, "reset_wan_admin", loginName, audit.Deny)
		return err
	}
	// 只存新哈希。
	hash, err := secret.HashPassword(password)
	if err != nil {
		return err
	}
	// 覆盖哈希并作废其全部会话。
	if err := s.store.SetAdminPassword(ctx, admin.ID, hash); err != nil {
		return err
	}
	if err := s.store.DeleteSessionsForAdmin(ctx, admin.ID); err != nil {
		return err
	}
	// 重置成功才记允许。
	return s.audit(ctx, &admin.ID, &loginName, nil, "reset_wan_admin", admin.ID.String(), audit.Allow)
}

// Logout 结束当前 WAN 会话，不等于停用管理员。
func (s *Auth) Logout(ctx context.Context, token string) error {
	// 按令牌哈希取当前会话，原文不入库。
	sess, err := s.store.SessionByTokenHash(ctx, secret.TokenHash(token))
	if err != nil {
		_ = s.audit(ctx, nil, nil, nil, "logout", "wan", audit.Deny)
		return domain.ErrUnauthorized
	}
	// 删掉这条会话。
	if err := s.store.DeleteSessionByTokenHash(ctx, secret.TokenHash(token)); err != nil {
		return err
	}
	// 登出成功才记允许。
	return s.audit(ctx, &sess.AdminID, nil, nil, "logout", "wan", audit.Allow)
}

// RequireAdmin 用会话取出当前 WAN 管理员；会话无效则拒绝。
func (s *kernel) RequireAdmin(ctx context.Context, token string) (Admin, error) {
	// 按令牌哈希取会话；无效则拒绝。
	sess, err := s.store.SessionByTokenHash(ctx, secret.TokenHash(token))
	if err != nil {
		return Admin{}, domain.ErrUnauthorized
	}
	return s.store.AdminByID(ctx, sess.AdminID)
}
