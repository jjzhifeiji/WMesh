import { useQuery } from "@tanstack/react-query";
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

export type PersonLoginLog = {
  id: string; // 登录记录稳定身份
  personId: string; // 本厂登录人
  kind: string; // pad / client / mqtt
  occurredAt: string; // 服务端记下的时间
  appVersion: number; // 示教器 versionCode；0 表示没报
  appVersionName: string; // 示教器 versionName
  deviceSerial: string; // 机械臂识别号
  deviceModel: string; // 平板型号
  deviceManufacturer: string; // 平板厂商
  androidRelease: string; // 平板系统版本
  networkName: string; // 当时 WiFi 名
  clientId?: string; // 已匹配本机
  clientName: string; // 当时设备名快照
};

// 超管点开某个人的示教器登录现场。
export function usePersonLogins(personId: string | null) {
  return useQuery({
    queryKey: ["person-logins", personId],
    queryFn: ({ signal }) => http.get<PersonLoginLog[]>(fpath(`/people/${personId}/logins`), signal),
    enabled: Boolean(personId),
  });
}
