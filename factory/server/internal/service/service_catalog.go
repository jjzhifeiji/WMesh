package service

import (
	"context"

	"wmesh/factory/internal/platform/audit"
)

// Catalog 是厂内名册快照，不含口令或激活口令。
type Catalog struct {
	Me          Account      `json:"me"`          // 当前会话账号
	MyGrants    []RoleGrant  `json:"myGrants"`    // 当前账号的有效角色，任何人都能看自己的
	People      []Account    `json:"people"`      // 本厂人员，不含认证秘密；非超管为空
	OrgTypes    []OrgType    `json:"orgTypes"`    // 本厂组织类型
	OrgUnits    []OrgUnit    `json:"orgUnits"`    // 本厂组织节点
	Assignments []Assignment `json:"assignments"` // 当前有效分配
	RoleGrants  []RoleGrant  `json:"roleGrants"`  // 当前有效角色授予
}

// Catalog 工厂超管可看本厂名册；其他人只看到自己和自己的角色，避免把全厂账号交给无许可者。
func (s *Service) Catalog(ctx context.Context, token string) (Catalog, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return Catalog{}, err
	}
	out := Catalog{
		Me:          acc,
		MyGrants:    []RoleGrant{},
		People:      []Account{},
		OrgTypes:    []OrgType{},
		OrgUnits:    []OrgUnit{},
		Assignments: []Assignment{},
		RoleGrants:  []RoleGrant{},
	}
	if mine, err := s.grantsOf(ctx, acc.ID); err != nil {
		return Catalog{}, err
	} else if mine != nil {
		out.MyGrants = mine
	}
	if err := s.can(ctx, acc, permManageAccount, nil); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "catalog", "self", audit.Allow)
		return out, nil
	}
	people, err := s.store.ListPeople(ctx)
	if err != nil {
		return Catalog{}, err
	}
	for _, p := range people {
		out.People = append(out.People, accountOf(p))
	}
	if out.OrgTypes, err = s.store.ListOrgTypes(ctx); err != nil {
		return Catalog{}, err
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
