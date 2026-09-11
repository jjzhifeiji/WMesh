// Package domain 放各阶段共用的业务错误。调用方用 errors.Is 判断，不解析 SQL。
package domain

import "errors"

var (
	ErrWANAdminExists      = errors.New("wan admin already exists")          // 已有唯一 WAN 管理员
	ErrInitialSAExists     = errors.New("initial super admin already bound") // 该厂已下发过初始超管
	ErrLoginNameTaken      = errors.New("login name already taken")          // 登录名只在本厂唯一
	ErrNotFound            = errors.New("not found")
	ErrCycle               = errors.New("org unit parent would create a cycle") // 挂到自己或后代
	ErrDisabledOrgUnit     = errors.New("org unit is disabled")                 // 不能再分配或当新工作上下文
	ErrHasActiveChildren   = errors.New("org unit still has active children")
	ErrReferenced          = errors.New("still referenced") // 还有下级、当前人员、有效角色或历史事实，不能物理删除
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
	ErrInvalidKey          = errors.New("invalid key material") // 公钥/私钥或签名长度不对
	ErrSigningKeyExists    = errors.New("factory signing key already set")
	ErrClientKeyMismatch   = errors.New("client public key does not match")
	ErrStaleRevision       = errors.New("revision is not strictly newer")         // 只接受更高修订
	ErrBindingVoid         = errors.New("client binding is void")                 // 作废后不得再签发
	ErrRevisionConflict    = errors.New("revision does not match")                // 资产期望修订对不上
	ErrIntegrity           = errors.New("asset integrity check failed")           // 正文与摘要不一致或摘要长度不对
	ErrAssetNotAvailable   = errors.New("asset is not available")                 // 草稿或停用，不能当可用资产
	ErrAssetNotCopyable    = errors.New("asset is not copyable")                  // 不可复制不得升档
	ErrAssetDependency     = errors.New("asset dependency missing or mismatched") // 工程依赖缺失或错配
	ErrClosureIncomplete   = errors.New("closure is incomplete")                  // 组包缺成员或读不到钉死修订
	ErrClosureMismatch     = errors.New("closure revision mismatch")              // 组包串版：身份或修订被顶替
	ErrClientCacheFull     = errors.New("client cache is full")                   // 工程份已达缓存上限
	ErrSyncRetry           = errors.New("sync retry required")                // 弱网汇聚失败，队列保留可再试
)
