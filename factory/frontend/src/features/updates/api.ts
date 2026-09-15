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

export const softwareKeys = {
  current: ["factory-software-current"] as const,
  pending: ["factory-software-pending"] as const,
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
    refetchInterval: 8_000,
  });
}

export function useConfirmFactoryUpdate() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { kind: string; version: number }) => http.post<void>(fpath("/software/confirm"), input),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: softwareKeys.pending });
      void qc.invalidateQueries({ queryKey: softwareKeys.current });
    },
  });
}
