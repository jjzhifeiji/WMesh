import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { factoryKeys } from "@/features/factories/api";
import { http } from "@/shared/api/client";

export type Client = {
  id: string; // 稳定身份，全局唯一
  name: string; // 给人看的名字，可改
  shortCode: string; // Client 短码 C0001…，登记后不改
  deviceSerial?: string; // 机械臂识别号
  publicKey?: string | null; // 本机公钥；未上线为空
  factoryId: string | null; // 当前所属工厂；空表示未分配
  bindingRevision: number; // 绑定修订；分配、改分、收回都升高
  boundAt: string | null; // 当前这次分配生效时间
  lastLoginAt?: string | null; // 厂端上报的最近一次示教器登录；没报过为空
  appVersion: number; // 最近登录时的示教器版本号；没报过为 0
  appVersionName: string; // 最近登录时的示教器版本名
  createdAt: string; // 身份登记时间
};

export type RegisterClientInput = {
  name: string;
  deviceSerial: string;
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

// 收回到未分配，原厂随即作废这台。
export function useUnassignClient() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => http.post<Client>(`/v1/clients/${id}/unassign`),
    onSuccess: () => invalidateClients(qc),
  });
}
