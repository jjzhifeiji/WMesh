// Package domain 放各阶段共用的业务错误。调用方用 errors.Is 判断，不解析 SQL。
package domain

import "errors"

var (
	ErrWANAdminExists      = errors.New("wan admin already exists")          // 已有唯一 WAN 管理员
	ErrInitialSAExists     = errors.New("initial super admin already bound") // 该厂已下发过初始超管
	ErrLoginNameTaken      = errors.New("login name already taken")          // 登录名只在本厂唯一
	ErrNotFound            = errors.New("not found")                            // 没有这条记录
	ErrReferenced          = errors.New("still referenced")                     // 已认领工厂不能从名录物理删除
	ErrCycle               = errors.New("org unit parent would create a cycle") // 挂到自己或后代
	ErrDisabledOrgType     = errors.New("org type is disabled")                 // 停用后不能再开该类型的节点
	ErrDisabledOrgUnit     = errors.New("org unit is disabled") // 不能再分配或当新工作上下文
	ErrHasActiveUnits      = errors.New("org type still has active units")      // 还有有效节点，不能停用该类型
	ErrHasActiveChildren   = errors.New("org unit still has active children")   // 还有有效下级，不能停用
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
	ErrWorkContext         = errors.New("invalid work context")       // 没选、选了未分配节点、或直属与节点同时选
	ErrFactoryBootstrap    = errors.New("factory bootstrap failed")   // 厂端引导不可达或拒绝；安装期入口仍可用
	ErrInvalidEnrollment   = errors.New("invalid enrollment")         // 建厂码不对或已用完
	ErrFactoryDisabled     = errors.New("factory is disabled")        // 停用后不得认领、登录或新开操作
	ErrFactoryRetired      = errors.New("factory is retired")         // 已注销，不能再启用
	ErrFactoryOffline      = errors.New("factory channel is offline") // 厂端通道不在线，不能拉升档
	ErrInvalidName         = errors.New("invalid name")               // 显示名空了或太长
	ErrDuplicateName       = errors.New("name already exists in this folder") // 同一文件夹下名字不能重复
	ErrInvalidKey          = errors.New("invalid key material")       // 公钥/私钥长度不对
	ErrClientKeyTaken      = errors.New("client public key already registered") // 该公钥已被别的 Client 占用
	ErrFactoryKeyExists    = errors.New("factory public key already registered") // 该厂公钥已登记，不能改绑
	ErrSigningKeyExists    = errors.New("wan signing key already set")           // 云端一把签发钥，不能换
	ErrClientBound           = errors.New("client already bound to a factory")    // 已属一厂，不能再绑到另一厂
	ErrUnbound               = errors.New("client is not bound")                  // 未绑定不能改绑
	ErrDeviceSerialRequired  = errors.New("device serial is required")            // 登记必须填写机械臂识别号
	ErrDeviceSerialTaken     = errors.New("device serial already bound")          // 该识别号已登记在另一台设备
	ErrStaleRevision       = errors.New("revision is not strictly newer")         // 只接受更高软件版本或节点修订
	ErrRevisionConflict    = errors.New("revision does not match")                // 资产期望修订对不上
	ErrIntegrity           = errors.New("asset integrity check failed")           // 正文与摘要不一致或摘要长度不对
	ErrContentLeaseExpired = errors.New("content lease expired")                    // 厂侧解包租约到期或尚未签发
	ErrAssetNotAvailable   = errors.New("asset is not available")                 // 草稿或停用，不能当可用资产
	ErrAssetNotCopyable    = errors.New("asset is not copyable")                  // 不可复制不得升档
	ErrAssetDependency     = errors.New("asset dependency missing or mismatched") // 工程依赖缺失或错配
	ErrInvalidWeldKind     = errors.New("invalid weld kind")                      // 作业类型只能是单层/多层/T排
	ErrWeldKindMismatch    = errors.New("weld kind mismatch")                     // 工程与工艺作业类型必须相同
	ErrClosureIncomplete   = errors.New("closure is incomplete")                  // 组包缺成员或读不到钉死修订
	ErrClosureMismatch     = errors.New("closure revision mismatch")              // 组包串版：身份或修订被顶替
	ErrClientCacheFull = errors.New("client cache is full")        // 工程份已达缓存上限
	ErrTemplateInvalid     = errors.New("content template is invalid")    // 字段表不合法
	ErrAssetCodeMissing    = errors.New("asset origin code is not assigned") // 未齐创建端短码，不得新建
	ErrAssetCodeConflict   = errors.New("asset code already exists")       // 同号不同身份，或同身份编号不一致
	ErrOriginCodeExhausted   = errors.New("origin code exhausted")            // 短码用尽或编号序号溢出
	ErrSoftwareInstallFailed = errors.New("software install failed")         // 确认后落地失败，旧版本继续跑
)
