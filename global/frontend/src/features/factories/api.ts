import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { http } from "@/shared/api/client";

export type Factory = {
  id: string; // 工厂稳定身份，也是厂端选库的依据
  name: string; // 工厂显示名，不当身份
  createdAt: string; // 名录入库时间
  status: "active" | "disabled" | "retired"; // 治理状态
  lifecycleRevision: number; // 厂端只接受更高修订
  statusChangedAt?: string; // 最近一次停用、启用或注销
  enrolledAt?: string; // 厂端认领成功时间
  channelOnline: boolean; // 当前有没有钉死的厂端通道
  channelConnectedAt?: string; // 当前这条通道连上的时间
  channelLastSeenAt?: string; // 最近一次心跳或握手
  channelDisconnectedAt?: string; // 最近一次断开
  webVersion: number; // 厂端前端版本号；离线为 0
  webVersionName: string; // 厂端前端版本名
  serviceVersion: number; // 厂端服务版本号；离线为 0
  serviceVersionName: string; // 厂端服务版本名
};

export type InitialSuperAdmin = {
  factoryId: string; // 一厂只能绑一名初始超管
  personId: string; // 落在厂库里的账号身份
  loginName: string; // 交付时的登录名，不是秘密
  createdAt: string; // 对账记录写入时间
};

export type Directory = {
  factories: Factory[]; // WAN 工厂名录
  initials: InitialSuperAdmin[]; // 各厂初始超管身份，不含密码
};

export type CreateFactoryInput = {
  name: string; // 工厂显示名
  saLogin: string; // 初始超管登录名
  saDisplay: string; // 初始超管显示名
};

export type CreatedFactory = {
  factory: Factory; // 刚写入名录的工厂
  superAdminId: string; // 约定落在厂库的初始超管身份
  enrollmentToken: string; // 一次性建厂码，只在这次响应里出现
};

export const factoryKeys = { directory: ["directory"] as const };

export function useDirectory() {
  return useQuery({
    queryKey: factoryKeys.directory,
    queryFn: ({ signal }) => http.get<Directory>("/v1/directory", signal),
    refetchInterval: 5000,
    placeholderData: keepPreviousData,
  });
}

// 建厂成功后名录会变，直接让缓存失效重拉。
export function useCreateFactory() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: CreateFactoryInput) => http.post<CreatedFactory>("/v1/factories", input),
    onSuccess: () => qc.invalidateQueries({ queryKey: factoryKeys.directory }),
  });
}

export function useDisableFactory() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => http.post<Factory>(`/v1/factories/${id}/disable`),
    onSuccess: () => qc.invalidateQueries({ queryKey: factoryKeys.directory }),
  });
}

export function useEnableFactory() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => http.post<Factory>(`/v1/factories/${id}/enable`),
    onSuccess: () => qc.invalidateQueries({ queryKey: factoryKeys.directory }),
  });
}

export function useDeleteFactory() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => http.del<Factory | undefined>(`/v1/factories/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: factoryKeys.directory }),
  });
}
