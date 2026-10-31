package wanchannel

// ClientLoginUp 是随保活上报给 WAN 的某台设备最近一次示教器登录。
type ClientLoginUp struct {
	ClientID       string `json:"clientId"`                 // 设备稳定身份
	At             string `json:"at"`                       // 登录时间 RFC3339
	AppVersion     int64  `json:"appVersion,omitempty"`     // 示教器版本号；没报为 0
	AppVersionName string `json:"appVersionName,omitempty"` // 示教器版本名
}

// HoldOption 给 Hold 挂可选的回调，不传就照旧只连通道。
type HoldOption func(*holdOpts)

// holdOpts 是 Hold 的可选回调集合。
type holdOpts struct {
	status func(connected bool)   // 通道连上、断开时通知外层；空则不报
	logins func() []ClientLoginUp // 每次保活前取各台最近登录；空则不带
}

// WithStatus 通道连上并报过在场时回调 true，Hold 离开时回调 false。
func WithStatus(fn func(connected bool)) HoldOption {
	// 交回一个把状态回调写进集合的选项。
	return func(o *holdOpts) {
		// 记下回调，Hold 连上和离开时调它。
		o.status = fn
	}
}

// WithClientLogins 每次保活前调 fn 取各台最近登录，随在场一起报给 WAN。
func WithClientLogins(fn func() []ClientLoginUp) HoldOption {
	// 交回一个把取数函数写进集合的选项。
	return func(o *holdOpts) {
		// 记下取数函数，保活时调它。
		o.logins = fn
	}
}

// presence 组一条在场回报；挂了取数函数就带上各台最近登录。
func (o holdOpts) presence() Cmd {
	// 先组只带版本的在场回报。
	cmd := presenceCmd()
	// 挂了取数函数才去取，取不到就只报版本。
	if o.logins != nil {
		// 带上各台最近登录，WAN 设备名录要看。
		cmd.ClientLogins = o.logins()
	}
	return cmd
}
