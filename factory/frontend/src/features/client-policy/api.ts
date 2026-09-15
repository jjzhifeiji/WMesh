import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { http } from "@/shared/api/client";
import { fpath } from "@/shared/auth/session";

export type CacheScope = "current" | "all";

export type ClientPolicy = {
  revision: number; // 策略修订；Client 只接受更高
  maxCachedProjects: number; // 每 Client 工程份上限，≥1
  cacheScope: CacheScope; // current：当前激活；all：该人获准全部
  persistUnwrapKey: boolean; // 包装后的解封材料可否落盘
  keyTtlSeconds: number; // 解封钥时效秒；0 表示仅进程存活
  extra: Record<string, unknown>; // 本厂扩展键；Client 忽略未知
};

export type ClientPolicyInput = Omit<ClientPolicy, "revision">;

export const clientPolicyKey = ["client-policy"] as const;

export function useClientPolicy() {
  return useQuery({
    queryKey: clientPolicyKey,
    queryFn: ({ signal }) => http.get<ClientPolicy>(fpath("/client-policy"), signal),
  });
}

export function useSaveClientPolicy() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: ClientPolicyInput) => http.put<ClientPolicy>(fpath("/client-policy"), input),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: clientPolicyKey });
    },
  });
}
