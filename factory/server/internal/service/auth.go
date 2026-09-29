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

const sessionTTL = 12 * time.Hour // 网页会话有效期；示教器另按登录时效

// appTokenExpiry 示教器令牌跟登录时效走；0 表示直到退出，不按时钟作废。
func appTokenExpiry(pol ClientPolicy, now time.Time) time.Time {
	// 登录时效大于零才按时钟作废，零表示直到退出
	if pol.KeyTTLSeconds > 0 {
		// 按登录时效算出示教器令牌何时失效
		return now.Add(time.Duration(pol.KeyTTLSeconds) * time.Second)
	}
	// 时效为零则不按时钟作废，远日表示一直有效
	return time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)
}

// BootstrapInitial 在本厂写入待启用初始超管和预置厂级超管角色，激活码只返回给交付方。
func (s *Auth) BootstrapInitial(ctx context.Context, saLogin, saDisplay string) (Account, string, error) {
	// 写入待启用初始超管，尚无日常密码。
	p, err := s.store.CreatePerson(ctx, saLogin, saDisplay, true)
	// 账号写不进去，不留下半截人员
	if err != nil {
		return Account{}, "", err
	}
	// 激活码只给交付方，库里只存哈希。
	token, err := secret.ActivationCode()
	// 激活码生成失败，交付中止
	if err != nil {
		return Account{}, "", err
	}
	// 令牌摘要失败，不能对上会话
	if err := s.store.SetActivationHash(ctx, p.ID, secret.TokenHash(token)); err != nil {
		return Account{}, "", err
	}
	// 预置厂级超管角色，待启用时还不生效。
	if _, err := s.store.GrantRole(ctx, p.ID, RoleFactorySuperAdmin, ScopeFactory, nil); err != nil {
		return Account{}, "", err
	}
	// 审计没写下则整次不算完成
	if err := s.audit(ctx, nil, &saLogin, "bootstrap_initial_sa", p.ID.String(), audit.Allow); err != nil {
		return Account{}, "", err
	}
	// 收成不含口令的对外账号，再交回调用方
	return accountOf(p), token, nil
}

// AcceptEnrollment 按 WAN 约定身份写入初始超管并当场自设密码，完成激活。
func (s *Auth) AcceptEnrollment(ctx context.Context, personID uuid.UUID, loginName, displayName, password string) (Account, error) {
	// 没带口令则拒绝，不能把空口令当成已激活
	if password == "" {
		return Account{}, domain.ErrInvalidActivation
	}
	// 身份须对上；尚无档则按约定写入。失败一律记拒绝。
	p, err := s.store.PersonByID(ctx, personID)
	// 还没有这个人，就按约定身份新建
	if errors.Is(err, domain.ErrNotFound) {
		// 按约定身份写入初始超管
		p, err = s.store.CreatePersonAt(ctx, personID, loginName, displayName, true)
		// 初始超管写不进去，建厂中止
		if err != nil {
			// 记下按约定建厂被拒绝，写失败不改变结果
			_ = s.audit(ctx, nil, &loginName, "enroll_factory", personID.String(), audit.Deny)
			return Account{}, err
		}
		// 其它错误则停住，只有没有记录才另走
	} else if err != nil {
		return Account{}, err
		// 不是约定的初始超管或登录名不符，拒绝建厂
	} else if !p.IsInitialSuperAdmin || p.LoginName != loginName {
		// 不是约定的初始超管，记下按约定建厂被拒绝，写失败不改变结果
		_ = s.audit(ctx, &p.ID, &loginName, "enroll_factory", personID.String(), audit.Deny)
		return Account{}, domain.ErrInvalidEnrollment
	}
	// 幂等补厂级超管角色；已有则跳过。
	if _, err := s.store.GrantRole(ctx, p.ID, RoleFactorySuperAdmin, ScopeFactory, nil); err != nil && !errors.Is(err, domain.ErrDuplicateRoleGrant) {
		// 记下按约定建厂被拒绝，写失败不改变结果
		_ = s.audit(ctx, &p.ID, &loginName, "enroll_factory", p.ID.String(), audit.Deny)
		return Account{}, err
	}
	// 已经有效则不必再激活，直接交回账号
	if p.Status == StatusActive {
		// 收成不含口令的对外账号，再交回调用方
		return accountOf(p), nil
	}
	// 当场自设日常密码，只存哈希。
	hash, err := secret.HashPassword(password)
	// 口令摘要失败，拒绝落库
	if err != nil {
		return Account{}, err
	}
	// 激活落库失败，账号仍待启用
	if err := s.store.ActivatePerson(ctx, p.ID, hash); err != nil {
		// 记下激活被拒绝，写失败不改变结果
		_ = s.audit(ctx, &p.ID, &loginName, "activate", p.ID.String(), audit.Deny)
		return Account{}, err
	}
	// 审计没写下则整次不算完成
	if err := s.audit(ctx, &p.ID, &loginName, "enroll_factory", p.ID.String(), audit.Allow); err != nil {
		return Account{}, err
	}
	// 激活成功后记审计，再把结果交回
	return accountOf(p), s.audit(ctx, &p.ID, &loginName, "activate", p.ID.String(), audit.Allow)
}

