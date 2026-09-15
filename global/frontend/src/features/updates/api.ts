import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { http } from "@/shared/api/client";

export type SoftwareKind = "factory_service" | "client_apk";

export type SoftwareRelease = {
  kind: SoftwareKind; // factory_service / client_apk
  version: number; // 单调整数
  versionName: string; // 给人看的版本名
  digest: string; // SHA-256，接口里是 base64
  createdAt: string; // 首次发布
};

export const softwareKindLabel: Record<SoftwareKind, string> = {
  factory_service: "厂端服务",
  client_apk: "客户端",
};

export const softwareKeys = { all: ["wan-software"] as const };

export function useSoftwareReleases() {
  return useQuery({
    queryKey: softwareKeys.all,
    queryFn: ({ signal }) => http.get<SoftwareRelease[]>("/v1/software", signal),
    placeholderData: keepPreviousData,
  });
}

export function usePublishSoftware() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (form: FormData) => http.postForm<SoftwareRelease>("/v1/software", form),
    onSuccess: () => qc.invalidateQueries({ queryKey: softwareKeys.all }),
  });
}

export function useDistributeSoftware() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { kind: SoftwareKind; version: number; factoryId: string }) =>
      http.post<SoftwareRelease>("/v1/software/distribute", input),
    onSuccess: () => qc.invalidateQueries({ queryKey: softwareKeys.all }),
  });
}
