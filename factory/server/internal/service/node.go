package service

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/nodekey"
)

// normalizeClientName 去掉首尾空白后须 1～64 字。
func normalizeClientName(name string) (string, error) {
	// 去掉首尾空白后再做判断
	name = strings.TrimSpace(name)
	// 按字数限制名字长度
	n := utf8.RuneCountInString(name)
	// 名字空了或超过六十四字，拒绝保存
	if n < 1 || n > 64 {
		return "", domain.ErrInvalidName
	}
	return name, nil
}

// AcceptBinding 把 WAN 送到本厂的节点落到本库；测试夹具与通道共用。
func (s *Node) AcceptBinding(ctx context.Context, clientID uuid.UUID, name string, publicKey []byte, revision int64) (Client, error) {
	// 名字或识别号为空则拒绝或去名录补
	if strings.TrimSpace(name) != "" {
		// 先留空错误，生成钥失败再填上
		var err error
		// 去掉空白并限制名字长度
		name, err = normalizeClientName(name)
		// 名字空或太长，拒绝改名
		if err != nil {
			// 记下登记绑定被拒绝，写失败不改变结果
			_ = s.audit(ctx, nil, nil, "accept_binding", clientID.String(), audit.Deny)
			return Client{}, err
		}
	}
	// 把 WAN 送到本厂的绑定落到本库；修订只向前。
	row, err := s.store.AcceptBinding(ctx, clientID, name, publicKey, revision)
	// 落库失败则本厂仍不认这台
	if err != nil {
		// 记下登记绑定被拒绝，写失败不改变结果
		_ = s.audit(ctx, nil, nil, "accept_binding", clientID.String(), audit.Deny)
		return Client{}, err
	}
	// 登记绑定成功后记审计，再把结果交回
	return row, s.audit(ctx, nil, nil, "accept_binding", clientID.String(), audit.Allow)
}

// WANBinding 是 WAN 经通道推来的一条设备分配，补拉和实时下行共用。
type WANBinding struct {
	ClientID     uuid.UUID // 固定识别号，与 WAN 相同的全局身份
	Name         string    // 给人看的设备名；空则不改本地名
	PublicKey    []byte    // 本机公钥，可空
	Revision     int64     // 绑定修订，只向前
	ShortCode    string    // Client 短码；空则不动已有短码
	DeviceSerial string    // WAN 登记的机械臂号；空则不补
}

// ApplyWANBinding 落绑定、写短码、补臂号；单台业务冲突只记日志跳过，库错才交回重连。
func (s *Node) ApplyWANBinding(ctx context.Context, in WANBinding) error {
	// 先把分配落库，修订、公钥或名字冲突就只跳过这一台。
	if _, err := s.AcceptBinding(ctx, in.ClientID, in.Name, in.PublicKey, in.Revision); err != nil {
		// 交给冲突判断，库错原样交回让通道重连。
		return skipWANBindingConflict(in.ClientID, "accept binding", err)
	}
	// 带了短码才写，空的不去清掉已有短码。
	if in.ShortCode != "" {
		// 短码和本机已有的对不上就记日志，臂号照常去补。
		if err := s.store.PutClientShortCode(ctx, in.ClientID, in.ShortCode); err != nil {
			// 不是业务冲突就交回，让通道退回重连。
			if err := skipWANBindingConflict(in.ClientID, "client short code", err); err != nil {
				return err
			}
		}
	}
	// 去掉首尾空白再看 WAN 有没有登记臂号。
	serial := strings.TrimSpace(in.DeviceSerial)
	// WAN 没登记臂号就到此为止，本机号不动。
	if serial == "" {
		return nil
	}
	// 本机没号才补上 WAN 的号，已有号以厂端从设备读到的为准。
	row, err := s.store.FillDeviceSerial(ctx, in.ClientID, serial)
	// 补号失败时，撞号只跳过这一台，库错交回重连。
	if err != nil {
		// 交给冲突判断，库错原样交回让通道重连。
		return skipWANBindingConflict(in.ClientID, "fill device serial", err)
	}
	// 有效绑定却留着别的号，说明本机号与 WAN 不同，保留本机的并记下。
	if row.Status == ClientStatusBound && row.DeviceSerial != serial {
		// 记下两边的号，便于 WAN 侧核对改号。
		slog.Warn("wan device serial differs, keep local", "client", in.ClientID, "local", row.DeviceSerial, "wan", serial)
	}
	return nil
}

