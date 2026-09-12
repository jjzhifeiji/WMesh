import { useCatalogMutation, type OrgUnit } from "@/features/catalog/api";
import { http } from "@/shared/api/client";
import { fpath } from "@/shared/auth/session";

export type CreateOrgUnitInput = { name: string; parentId: string | null };

export function useCreateOrgUnit() {
  return useCatalogMutation((input: CreateOrgUnitInput) => http.post<OrgUnit>(fpath("/org-units"), input));
}

// 停用节点：其下不能还有有效子节点；已落库的路径快照不受影响。
export function useDisableOrgUnit() {
  return useCatalogMutation((unitId: string) => http.post<void>(fpath(`/org-units/${unitId}/disable`)));
}

// 停用后可再启用；上级必须已经有效。
export function useEnableOrgUnit() {
  return useCatalogMutation((unitId: string) => http.post<void>(fpath(`/org-units/${unitId}/enable`)));
}

// 零引用才物理删除；有下级、当前人员、有效角色或历史事实会被拒绝。
export function useDeleteOrgUnit() {
  return useCatalogMutation((unitId: string) => http.del<void>(fpath(`/org-units/${unitId}`)));
}

export type UnitTreeNode = OrgUnit & { children?: UnitTreeNode[] };

function sortByCreatedDesc(nodes: UnitTreeNode[]) {
  nodes.sort((a, b) => b.createdAt.localeCompare(a.createdAt));
  for (const n of nodes) {
    if (n.children) sortByCreatedDesc(n.children);
  }
}

// 把平铺的节点按 parentId 拼成树；同级按创建时间从新到旧；父节点缺失挂到根上兜底。
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
  sortByCreatedDesc(roots);
  return roots;
}