// Activate 由持有者自己设日常密码，账号转为有效；WAN 不得代设。
func (s *Auth) Activate(ctx context.Context, loginName, activationToken, password string) error {
	// 工厂须开着；码不对或已激活一律记拒绝。
	if err := s.requireFactoryOpen(ctx); err != nil {
		// 厂已停用、注销或租约到期，记下激活被拒绝，写失败不改变结果
		_ = s.audit(ctx, nil, &loginName, "activate", s.store.FactoryID().String(), audit.Deny)
		return err
	}
	// 去掉首尾空白后再做判断
	activationToken = strings.TrimSpace(activationToken)
	// 按登录名找本厂人员
	p, err := s.store.PersonByLogin(ctx, loginName)
	// 登录名对不上人，按拒绝停住
	if err != nil {
		// 记下激活被拒绝，写失败不改变结果
		_ = s.audit(ctx, nil, &loginName, "activate", loginName, audit.Deny)
		return domain.ErrInvalidActivation
	}
	// 不是待启用则拒绝激活，避免重复开通
	if p.Status != StatusPending {
		// 账号仍待启用，记下激活被拒绝，写失败不改变结果
		_ = s.audit(ctx, &p.ID, &loginName, "activate", p.ID.String(), audit.Deny)
		return domain.ErrAlreadyActivated
	}
	// 激活码为空或不对，拒绝开通
	if p.ActivationTokenHash == nil || !activationOK(*p.ActivationTokenHash, activationToken) {
		// 激活码不对或已失效，记下激活被拒绝，写失败不改变结果
		_ = s.audit(ctx, &p.ID, &loginName, "activate", p.ID.String(), audit.Deny)
		return domain.ErrInvalidActivation
	}
	// 日常密码只存哈希；激活后激活码作废。
	hash, err := secret.HashPassword(password)
	// 口令摘要失败，拒绝落库
	if err != nil {
		return err
	}
	// 激活落库失败，账号仍待启用
	if err := s.store.ActivatePerson(ctx, p.ID, hash); err != nil {
		// 记下激活被拒绝，写失败不改变结果
		_ = s.audit(ctx, &p.ID, &loginName, "activate", p.ID.String(), audit.Deny)
		return err
	}
	// 激活成功后记审计，再把结果交回
	return s.audit(ctx, &p.ID, &loginName, "activate", p.ID.String(), audit.Allow)
}

// activationOK 恒定时间比对激活码哈希，不给按位猜测的机会。
func activationOK(storedHash, token string) bool {
	// 只拿令牌摘要对会话，原文不入库，再交回调用方
	return secret.Equal(storedHash, secret.TokenHash(token))
}