// skipWANBindingConflict 单台设备的业务冲突记日志后吞掉，其余错误原样交回。
func skipWANBindingConflict(clientID uuid.UUID, step string, err error) error {
	// 只认这几类单台冲突，库连不上之类不能吞掉。
	switch {
	// 修订过旧、公钥不符、名字或公钥不合法、短码冲突、臂号被占。
	case errors.Is(err, domain.ErrStaleRevision), errors.Is(err, domain.ErrClientKeyMismatch),
		errors.Is(err, domain.ErrInvalidName), errors.Is(err, domain.ErrInvalidKey),
		errors.Is(err, domain.ErrAssetCodeConflict), errors.Is(err, domain.ErrDeviceSerialTaken):
		// 记下跳过的设备和原因，后面的设备与对账照常进行。
		slog.Warn("skip wan client binding", "client", clientID, "step", step, "err", err)
		return nil
	// 其余错误交回，通道退回外层重连再试。
	default:
		return err
	}
}

// VoidBinding 模拟 WAN 改绑后旧厂作废；此后本厂不得再签发。
func (s *Node) VoidBinding(ctx context.Context, clientID uuid.UUID) error {
	// 作废后本厂不得再签发。
	if err := s.store.VoidBinding(ctx, clientID); err != nil {
		// 记下作废绑定被拒绝，写失败不改变结果
		_ = s.audit(ctx, nil, nil, "void_binding", clientID.String(), audit.Deny)
		return err
	}
	// 作废绑定成功后记审计，再把结果交回
	return s.audit(ctx, nil, nil, "void_binding", clientID.String(), audit.Allow)
}

// ApplyWANVoid 按 WAN 作废指令带的修订作废本厂绑定；迟到的旧作废只记日志忽略。
// 先落了索引里的新绑定、再处理队列里迟到的旧作废时，设备不会被永久作废。
func (s *Node) ApplyWANVoid(ctx context.Context, clientID uuid.UUID, revision int64) error {
	// 作废指令修订高于本地才作废，条件在库里同一条更新判定。
	err := s.store.VoidBindingByRevision(ctx, clientID, revision)
	// 本地修订不更低，说明这是迟到的旧作废，记日志后忽略。
	if errors.Is(err, domain.ErrStaleRevision) {
		// 记下作废绑定被拒绝，写失败不改变结果
		_ = s.audit(ctx, nil, nil, "void_binding", clientID.String(), audit.Deny)
		// 记下设备和修订，便于核对 WAN 改分的先后。
		slog.Warn("ignore stale wan client void", "client", clientID, "revision", revision)
		return nil
	}
	// 其余失败照旧交回，没有这台由调用方当已完成。
	if err != nil {
		// 记下作废绑定被拒绝，写失败不改变结果
		_ = s.audit(ctx, nil, nil, "void_binding", clientID.String(), audit.Deny)
		return err
	}
	// 作废绑定成功后记审计，再把结果交回
	return s.audit(ctx, nil, nil, "void_binding", clientID.String(), audit.Allow)
}

// ReconcileBindings 按 WAN 当前绑定作废本厂漏掉的；没有名单时清空有效绑定。
func (s *Node) ReconcileBindings(ctx context.Context, keep []uuid.UUID) error {
	// 按条数决定是空、超限还是继续
	want := make(map[uuid.UUID]struct{}, len(keep))
	// 逐条处理，某一条失败不把整批悄悄算成功
	for _, id := range keep {
		// 记下名录上仍要保留的绑定
		want[id] = struct{}{}
	}
	// 列出本厂已接受的设备，不含私钥
	rows, err := s.store.ListClients(ctx)
	// 设备名录读失败，不返回残缺列表
	if err != nil {
		return err
	}
	// 逐行按调用方作用域裁，看不到的丢掉
	for _, row := range rows {
		// 这一条不符合就跳过，其它条继续
		if row.Status != ClientStatusBound {
			continue
		}
		// 名录上还有这台就保留，不在名单里的绑定要作废
		if _, ok := want[row.ID]; ok {
			continue
		}
		// 不是没有记录，读取真失败必须停住
		if err := s.VoidBinding(ctx, row.ID); err != nil && !errors.Is(err, domain.ErrNotFound) {
			return err
		}
	}
	return nil
}

