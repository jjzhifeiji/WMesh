import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { factoryKeys } from "@/features/factories/api";
import { http } from "@/shared/api/client";

export type Client = {
  id: string; // 固定识别号，全局唯一身份
  name: string; // 给人看的名字，可改
  publicKey?: string | null; // 本机公钥；未上线为空
  factoryId: string | null; // 当前所属工厂；空表示未分配
  bindingRevision: number; // 绑定修订；未分配为 0
  boundAt: string | null; // 当前这次分配生效时间
  createdAt: string; // 身份登记时间
};

export type RegisterClientInput = {
  name: string;
  factoryId?: string;
};

export const clientKeys = { all: ["wan-clients"] as const };

export function useClients() {
  return useQuery({
    queryKey: clientKeys.all,
    queryFn: ({ signal }) => http.get<Client[]>("/v1/clients", signal),
    refetchInterval: 5000,
    placeholderData: keepPreviousData,
  });
}

function invalidateClients(qc: ReturnType<typeof useQueryClient>) {
  void qc.invalidateQueries({ queryKey: clientKeys.all });
  void qc.invalidateQueries({ queryKey: factoryKeys.directory });
}

export function useRegisterClient() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: RegisterClientInput) => http.post<Client>("/v1/clients", input),
    onSuccess: () => invalidateClients(qc),
  });
}

export function useRenameClient() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { id: string; name: string }) => http.patch<Client>(`/v1/clients/${input.id}`, { name: input.name }),
    onSuccess: () => invalidateClients(qc),
  });
}

export function useAssignClient() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { id: string; factoryId: string }) => http.post<Client>(`/v1/clients/${input.id}/assign`, { factoryId: input.factoryId }),
    onSuccess: () => invalidateClients(qc),
  });
}

export function useRebindClient() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { id: string; factoryId: string }) => http.post<Client>(`/v1/clients/${input.id}/rebind`, { factoryId: input.factoryId }),
    onSuccess: () => invalidateClients(qc),
  });
}
