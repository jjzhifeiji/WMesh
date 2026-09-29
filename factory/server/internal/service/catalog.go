package service

import (
	"context"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/audit"
)

// Catalog 是厂内名册快照，不含密码或激活码。
type Catalog struct {
	Me          Account      `json:"me"`          // 当前会话账号
	MyGrants    []RoleGrant  `json:"myGrants"`    // 当前账号的有效角色，任何人都能看自己的
	People      []Account    `json:"people"`      // 本厂人员，不含认证秘密；非超管为空
	OrgUnits    []OrgUnit    `json:"orgUnits"`    // 本厂组织节点
	Assignments []Assignment `json:"assignments"` // 当前有效分配
	RoleGrants  []RoleGrant  `json:"roleGrants"`  // 当前有效角色授予
}

// Catalog 工厂超管可看本厂名册；其他人只看到自己和自己的角色，避免把全厂账号交给无许可者。
func (s *Org) Catalog(ctx context.Context, token string) (Catalog, error) {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return Catalog{}, err
	}
	// 先放上自己和空名册，超管才填全厂
	out := Catalog{
		Me:          acc,
		MyGrants:    []RoleGrant{},
		People:      []Account{},
		OrgUnits:    []OrgUnit{},
		Assignments: []Assignment{},
		RoleGrants:  []RoleGrant{},
	}
	// 角色读失败就无法判定，拒绝放行
	if mine, err := s.grantsOf(ctx, acc.ID); err != nil {
		return Catalog{}, err
		// 读到了自己的角色就填上，没有则保持空列表
	} else if mine != nil {
		// 填上自己的有效角色，谁都能看自己的
		out.MyGrants = mine
	}
	// 只把示教器连着的人当成在线
	online := s.appMQTTPersonIDs()
	// 名册在线只认本厂示教器 MQTT 连着；管理端登录和 12 小时会话都不算。
	markAppOnline(&out.Me, online)
	// 取每人最近一次带设备的登录
	devices, err := s.store.LatestLoginDevices(ctx)
	// 登录现场读失败，名册中止
	if err != nil {
		return Catalog{}, err
	}
	// 补最近一次带设备的登录现场
	markLastDevice(&out.Me, devices)
	// 不是工厂超管，拒绝这次账号或策略操作
	if err := s.can(ctx, acc, permManageAccount, nil); err != nil {
		// 看不到全厂名册，只记下查看了自己
		_ = s.audit(ctx, &acc.ID, nil, "catalog", "self", audit.Allow)
		return out, nil
	}
	// 超管才看全厂名册，不含秘密。
	people, err := s.store.ListPeople(ctx)
	// 人员列表读失败，名册不交残缺
	if err != nil {
		return Catalog{}, err
	}
	// 逐个人员收进名册，并标在线和最近设备
	for _, p := range people {
		// 收成不含口令的对外账号
		row := accountOf(p)
		// 有效且示教器连着才标在线
		markAppOnline(&row, online)
		// 补最近一次带设备的登录现场
		markLastDevice(&row, devices)
		// 把这一条收进结果，漏了清单就不齐
		out.People = append(out.People, row)
	}
	// 组织读失败，名册不交残缺
	if out.OrgUnits, err = s.store.ListOrgUnits(ctx); err != nil {
		return Catalog{}, err
	}
	// 分配读失败，名册不交残缺
	if out.Assignments, err = s.store.ListAssignments(ctx); err != nil {
		return Catalog{}, err
	}
	// 角色列表读失败，名册不交残缺
	if out.RoleGrants, err = s.store.ListRoleGrants(ctx); err != nil {
		return Catalog{}, err
	}
	// 审计没写下则整次不算完成
	if err := s.audit(ctx, &acc.ID, nil, "catalog", s.store.FactoryID().String(), audit.Allow); err != nil {
		return Catalog{}, err
	}
	return out, nil
}

// 有效账号且示教器 MQTT 连着才标在线。
func markAppOnline(acc *Account, online map[uuid.UUID]struct{}) {
	if acc == nil || acc.Status != StatusActive {
		return
	}
	_, acc.AppOnline = online[acc.ID]
}

// 最近一次带上设备的登录现场；没有则空。
func markLastDevice(acc *Account, devices map[uuid.UUID]PersonLoginLog) {
	// 没有账号或没有现场表则不动展示字段
	if acc == nil || devices == nil {
		return
	}
	// 取出这一条并看是否存在，没有则留空
	row, ok := devices[acc.ID]
	// 没有登录现场则设备名留空
	if !ok {
		return
	}
	// 补上最近一次登录用过的设备名
	acc.AppClientName = row.ClientName
	// 补上最近一次登录用过的识别号
	acc.AppDeviceSerial = row.DeviceSerial
}
