package wanchannel

import "encoding/json"

const (
	CmdClosure       = "closure"           // 组包下发，正文另走拉取
	CmdTemplate      = "template"          // 内容模版下发，正文另走拉取
	CmdRetract       = "retract"           // 平台删除撤回，只带资产身份
	CmdClientBind    = "client_bind"       // 把设备分配或改分到本厂
	CmdClientVoid    = "client_void"       // 作废已经分配到本厂的设备
	CmdFactoryState  = "factory_state"     // 工厂启用、停用或注销
	CmdLease         = "lease"             // 下发内容解包钥，只进内存
	CmdLeaseRenew    = "lease_renew"       // 到期前续上内容解包钥
	CmdPresence      = "presence"          // 上报本进程在线时的版本
	CmdAssetList     = "asset_list"        // 询问本厂可升档的清单
	CmdAssetSnapshot = "asset_snapshot"    // 询问一笔升档快照正文
	CmdFSList        = "fs_list"           // 询问本厂当前平台目录
	CmdFSApply       = "fs_apply"          // 对齐平台目录
	CmdAssetListOK   = "asset_list_ok"     // 升档清单已经答回
	CmdAssetSnapOK   = "asset_snapshot_ok" // 升档快照已经答回
	CmdFSListOK      = "fs_list_ok"        // 平台目录已经答回
	CmdSoftware      = "software"          // 厂服务包或 APK 已发布，厂去拉正文
)

// Cmd 与 WAN 控制面指令对齐，不含闭包或软件包正文。
type Cmd struct {
	Typ                string          `json:"typ"`                          // 指令类型
	ReqID              string          `json:"reqId,omitempty"`              // 问询与回执配对
	AssetID            string          `json:"assetId,omitempty"`            // 平台级或撤回身份
	TemplateID         string          `json:"templateId,omitempty"`         // 内容模版身份
	Revision           int64           `json:"revision,omitempty"`           // 资产/治理修订
	Kind               string          `json:"kind,omitempty"`               // process / project / 软件种类
	Version            int64           `json:"version,omitempty"`            // 软件单调整数
	VersionName        string          `json:"versionName,omitempty"`        // 给人看的软件版本名
	WebVersion         int64           `json:"webVersion,omitempty"`         // 厂端前端版本号
	WebVersionName     string          `json:"webVersionName,omitempty"`     // 厂端前端版本名
	ServiceVersion     int64           `json:"serviceVersion,omitempty"`     // 厂端服务版本号
	ServiceVersionName string          `json:"serviceVersionName,omitempty"` // 厂端服务版本名
	Status             string          `json:"status,omitempty"`             // 工厂治理状态
	ShortCode          string          `json:"shortCode,omitempty"`          // 本厂短码
	FactoryName        string          `json:"factoryName,omitempty"`        // 本厂显示名
	ClientID           string          `json:"clientId,omitempty"`           // 现场设备身份
	ClientName         string          `json:"clientName,omitempty"`         // 设备显示名
	ClientShortCode    string          `json:"clientShortCode,omitempty"`    // Client 短码
	DeviceSerial       string          `json:"deviceSerial,omitempty"`       // 机械臂识别号；未填为空
	PublicKey          []byte          `json:"publicKey,omitempty"`          // 设备公钥，可空
	BindingRevision    int64           `json:"bindingRevision,omitempty"`    // 绑定修订
	Lease              []byte          `json:"lease,omitempty"`              // 租约钥 L
	NotAfter           string          `json:"notAfter,omitempty"`           // 租约到期 RFC3339
	Assets             json.RawMessage `json:"assets,omitempty"`             // 升档清单 JSON
	Snapshot           json.RawMessage `json:"snapshot,omitempty"`           // 升档快照 JSON
	Error              string          `json:"error,omitempty"`              // 英文业务错误
}