// ensureSigningKey 没有签发钥则当场生成一对，私钥不外送。
func (s *kernel) ensureSigningKey(ctx context.Context) (SigningKey, error) {
	// 读取本厂签发钥，避免留下不一致的结果
	k, err := s.store.SigningKey(ctx)
	// 没有错误才采用这次结果，失败另走拒绝
	if err == nil {
		return k, nil
	}
	// 不是没有记录，读取真失败必须停住
	if !errors.Is(err, domain.ErrNotFound) {
		return SigningKey{}, err
	}
	// 生成签发用的密钥对，私钥留在本厂
	pub, priv, err := nodekey.Generate()
	// 密钥对生成失败，不能开始签发
	if err != nil {
		return SigningKey{}, err
	}
	// 写入认领时用过的签发钥，再交回调用方
	return s.store.PutSigningKey(ctx, pub, priv)
}

// SigningPublicKey 给出本厂签发公钥，供本机袋验证；私钥不外送。
func (s *Node) SigningPublicKey(ctx context.Context) ([]byte, error) {
	// 没有签发钥就生成一对，私钥不外送
	k, err := s.ensureSigningKey(ctx)
	// 签发钥备不齐，不能发运行凭证
	if err != nil {
		return nil, err
	}
	return k.PublicKey, nil
}

// InstallSigningKey 写入认领时用过的签发钥；已有且公钥相同则通过。
func (s *Node) InstallSigningKey(ctx context.Context, publicKey, privateKey []byte) error {
	// 条件不成立则拒绝或跳过，不往下改数据
	if !nodekey.Match(privateKey, publicKey) {
		return domain.ErrInvalidKey
	}
	// 读取本厂签发钥，避免留下不一致的结果
	k, err := s.store.SigningKey(ctx)
	// 没有错误才采用这次结果，失败另走拒绝
	if err == nil {
		// 条件不成立则拒绝或跳过，不往下改数据
		if !bytes.Equal(k.PublicKey, publicKey) {
			return domain.ErrSigningKeyExists
		}
		return nil
	}
	// 不是没有记录，读取真失败必须停住
	if !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	// 尚无钥才写入认领时用过的那对。
	_, err = s.store.PutSigningKey(ctx, publicKey, privateKey)
	return err
}

// WANKey 读本厂已钉的 WAN 指令验签公钥；还没钉就交回空。
func (s *Node) WANKey(ctx context.Context) ([]byte, error) {
	// 读唯一那一行钉钥。
	row, err := s.store.WANTrust(ctx)
	// 还没钉不算错，等首轮 HTTPS 索引来钉。
	if errors.Is(err, domain.ErrNotFound) {
		return nil, nil
	}
	// 读库失败要交回，不能当成没钉而放过验签。
	if err != nil {
		return nil, err
	}
	return row.PublicKey, nil
}

// PinWANKey 钉住经证书校验的 HTTPS 拿到的 WAN 公钥；同钥幂等，换钥拒绝并留审计。
func (s *Node) PinWANKey(ctx context.Context, publicKey []byte) error {
	// 取本厂身份当审计目标，钥原文不进审计。
	target := s.store.FactoryID().String()
	// 不是 32 字节的 Ed25519 公钥就拒绝。
	if len(publicKey) != ed25519.PublicKeySize {
		return domain.ErrInvalidKey
	}
	// 写入或核对钉钥，库里已是另一把会被拒绝。
	err := s.store.PutWANTrust(ctx, publicKey)
	// 换钥被拒要留一条拒绝审计。
	if errors.Is(err, domain.ErrIntegrity) {
		// 记下拒绝，原错误照旧交回。
		_ = s.audit(ctx, nil, nil, "pin_wan_key", target, audit.Deny)
		return err
	}
	// 库错原样交回，调用方只记日志。
	if err != nil {
		return err
	}
	// 钉上或同钥核对通过才记允许。
	return s.audit(ctx, nil, nil, "pin_wan_key", target, audit.Allow)
}

