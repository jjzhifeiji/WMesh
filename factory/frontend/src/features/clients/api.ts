import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { http } from "@/shared/api/client";
import { fpath } from "@/shared/auth/session";

export type ClientStatus = "bound" | "void";

export type Client = {
  id: string; // 稳定身份，与 WAN 相同
  shortCode?: string; // 设备短码 Cxxxx，随绑定补齐
  name: string; // WAN 下发的设备名，厂端只读
  nickname: string; // 厂端起的昵称；空表示没起
  deviceSerial?: string; // 机械臂识别号；未登记为空
  bindingRevision: number; // 已接受的绑定修订
  status: ClientStatus; // bound / void
  boundAt: string; // 最近一次接受为 bound 的时间
  voidedAt: string | null; // 作废时间
  lastUserLogin?: string; // 最近一次焊的人的登录名
  lastUserDisplay?: string; // 最近一次焊的人的显示名
  lastUsedAt?: string; // 最近一次焊完时间
  weldLengthMm: number; // 累计焊长毫米
  weldRunCount: number; // 累计焊次
};

export const clientKeys = { all: ["clients"] as const };

export function useClients() {
  return useQuery({
    queryKey: clientKeys.all,
    queryFn: ({ signal }) => http.get<Client[]>(fpath("/clients"), signal),
    refetchInterval: 5000,
  });
}

// 改昵称；留空表示去掉昵称，改回显示 WAN 设备名。
export function useRenameClient() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { id: string; nickname: string }) => http.patch<Client>(fpath(`/clients/${input.id}`), { name: input.nickname }),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: clientKeys.all });
    },
  });
}
