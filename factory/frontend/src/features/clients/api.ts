import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { http } from "@/shared/api/client";
import { fpath } from "@/shared/auth/session";

export type ClientStatus = "bound" | "void";

export type Client = {
  id: string; // 固定识别号，与 WAN 相同的全局身份
  name: string; // 给人看的名字，可改
  publicKey?: string | null; // 本机公钥；未上线为空
  bindingRevision: number; // 已接受的绑定修订
  status: ClientStatus; // bound / void
  boundAt: string; // 最近一次接受为 bound 的时间
  voidedAt: string | null; // 作废时间
  operatorId?: string; // 当前使用人稳定身份
  operatorLogin?: string; // 当前使用人登录名
  operatorDisplay?: string; // 当前使用人显示名
};

export type RuntimeGrant = {
  clientId: string; // 签给哪台 Client
  revision: number; // 该 Client 最高修订
  canRun: boolean; // 本修订是否允许运行
  notBefore: string; // 生效时间
  notAfter: string; // 失效时间
  createdAt?: string; // 写入时间
};

export const clientKeys = {
  all: ["clients"] as const,
  runtime: ["runtime-grants"] as const,
};

export function grantWindow(days: number) {
  const notBefore = new Date(Date.now() - 60 * 60 * 1000).toISOString();
  const notAfter = new Date(Date.now() + days * 24 * 60 * 60 * 1000).toISOString();
  return { notBefore, notAfter };
}

export function useClients() {
  return useQuery({
    queryKey: clientKeys.all,
    queryFn: ({ signal }) => http.get<Client[]>(fpath("/clients"), signal),
    refetchInterval: 5000,
  });
}

export function useRuntimeGrants() {
  return useQuery({
    queryKey: clientKeys.runtime,
    queryFn: ({ signal }) => http.get<RuntimeGrant[]>(fpath("/runtime-grants"), signal),
  });
}

function useClientMutation<TData, TVars>(mutationFn: (vars: TVars) => Promise<TData>) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn,
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: clientKeys.all });
      await qc.invalidateQueries({ queryKey: clientKeys.runtime });
    },
  });
}

export function useRenameClient() {
  return useClientMutation((input: { id: string; name: string }) => http.patch<Client>(fpath(`/clients/${input.id}`), { name: input.name }));
}

export function useIssueRuntime() {
  return useClientMutation((input: { clientId: string; days: number }) =>
    http.post<RuntimeGrant>(fpath(`/clients/${input.clientId}/runtime`), grantWindow(input.days)),
  );
}

export function useRevokeRuntime() {
  return useClientMutation((input: { clientId: string; days: number }) =>
    http.post<RuntimeGrant>(fpath(`/clients/${input.clientId}/runtime/revoke`), grantWindow(input.days)),
  );
}
