import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { http } from "@/shared/api/client";
import { fpath } from "@/shared/auth/session";

export type CacheScope = "current" | "all";

export type ClientPolicy = {
  revision: number; // 策略修订；Client 只接受更高
  maxCachedProjects: number; // 已不再限制份数，保存时原样带回
  cacheScope: CacheScope; // 现已固定 all，后台不再限制范围
  persistUnwrapKey: boolean; // 解封钥可否落盘；退出或登录到期必清
  keyTtlSeconds: number; // 登录时效秒；0 表示直到退出
  encryptPouch: boolean; // 本机袋是否 SQLCipher 整库加密
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
    onSuccess: async (row) => {
      qc.setQueryData(clientPolicyKey, row);
      await qc.invalidateQueries({ queryKey: clientPolicyKey });
    },
  });
}
