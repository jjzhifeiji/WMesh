import { useQuery } from "@tanstack/react-query";
import { useCatalogMutation, type Account, type RoleGrant } from "@/features/catalog/api";
import { http } from "@/shared/api/client";
import { fpath } from "@/shared/auth/session";
import type { Role } from "@/shared/labels";

export type CreatePersonInput = { loginName: string; displayName: string };

export type CreatedPerson = {
  account: Account; // 新建即有效，默认密码为登录名+123456，不回传原文
};

export type GrantRoleInput = {
  personId: string;
  role: Role; // 三种固定角色，都覆盖整厂
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

// 超管按人设置退出后是否留下示教器库文件。
export function useSetKeepPouch() {
  return useCatalogMutation((input: { personId: string; keepPouch: boolean }) =>
    http.post<CreatedPerson>(fpath(`/people/${input.personId}/keep-pouch`), { keepPouch: input.keepPouch }),
  );
}

// 超管重置他人密码：旧密码立刻失效，改回登录名+123456。
export function useResetPersonPassword() {
  return useCatalogMutation((personId: string) => http.post<CreatedPerson>(fpath(`/people/${personId}/reset-password`)));
}

// 超管改人员的所属组织；空表示厂直属。只用于按组织统计，不给权限。
export function useSetPersonOrg() {
  return useCatalogMutation((input: { personId: string; orgUnitId: string | null }) =>
    http.post<CreatedPerson>(fpath(`/people/${input.personId}/org`), { orgUnitId: input.orgUnitId }),
  );
}

// 超管改别人的显示名；登录名不变。
export function useSetPersonDisplayName() {
  return useCatalogMutation((input: { personId: string; displayName: string }) =>
    http.post<CreatedPerson>(fpath(`/people/${input.personId}/display-name`), { displayName: input.displayName }),
  );
}

// 授角色，都覆盖整厂；所属组织不给权限。
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
