import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { http } from "@/shared/api/client";
import { fpath } from "@/shared/auth/session";

export type FactorySoftwareCurrent = {
  kind: "factory_service"; // 厂服务包
  version: number; // 已确认安装版本；未装为 0
  versionName: string; // 已装版本名；未装为空
};

export type PendingSoftware = {
  kind: "factory_service"; // 厂服务包
  version: number; // 待确认版本
  versionName: string; // 给人看的版本名
};

export type FactorySoftwareKind = "factory_service" | "client_apk";

export type FactorySoftwareRow = {
  kind: FactorySoftwareKind; // factory_service / client_apk
  version: number; // 单调整数
  versionName: string; // 给人看的版本名
  digest: string; // SHA-256，接口里是 base64
  receivedAt: string; // 收到时间
  keep?: "latest" | "installed" | ""; // 空则可清
};

export type ImagePrune = {
  ready: boolean; // updater 已写结果
  ok: boolean; // 清完
  reclaimed?: string; // 回报空间
};

export type ImageItem = {
  ref: string; // repository:tag
  id: string; // docker 镜像短号
  size: number; // 该标签字节
  keep?: "current" | "previous" | ""; // 空则可清
};

export type ImageUsage = {
  kind?: string; // 本机服务种类
  used: number; // 去重后字节
  count: number; // 去重后个数
  items?: ImageItem[]; // 标签列表
};

export type StorageUsage = {
  disk: { path: string; total: number; used: number; avail: number }; // 本机根盘
  oss: { used: number; objects: number; bucket?: string }; // 桶内对象
  database: number; // 当前厂库字节
  images: ImageUsage; // 本机 app 镜像
};

// 种类给人看的名字。
export const softwareKindLabel: Record<FactorySoftwareKind, string> = {
  factory_service: "厂端服务",
  client_apk: "客户端",
};

export const softwareKeys = {
  current: ["factory-software-current"] as const,
  pending: ["factory-software-pending"] as const,
  list: ["factory-software-list"] as const,
  images: ["factory-software-images"] as const,
  storage: ["factory-software-storage"] as const,
};

export function useCurrentFactorySoftware() {
  return useQuery({
    queryKey: softwareKeys.current,
    queryFn: ({ signal }) => http.get<FactorySoftwareCurrent>(fpath("/software/current"), signal),
    refetchInterval: 30_000,
  });
}

export function usePendingFactorySoftware(enabled: boolean) {
  return useQuery({
    queryKey: softwareKeys.pending,
    queryFn: ({ signal }) => http.get<PendingSoftware | null>(fpath("/software/pending"), signal),
    enabled,
    refetchOnMount: "always",
    refetchInterval: 8_000,
  });
}

export function useFactorySoftware() {
  return useQuery({
    queryKey: softwareKeys.list,
    queryFn: ({ signal }) => http.get<FactorySoftwareRow[]>(fpath("/software"), signal),
  });
}

export function useDeleteFactorySoftware() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { kind: FactorySoftwareKind; version: number }) =>
      http.del<void>(fpath(`/software/${input.kind}/${input.version}`)),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: softwareKeys.list });
      void qc.invalidateQueries({ queryKey: softwareKeys.pending });
      void qc.invalidateQueries({ queryKey: softwareKeys.storage });
    },
  });
}

export function usePruneFactorySoftware() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (kind?: FactorySoftwareKind) =>
      http.post<{ deleted: number }>(fpath("/software/gc"), kind ? { kind } : {}),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: softwareKeys.list });
      void qc.invalidateQueries({ queryKey: softwareKeys.pending });
      void qc.invalidateQueries({ queryKey: softwareKeys.storage });
    },
  });
}

export function useStorageUsage() {
  return useQuery({
    queryKey: softwareKeys.storage,
    queryFn: ({ signal }) => http.get<StorageUsage>(fpath("/software/storage"), signal),
    refetchInterval: 30_000,
  });
}

export function useRequestImagePrune() {
  return useMutation({
    mutationFn: () => http.post<void>(fpath("/software/images/prune")),
  });
}

export function useImagePrune(enabled: boolean, nonce = 0) {
  const qc = useQueryClient();
  return useQuery({
    queryKey: [...softwareKeys.images, nonce] as const,
    queryFn: async ({ signal }) => {
      const row = await http.get<ImagePrune>(fpath("/software/images/prune"), signal);
      if (row.ready) {
        void qc.invalidateQueries({ queryKey: softwareKeys.storage });
      }
      return row;
    },
    enabled,
    refetchInterval: (q) => {
      if (!enabled || q.state.data?.ready) return false;
      if ((q.state.dataUpdateCount ?? 0) >= 15) return false;
      return 2_000;
    },
  });
}

export function useConfirmFactoryUpdate() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { kind: string; version: number }) => http.post<void>(fpath("/software/confirm"), input),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: softwareKeys.pending });
      void qc.invalidateQueries({ queryKey: softwareKeys.current });
      void qc.invalidateQueries({ queryKey: softwareKeys.list });
    },
  });
}
