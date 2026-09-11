import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { http } from "@/shared/api/client";

export type AssetKind = "process" | "project";
export type AssetStatus = "draft" | "available" | "disabled";

export type AssetDep = {
  id: string; // 被依赖工艺稳定身份
  revision: number; // 钉死的工艺修订
  digest: string; // 当时该修订的摘要
};

export type Asset = {
  id: string; // 稳定身份
  kind: AssetKind; // process / project
  level: "platform"; // 固定平台级
  name: string; // 显示名
  status: AssetStatus; // draft / available / disabled
  copyable: boolean; // 平台级必须为否
  revision: number; // 当前修订
  digest: string; // SHA-256
  creatorId: string; // WAN 管理员
  sourceId: string | null; // 升档源厂级身份
  sourceRevision: number | null; // 升档源修订
  sourceFactoryId: string | null; // 升档源厂
  deps: AssetDep[]; // 工艺必须空
  createdAt: string; // 创建时间
  updatedAt: string; // 最近升高修订的时间
};

export type CreateAssetInput = {
  kind: AssetKind;
  name: string;
  content: string;
  deps?: AssetDep[];
};

export const assetKeys = {
  all: ["wan-assets"] as const,
  kind: (kind: AssetKind) => ["wan-assets", kind] as const,
};

export function useAssets(kind: AssetKind) {
  return useQuery({
    queryKey: assetKeys.kind(kind),
    queryFn: ({ signal }) => http.get<Asset[]>(`/v1/assets?kind=${kind}`, signal),
  });
}

function useAssetMutation<TData, TVars>(mutationFn: (vars: TVars) => Promise<TData>) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn,
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: assetKeys.all });
    },
  });
}

export function useCreateAsset() {
  return useAssetMutation((input: CreateAssetInput) => http.post<Asset>("/v1/assets", input));
}

export function useRenameAsset() {
  return useAssetMutation((input: { id: string; expected: number; name: string }) =>
    http.post<Asset>(`/v1/assets/${input.id}/rename`, { expected: input.expected, name: input.name }),
  );
}

export function usePublishAsset() {
  return useAssetMutation((input: { id: string; expected: number }) =>
    http.post<Asset>(`/v1/assets/${input.id}/publish`, { expected: input.expected }),
  );
}

export function usePromoteSnapshot() {
  return useAssetMutation((snap: unknown) => http.post<Asset>("/v1/assets/promote", snap));
}

export function useAssetContent(id: string | null) {
  return useQuery({
    queryKey: ["wan-asset-content", id],
    queryFn: ({ signal }) => http.get<{ content: string }>(`/v1/assets/${id}/content`, signal),
    enabled: Boolean(id),
  });
}