// Login 只接受本厂有效账号；待启用、已停用或密码错误都拒绝，审计不记秘密。
func (s *Auth) Login(ctx context.Context, loginName, password string) (string, error) {
	// 只接受本厂有效账号；失败一律记拒绝，不记密码。
	if err := s.requireFactoryOpen(ctx); err != nil {
		// 厂已停用、注销或租约到期，记下登录被拒绝，写失败不改变结果
		_ = s.audit(ctx, nil, &loginName, "login", s.store.FactoryID().String(), audit.Deny)
		return "", err
	}
	// 按登录名找本厂人员
	p, err := s.store.PersonByLogin(ctx, loginName)
	// 登录名对不上人，按拒绝停住
	if err != nil {
		// 记下登录被拒绝，写失败不改变结果
		_ = s.audit(ctx, nil, &loginName, "login", s.store.FactoryID().String(), audit.Deny)
		return "", domain.ErrInvalidCredentials
	}
	// 待启用可预写角色，但角色不生效，也不能登录。
	if p.Status == StatusPending {
		// 账号仍待启用，记下登录被拒绝，写失败不改变结果
		_ = s.audit(ctx, &p.ID, &loginName, "login", s.store.FactoryID().String(), audit.Deny)
		return "", domain.ErrAccountPending
	}
	// 已停用则拒绝登录或保持停用只换口令
	if p.Status == StatusDisabled {
		// 账号已停用，记下登录被拒绝，写失败不改变结果
		_ = s.audit(ctx, &p.ID, &loginName, "login", s.store.FactoryID().String(), audit.Deny)
		return "", domain.ErrAccountDisabled
	}
	// 还没有口令或口令不对，拒绝登录
	if p.PasswordHash == nil || !secret.VerifyPassword(*p.PasswordHash, password) {
		// 口令不对，记下登录被拒绝，写失败不改变结果
		_ = s.audit(ctx, &p.ID, &loginName, "login", s.store.FactoryID().String(), audit.Deny)
		return "", domain.ErrInvalidCredentials
	}
	// 发会话令牌，库只存哈希。
	token, err := secret.RandomToken()
	// 激活码生成失败，不写半截超管
	if err != nil {
		return "", err
	}
	// 加上时效才知道令牌何时作废
	if _, err := s.store.CreateSession(ctx, p.ID, secret.TokenHash(token), time.Now().UTC().Add(sessionTTL)); err != nil {
		return "", err
	}
	// 审计没写下则整次不算完成
	if err := s.audit(ctx, &p.ID, &loginName, "login", s.store.FactoryID().String(), audit.Allow); err != nil {
		// 审计失败则立刻作废刚开会话。
		_ = s.store.DeleteSessionByTokenHash(ctx, secret.TokenHash(token))
		return "", err
	}
	return token, nil
}

// Logout 立刻结束当前会话，账号仍保持原状态。
func (s *Auth) Logout(ctx context.Context, token string) error {
	// 只拿令牌摘要对会话，原文不入库
	sess, err := s.store.SessionByTokenHash(ctx, secret.TokenHash(token))
	// 令牌摘要失败，不能对上会话
	if err != nil {
		// 记下退出被拒绝，写失败不改变结果
		_ = s.audit(ctx, nil, nil, "logout", s.store.FactoryID().String(), audit.Deny)
		return domain.ErrUnauthorized
	}
	// 立刻结束会话，账号状态不变。
	if err := s.store.DeleteSessionByTokenHash(ctx, secret.TokenHash(token)); err != nil {
		return err
	}
	// 退出成功后记审计，再把结果交回
	return s.audit(ctx, &sess.PersonID, nil, "logout", s.store.FactoryID().String(), audit.Allow)
}

