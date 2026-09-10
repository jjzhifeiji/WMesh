import { useCatalogMutation, type RoleGrant } from "@/features/catalog/api";
import { http } from "@/shared/api/client";
import { fpath } from "@/shared/auth/session";
import type { Role, ScopeKind } from "@/shared/labels";

export type GrantRoleInput = {
  personId: string;
  role: Role;
  scopeKind: ScopeKind;
  orgUnitId: string | null; // 整厂作用域必须为空
};

// 授角色必须带作用域；分配组织不会自动产生角色。
export function useGrantRole() {
  return useCatalogMutation((input: GrantRoleInput) => http.post<RoleGrant>(fpath("/grants"), input));
}

// 收回后旧会话上的新操作立刻按新角色判定；最后一名有效超管的角色不能收。
export function useRevokeRole() {
  return useCatalogMutation((grantId: string) => http.post<void>(fpath(`/grants/${grantId}/revoke`)));
}
