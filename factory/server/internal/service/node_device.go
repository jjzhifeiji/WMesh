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

// ClientSession 是本机登录结果：会话令牌与登录人解封钥只回给这台设备。
type ClientSession struct {
	Token            string       `json:"token"`                      // 会话令牌原文，只回给调用方
	UnwrapKey        []byte       `json:"unwrapKey"`                  // 登录人解封钥，焊机不持钥
	Account          Account      `json:"account"`                    // 当前登录人，无密码
	Policy           ClientPolicy `json:"policy"`                     // 本厂现行 Client 策略
	SigningPublicKey []byte       `json:"signingPublicKey,omitempty"` // 本厂签发公钥，验下行 Intent
	MqttURL          string       `json:"mqttUrl,omitempty"`          // 本厂 Client MQTT 地址；HTTP 层可补
	ClientShortCode  string       `json:"clientShortCode,omitempty"`  // 本机短码 Cxxxx，未齐则空
	Roles            []string     `json:"roles,omitempty"`            // 当前登录人有效角色，供本机入队判定
}

// RegisterDevice 把读到的机械臂号钉到已绑定 Client；空号拒绝，本厂未作废号不得重复。
func (s *Node) RegisterDevice(ctx context.Context, clientID uuid.UUID, serial string) (Client, error) {
	// 厂已停用、注销或租约到期，拒绝本次
	if err := s.requireFactoryOpen(ctx); err != nil {
		// 厂已停用、注销或租约到期，记下钉机械臂号被拒绝，写失败不改变结果
		_ = s.audit(ctx, nil, nil, "client_register", clientID.String(), audit.Deny)
		return Client{}, err
	}
	// 把读到的机械臂号钉到这台
	row, err := s.store.PinDeviceSerial(ctx, clientID, serial)
	// 钉号失败，避免两台抢同一个号
	if err != nil {
		// 记下钉机械臂号被拒绝，写失败不改变结果
		_ = s.audit(ctx, nil, nil, "client_register", clientID.String(), audit.Deny)
		return Client{}, err
	}
	// 解封钥不放进返回，避免把秘密交给调用方
	row.UnwrapKey = nil
	// 钉机械臂号成功后记审计，再把结果交回
	return row, s.audit(ctx, nil, nil, "client_register", clientID.String(), audit.Allow)
}

