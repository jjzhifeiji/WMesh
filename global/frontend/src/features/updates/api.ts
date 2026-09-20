import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { http, type FormProgress } from "@/shared/api/client";

export type SoftwareKind = "wan_service" | "factory_service" | "client_apk";

export type SoftwareRelease = {
  kind: SoftwareKind; // wan_service / factory_service / client_apk
  version: number; // 单调整数
  versionName: string; // 给人看的版本名
  digest: string; // SHA-256，接口里是 base64
  createdAt: string; // 首次发布
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
  database: number; // 当前库字节
  images: ImageUsage; // 本机 app 镜像
};

// 种类给人看的名字。
export const softwareKindLabel: Record<SoftwareKind, string> = {
  wan_service: "云端服务",
  factory_service: "厂端服务",
  client_apk: "客户端",
};

export const softwareKeys = {
  all: ["wan-software"] as const,
  pending: ["wan-software-pending"] as const,
  storage: ["wan-software-storage"] as const,
};

export function useSoftwareReleases() {
  return useQuery({
    queryKey: softwareKeys.all,
    queryFn: ({ signal }) => http.get<SoftwareRelease[]>("/v1/software", signal),
    placeholderData: keepPreviousData,
  });
}

export function usePendingWANSoftware(enabled: boolean) {
  return useQuery({
    queryKey: softwareKeys.pending,
    queryFn: ({ signal }) => http.get<SoftwareRelease | null>("/v1/software/pending", signal),
    enabled,
    refetchOnMount: "always",
    refetchInterval: 8_000,
  });
}

export function publishSoftware(form: FormData, onProgress?: (ev: FormProgress) => void, signal?: AbortSignal) {
  return http.postFormProgress<SoftwareRelease>("/v1/software", form, onProgress, signal);
}

export function usePublishSoftware() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (form: FormData) => publishSoftware(form),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: softwareKeys.all });
      void qc.invalidateQueries({ queryKey: softwareKeys.pending });
    },
  });
}

export function useDeleteSoftware() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { kind: SoftwareKind; version: number }) =>
      http.del<void>(`/v1/software/${input.kind}/${input.version}`),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: softwareKeys.all });
      void qc.invalidateQueries({ queryKey: softwareKeys.storage });
    },
  });
}

export function usePruneSoftware() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (kind?: SoftwareKind) => http.post<{ deleted: number }>("/v1/software/gc", kind ? { kind } : {}),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: softwareKeys.all });
      void qc.invalidateQueries({ queryKey: softwareKeys.storage });
    },
  });
}

export function useStorageUsage() {
  return useQuery({
    queryKey: softwareKeys.storage,
    queryFn: ({ signal }) => http.get<StorageUsage>("/v1/software/storage", signal),
    refetchInterval: 30_000,
  });
}

export type ImagePruneRequest = { ref: string } | { all: true };

export function useRequestImagePrune() {
  return useMutation({
    mutationFn: (body: ImagePruneRequest) => http.post<void>("/v1/software/images/prune", body),
  });
}

export function useImagePrune(enabled: boolean, nonce = 0) {
  const qc = useQueryClient();
  return useQuery({
    queryKey: [...softwareKeys.all, "images", nonce] as const,
    queryFn: async ({ signal }) => {
      const row = await http.get<ImagePrune>("/v1/software/images/prune", signal);
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

export type ImagePruneJob = { kind: "one"; ref: string } | { kind: "all" };

// 单条和全部清理各自跟自己的进度，互不借用。
export function useImagePruneJob() {
  const request = useRequestImagePrune();
  const [job, setJob] = useState<ImagePruneJob | null>(null);
  const [nonce, setNonce] = useState(0);
  const [posted, setPosted] = useState(false);
  const [stale, setStale] = useState(false);
  const progress = useImagePrune(job !== null && posted, nonce);
  const waiting = job !== null && !progress.data?.ready && !stale;

  useEffect(() => {
    if (!posted || progress.data?.ready) {
      setStale(false);
      return;
    }
    const t = window.setTimeout(() => setStale(true), 30_000);
    return () => window.clearTimeout(t);
  }, [posted, nonce, progress.data?.ready]);

  const run = async (body: ImagePruneRequest) => {
    setJob("ref" in body ? { kind: "one", ref: body.ref } : { kind: "all" });
    setPosted(false);
    setNonce((n) => n + 1);
    try {
      await request.mutateAsync(body);
      setPosted(true);
    } catch (e) {
      setJob(null);
      throw e;
    }
  };

  const rowBusy = (ref: string) => job?.kind === "one" && job.ref === ref && waiting;
  const allBusy = job?.kind === "all" && waiting;
  const rowText = (ref: string) => {
    if (job?.kind !== "one" || job.ref !== ref) return null;
    if (progress.data?.ready) return progress.data.ok ? "已删除" : "清理失败";
    if (stale) return "本机还没回报";
    return "清理中…";
  };
  const allText =
    job?.kind !== "all"
      ? null
      : progress.data?.ready
        ? progress.data.ok
          ? `已释放 ${progress.data.reclaimed || "空间"}`
          : "清理镜像失败"
        : stale
          ? "本机还没回报"
          : "清理中…";

  return { run, waiting, rowBusy, allBusy, rowText, allText };
}

export function useConfirmWANUpdate() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { kind: string; version: number }) => http.post<void>("/v1/software/confirm", input),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: softwareKeys.pending });
      void qc.invalidateQueries({ queryKey: softwareKeys.all });
    },
  });
}
