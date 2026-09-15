// Package domain 放各阶段共用的业务错误。调用方用 errors.Is 判断，不解析 SQL。
package domain

import "errors"

var (
	ErrWANAdminExists      = errors.New("wan admin already exists")          // 已有唯一 WAN 管理员
	ErrInitialSAExists     = errors.New("initial super admin already bound") // 该厂已下发过初始超管
	ErrLoginNameTaken      = errors.New("login name already taken")          // 登录名只在本厂唯一
	ErrNotFound            = errors.New("not found")                            // 没有这条记录
	ErrCycle               = errors.New("org unit parent would create a cycle") // 挂到自己或后代
	ErrDisabledOrgUnit     = errors.New("org unit is disabled")                 // 不能再分配或当新工作上下文
	ErrHasActiveChildren   = errors.New("org unit still has active children")   // 还有有效下级，不能停用
	ErrReferenced          = errors.New("still referenced") // 还有下级、当前人员、有效角色或历史事实，不能物理删除
	ErrDuplicateAssignment = errors.New("active assignment already exists")     // 同一人同一节点已有有效分配
	ErrInvalidRoleScope    = errors.New("role and scope do not match") // 角色与允许的作用域不一致
	ErrDuplicateRoleGrant  = errors.New("active role grant already exists")     // 同一人同一角色作用域已有有效授予
	ErrDuplicateSession    = errors.New("session token hash already exists")    // 会话令牌哈希已占用
	ErrUnauthorized        = errors.New("unauthorized") // 无会话或会话已失效
	ErrForbidden           = errors.New("forbidden")    // 已登录但没有这份许可
	ErrInvalidCredentials  = errors.New("invalid credentials")                  // 登录名或密码不对
	ErrInvalidActivation   = errors.New("invalid activation")                   // 激活码不对或已失效
	ErrAlreadyActivated    = errors.New("already activated")                    // 已经激活过，不能再用激活码
	ErrAccountPending      = errors.New("account pending")  // 待启用不能登录
	ErrAccountDisabled     = errors.New("account disabled") // 已停用不能登录或新开操作
	ErrSessionExpired      = errors.New("session expired")                      // 会话过期，须重新登录
	ErrLastAdmin           = errors.New("last factory super admin") // 会把有效厂级超管入口变成零
	ErrMultiParent         = errors.New("org unit cannot have two parents") // 树必须保持单父
	ErrWorkContext         = errors.New("invalid work context") // 没选、选了未分配节点、或直属与节点同时选
	ErrInvalidName         = errors.New("invalid name")         // 显示名空了或太长
	ErrInvalidKey          = errors.New("invalid key material") // 公钥/私钥或签名长度不对
	ErrSigningKeyExists    = errors.New("factory signing key already set")  // 一厂一把签发钥，不能换
	ErrClientKeyMismatch   = errors.New("client public key does not match") // 已登记公钥与本次不一致
	ErrStaleRevision       = errors.New("revision is not strictly newer")         // 只接受更高修订
	ErrBindingVoid         = errors.New("client binding is void")                 // 作废后不得再签发
	ErrRevisionConflict    = errors.New("revision does not match")                // 资产期望修订对不上
	ErrIntegrity           = errors.New("asset integrity check failed")           // 正文与摘要不一致或摘要长度不对
	ErrContentLeaseExpired = errors.New("content lease expired")                    // 厂侧解包租约到期或尚未签发
	ErrAssetNotAvailable   = errors.New("asset is not available")                 // 草稿或停用，不能当可用资产
	ErrAssetNotCopyable    = errors.New("asset is not copyable")                  // 不可复制不得升档
	ErrAssetDependency     = errors.New("asset dependency missing or mismatched") // 工程依赖缺失或错配
	ErrClosureIncomplete   = errors.New("closure is incomplete")                  // 组包缺成员或读不到钉死修订
	ErrClosureMismatch     = errors.New("closure revision mismatch")              // 组包串版：身份或修订被顶替
	ErrClientCacheFull     = errors.New("client cache is full")                   // 工程份已达缓存上限
	ErrSyncRetry           = errors.New("sync retry required")                    // 弱网汇聚失败，队列保留可再试
	ErrInvalidEnrollment   = errors.New("invalid enrollment")                     // 建厂码不对或已用完
	ErrWANUnreachable      = errors.New("wan channel unreachable")                // 厂端连不上 WAN 通道
	ErrFactoryDisabled     = errors.New("factory is disabled")                    // WAN 停用后不得登录或新开操作
	ErrFactoryRetired  = errors.New("factory is retired")          // 已注销，通道不再重连
	ErrTemplateInvalid     = errors.New("content template is invalid")    // 字段表不合法
	ErrAssetCodeMissing    = errors.New("asset origin code is not assigned") // 未齐创建端短码，不得新建
	ErrAssetCodeConflict   = errors.New("asset code already exists")       // 同号不同身份，或同身份编号不一致
	ErrOriginCodeExhausted = errors.New("origin code exhausted")            // 短码用尽或编号序号溢出
	ErrSoftwareInstallFailed = errors.New("software install failed")        // 确认后安装失败，旧版本继续跑
)
