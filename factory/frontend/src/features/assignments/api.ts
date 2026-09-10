import { useCatalogMutation } from "@/features/catalog/api";
import { http } from "@/shared/api/client";
import { fpath } from "@/shared/auth/session";

export type AssignInput = { personId: string; orgUnitId: string };

// 分配只决定人员能选哪些工作上下文，不产生任何权限。
export function useAssign() {
  return useCatalogMutation((input: AssignInput) => http.post<void>(fpath("/assignments"), input));
}

// 取消只结束当前分配，历史事实仍记在原路径上。
export function useUnassign() {
  return useCatalogMutation((input: AssignInput) => http.post<void>(fpath("/assignments/end"), input));
}