// LoginOnClient 本厂有效账号在已钉设备号的本机登录并领取人钥；他厂、未绑定、号不对都拒绝。
func (s *Node) LoginOnClient(ctx context.Context, clientID uuid.UUID, serial, loginName, password string) (ClientSession, error) {
	// 去掉首尾空白后再做判断
	loginName = strings.TrimSpace(loginName)
	// 去掉首尾空白后再做判断
	serial = strings.TrimSpace(serial)
	// 厂已停用、注销或租约到期，拒绝本次
	if err := s.requireFactoryOpen(ctx); err != nil {
		// 厂已停用、注销或租约到期，记下本机登录被拒绝，写失败不改变结果
		_ = s.audit(ctx, nil, &loginName, "client_login", clientID.String(), audit.Deny)
		return ClientSession{}, err
	}
	// 空识别号拒绝，登记和登录都必须读到号
	if serial == "" {
		// 机械臂号空或不符，记下本机登录被拒绝，写失败不改变结果
		_ = s.audit(ctx, nil, &loginName, "client_login", clientID.String(), audit.Deny)
		return ClientSession{}, domain.ErrDeviceSerialRequired
	}
	// 按身份读本厂这台设备
	cli, err := s.store.ClientByID(ctx, clientID)
	// 这台设备不在名录，拒绝
	if err != nil {
		// 记下本机登录被拒绝，写失败不改变结果
		_ = s.audit(ctx, nil, &loginName, "client_login", clientID.String(), audit.Deny)
		return ClientSession{}, err
	}
	// 不是有效绑定则拒绝签发、登录或对账
	if cli.Status != ClientStatusBound {
		// 设备未绑定或已作废，记下本机登录被拒绝，写失败不改变结果
		_ = s.audit(ctx, nil, &loginName, "client_login", clientID.String(), audit.Deny)
		return ClientSession{}, domain.ErrBindingVoid
	}
	// 还没钉号，或读到的号对不上，拒绝登录
	if cli.DeviceSerial == "" || cli.DeviceSerial != serial {
		// 机械臂号空或不符，记下本机登录被拒绝，写失败不改变结果
		_ = s.audit(ctx, nil, &loginName, "client_login", clientID.String(), audit.Deny)
		return ClientSession{}, domain.ErrDeviceSerialMismatch
	}
	// 按登录名找本厂人员
	p, err := s.store.PersonByLogin(ctx, loginName)
	// 登录名对不上人，按拒绝停住
	if err != nil {
		// 记下本机登录被拒绝，写失败不改变结果
		_ = s.audit(ctx, nil, &loginName, "client_login", clientID.String(), audit.Deny)
		return ClientSession{}, domain.ErrInvalidCredentials
	}
	// 还在待启用，不能登录也不能当有效超管
	if p.Status == StatusPending {
		// 账号仍待启用，记下本机登录被拒绝，写失败不改变结果
		_ = s.audit(ctx, &p.ID, &loginName, "client_login", clientID.String(), audit.Deny)
		return ClientSession{}, domain.ErrAccountPending
	}
	// 已停用则拒绝登录或保持停用只换口令
	if p.Status == StatusDisabled {
		// 账号已停用，记下本机登录被拒绝，写失败不改变结果
		_ = s.audit(ctx, &p.ID, &loginName, "client_login", clientID.String(), audit.Deny)
		return ClientSession{}, domain.ErrAccountDisabled
	}
	// 还没有口令或口令不对，拒绝登录
	if p.PasswordHash == nil || !secret.VerifyPassword(*p.PasswordHash, password) {
		// 口令不对，记下本机登录被拒绝，写失败不改变结果
		_ = s.audit(ctx, &p.ID, &loginName, "client_login", clientID.String(), audit.Deny)
		return ClientSession{}, domain.ErrInvalidCredentials
	}
	// 解封钥跟人走，焊机不持钥。
	who, err := s.store.EnsurePersonUnwrapKey(ctx, p.ID)
	// 解封钥读不到或长度不对，拒绝继续
	if err != nil || len(who.UnwrapKey) != 32 {
		// 解封钥读不到或长度不对，记下本机登录被拒绝，写失败不改变结果
		_ = s.audit(ctx, &p.ID, &loginName, "client_login", clientID.String(), audit.Deny)
		// 读钥失败则交回错误，长度不对另按无效钥拒绝
		if err != nil {
			return ClientSession{}, err
		}
		return ClientSession{}, domain.ErrForbidden
	}
	// 读对本厂全部本机生效的策略
	pol, err := s.store.ClientPolicy(ctx)
	// 策略读失败，不能改或下发
	if err != nil {
		// 记下本机登录被拒绝，写失败不改变结果
		_ = s.audit(ctx, &p.ID, &loginName, "client_login", clientID.String(), audit.Deny)
		return ClientSession{}, err
	}
	// 生成只交回一次的激活码
	token, err := secret.RandomToken()
	// 激活码生成失败，不写半截超管
	if err != nil {
		return ClientSession{}, err
	}
	// 令牌时长跟当时的登录时效，不跟网页那条 12 小时。
	if _, err := s.store.CreateAppSession(ctx, p.ID, secret.TokenHash(token), appTokenExpiry(pol, time.Now().UTC())); err != nil {
		return ClientSession{}, err
	}
	// 示教器登录立刻记最近见到，版本由 HTTP 层补。
	_ = s.store.NotePersonApp(ctx, p.ID, 0, "")
	// 旧占用清不掉，会显示同时占多台
	if err := s.store.ClearOperatorForPersonExcept(ctx, p.ID, clientID); err != nil {
		// 只拿令牌摘要对会话，原文不入库，失败不打断当前返回
		_ = s.store.DeleteSessionByTokenHash(ctx, secret.TokenHash(token))
		return ClientSession{}, err
	}
	// 使用人写不进去，管理端会看错人
	if err := s.store.SetClientOperator(ctx, clientID, p.ID); err != nil {
		// 只拿令牌摘要对会话，原文不入库，失败不打断当前返回
		_ = s.store.DeleteSessionByTokenHash(ctx, secret.TokenHash(token))
		return ClientSession{}, err
	}
	// 审计没写下则整次不算完成
	if err := s.audit(ctx, &p.ID, &loginName, "client_login", clientID.String(), audit.Allow); err != nil {
		// 只拿令牌摘要对会话，原文不入库，失败不打断当前返回
		_ = s.store.DeleteSessionByTokenHash(ctx, secret.TokenHash(token))
		return ClientSession{}, err
	}
	// 只取签发公钥给本机验，私钥不外送
	pub, err := s.SigningPublicKey(ctx)
	// 公钥读失败，本机无法验签
	if err != nil {
		// 只拿令牌摘要对会话，原文不入库，失败不打断当前返回
		_ = s.store.DeleteSessionByTokenHash(ctx, secret.TokenHash(token))
		return ClientSession{}, err
	}
	// 短码与角色只回给这台机，供本机发号和入队。
	grants, err := s.store.ActiveGrants(ctx, p.ID)
	// 角色读失败，不能判定许可
	if err != nil {
		// 只拿令牌摘要对会话，原文不入库，失败不打断当前返回
		_ = s.store.DeleteSessionByTokenHash(ctx, secret.TokenHash(token))
		return ClientSession{}, err
	}
	// 按条数决定是空、超限还是继续
	roles := make([]string, 0, len(grants))
	// 用来挡重复，同一份只收一次
	seen := map[string]struct{}{}
	// 逐条有效授权看是否盖住目标
	for _, g := range grants {
		// 已经收过则跳过，保证一份只出现一次
		if _, ok := seen[g.Role]; ok {
			continue
		}
		// 记下已经收过，后面的重复直接跳过
		seen[g.Role] = struct{}{}
		// 把这一条收进结果，漏了清单就不齐
		roles = append(roles, g.Role)
	}
	// 复制一份字节，避免和原来的钥或正文共用底层
	key := append([]byte(nil), who.UnwrapKey...)
	return ClientSession{
		Token: token, UnwrapKey: key, Account: accountOf(p), Policy: pol,
		SigningPublicKey: pub, ClientShortCode: cli.ShortCode, Roles: roles,
	}, nil
}

