// Package domain 放各阶段共用的业务错误。调用方用 errors.Is 判断，不解析 SQL。
package domain

import "errors"

var (
	ErrWANAdminExists      = errors.New("wan admin already exists")          // 已有唯一 WAN 管理员
	ErrInitialSAExists     = errors.New("initial super admin already bound") // 该厂已下发过初始超管
	ErrLoginNameTaken      = errors.New("login name already taken")          // 登录名只在本厂唯一
	ErrNotFound            = errors.New("not found")
	ErrCycle               = errors.New("org unit parent would create a cycle") // 挂到自己或后代
	ErrDisabledOrgType     = errors.New("org type is disabled")
	ErrDisabledOrgUnit     = errors.New("org unit is disabled") // 不能再分配或当新工作上下文
	ErrHasActiveUnits      = errors.New("org type still has active units")
	ErrHasActiveChildren   = errors.New("org unit still has active children")
	ErrDuplicateAssignment = errors.New("active assignment already exists")
	ErrInvalidRoleScope    = errors.New("role and scope do not match") // 角色与允许的作用域不一致
	ErrDuplicateRoleGrant  = errors.New("active role grant already exists")
	ErrDuplicateSession    = errors.New("session token hash already exists")
	ErrUnauthorized        = errors.New("unauthorized") // 无会话或会话已失效
	ErrForbidden           = errors.New("forbidden")    // 已登录但没有这份许可
	ErrInvalidCredentials  = errors.New("invalid credentials")
	ErrInvalidActivation   = errors.New("invalid activation")
	ErrAlreadyActivated    = errors.New("already activated")
	ErrAccountPending      = errors.New("account pending")  // 待启用不能登录
	ErrAccountDisabled     = errors.New("account disabled") // 已停用不能登录或新开操作
	ErrSessionExpired      = errors.New("session expired")
	ErrLastAdmin           = errors.New("last factory super admin") // 会把有效厂级超管入口变成零
	ErrMultiParent         = errors.New("org unit cannot have two parents")
	ErrWorkContext         = errors.New("invalid work context") // 没选、选了未分配节点、或直属与节点同时选
)
