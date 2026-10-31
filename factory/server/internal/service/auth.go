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
	"wmesh/factory/internal/store"
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

// AcceptEnrollment 按 WAN 约定身份写入初始超管并当场自设密码。
// 只有 Hub.Claim 在 WAN 刚认可建厂码后才调；WAN 只在确认成功时清码且不重签，
// 所以超管已有效时一定是同一次没确认完的认领：口令一致按幂等成功，
// 不一致就覆盖成这次的新口令并作废旧会话，不另设建厂码失效的分支。
func (s *Auth) AcceptEnrollment(ctx context.Context, personID uuid.UUID, loginName, displayName, password string) (Account, error) {
	// 没带口令则拒绝，不能把空口令当成已激活
	if password == "" {
		return Account{}, domain.ErrInvalidActivation
	}
	// 先去首尾空白再限长，库里去过空白的超管重试也对得上
	loginName, err := normalizeLoginName(loginName)
	// 名字空了或太长，拒绝建厂
	if err != nil {
		// 记下按约定建厂被拒绝，写失败不改变结果
		_ = s.audit(ctx, nil, nil, "enroll_factory", personID.String(), audit.Deny)
		return Account{}, err
	}
	// 自设日常密码只存哈希，新建和重试都用这一份
	hash, err := secret.HashPassword(password)
	// 口令摘要失败，拒绝落库
	if err != nil {
		return Account{}, err
	}
	// 身份须对上；尚无档则按约定写入。失败一律记拒绝。
	p, err := s.store.PersonByID(ctx, personID)
	// 记下这次是不是新建的
	created := false
	// 还没有这个人，就按约定身份新建
	if errors.Is(err, domain.ErrNotFound) {
		// 走新建这一支
		created = true
		// 按约定身份写入初始超管，当场有效
		p, err = s.store.CreatePersonAt(ctx, personID, loginName, displayName, hash, true)
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
	if _, err := s.store.GrantRole(ctx, p.ID, RoleFactorySuperAdmin); err != nil && !errors.Is(err, domain.ErrDuplicateRoleGrant) {
		// 记下按约定建厂被拒绝，写失败不改变结果
		_ = s.audit(ctx, &p.ID, &loginName, "enroll_factory", p.ID.String(), audit.Deny)
		return Account{}, err
	}
	// 刚按约定新建的，记审计后交回
	if created {
		// 新建成功后记审计，再把结果交回
		return accountOf(p), s.audit(ctx, &p.ID, &loginName, "enroll_factory", p.ID.String(), audit.Allow)
	}
	// 停用的初始超管不靠认领复活
	if p.Status != StatusActive {
		// 记下按约定建厂被拒绝，写失败不改变结果
		_ = s.audit(ctx, &p.ID, &loginName, "enroll_factory", p.ID.String(), audit.Deny)
		return Account{}, domain.ErrAccountDisabled
	}
	// 已经有效说明是认领重试；口令和库里一致，按同一次重试幂等交回
	if p.PasswordHash != nil && secret.VerifyPassword(*p.PasswordHash, password) {
		// 收成不含口令的对外账号，再交回调用方
		return accountOf(p), nil
	}
	// 这次换了口令，仍是同一次认领，只存新哈希；换口令和清旧会话同一事务，免得旧口令开的会话留下
	if err := s.store.ReplacePasswordDropSessions(ctx, p.ID, hash); err != nil {
		// 记下按约定建厂被拒绝，写失败不改变结果
		_ = s.audit(ctx, &p.ID, &loginName, "enroll_factory", p.ID.String(), audit.Deny)
		return Account{}, err
	}
	// 换口令成功后记审计，再把结果交回
	return accountOf(p), s.audit(ctx, &p.ID, &loginName, "enroll_factory", p.ID.String(), audit.Allow)
}

// Login 只接受本厂有效账号；待启用、已停用或密码错误都拒绝，审计不记秘密。
func (s *Auth) Login(ctx context.Context, loginName, password string) (string, error) {
	// 去掉登录名首尾空白，和示教器登录一样认人
	loginName = strings.TrimSpace(loginName)
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
	// 不是有效状态则拒绝，待启用和停用都不能做
	if p.Status != StatusActive {
		// 记下续接会话被拒绝，写失败不改变结果
		_ = s.audit(ctx, &p.ID, &p.LoginName, "protect", p.ID.String(), audit.Deny)
		return Account{}, domain.ErrAccountDisabled
	}
	// 收成不含口令的对外账号，再交回调用方
	return accountOf(p), nil
}

// ChangePassword 改日常密码并作废本人其它会话；网页会话须核对当前密码，审计不写原文。
// 示教器暂时没有旧密码框，app 会话不带当前密码也放行，带了照样核对。
func (s *Auth) ChangePassword(ctx context.Context, token, currentPassword, newPassword string) error {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return err
	}
	// 再取当前会话，分清是网页还是示教器
	sess, err := s.store.SessionByTokenHash(ctx, secret.TokenHash(token))
	// 刚核过的会话又对不上，按未登录拒绝
	if err != nil {
		return domain.ErrUnauthorized
	}
	// 按稳定身份读人员，拿到现有口令哈希
	p, err := s.store.PersonByID(ctx, acc.ID)
	// 人员读不到，拒绝这次改密
	if err != nil {
		return err
	}
	// 网页会话必须核旧密码；示教器带了才核
	if sess.Kind != store.SessionKindApp || currentPassword != "" {
		// 没有口令或当前密码不对，捡到的会话不能接管账号
		if p.PasswordHash == nil || !secret.VerifyPassword(*p.PasswordHash, currentPassword) {
			// 记下改密被拒绝，写失败不改变结果
			_ = s.audit(ctx, &acc.ID, &acc.LoginName, "change_password", acc.ID.String(), audit.Deny)
			return domain.ErrWrongCurrentPassword
		}
	}
	// 日常密码只存哈希，不写原文。
	hash, err := secret.HashPassword(newPassword)
	// 口令摘要失败，拒绝落库
	if err != nil {
		return err
	}
	// 换口令并作废其它会话同一事务，泄露的旧令牌随之失效
	if err := s.store.ReplacePasswordKeepSession(ctx, acc.ID, hash, secret.TokenHash(token)); err != nil {
		return err
	}
	// 改口令成功后记审计，再把结果交回
	return s.audit(ctx, &acc.ID, &acc.LoginName, "change_password", acc.ID.String(), audit.Allow)
}

