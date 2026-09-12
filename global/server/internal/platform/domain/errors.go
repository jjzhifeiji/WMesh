// Package domain 放各阶段共用的业务错误。调用方用 errors.Is 判断，不解析 SQL。
package domain

import "errors"

var (
	ErrWANAdminExists      = errors.New("wan admin already exists")          // 已有唯一 WAN 管理员
	ErrInitialSAExists     = errors.New("initial super admin already bound") // 该厂已下发过初始超管
	ErrLoginNameTaken      = errors.New("login name already taken")          // 登录名只在本厂唯一
	ErrNotFound            = errors.New("not found")
	ErrReferenced          = errors.New("still referenced")                     // 已认领工厂不能从名录物理删除
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
	ErrWorkContext         = errors.New("invalid work context")       // 没选、选了未分配节点、或直属与节点同时选
	ErrFactoryBootstrap    = errors.New("factory bootstrap failed")   // 厂端引导不可达或拒绝；安装期入口仍可用
	ErrInvalidEnrollment   = errors.New("invalid enrollment")         // 建厂码不对或已用完
	ErrFactoryDisabled     = errors.New("factory is disabled")        // 停用后不得认领、登录或新开操作
	ErrFactoryRetired      = errors.New("factory is retired")         // 已注销，不能再启用
	ErrFactoryOffline      = errors.New("factory channel is offline") // 厂端通道不在线，不能拉升档
	ErrInvalidName         = errors.New("invalid name")               // 显示名空了或太长
	ErrInvalidKey          = errors.New("invalid key material")       // 公钥/私钥长度不对
	ErrClientKeyTaken      = errors.New("client public key already registered")
	ErrFactoryKeyExists    = errors.New("factory public key already registered")
	ErrClientBound         = errors.New("client already bound to a factory")      // 已属一厂，不能再绑到另一厂
	ErrUnbound             = errors.New("client is not bound")                    // 未绑定不能改绑
	ErrRevisionConflict    = errors.New("revision does not match")                // 资产期望修订对不上
	ErrIntegrity           = errors.New("asset integrity check failed")           // 正文与摘要不一致或摘要长度不对
	ErrAssetNotAvailable   = errors.New("asset is not available")                 // 草稿或停用，不能当可用资产
	ErrAssetNotCopyable    = errors.New("asset is not copyable")                  // 不可复制不得升档
	ErrAssetDependency     = errors.New("asset dependency missing or mismatched") // 工程依赖缺失或错配
	ErrClosureIncomplete   = errors.New("closure is incomplete")                  // 组包缺成员或读不到钉死修订
	ErrClosureMismatch     = errors.New("closure revision mismatch")              // 组包串版：身份或修订被顶替
	ErrClientCacheFull     = errors.New("client cache is full")                   // 工程份已达缓存上限
	ErrTemplateInvalid     = errors.New("content template is invalid")            // 字段表不合法
)
