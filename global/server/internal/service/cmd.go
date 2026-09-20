package service

import "encoding/json"

const (
	CmdClosure       = "closure"           // 去拉密封闭包
	CmdTemplate      = "template"          // 去拉内容模版
	CmdRetract       = "retract"           // 撤回已删平台级
	CmdClientBind    = "client_bind"       // 接受设备绑定
	CmdClientVoid    = "client_void"       // 作废本厂绑定
	CmdFactoryState  = "factory_state"     // 治理状态只向前
	CmdLease         = "lease"             // 内容租约钥
	CmdLeaseRenew    = "lease_renew"       // 厂端申请续租
	CmdPresence      = "presence"          // 厂端自报正在跑的版本
	CmdAssetList     = "asset_list"        // 问升档清单
	CmdAssetSnapshot = "asset_snapshot"    // 问升档快照
	CmdAssetListOK   = "asset_list_ok"     // 升档清单回执
	CmdAssetSnapOK   = "asset_snapshot_ok" // 升档快照回执
	CmdSoftware      = "software"          // 厂服务包或 APK 已发布，厂去拉正文
)

// Cmd 是厂↔WAN 控制面指令，不含闭包或软件包正文。
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