// SetMyDisplayName 本人改自己的显示名；登录名和稳定身份不变。
func (s *Auth) SetMyDisplayName(ctx context.Context, token, displayName string) (Account, error) {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return Account{}, err
	}
	// 落库并交回改后的账号
	return s.renameDisplay(ctx, acc, acc.ID, displayName)
}

// DisableAccount 停用本厂账号并作废其全部会话；若会去掉最后一名有效厂级超管则拒绝。
func (s *Auth) DisableAccount(ctx context.Context, token string, targetID uuid.UUID) error {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return err
	}
	// 只有工厂超管能停用；最后一名有效超管入口不能停。失败一律记拒绝。
	if err := s.can(ctx, acc, permManageAccount); err != nil {
		// 不是工厂超管，记下停用账号被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "disable_account", targetID.String(), audit.Deny)
		return err
	}
	// 激活后必须至少留一个「有效账号 + 厂级超管角色」；这里先快速预检。
	if err := s.guardLastAdminOnDisable(ctx, targetID); err != nil {
		// 会去掉最后一名有效超管，记下停用账号被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "disable_account", targetID.String(), audit.Deny)
		return err
	}
	// 拿入口锁重数后改停用并清全部会话，两名超管互停也只成一个。
	if err := s.store.DisablePersonKeepAdmin(ctx, targetID); err != nil {
		// 停用失败或会去掉最后一名超管，记下停用账号被拒绝，写失败不改变结果
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
	if err := s.can(ctx, acc, permManageAccount); err != nil {
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
	if err := s.can(ctx, acc, permManageAccount); err != nil {
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
	// 没有日常口令就补上默认口令，恢复后照常能登录
	if p.PasswordHash == nil || *p.PasswordHash == "" {
		// 默认口令只存哈希
		hash, err := secret.HashPassword(secret.DefaultPersonPassword(p.LoginName))
		// 口令摘要失败，拒绝恢复
		if err != nil {
			return err
		}
		// 补口令并转有效
		if err := s.store.ApplyPersonPassword(ctx, targetID, hash, StatusActive); err != nil {
			// 记下恢复账号被拒绝，写失败不改变结果
			_ = s.audit(ctx, &acc.ID, nil, "enable_account", targetID.String(), audit.Deny)
			return err
		}
		// 恢复账号成功后记审计，再把结果交回
		return s.audit(ctx, &acc.ID, nil, "enable_account", targetID.String(), audit.Allow)
	}
	// 已有日常口令则直接恢复为可登录
	if err := s.store.SetPersonStatus(ctx, targetID, StatusActive); err != nil {
		// 记下恢复账号被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "enable_account", targetID.String(), audit.Deny)
		return err
	}
	// 恢复账号成功后记审计，再把结果交回
	return s.audit(ctx, &acc.ID, nil, "enable_account", targetID.String(), audit.Allow)
}
