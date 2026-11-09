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
  shortCode: string; // 本厂短码 F01…F99，创建后不改
  saLogin: string; // 初始超管登录名
  saDisplay: string; // 初始超管显示名
};

export type Directory = {
  factories: Factory[]; // WAN 工厂名录
};

export type OnboardingRequest = {
  id: string; // 申请稳定身份
  kind: "enroll" | "reset_sa"; // 新厂接入 / 找回初始超管密码
  status: string; // 列表里都是 pending
  verifyCode: string; // 六位核对码，和现场比对
  factoryId?: string; // 找回时对应的工厂
  factoryName: string; // 工厂显示名
  saLogin: string; // 初始超管登录名
  saDisplay: string; // 初始超管显示名
  sourceAddr: string; // 发起申请的来源地址
  serviceVersionName: string; // 发起方厂端服务版本名
  createdAt: string; // 申请时间
  expiresAt: string; // 到期时间
};

export const factoryKeys = { directory: ["directory"] as const, onboarding: ["onboarding"] as const };

export function useDirectory() {
  return useQuery({
    queryKey: factoryKeys.directory,
    queryFn: ({ signal }) => http.get<Directory>("/v1/directory", signal),
    refetchInterval: 5000,
    placeholderData: keepPreviousData,
  });
}

export function usePendingOnboarding() {
  return useQuery({
    queryKey: factoryKeys.onboarding,
    queryFn: ({ signal }) => http.get<{ requests: OnboardingRequest[] }>("/v1/onboarding", signal),
    refetchInterval: 5000,
    placeholderData: keepPreviousData,
  });
}

// 批准后新厂才进名录，两边一起刷新。
export function useApproveOnboarding() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, name }: { id: string; name: string }) => http.post<OnboardingRequest>(`/v1/onboarding/${id}/approve`, { name }),
    onSuccess: () => Promise.all([qc.invalidateQueries({ queryKey: factoryKeys.onboarding }), qc.invalidateQueries({ queryKey: factoryKeys.directory })]),
  });
}

export function useRejectOnboarding() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => http.post<OnboardingRequest>(`/v1/onboarding/${id}/reject`),
    onSuccess: () => qc.invalidateQueries({ queryKey: factoryKeys.onboarding }),
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
