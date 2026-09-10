import { useCatalogMutation, type OrgType, type OrgUnit } from "@/features/catalog/api";
import { http } from "@/shared/api/client";
import { fpath } from "@/shared/auth/session";

export type CreateOrgTypeInput = { name: string };
export type CreateOrgUnitInput = { typeId: string; name: string; parentId: string | null };

export function useCreateOrgType() {
  return useCatalogMutation((input: CreateOrgTypeInput) => http.post<OrgType>(fpath("/org-types"), input));
}

// 停用而非删除：被历史引用过的类型只能停，且其下不能还有有效节点。
export function useDisableOrgType() {
  return useCatalogMutation((typeId: string) => http.post<void>(fpath(`/org-types/${typeId}/disable`)));
}

export function useCreateOrgUnit() {
  return useCatalogMutation((input: CreateOrgUnitInput) => http.post<OrgUnit>(fpath("/org-units"), input));
}

// 停用节点：其下不能还有有效子节点；已落库的路径快照不受影响。
export function useDisableOrgUnit() {
  return useCatalogMutation((unitId: string) => http.post<void>(fpath(`/org-units/${unitId}/disable`)));
}

export type UnitTreeNode = OrgUnit & { children?: UnitTreeNode[] };

// 把平铺的节点按 parentId 拼成树；父节点缺失（不该发生）的挂到根上兜底。
export function buildUnitTree(units: OrgUnit[]): UnitTreeNode[] {
  const byId = new Map<string, UnitTreeNode>(units.map((u) => [u.id, { ...u }]));
  const roots: UnitTreeNode[] = [];
  for (const node of byId.values()) {
    const parent = node.parentId ? byId.get(node.parentId) : undefined;
    if (parent) {
      (parent.children ??= []).push(node);
    } else {
      roots.push(node);
    }
  }
  return roots;
}
