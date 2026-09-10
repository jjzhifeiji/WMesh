package service

import "wmesh/global/internal/store"

// 与 store 同源的表模型，给 WAN 应用服务和验收当入口类型用。
type (
	Store             = store.Store
	Admin             = store.Admin
	Factory           = store.Factory
	InitialSuperAdmin = store.InitialSuperAdmin
	Session           = store.Session
)