// RequireActive 校验会话后重查账号状态；停用或待启用的旧会话也不能再做新操作。
func (s *kernel) RequireActive(ctx context.Context, token string) (Account, error) {
	// 令牌只比哈希；过期、停用、待启用都拒绝，会话不当权限缓存。
	sess, err := s.store.SessionByTokenHash(ctx, secret.TokenHash(token))
	// 令牌摘要失败，不能对上会话
	if err != nil {
		// 会话过期单独拒绝，并准备记审计
		if errors.Is(err, domain.ErrSessionExpired) {
			// 会话已经过期，记下续接会话被拒绝，写失败不改变结果
			_ = s.audit(ctx, nil, nil, "protect", s.store.FactoryID().String(), audit.Deny)
			return Account{}, domain.ErrUnauthorized
		}
		// 记下续接会话被拒绝，写失败不改变结果
		_ = s.audit(ctx, nil, nil, "protect", s.store.FactoryID().String(), audit.Deny)
		return Account{}, domain.ErrUnauthorized
	}
	// 厂已停用、注销或租约到期，拒绝本次
	if err := s.requireFactoryOpen(ctx); err != nil {
		// 厂已停用、注销或租约到期，记下续接会话被拒绝，写失败不改变结果
		_ = s.audit(ctx, &sess.PersonID, nil, "protect", s.store.FactoryID().String(), audit.Deny)
		return Account{}, err
	}
	// 按稳定身份读人员，改名也不变
	p, err := s.store.PersonByID(ctx, sess.PersonID)
	// 人员不存在，拒绝这次变更
	if err != nil {
		return Account{}, err
	}
	// 还在待启用，不能登录也不能当有效超管
	if p.Status == StatusPending {
		// 账号仍待启用，记下续接会话被拒绝，写失败不改变结果
		_ = s.audit(ctx, &p.ID, &p.LoginName, "protect", p.ID.String(), audit.Deny)
		return Account{}, domain.ErrAccountPending
	}
	// 不是有效状态则拒绝，待启用和停用都不能做
	if p.Status != StatusActive {
		// 记下续接会话被拒绝，写失败不改变结果
		_ = s.audit(ctx, &p.ID, &p.LoginName, "protect", p.ID.String(), audit.Deny)
		return Account{}, domain.ErrAccountDisabled
	}
	// 收成不含口令的对外账号，再交回调用方
	return accountOf(p), nil
}

// ChangePassword 改日常密码，稳定身份不变，审计不写密码原文。
func (s *Auth) ChangePassword(ctx context.Context, token, newPassword string) error {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return err
	}
	// 日常密码只存哈希，不写原文。
	hash, err := secret.HashPassword(newPassword)
	// 口令摘要失败，拒绝落库
	if err != nil {
		return err
	}
	// 新口令写不进去，保持原口令
	if err := s.store.SetPasswordHash(ctx, acc.ID, hash); err != nil {
		return err
	}
	// 改口令成功后记审计，再把结果交回
	return s.audit(ctx, &acc.ID, &acc.LoginName, "change_password", acc.ID.String(), audit.Allow)
}

// Rename 改显示名或登录名，不改稳定身份。
func (s *Auth) Rename(ctx context.Context, token, displayName, loginName string) error {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return err
	}
	// 改显示名或登录名，稳定身份不变。
	if err := s.store.RenamePerson(ctx, acc.ID, displayName, loginName); err != nil {
		// 记下改名被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, &loginName, "rename", acc.ID.String(), audit.Deny)
		return err
	}
	// 改名成功后记审计，再把结果交回
	return s.audit(ctx, &acc.ID, &loginName, "rename", acc.ID.String(), audit.Allow)
}

// DisableAccount 停用本厂账号；若会去掉最后一名有效厂级超管则拒绝。
func (s *Auth) DisableAccount(ctx context.Context, token string, targetID uuid.UUID) error {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return err
	}
	// 只有工厂超管能停用；最后一名有效超管入口不能停。失败一律记拒绝。
	if err := s.can(ctx, acc, permManageAccount, nil); err != nil {
		// 不是工厂超管，记下停用账号被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "disable_account", targetID.String(), audit.Deny)
		return err
	}
	// 激活后必须至少留一个「有效账号 + 厂级超管角色」。
	if err := s.guardLastAdminOnDisable(ctx, targetID); err != nil {
		// 会去掉最后一名有效超管，记下停用账号被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "disable_account", targetID.String(), audit.Deny)
		return err
	}
	// 停用后新操作立刻按新状态判定。
	if err := s.store.SetPersonStatus(ctx, targetID, StatusDisabled); err != nil {
		// 账号已停用，记下停用账号被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "disable_account", targetID.String(), audit.Deny)
		return err
	}
	// 停用账号成功后记审计，再把结果交回
	return s.audit(ctx, &acc.ID, nil, "disable_account", targetID.String(), audit.Allow)
}

