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
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return Catalog{}, err
	}
	out := Catalog{
		Me:          acc,
		MyGrants:    []RoleGrant{},
		People:      []Account{},
		OrgUnits:    []OrgUnit{},
		Assignments: []Assignment{},
		RoleGrants:  []RoleGrant{},
	}
	if mine, err := s.grantsOf(ctx, acc.ID); err != nil {
		return Catalog{}, err
	} else if mine != nil {
		out.MyGrants = mine
	}
	online := s.appMQTTPersonIDs()
	// 名册在线只认本厂示教器 MQTT 连着；管理端登录和 12 小时会话都不算。
	markAppOnline(&out.Me, online)
	devices, err := s.store.LatestLoginDevices(ctx)
	if err != nil {
		return Catalog{}, err
	}
	markLastDevice(&out.Me, devices)
	if err := s.can(ctx, acc, permManageAccount, nil); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "catalog", "self", audit.Allow)
		return out, nil
	}
	// 超管才看全厂名册，不含秘密。
	people, err := s.store.ListPeople(ctx)
	if err != nil {
		return Catalog{}, err
	}
	for _, p := range people {
		row := accountOf(p)
		markAppOnline(&row, online)
		markLastDevice(&row, devices)
		out.People = append(out.People, row)
	}
	if out.OrgUnits, err = s.store.ListOrgUnits(ctx); err != nil {
		return Catalog{}, err
	}
	if out.Assignments, err = s.store.ListAssignments(ctx); err != nil {
		return Catalog{}, err
	}
	if out.RoleGrants, err = s.store.ListRoleGrants(ctx); err != nil {
		return Catalog{}, err
	}
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
	if acc == nil || devices == nil {
		return
	}
	row, ok := devices[acc.ID]
	if !ok {
		return
	}
	acc.AppClientName = row.ClientName
	acc.AppDeviceSerial = row.DeviceSerial
}