// PadDevice 是厂网登录带回的本厂设备行；焊机只有编号，不带解封钥。
type PadDevice struct {
	ID           uuid.UUID `json:"id"`                     // Client 稳定身份
	Name         string    `json:"name"`                   // 给人看的设备名
	DeviceSerial string    `json:"deviceSerial,omitempty"` // 已钉机械臂识别号；未登记为空
	ShortCode    string    `json:"shortCode,omitempty"`    // 本机短码 Cxxxx，未齐则空
}

// PadSession 是厂网登录结果：人员会话、登录人解封钥、本厂未作废设备。
type PadSession struct {
	Token            string       `json:"token"`                      // 会话令牌原文，只回给调用方
	UnwrapKey        []byte       `json:"unwrapKey"`                  // 登录人解封钥，焊机不持钥
	Account          Account      `json:"account"`                    // 当前登录人，无密码
	Policy           ClientPolicy `json:"policy"`                     // 本厂现行 Client 策略
	SigningPublicKey []byte       `json:"signingPublicKey,omitempty"` // 本厂签发公钥，验下行 Intent
	MqttURL          string       `json:"mqttUrl,omitempty"`          // 本厂 Client MQTT 地址；HTTP 层可补
	Roles            []string     `json:"roles,omitempty"`            // 当前登录人有效角色
	Devices          []PadDevice  `json:"devices"`                    // 本厂未作废设备；连臂时再按识别号匹配
	Work             PadWorkSnap  `json:"work"`                       // 当时分配快照，供焊事实用
}

