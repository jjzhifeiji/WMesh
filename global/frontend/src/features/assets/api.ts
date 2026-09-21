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
  code: string; // 只读编号，创建后不改
  status: AssetStatus; // draft / available / disabled
	copyable: boolean; // 可否被上一级复制；新建默认为否
  weldKind: string; // 作业类型：single / multilayer / tbar
  revision: number; // 当前修订
  digest: string; // SHA-256
  creatorId: string; // WAN 管理员
  sourceId: string | null; // 升档源厂级身份
  sourceRevision: number | null; // 升档源修订
  sourceFactory: string; // 来源厂显示名；本端新建由服务端写好
  deps: AssetDep[]; // 工艺必须空
  createdAt: string; // 创建时间
  updatedAt: string; // 最近升高修订的时间
};

export type CreateAssetInput = {
  kind: AssetKind;
  name: string;
  content: string;
  weldKind: string; // 作业类型
  copyable?: boolean; // 工艺可复制；空则默认否
  deps?: AssetDep[];
  parentId?: string; // 目录父文件夹；空则挂根
};

export type FSNode = {
  id: string;
  name: string;
  parentId: string | null;
  nodeKind: "folder" | "file";
  assetKind: AssetKind;
  treeLevel: "platform" | "factory" | "personal";
  ownerId?: string | null;
  ownerName?: string;
  assetId?: string | null;
  asset?: Asset | null;
  createdAt: string;
  updatedAt: string;
};

export const fsKeys = {
  all: ["wan-fs"] as const,
  kind: (kind: AssetKind) => ["wan-fs", kind] as const,
  factory: (factoryId: string, kind: AssetKind) => ["wan-fs", "factory", factoryId, kind] as const,
};

export function useFS(kind: AssetKind) {
  return useQuery({
    queryKey: fsKeys.kind(kind),
    queryFn: ({ signal }) => http.get<FSNode[]>(`/v1/fs?kind=${kind}`, signal),
  });
}

export function useFactoryFS(factoryId: string | null, kind: AssetKind) {
  return useQuery({
    queryKey: fsKeys.factory(factoryId ?? "", kind),
    queryFn: ({ signal }) => http.get<FSNode[]>(`/v1/factories/${factoryId}/fs?kind=${kind}`, signal),
    enabled: Boolean(factoryId),
  });
}

function useFSMutation<TData, TVars>(mutationFn: (vars: TVars) => Promise<TData>) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn,
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: fsKeys.all });
      await qc.invalidateQueries({ queryKey: assetKeys.all });
      await qc.invalidateQueries({ queryKey: ["wan-asset-content"] });
    },
  });
}

export function useCreateFSFolder() {
  return useFSMutation((input: { parentId: string; name: string }) => http.post<FSNode>("/v1/fs/folders", input));
}

export function useRenameFSNode() {
  return useFSMutation((input: { id: string; name: string }) => http.post<FSNode>(`/v1/fs/${input.id}/rename`, { name: input.name }));
}

export function useMoveFSNode() {
  return useFSMutation((input: { id: string; parentId: string }) => http.post<FSNode>(`/v1/fs/${input.id}/move`, { parentId: input.parentId }));
}

export function useCopyFSNode() {
  return useFSMutation((input: { id: string; parentId: string }) => http.post<FSNode>(`/v1/fs/${input.id}/copy`, { parentId: input.parentId }));
}

export function useDeleteFSNode() {
  return useFSMutation((id: string) => http.post(`/v1/fs/${id}/delete`));
}

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
      await qc.invalidateQueries({ queryKey: fsKeys.all });
      await qc.invalidateQueries({ queryKey: ["wan-asset-content"] });
    },
  });
}

export function useCreateAsset() {
  return useAssetMutation((input: CreateAssetInput) => http.post<Asset>("/v1/assets", input));
}

export function useCopyAsset() {
  return useAssetMutation((input: { id: string; name: string }) =>
    http.post<Asset>(`/v1/assets/${input.id}/copy`, { name: input.name }),
  );
}

export function useRenameAsset() {
  return useAssetMutation((input: { id: string; expected: number; name: string }) =>
    http.post<Asset>(`/v1/assets/${input.id}/rename`, { expected: input.expected, name: input.name }),
  );
}

export function useSetAssetDeps() {
  return useAssetMutation((input: { id: string; expected: number; deps: AssetDep[] }) =>
    http.post<Asset>(`/v1/assets/${input.id}/deps`, { expected: input.expected, deps: input.deps }),
  );
}

export function useUpdateAssetContent() {
  return useAssetMutation((input: { id: string; expected: number; content: string }) =>
    http.post<Asset>(`/v1/assets/${input.id}/content`, { expected: input.expected, content: input.content }),
  );
}

export function useSetAssetCopyable() {
  return useAssetMutation((input: { id: string; expected: number; copyable: boolean }) =>
    http.post<Asset>(`/v1/assets/${input.id}/copyable`, { expected: input.expected, copyable: input.copyable }),
  );
}

export function usePublishAsset() {
  return useAssetMutation((input: { id: string; expected: number }) =>
    http.post<Asset>(`/v1/assets/${input.id}/publish`, { expected: input.expected }),
  );
}

export function useDisableAsset() {
  return useAssetMutation((input: { id: string; expected: number }) =>
    http.post<Asset>(`/v1/assets/${input.id}/disable`, { expected: input.expected }),
  );
}

export function useEnableAsset() {
  return useAssetMutation((input: { id: string; expected: number }) =>
    http.post<Asset>(`/v1/assets/${input.id}/enable`, { expected: input.expected }),
  );
}

export function useDeleteAsset() {
  return useAssetMutation((id: string) => http.post(`/v1/assets/${id}/delete`));
}

export type PromotableAsset = {
  id: string; // 稳定身份
  kind: AssetKind;
  level: "factory" | "personal" | "platform"; // 厂内级别
  name: string; // 显示名
  code: string; // 只读编号
  revision: number;
  digest: string; // 摘要
  status: AssetStatus;
  copyable: boolean;
  weldKind: string; // 作业类型
};

export function usePromotableAssets(factoryId: string | null, kind: AssetKind) {
  return useQuery({
    queryKey: ["promotable", factoryId, kind],
    queryFn: ({ signal }) => http.get<PromotableAsset[]>(`/v1/factories/${factoryId}/promotable-assets?kind=${kind}`, signal),
    enabled: Boolean(factoryId),
  });
}

export function usePromoteFromFactory() {
  return useAssetMutation((input: { factoryId: string; assetId: string }) =>
    http.post<Asset>("/v1/assets/promote-from", input),
  );
}

export function useAssetContent(id: string | null) {
  return useQuery({
    queryKey: ["wan-asset-content", id],
    queryFn: ({ signal }) => http.get<{ content: string }>(`/v1/assets/${id}/content`, signal),
    enabled: Boolean(id),
  });
}