// 运行凭证窗口：天数上限与界面一致，生效提前一小时容下时钟误差。
const (
	maxRuntimeGrantDays = 365       // 一次最多签一年，挡住直调接口签超长窗口
	runtimeGrantSkew    = time.Hour // 生效时间往前放一小时，示教器钟稍慢也能用
)

// IssueRuntimeGrant 由本厂有效超管给已绑定 Client 签发可运行凭证，窗口按服务器时钟算。
func (s *Node) IssueRuntimeGrant(ctx context.Context, token string, clientID uuid.UUID, days int) (RuntimeCred, error) {
	// 签发更高修订，不能运行表示撤销，再交回调用方
	return s.issueRuntime(ctx, token, clientID, days, true, "issue_runtime")
}

// RevokeRuntimeGrant 签发更高修订且 can_run=false，表示撤销或缩权；窗口同样按服务器时钟算。
func (s *Node) RevokeRuntimeGrant(ctx context.Context, token string, clientID uuid.UUID, days int) (RuntimeCred, error) {
	// 签发更高修订，不能运行表示撤销，再交回调用方
	return s.issueRuntime(ctx, token, clientID, days, false, "revoke_runtime")
}

// issueRuntime 签发更高修订的运行凭证；can_run=false 表示撤销。
func (s *Node) issueRuntime(ctx context.Context, token string, clientID uuid.UUID, days int, canRun bool, action string) (RuntimeCred, error) {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return RuntimeCred{}, err
	}
	// 只有工厂超管能签发或撤销运行凭证。失败一律记拒绝。
	if err := s.can(ctx, acc, permManageAccount, nil); err != nil {
		// 不是工厂超管，记下签发或撤销运行凭证被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, action, clientID.String(), audit.Deny)
		return RuntimeCred{}, err
	}
	// 天数只认一到三百六十五，挡住零窗口和超长窗口
	if days < 1 || days > maxRuntimeGrantDays {
		// 天数越界，记下签发或撤销运行凭证被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, action, clientID.String(), audit.Deny)
		return RuntimeCred{}, domain.ErrInvalidGrantDays
	}
	// 按身份读本厂这台设备
	cl, err := s.store.ClientByID(ctx, clientID)
	// 这台设备不在名录，拒绝
	if err != nil {
		// 记下签发或撤销运行凭证被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, action, clientID.String(), audit.Deny)
		return RuntimeCred{}, err
	}
	// 不是有效绑定则拒绝签发、登录或对账
	if cl.Status != ClientStatusBound {
		// 设备未绑定或已作废，记下签发或撤销运行凭证被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, action, clientID.String(), audit.Deny)
		return RuntimeCred{}, domain.ErrBindingVoid
	}
	// 长度或数量不合法则拒绝，避免坏数据生效
	if len(cl.PublicKey) != 32 {
		// 记下签发或撤销运行凭证被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, action, clientID.String(), audit.Deny)
		return RuntimeCred{}, domain.ErrInvalidKey
	}
	// 没有签发钥就生成一对，私钥不外送
	key, err := s.ensureSigningKey(ctx)
	// 签发钥备不齐，不能发运行凭证
	if err != nil {
		return RuntimeCred{}, err
	}
	// 修订只向前加一。
	rev := int64(1)
	// 没有错误才采用这次结果，失败另走拒绝
	if latest, err := s.store.LatestRuntimeGrant(ctx, clientID); err == nil {
		// 已有凭证则修订加一，旧修订不能覆盖
		rev = latest.Revision + 1
		// 不是没有记录，读取真失败必须停住
	} else if !errors.Is(err, domain.ErrNotFound) {
		return RuntimeCred{}, err
	}
	// 窗口取服务器当前时刻，不信调用方浏览器的钟
	now := time.Now().UTC()
	// 组装待签发的运行凭证，修订只向前
	cred := RuntimeCred{
		FactoryID:    s.store.FactoryID(),
		ClientID:     clientID,
		ClientPublic: cl.PublicKey,
		CanRun:       canRun,
		NotBefore:    now.Add(-runtimeGrantSkew),
		NotAfter:     now.AddDate(0, 0, days),
		Revision:     rev,
	}
	// 把声明收成稳定正文再签名
	payload, err := encodeRuntime(cred)
	// 声明收不成正文，拒绝签发
	if err != nil {
		return RuntimeCred{}, err
	}
	// 收成待签名或已签名的声明正文
	cred.Payload = payload
	// 用本厂私钥签声明，并落一版凭证。
	cred.Signature = nodekey.Sign(key.PrivateKey, payload)
	// 凭证写不进去，修订不能升高
	if _, err := s.store.InsertRuntimeGrant(ctx, RuntimeGrant{
		// 落下这张凭证或现场所属的设备
		ClientID:  clientID,
		Revision:  rev,
		CanRun:    canRun,
		NotBefore: cred.NotBefore,
		NotAfter:  cred.NotAfter,
		Payload:   payload,
		Signature: cred.Signature,
	}); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, action, clientID.String(), audit.Deny)
		return RuntimeCred{}, err
	}
	// 审计没写下则整次不算完成
	if err := s.audit(ctx, &acc.ID, nil, action, clientID.String(), audit.Allow); err != nil {
		return RuntimeCred{}, err
	}
	return cred, nil
}

