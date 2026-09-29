package service

import (
	"bytes"
	"context"
	"errors"
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

// IssueRuntimeGrant 由本厂有效超管给已绑定 Client 签发可运行凭证。
func (s *Node) IssueRuntimeGrant(ctx context.Context, token string, clientID uuid.UUID, notBefore, notAfter time.Time) (RuntimeCred, error) {
	// 签发更高修订，不能运行表示撤销，再交回调用方
	return s.issueRuntime(ctx, token, clientID, notBefore, notAfter, true, "issue_runtime")
}

// RevokeRuntimeGrant 签发更高修订且 can_run=false，表示撤销或缩权。
func (s *Node) RevokeRuntimeGrant(ctx context.Context, token string, clientID uuid.UUID, notBefore, notAfter time.Time) (RuntimeCred, error) {
	// 签发更高修订，不能运行表示撤销，再交回调用方
	return s.issueRuntime(ctx, token, clientID, notBefore, notAfter, false, "revoke_runtime")
}

// issueRuntime 签发更高修订的运行凭证；can_run=false 表示撤销。
func (s *Node) issueRuntime(ctx context.Context, token string, clientID uuid.UUID, notBefore, notAfter time.Time, canRun bool, action string) (RuntimeCred, error) {
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
	// 组装待签发的运行凭证，修订只向前
	cred := RuntimeCred{
		FactoryID:    s.store.FactoryID(),
		ClientID:     clientID,
		ClientPublic: cl.PublicKey,
		CanRun:       canRun,
		NotBefore:    notBefore.UTC(),
		NotAfter:     notAfter.UTC(),
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
