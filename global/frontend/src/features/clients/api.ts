import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { factoryKeys } from "@/features/factories/api";
import { http } from "@/shared/api/client";

export type Client = {
  id: string; // Client 稳定身份
  publicKey: string; // 本机公钥，无私钥
  factoryId: string | null; // 当前所属工厂；空表示未绑定
  bindingRevision: number; // 绑定修订；未绑定为 0
  boundAt: string | null; // 当前这次绑定生效时间
  createdAt: string; // 身份登记时间
};

export type BindClientInput = {
  id: string;
  factoryId: string;
  publicKey: string;
};

export const clientKeys = { all: ["wan-clients"] as const };

export function useClients() {
  return useQuery({
    queryKey: clientKeys.all,
    queryFn: ({ signal }) => http.get<Client[]>("/v1/clients", signal),
  });
}

export function useBindClient() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: BindClientInput) => http.post<Client>("/v1/clients", input),
    onSuccess: () => qc.invalidateQueries({ queryKey: clientKeys.all }),
  });
}

export function useRebindClient() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { id: string; factoryId: string }) => http.post<Client>(`/v1/clients/${input.id}/rebind`, { factoryId: input.factoryId }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: clientKeys.all });
      void qc.invalidateQueries({ queryKey: factoryKeys.directory });
    },
  });
}
