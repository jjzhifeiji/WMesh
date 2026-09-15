package service

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/secret"
)

// ClientSession 是本机登录结果：会话令牌与解封钥只回给这台设备。
type ClientSession struct {
	Token            string       `json:"token"`                      // 会话令牌原文，只回给调用方
	UnwrapKey        []byte       `json:"unwrapKey"`                  // 到站解封钥，只回内存，禁止落盘原文
	Account          Account      `json:"account"`                    // 当前登录人，无密码
	Policy           ClientPolicy `json:"policy"`                     // 本厂现行 Client 策略
	SigningPublicKey []byte       `json:"signingPublicKey,omitempty"` // 本厂签发公钥，验下行 Intent
	MqttURL          string       `json:"mqttUrl,omitempty"`          // 本厂 Client MQTT 地址；HTTP 层可补
	ClientShortCode  string       `json:"clientShortCode,omitempty"`  // 本机短码 Cxxxx，未齐则空
	Roles            []string     `json:"roles,omitempty"`            // 当前登录人有效角色，供本机入队判定
}

// RegisterDevice 把读到的机械臂号钉到已绑定 Client；空号拒绝，本厂未作废号不得重复。
func (s *Node) RegisterDevice(ctx context.Context, clientID uuid.UUID, serial string) (Client, error) {
	if err := s.requireFactoryOpen(ctx); err != nil {
		_ = s.audit(ctx, nil, nil, "client_register", clientID.String(), audit.Deny)
		return Client{}, err
	}
	row, err := s.store.PinDeviceSerial(ctx, clientID, serial)
	if err != nil {
		_ = s.audit(ctx, nil, nil, "client_register", clientID.String(), audit.Deny)
		return Client{}, err
	}
	row.UnwrapKey = nil
	return row, s.audit(ctx, nil, nil, "client_register", clientID.String(), audit.Allow)
}

// LoginOnClient 本厂有效账号在已钉设备号的本机登录并领取解封钥；他厂、未绑定、号不对都拒绝。
func (s *Node) LoginOnClient(ctx context.Context, clientID uuid.UUID, serial, loginName, password string) (ClientSession, error) {
	loginName = strings.TrimSpace(loginName)
	serial = strings.TrimSpace(serial)
	if err := s.requireFactoryOpen(ctx); err != nil {
		_ = s.audit(ctx, nil, &loginName, "client_login", clientID.String(), audit.Deny)
		return ClientSession{}, err
	}
	if serial == "" {
		_ = s.audit(ctx, nil, &loginName, "client_login", clientID.String(), audit.Deny)
		return ClientSession{}, domain.ErrDeviceSerialRequired
	}
	cli, err := s.store.ClientByID(ctx, clientID)
	if err != nil {
		_ = s.audit(ctx, nil, &loginName, "client_login", clientID.String(), audit.Deny)
		return ClientSession{}, err
	}
	if cli.Status != ClientStatusBound {
		_ = s.audit(ctx, nil, &loginName, "client_login", clientID.String(), audit.Deny)
		return ClientSession{}, domain.ErrBindingVoid
	}
	if cli.DeviceSerial == "" || cli.DeviceSerial != serial {
		_ = s.audit(ctx, nil, &loginName, "client_login", clientID.String(), audit.Deny)
		return ClientSession{}, domain.ErrDeviceSerialMismatch
	}
	if len(cli.UnwrapKey) != 32 {
		_ = s.audit(ctx, nil, &loginName, "client_login", clientID.String(), audit.Deny)
		return ClientSession{}, domain.ErrForbidden
	}
	p, err := s.store.PersonByLogin(ctx, loginName)
	if err != nil {
		_ = s.audit(ctx, nil, &loginName, "client_login", clientID.String(), audit.Deny)
		return ClientSession{}, domain.ErrInvalidCredentials
	}
	if p.Status == StatusPending {
		_ = s.audit(ctx, &p.ID, &loginName, "client_login", clientID.String(), audit.Deny)
		return ClientSession{}, domain.ErrAccountPending
	}
	if p.Status == StatusDisabled {
		_ = s.audit(ctx, &p.ID, &loginName, "client_login", clientID.String(), audit.Deny)
		return ClientSession{}, domain.ErrAccountDisabled
	}
	if p.PasswordHash == nil || !secret.VerifyPassword(*p.PasswordHash, password) {
		_ = s.audit(ctx, &p.ID, &loginName, "client_login", clientID.String(), audit.Deny)
		return ClientSession{}, domain.ErrInvalidCredentials
	}
	token, err := secret.RandomToken()
	if err != nil {
		return ClientSession{}, err
	}
	if _, err := s.store.CreateSession(ctx, p.ID, secret.TokenHash(token), time.Now().UTC().Add(sessionTTL)); err != nil {
		return ClientSession{}, err
	}
	if err := s.store.ClearOperatorForPersonExcept(ctx, p.ID, clientID); err != nil {
		_ = s.store.DeleteSessionByTokenHash(ctx, secret.TokenHash(token))
		return ClientSession{}, err
	}
	if err := s.store.SetClientOperator(ctx, clientID, p.ID); err != nil {
		_ = s.store.DeleteSessionByTokenHash(ctx, secret.TokenHash(token))
		return ClientSession{}, err
	}
	pol, err := s.store.ClientPolicy(ctx)
	if err != nil {
		_ = s.store.DeleteSessionByTokenHash(ctx, secret.TokenHash(token))
		return ClientSession{}, err
	}
	if err := s.audit(ctx, &p.ID, &loginName, "client_login", clientID.String(), audit.Allow); err != nil {
		_ = s.store.DeleteSessionByTokenHash(ctx, secret.TokenHash(token))
		return ClientSession{}, err
	}
	pub, err := s.SigningPublicKey(ctx)
	if err != nil {
		_ = s.store.DeleteSessionByTokenHash(ctx, secret.TokenHash(token))
		return ClientSession{}, err
	}
	// 短码与角色只回给这台机，供本机发号和入队。
	grants, err := s.store.ActiveGrants(ctx, p.ID)
	if err != nil {
		_ = s.store.DeleteSessionByTokenHash(ctx, secret.TokenHash(token))
		return ClientSession{}, err
	}
	roles := make([]string, 0, len(grants))
	seen := map[string]struct{}{}
	for _, g := range grants {
		if _, ok := seen[g.Role]; ok {
			continue
		}
		seen[g.Role] = struct{}{}
		roles = append(roles, g.Role)
	}
	key := append([]byte(nil), cli.UnwrapKey...)
	return ClientSession{
		Token: token, UnwrapKey: key, Account: accountOf(p), Policy: pol,
		SigningPublicKey: pub, ClientShortCode: cli.ShortCode, Roles: roles,
	}, nil
}