// LoginPad 本厂有效账号在厂网登录，领取人钥和设备名录；不校验机械臂号，不占操作员位。
func (s *Node) LoginPad(ctx context.Context, loginName, password string) (PadSession, error) {
	// 去掉首尾空白后再做判断
	loginName = strings.TrimSpace(loginName)
	// 厂已停用、注销或租约到期，拒绝本次
	if err := s.requireFactoryOpen(ctx); err != nil {
		// 厂已停用、注销或租约到期，记下厂网登录被拒绝，写失败不改变结果
		_ = s.audit(ctx, nil, &loginName, "pad_login", s.store.FactoryID().String(), audit.Deny)
		return PadSession{}, err
	}
	// 按登录名找本厂人员
	p, err := s.store.PersonByLogin(ctx, loginName)
	// 登录名对不上人，按拒绝停住
	if err != nil {
		// 记下厂网登录被拒绝，写失败不改变结果
		_ = s.audit(ctx, nil, &loginName, "pad_login", s.store.FactoryID().String(), audit.Deny)
		return PadSession{}, domain.ErrInvalidCredentials
	}
	// 还在待启用，不能登录也不能当有效超管
	if p.Status == StatusPending {
		// 账号仍待启用，记下厂网登录被拒绝，写失败不改变结果
		_ = s.audit(ctx, &p.ID, &loginName, "pad_login", s.store.FactoryID().String(), audit.Deny)
		return PadSession{}, domain.ErrAccountPending
	}
	// 已停用则拒绝登录或保持停用只换口令
	if p.Status == StatusDisabled {
		// 账号已停用，记下厂网登录被拒绝，写失败不改变结果
		_ = s.audit(ctx, &p.ID, &loginName, "pad_login", s.store.FactoryID().String(), audit.Deny)
		return PadSession{}, domain.ErrAccountDisabled
	}
	// 还没有口令或口令不对，拒绝登录
	if p.PasswordHash == nil || !secret.VerifyPassword(*p.PasswordHash, password) {
		// 口令不对，记下厂网登录被拒绝，写失败不改变结果
		_ = s.audit(ctx, &p.ID, &loginName, "pad_login", s.store.FactoryID().String(), audit.Deny)
		return PadSession{}, domain.ErrInvalidCredentials
	}
	// 解封钥跟人走，焊机不持钥。
	who, err := s.store.EnsurePersonUnwrapKey(ctx, p.ID)
	// 解封钥读不到或长度不对，拒绝继续
	if err != nil || len(who.UnwrapKey) != 32 {
		// 解封钥读不到或长度不对，记下厂网登录被拒绝，写失败不改变结果
		_ = s.audit(ctx, &p.ID, &loginName, "pad_login", s.store.FactoryID().String(), audit.Deny)
		// 读钥失败则交回错误，长度不对另按无效钥拒绝
		if err != nil {
			return PadSession{}, err
		}
		return PadSession{}, domain.ErrForbidden
	}
	// 读对本厂全部本机生效的策略
	pol, err := s.store.ClientPolicy(ctx)
	// 策略读失败，不能改或下发
	if err != nil {
		// 记下厂网登录被拒绝，写失败不改变结果
		_ = s.audit(ctx, &p.ID, &loginName, "pad_login", s.store.FactoryID().String(), audit.Deny)
		return PadSession{}, err
	}
	// 生成只交回一次的激活码
	token, err := secret.RandomToken()
	// 激活码生成失败，不写半截超管
	if err != nil {
		return PadSession{}, err
	}
	// 令牌时长跟当时的登录时效，不跟网页那条 12 小时。
	if _, err := s.store.CreateAppSession(ctx, p.ID, secret.TokenHash(token), appTokenExpiry(pol, time.Now().UTC())); err != nil {
		return PadSession{}, err
	}
	// 示教器登录立刻记最近见到，版本由 HTTP 层补。
	_ = s.store.NotePersonApp(ctx, p.ID, 0, "")
	// 列出本厂已接受的设备，不含私钥
	rows, err := s.store.ListClients(ctx)
	// 设备名录读失败，不返回残缺列表
	if err != nil {
		// 只拿令牌摘要对会话，原文不入库，失败不打断当前返回
		_ = s.store.DeleteSessionByTokenHash(ctx, secret.TokenHash(token))
		return PadSession{}, err
	}
	// 只取签发公钥给本机验，私钥不外送
	pub, err := s.SigningPublicKey(ctx)
	// 公钥读失败，本机无法验签
	if err != nil {
		// 只拿令牌摘要对会话，原文不入库，失败不打断当前返回
		_ = s.store.DeleteSessionByTokenHash(ctx, secret.TokenHash(token))
		return PadSession{}, err
	}
	// 只取仍有效的角色
	grants, err := s.store.ActiveGrants(ctx, p.ID)
	// 角色读失败，不能判定许可
	if err != nil {
		// 只拿令牌摘要对会话，原文不入库，失败不打断当前返回
		_ = s.store.DeleteSessionByTokenHash(ctx, secret.TokenHash(token))
		return PadSession{}, err
	}
	// 按条数决定是空、超限还是继续
	roles := make([]string, 0, len(grants))
	// 用来挡重复，同一份只收一次
	seen := map[string]struct{}{}
	// 逐条有效授权看是否盖住目标
	for _, g := range grants {
		// 已经收过则跳过，保证一份只出现一次
		if _, ok := seen[g.Role]; ok {
			continue
		}
		// 记下已经收过，后面的重复直接跳过
		seen[g.Role] = struct{}{}
		// 把这一条收进结果，漏了清单就不齐
		roles = append(roles, g.Role)
	}
	// 按条数决定是空、超限还是继续
	devices := make([]PadDevice, 0, len(rows))
	// 逐行按调用方作用域裁，看不到的丢掉
	for _, row := range rows {
		// 这一条不符合就跳过，其它条继续
		if row.Status != ClientStatusBound {
			continue
		}
		// 把这一条收进结果，漏了清单就不齐
		devices = append(devices, PadDevice{ID: row.ID, Name: row.Name, DeviceSerial: row.DeviceSerial, ShortCode: row.ShortCode})
	}
	// 用当前有效分配钉路径，没有则厂直属
	work, err := s.personWorkSnap(ctx, p.ID)
	// 路径钉不住则这条焊次拒绝
	if err != nil {
		// 只拿令牌摘要对会话，原文不入库，失败不打断当前返回
		_ = s.store.DeleteSessionByTokenHash(ctx, secret.TokenHash(token))
		return PadSession{}, err
	}
	// 审计没写下则整次不算完成
	if err := s.audit(ctx, &p.ID, &loginName, "pad_login", s.store.FactoryID().String(), audit.Allow); err != nil {
		// 只拿令牌摘要对会话，原文不入库，失败不打断当前返回
		_ = s.store.DeleteSessionByTokenHash(ctx, secret.TokenHash(token))
		return PadSession{}, err
	}
	return PadSession{
		Token: token, UnwrapKey: append([]byte(nil), who.UnwrapKey...), Account: accountOf(p), Policy: pol,
		SigningPublicKey: pub, Roles: roles, Devices: devices, Work: work,
	}, nil
}
