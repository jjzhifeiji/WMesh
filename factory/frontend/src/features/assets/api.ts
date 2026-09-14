import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { http } from "@/shared/api/client";
import { fpath } from "@/shared/auth/session";

export type AssetKind = "process" | "project";
export type AssetLevel = "factory" | "personal" | "platform";
export type AssetStatus = "draft" | "available" | "disabled";

export type AssetDep = {
  id: string; // 被依赖工艺稳定身份
  revision: number; // 钉死的工艺修订
  digest: string; // 当时该修订的摘要
};

export type Asset = {
  id: string; // 稳定身份
  kind: AssetKind; // process / project
  level: AssetLevel; // factory / personal
  name: string; // 显示名，不当身份
  code: string; // 只读编号，创建后不改
  status: AssetStatus; // draft / available / disabled
  copyable: boolean; // 可否升档
  revision: number; // 当前修订
  digest: string; // SHA-256
  creatorId: string; // 创建人
  creatorLogin?: string; // 创建人登录名
  creatorDisplay?: string; // 创建人显示名
  factoryId: string; // 所属本厂
  orgUnitId: string | null; // 创建时节点；直属为空
  orgPath: { id: string; name: string }[]; // 创建时路径
  sourceId: string | null; // 升档源身份
  sourceRevision: number | null; // 升档源修订
  deps: AssetDep[]; // 工艺必须空
  createdAt: string; // 创建时间
  updatedAt: string; // 最近升高修订的时间
};

export type CreateAssetInput = {
  kind: AssetKind;
  level: AssetLevel;
  name: string;
  content: string;
  deps?: AssetDep[];
};

export const assetKeys = {
  all: ["assets"] as const,
  kind: (kind: AssetKind) => ["assets", kind] as const,
};

export function useAssets(kind: AssetKind) {
  return useQuery({
    queryKey: assetKeys.kind(kind),
    queryFn: ({ signal }) => http.get<Asset[]>(fpath(`/assets?kind=${kind}`), signal),
  });
}

function useAssetMutation<TData, TVars>(mutationFn: (vars: TVars) => Promise<TData>) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn,
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: assetKeys.all });
      await qc.invalidateQueries({ queryKey: ["asset-content"] });
    },
  });
}

export function useCreateAsset() {
  return useAssetMutation((input: CreateAssetInput) => http.post<Asset>(fpath("/assets"), input));
}

export function useCopyAsset() {
  return useAssetMutation((input: { id: string; name: string }) =>
    http.post<Asset>(fpath(`/assets/${input.id}/copy`), { name: input.name }),
  );
}

export function useRenameAsset() {
  return useAssetMutation((input: { id: string; expected: number; name: string }) =>
    http.post<Asset>(fpath(`/assets/${input.id}/rename`), { expected: input.expected, name: input.name }),
  );
}

export function useSetAssetDeps() {
  return useAssetMutation((input: { id: string; expected: number; deps: AssetDep[] }) =>
    http.post<Asset>(fpath(`/assets/${input.id}/deps`), { expected: input.expected, deps: input.deps }),
  );
}

export function useUpdateAssetContent() {
  return useAssetMutation((input: { id: string; expected: number; content: string }) =>
    http.post<Asset>(fpath(`/assets/${input.id}/content`), { expected: input.expected, content: input.content }),
  );
}

export function useSetAssetCopyable() {
  return useAssetMutation((input: { id: string; expected: number; copyable: boolean }) =>
    http.post<Asset>(fpath(`/assets/${input.id}/copyable`), { expected: input.expected, copyable: input.copyable }),
  );
}

export function usePublishAsset() {
  return useAssetMutation((input: { id: string; expected: number }) =>
    http.post<Asset>(fpath(`/assets/${input.id}/publish`), { expected: input.expected }),
  );
}

export function useDisableAsset() {
  return useAssetMutation((input: { id: string; expected: number }) =>
    http.post<Asset>(fpath(`/assets/${input.id}/disable`), { expected: input.expected }),
  );
}

export function useEnableAsset() {
  return useAssetMutation((input: { id: string; expected: number }) =>
    http.post<Asset>(fpath(`/assets/${input.id}/enable`), { expected: input.expected }),
  );
}

export function useDeleteAsset() {
  return useAssetMutation((id: string) => http.post(fpath(`/assets/${id}/delete`)));
}

export function usePromoteAsset() {
  return useAssetMutation((id: string) => http.post<Asset>(fpath(`/assets/${id}/promote`)));
}

export function useAssetContent(id: string | null) {
  return useQuery({
    queryKey: ["asset-content", id],
    queryFn: ({ signal }) => http.get<{ content: string }>(fpath(`/assets/${id}/content`), signal),
    enabled: Boolean(id),
  });
}
