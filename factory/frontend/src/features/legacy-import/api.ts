import { useMutation, useQueryClient } from "@tanstack/react-query";
import { http } from "@/shared/api/client";
import { fpath } from "@/shared/auth/session";
import { assetKeys, type Asset } from "@/features/assets/api";

export type LegacyFile = {
  path: string; // 相对路径
  name?: string; // 显示名，可空
  content: string; // UTF-8 JSON
};

export type LegacyReject = {
  path: string; // 相对路径
  reason: string; // 英文原因
};

export type LegacyImportResult = {
  processes: Asset[]; // 已入本厂工艺
  projects: Asset[]; // 已入本厂工程
  rejected: LegacyReject[]; // 未入的工程或坏文件
};

export function useImportLegacy() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { processes: LegacyFile[]; projects: LegacyFile[] }) =>
      http.post<LegacyImportResult>(fpath("/legacy-import"), input),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: assetKeys.all });
    },
  });
}
