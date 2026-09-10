import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { http } from "@/shared/api/client";
import { fpath } from "@/shared/auth/session";

export type ClientStatus = "bound" | "void";

export type Client = {
  id: string; // 与 WAN 相同的 Client 稳定身份
  publicKey: string; // 本机公钥，无私钥
  bindingRevision: number; // 已接受的绑定修订
  status: ClientStatus; // bound / void
  boundAt: string; // 最近一次接受为 bound 的时间
  voidedAt: string | null; // 作废时间
};

export type RuntimeGrant = {
  clientId: string; // 签给哪台 Client
  revision: number; // 该 Client 最高修订
  canRun: boolean; // 本修订是否允许运行
  notBefore: string; // 生效时间
  notAfter: string; // 失效时间
  createdAt?: string; // 写入时间
};

export type AcceptClientInput = {
  id: string;
  publicKey: string;
  bindingRevision: number;
};

export type SigningKey = { publicKey: string }; // 本厂签发公钥

export const clientKeys = {
  all: ["clients"] as const,
  runtime: ["runtime-grants"] as const,
  signing: ["signing-key"] as const,
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
  });
}

export function useRuntimeGrants() {
  return useQuery({
    queryKey: clientKeys.runtime,
    queryFn: ({ signal }) => http.get<RuntimeGrant[]>(fpath("/runtime-grants"), signal),
  });
}

export function useSigningKey() {
  return useQuery({
    queryKey: clientKeys.signing,
    queryFn: ({ signal }) => http.get<SigningKey>(fpath("/signing-key"), signal),
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

export function useAcceptClient() {
  return useClientMutation((input: AcceptClientInput) => http.post<Client>(fpath("/clients"), input));
}

export function useVoidClient() {
  return useClientMutation((clientId: string) => http.post<void>(fpath(`/clients/${clientId}/void`)));
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
