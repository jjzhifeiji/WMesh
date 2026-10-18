import { useMutation, useQueryClient } from "@tanstack/react-query";
import { http } from "@/shared/api/client";
import { assetKeys, type Asset } from "@/features/assets/api";

export type LegacyFile = {
  path: string; // 相对路径
  name?: string; // 显示名，可空
  content: string; // UTF-8 JSON
  overwrite?: boolean; // 本份覆盖
  rename?: boolean; // 本份按路径重命名
  weldKind?: string; // 重试工艺时带回的作业类型，只处理这一份
  suffix?: string; // 和作业类型一起带回的名字后缀
};

export type LegacyReject = {
  path: string; // 相对路径
  reason: string; // 英文原因
  weldKind?: string; // 工艺按哪种作业类型那份；工程或不限类型时没有
  suffix?: string; // 那份工艺名字后接的作业类型后缀
};

export type LegacyImportResult = {
  processes: Asset[]; // 新建或覆盖后的平台工艺
  projects: Asset[]; // 新建或覆盖后的平台工程
  rejected: LegacyReject[]; // 未入的工程或坏文件
  skipped: LegacyReject[]; // 同名跳过
};

export function useImportLegacy() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { processes: LegacyFile[]; projects: LegacyFile[]; overwrite: boolean; rename: boolean }) =>
      http.post<LegacyImportResult>("/v1/legacy-import", input),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: assetKeys.all });
    },
  });
}