// EvaluateNode 按本机袋判定，并记下允许/拒绝与时间来源；不改密钥。
func (s *Node) EvaluateNode(ctx context.Context, bag Bag, clocks Clocks, action NodeAction) (NodeEval, error) {
	// 断网只查本机袋里的凭证
	ev := EvaluateRuntime(bag, clocks, action)
	// 先按新开操作记账，继续焊接再改名
	name := "node_open"
	result := audit.Deny
	// 按判定结果决定记新开还是继续焊接
	switch {
	// 继续焊接且凭证允许做完，记为允许
	case action == NodeContinue && ev.Decision == NodeContinueWeld:
		// 这是继续焊接，审计改记继续而不是新开
		name = "node_continue"
		result = audit.Allow
	// 判定为允许，审计改记允许
	case ev.Decision == NodeAllow:
		// 正在焊接才允许在过期后把这一道做完
		if action == NodeContinue {
			// 这是继续焊接，审计改记继续而不是新开
			name = "node_continue"
		}
		// 判定通过，审计按允许落
		result = audit.Allow
	}
	// 收成文本身份，供审计或对账对准
	target := bag.ClientID.String()
	// 审计没写下则整次不算完成
	if err := s.auditTimed(ctx, nil, nil, name, target, result, ev.TimeSource); err != nil {
		return ev, err
	}
	return ev, nil
}

// RecordAnomaly 发现 Root 或改钟只留痕，不清密钥、不停用节点。
func (s *Node) RecordAnomaly(ctx context.Context, clientID uuid.UUID, kind string) error {
	// 异常留痕成功后记审计，再把结果交回
	return s.audit(ctx, nil, nil, "node_anomaly", clientID.String()+" "+kind, audit.Allow)
}

// auditTimed 记下带时间来源的允许或拒绝。
func (s *kernel) auditTimed(ctx context.Context, actor *uuid.UUID, claimed *string, action, target, result, timeSource string) error {
	// 取出本厂稳定身份，封包和审计都要用
	fid := s.store.FactoryID()
	// 把允许或拒绝写入审计，不含口令
	return s.store.AppendAudit(ctx, audit.Event{
		ActorID:      actor,
		ClaimedLogin: claimed,
		FactoryID:    &fid,
		Action:       action,
		Target:       target,
		Result:       result,
		TimeSource:   timeSource,
	})
}
