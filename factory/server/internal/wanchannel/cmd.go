package wanchannel

import "encoding/json"

const (
	CmdClosure       = "closure"
	CmdTemplate      = "template"
	CmdSoftware      = "software"
	CmdRetract       = "retract"
	CmdClientBind    = "client_bind"
	CmdClientVoid    = "client_void"
	CmdFactoryState  = "factory_state"
	CmdLease         = "lease"
	CmdLeaseRenew    = "lease_renew"
	CmdAssetList     = "asset_list"
	CmdAssetSnapshot = "asset_snapshot"
	CmdAssetListOK   = "asset_list_ok"
	CmdAssetSnapOK   = "asset_snapshot_ok"
)

// Cmd 与 WAN 控制面指令对齐，不含闭包/软件正文。
type Cmd struct {
	Typ             string          `json:"typ"`                       // 指令类型
	ReqID           string          `json:"reqId,omitempty"`          // 问询与回执配对
	AssetID         string          `json:"assetId,omitempty"`        // 平台级或撤回身份
	TemplateID      string          `json:"templateId,omitempty"`      // 内容模版身份
	Revision        int64           `json:"revision,omitempty"`       // 资产/治理修订
	Kind            string          `json:"kind,omitempty"`            // process / project / 软件种类
	Version         int64           `json:"version,omitempty"`         // 软件单调整数
	Status          string          `json:"status,omitempty"`         // 工厂治理状态
	ShortCode       string          `json:"shortCode,omitempty"`      // 本厂短码
	ClientID        string          `json:"clientId,omitempty"`        // 现场设备身份
	ClientName      string          `json:"clientName,omitempty"`      // 设备显示名
	ClientShortCode string          `json:"clientShortCode,omitempty"` // Client 短码
	PublicKey       []byte          `json:"publicKey,omitempty"`       // 设备公钥，可空
	BindingRevision int64           `json:"bindingRevision,omitempty"` // 绑定修订
	Lease           []byte          `json:"lease,omitempty"`           // 租约钥 L
	NotAfter        string          `json:"notAfter,omitempty"`       // 租约到期 RFC3339
	Assets          json.RawMessage `json:"assets,omitempty"`        // 升档清单 JSON
	Snapshot        json.RawMessage `json:"snapshot,omitempty"`        // 升档快照 JSON
	Error           string          `json:"error,omitempty"`           // 英文业务错误
}