// ResetPassword 超管把本厂其他人日常密码改回登录名+123456；旧密码立刻失效，会话作废。
func (s *Auth) ResetPassword(ctx context.Context, token string, targetID uuid.UUID) (Account, error) {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return Account{}, err
	}
	// 只有工厂超管能重置他人密码。失败一律记拒绝。
	if err := s.can(ctx, acc, permManageAccount, nil); err != nil {
		// 不是工厂超管，记下重置口令被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "reset_password", targetID.String(), audit.Deny)
		return Account{}, err
	}
	// 自己忘了也得由另一名超管改；登录着的人走改密码。
	if acc.ID == targetID {
		// 记下重置口令被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "reset_password", targetID.String(), audit.Deny)
		return Account{}, domain.ErrForbidden
	}
	// 按稳定身份读人员，改名也不变
	p, err := s.store.PersonByID(ctx, targetID)
	// 人员不存在，拒绝这次变更
	if err != nil {
		// 记下重置口令被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "reset_password", targetID.String(), audit.Deny)
		return Account{}, err
	}
	// 用登录名拼出重置后的日常口令
	hash, err := secret.HashPassword(secret.DefaultPersonPassword(p.LoginName))
	// 重置口令拼不出来，中止
	if err != nil {
		return Account{}, err
	}
	// 重置后默认回到可登录
	next := StatusActive
	// 已停用则拒绝登录或保持停用只换口令
	if p.Status == StatusDisabled {
		// 原本停用的仍保持停用，只换口令
		next = StatusDisabled
	}
	// 覆盖哈希；停用账号保持停用。旧会话立刻作废。
	if err := s.store.ApplyPersonPassword(ctx, p.ID, hash, next); err != nil {
		// 记下重置口令被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, &p.LoginName, "reset_password", p.ID.String(), audit.Deny)
		return Account{}, err
	}
	// 旧会话清不掉，新口令仍可能被旧会话用
	if err := s.store.DeleteSessionsForPerson(ctx, p.ID); err != nil {
		return Account{}, err
	}
	// 按稳定身份读人员，改名也不变
	p, err = s.store.PersonByID(ctx, p.ID)
	// 人员不存在，拒绝这次变更
	if err != nil {
		return Account{}, err
	}
	// 审计没写下则整次不算完成
	if err := s.audit(ctx, &acc.ID, &p.LoginName, "reset_password", p.ID.String(), audit.Allow); err != nil {
		return Account{}, err
	}
	// 收成不含口令的对外账号，再交回调用方
	return accountOf(p), nil
}

// EnableAccount 恢复已停用账号；有日常密码的回到有效，否则回到待启用。不删账号。
func (s *Auth) EnableAccount(ctx context.Context, token string, targetID uuid.UUID) error {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return err
	}
	// 只有工厂超管能恢复账号。失败一律记拒绝。
	if err := s.can(ctx, acc, permManageAccount, nil); err != nil {
		// 不是工厂超管，记下恢复账号被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "enable_account", targetID.String(), audit.Deny)
		return err
	}
	// 按稳定身份读人员，改名也不变
	p, err := s.store.PersonByID(ctx, targetID)
	// 人员不存在，拒绝这次变更
	if err != nil {
		// 记下恢复账号被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "enable_account", targetID.String(), audit.Deny)
		return err
	}
	// 不是停用状态就不必恢复，记一笔允许即可
	if p.Status != StatusDisabled {
		// 恢复账号成功后记审计，再把结果交回
		return s.audit(ctx, &acc.ID, nil, "enable_account", targetID.String(), audit.Allow)
	}
	// 没有日常口令就回到待启用，不能直接登录
	next := StatusPending
	// 已经有日常口令则恢复为可登录，否则仍待启用
	if p.PasswordHash != nil && *p.PasswordHash != "" {
		// 已有日常口令则直接恢复为可登录
		next = StatusActive
	}
	// 有日常密码的回到有效，否则回到待启用。
	if err := s.store.SetPersonStatus(ctx, targetID, next); err != nil {
		// 记下恢复账号被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "enable_account", targetID.String(), audit.Deny)
		return err
	}
	// 恢复账号成功后记审计，再把结果交回
	return s.audit(ctx, &acc.ID, nil, "enable_account", targetID.String(), audit.Allow)
}
