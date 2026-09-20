import { keepPreviousData, useMutation, useQuery, useQueryClient, type UseMutationOptions } from "@tanstack/react-query";
import { http } from "@/shared/api/client";
import { fpath } from "@/shared/auth/session";
import type { ScopeKind } from "@/shared/labels";

// 名册是厂内管理端的唯一读模型：一次拉全，改动后整体失效重拉。

export type AccountStatus = "pending" | "active" | "disabled";
export type ActiveStatus = "active" | "disabled";

export type Account = {
  id: string; // 稳定身份，改名也不变
  loginName: string; // 本厂内唯一登录名，不是身份
  displayName: string; // 显示名，可改
  status: AccountStatus; // pending / active / disabled
  appOnline: boolean; // 示教器 MQTT 连着才算在线；管理端登录不算
  appLastSeenAt?: string; // 最近一次示教器登录或 MQTT 见到
  appVersion: number; // 示教器自报 versionCode；0 表示还没报到
  appVersionName: string; // 示教器自报 versionName
  appClientName: string; // 最近一次登录用过的设备名
  appDeviceSerial: string; // 最近一次登录用过的识别号
};

export type OrgUnit = {
  id: string; // 节点稳定身份
  parentId: string | null; // 空表示直接挂在工厂下
  name: string; // 显示名，改名不改历史快照
  status: ActiveStatus; // 停用后不能再当新工作上下文
  createdAt: string; // 创建时间
};

export type Assignment = {
  id: string; // 分配关系稳定身份
  personId: string; // 本厂人员
  orgUnitId: string; // 分配到的组织节点
  status: "active" | "ended"; // active 或 ended；取消不删行
  createdAt: string; // 分配开始时间
  endedAt: string | null; // 取消分配的时间；有效分配必须为空
};

export type RoleGrant = {
  id: string; // 授予记录稳定身份
  personId: string; // 被授予的本厂人员
  role: string; // 固定角色；历史授予可能仍是工艺工程师
  scopeKind: ScopeKind; // factory 或 org_unit
  orgUnitId: string | null; // Factory 作用域必须为空
  status: "active" | "revoked"; // active 或 revoked
  createdAt: string; // 授予时间
  revokedAt: string | null; // 收回时间；有效授予必须为空
};

export type Catalog = {
  me: Account; // 当前会话账号
  myGrants: RoleGrant[]; // 自己的有效角色，任何人都能看到
  people: Account[]; // 本厂人员；非超管为空
  orgUnits: OrgUnit[]; // 本厂组织节点
  assignments: Assignment[]; // 当前有效分配
  roleGrants: RoleGrant[]; // 当前有效角色授予
};

export const catalogKeys = { all: ["catalog"] as const };

export function useCatalog() {
  return useQuery({
    queryKey: catalogKeys.all,
    queryFn: ({ signal }) => http.get<Catalog>(fpath("/catalog"), signal),
    refetchInterval: 5000,
    placeholderData: keepPreviousData,
  });
}

// 是否持有整厂作用域的工厂超管角色；菜单裁剪与页面守卫都以此为准，服务端仍会独立判定。
export function useIsSuperAdmin() {
  const { data } = useCatalog();
  return data?.myGrants.some((g) => g.role === "factory_super_admin" && g.scopeKind === "factory") ?? false;
}

/** 超管或覆盖该节点的管理员；直属厂（无节点）要整厂作用域。 */
export function coversOrg(catalog: Catalog | undefined, orgUnitId: string | null | undefined): boolean {
  const grants = catalog?.myGrants ?? [];
  if (grants.some((g) => g.role === "factory_super_admin" && g.scopeKind === "factory")) return true;
  if (grants.some((g) => g.role === "org_admin" && g.scopeKind === "factory")) return true;
  if (!orgUnitId) return false;
  const byId = new Map((catalog?.orgUnits ?? []).map((u) => [u.id, u]));
  const chain = new Set<string>();
  let cur: string | null | undefined = orgUnitId;
  while (cur) {
    chain.add(cur);
    cur = byId.get(cur)?.parentId;
  }
  return grants.some((g) => g.role === "org_admin" && g.scopeKind === "org_unit" && g.orgUnitId != null && chain.has(g.orgUnitId));
}

// 所有改名册的写操作都用它：成功后让名册失效重拉。
export function useCatalogMutation<TData, TVars>(
  mutationFn: (vars: TVars) => Promise<TData>,
  options?: Omit<UseMutationOptions<TData, Error, TVars>, "mutationFn">,
) {
  const qc = useQueryClient();
  return useMutation<TData, Error, TVars>({
    mutationFn,
    ...options,
    onSuccess: async (data, vars, ctx, mutation) => {
      await qc.invalidateQueries({ queryKey: catalogKeys.all });
      await options?.onSuccess?.(data, vars, ctx, mutation);
    },
  });
}

// 名册里按 ID 找人/找节点的小工具，页面里到处要用。
export function personName(catalog: Catalog | undefined, personId: string) {
  const p = catalog?.people.find((x) => x.id === personId) ?? (catalog?.me.id === personId ? catalog.me : undefined);
  return p ? `${p.displayName}（${p.loginName}）` : personId;
}

export function unitName(catalog: Catalog | undefined, unitId: string | null) {
  if (!unitId) return "（工厂）";
  return catalog?.orgUnits.find((u) => u.id === unitId)?.name ?? unitId;
}
