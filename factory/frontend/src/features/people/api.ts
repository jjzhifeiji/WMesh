import { useCatalogMutation, type Account, type RoleGrant } from "@/features/catalog/api";
import { http } from "@/shared/api/client";
import { fpath } from "@/shared/auth/session";
import type { Role, ScopeKind } from "@/shared/labels";

export type CreatePersonInput = { loginName: string; displayName: string };

export type CreatedPerson = {
  account: Account; // 新建即有效，默认密码为登录名+123456，不回传原文
};

export type GrantRoleInput = {
  personId: string;
  role: Role;
  scopeKind: ScopeKind;
  orgUnitId: string | null; // 整厂作用域必须为空
};

// 建完即可登录；默认密码为登录名+123456，不回传原文。
export function useCreatePerson() {
  return useCatalogMutation((input: CreatePersonInput) => http.post<CreatedPerson>(fpath("/people"), input));
}

// 停用不删，可再启用；最后一名有效超管不能停。
export function useDisablePerson() {
  return useCatalogMutation((personId: string) => http.post<void>(fpath(`/people/${personId}/disable`)));
}

// 停用后可再启用；有日常密码的直接能登录。
export function useEnablePerson() {
  return useCatalogMutation((personId: string) => http.post<void>(fpath(`/people/${personId}/enable`)));
}

// 超管重置他人密码：旧密码立刻失效，改回登录名+123456。
export function useResetPersonPassword() {
  return useCatalogMutation((personId: string) => http.post<CreatedPerson>(fpath(`/people/${personId}/reset-password`)));
}

// 授角色必须带作用域；分配组织不会自动产生角色。
export function useGrantRole() {
  return useCatalogMutation((input: GrantRoleInput) => http.post<RoleGrant>(fpath("/grants"), input));
}

// 收回后旧会话上的新操作立刻按新角色判定；最后一名有效超管的角色不能收。
export function useRevokeRole() {
  return useCatalogMutation((grantId: string) => http.post<void>(fpath(`/grants/${grantId}/revoke`)));
}
