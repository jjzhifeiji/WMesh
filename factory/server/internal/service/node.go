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
	name = strings.TrimSpace(name)
	n := utf8.RuneCountInString(name)
	if n < 1 || n > 64 {
		return "", domain.ErrInvalidName
	}
	return name, nil
}

// AcceptBinding 把 WAN 送到本厂的节点落到本库；测试夹具与通道共用。
func (s *Node) AcceptBinding(ctx context.Context, clientID uuid.UUID, name string, publicKey []byte, revision int64) (Client, error) {
	if strings.TrimSpace(name) != "" {
		var err error
		name, err = normalizeClientName(name)
		if err != nil {
			_ = s.audit(ctx, nil, nil, "accept_binding", clientID.String(), audit.Deny)
			return Client{}, err
		}
	}
	// 把 WAN 送到本厂的绑定落到本库；修订只向前。
	row, err := s.store.AcceptBinding(ctx, clientID, name, publicKey, revision)
	if err != nil {
		_ = s.audit(ctx, nil, nil, "accept_binding", clientID.String(), audit.Deny)
		return Client{}, err
	}
	return row, s.audit(ctx, nil, nil, "accept_binding", clientID.String(), audit.Allow)
}

// VoidBinding 模拟 WAN 改绑后旧厂作废；此后本厂不得再签发。
func (s *Node) VoidBinding(ctx context.Context, clientID uuid.UUID) error {
	// 作废后本厂不得再签发。
	if err := s.store.VoidBinding(ctx, clientID); err != nil {
		_ = s.audit(ctx, nil, nil, "void_binding", clientID.String(), audit.Deny)
		return err
	}
	return s.audit(ctx, nil, nil, "void_binding", clientID.String(), audit.Allow)
}

// ensureSigningKey 没有签发钥则当场生成一对，私钥不外送。
func (s *Node) ensureSigningKey(ctx context.Context) (SigningKey, error) {
	k, err := s.store.SigningKey(ctx)
	if err == nil {
		return k, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return SigningKey{}, err
	}
	pub, priv, err := nodekey.Generate()
	if err != nil {
		return SigningKey{}, err
	}
	return s.store.PutSigningKey(ctx, pub, priv)
}

// SigningPublicKey 给出本厂签发公钥，供本机袋验证；私钥不外送。
func (s *Node) SigningPublicKey(ctx context.Context) ([]byte, error) {
	k, err := s.ensureSigningKey(ctx)
	if err != nil {
		return nil, err
	}
	return k.PublicKey, nil
}

// InstallSigningKey 写入认领时用过的签发钥；已有且公钥相同则通过。
func (s *Node) InstallSigningKey(ctx context.Context, publicKey, privateKey []byte) error {
	if !nodekey.Match(privateKey, publicKey) {
		return domain.ErrInvalidKey
	}
	k, err := s.store.SigningKey(ctx)
	if err == nil {
		if !bytes.Equal(k.PublicKey, publicKey) {
			return domain.ErrSigningKeyExists
		}
		return nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	// 尚无钥才写入认领时用过的那对。
	_, err = s.store.PutSigningKey(ctx, publicKey, privateKey)
	return err
}

// IssueRuntimeGrant 由本厂有效超管给已绑定 Client 签发可运行凭证。
func (s *Node) IssueRuntimeGrant(ctx context.Context, token string, clientID uuid.UUID, notBefore, notAfter time.Time) (RuntimeCred, error) {
	return s.issueRuntime(ctx, token, clientID, notBefore, notAfter, true, "issue_runtime")
}

// RevokeRuntimeGrant 签发更高修订且 can_run=false，表示撤销或缩权。
func (s *Node) RevokeRuntimeGrant(ctx context.Context, token string, clientID uuid.UUID, notBefore, notAfter time.Time) (RuntimeCred, error) {
	return s.issueRuntime(ctx, token, clientID, notBefore, notAfter, false, "revoke_runtime")
}

// issueRuntime 签发更高修订的运行凭证；can_run=false 表示撤销。
func (s *Node) issueRuntime(ctx context.Context, token string, clientID uuid.UUID, notBefore, notAfter time.Time, canRun bool, action string) (RuntimeCred, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return RuntimeCred{}, err
	}
	// 只有工厂超管能签发或撤销运行凭证。失败一律记拒绝。
	if err := s.can(ctx, acc, permManageAccount, nil); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, action, clientID.String(), audit.Deny)
		return RuntimeCred{}, err
	}
	cl, err := s.store.ClientByID(ctx, clientID)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, action, clientID.String(), audit.Deny)
		return RuntimeCred{}, err
	}
	if cl.Status != ClientStatusBound {
		_ = s.audit(ctx, &acc.ID, nil, action, clientID.String(), audit.Deny)
		return RuntimeCred{}, domain.ErrBindingVoid
	}
	if len(cl.PublicKey) != 32 {
		_ = s.audit(ctx, &acc.ID, nil, action, clientID.String(), audit.Deny)
		return RuntimeCred{}, domain.ErrInvalidKey
	}
	key, err := s.ensureSigningKey(ctx)
	if err != nil {
		return RuntimeCred{}, err
	}
	// 修订只向前加一。
	rev := int64(1)
	if latest, err := s.store.LatestRuntimeGrant(ctx, clientID); err == nil {
		rev = latest.Revision + 1
	} else if !errors.Is(err, domain.ErrNotFound) {
		return RuntimeCred{}, err
	}
	cred := RuntimeCred{
		FactoryID:    s.store.FactoryID(),
		ClientID:     clientID,
		ClientPublic: cl.PublicKey,
		CanRun:       canRun,
		NotBefore:    notBefore.UTC(),
		NotAfter:     notAfter.UTC(),
		Revision:     rev,
	}
	payload, err := encodeRuntime(cred)
	if err != nil {
		return RuntimeCred{}, err
	}
	cred.Payload = payload
	// 用本厂私钥签声明，并落一版凭证。
	cred.Signature = nodekey.Sign(key.PrivateKey, payload)
	if _, err := s.store.InsertRuntimeGrant(ctx, RuntimeGrant{
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
	if err := s.audit(ctx, &acc.ID, nil, action, clientID.String(), audit.Allow); err != nil {
		return RuntimeCred{}, err
	}
	return cred, nil
}

// EvaluateNode 按本机袋判定，并记下允许/拒绝与时间来源；不改密钥。
func (s *Node) EvaluateNode(ctx context.Context, bag Bag, clocks Clocks, action NodeAction) (NodeEval, error) {
	ev := EvaluateRuntime(bag, clocks, action)
	name := "node_open"
	result := audit.Deny
	switch {
	case action == NodeContinue && ev.Decision == NodeContinueWeld:
		name = "node_continue"
		result = audit.Allow
	case ev.Decision == NodeAllow:
		if action == NodeContinue {
			name = "node_continue"
		}
		result = audit.Allow
	}
	target := bag.ClientID.String()
	if err := s.auditTimed(ctx, nil, nil, name, target, result, ev.TimeSource); err != nil {
		return ev, err
	}
	return ev, nil
}

// RecordAnomaly 发现 Root 或改钟只留痕，不清密钥、不停用节点。
func (s *Node) RecordAnomaly(ctx context.Context, clientID uuid.UUID, kind string) error {
	return s.audit(ctx, nil, nil, "node_anomaly", clientID.String()+" "+kind, audit.Allow)
}

// auditTimed 记下带时间来源的允许或拒绝。
func (s *kernel) auditTimed(ctx context.Context, actor *uuid.UUID, claimed *string, action, target, result, timeSource string) error {
	fid := s.store.FactoryID()
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
