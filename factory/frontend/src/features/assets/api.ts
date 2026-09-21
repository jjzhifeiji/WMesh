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
  weldKind: string; // 作业类型：single / multilayer / tbar
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
  weldKind: string; // 作业类型
  copyable?: boolean; // 工艺可复制；空则默认可复制
  deps?: AssetDep[];
  parentId?: string; // 目录父文件夹；空则挂对应树的根
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
  all: ["fs"] as const,
  kind: (kind: AssetKind) => ["fs", kind] as const,
};

export function useFS(kind: AssetKind) {
  return useQuery({
    queryKey: fsKeys.kind(kind),
    queryFn: ({ signal }) => http.get<FSNode[]>(fpath(`/fs?kind=${kind}`), signal),
    refetchInterval: 8000,
  });
}

function useFSMutation<TData, TVars>(mutationFn: (vars: TVars) => Promise<TData>) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn,
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: fsKeys.all });
      await qc.invalidateQueries({ queryKey: assetKeys.all });
      await qc.invalidateQueries({ queryKey: ["asset-content"] });
    },
  });
}

export function useCreateFSFolder() {
  return useFSMutation((input: { parentId: string; name: string }) => http.post<FSNode>(fpath("/fs/folders"), input));
}

export function useRenameFSNode() {
  return useFSMutation((input: { id: string; name: string }) => http.post<FSNode>(fpath(`/fs/${input.id}/rename`), { name: input.name }));
}

export function useMoveFSNode() {
  return useFSMutation((input: { id: string; parentId: string }) => http.post<FSNode>(fpath(`/fs/${input.id}/move`), { parentId: input.parentId }));
}

export function useCopyFSNode() {
  return useFSMutation((input: { id: string; parentId: string }) => http.post<FSNode>(fpath(`/fs/${input.id}/copy`), { parentId: input.parentId }));
}

export function useDeleteFSNode() {
  return useFSMutation((id: string) => http.post(fpath(`/fs/${id}/delete`)));
}

export const assetKeys = {
  all: ["assets"] as const,
  kind: (kind: AssetKind) => ["assets", kind] as const,
};

export function useAssets(kind: AssetKind) {
  return useQuery({
    queryKey: assetKeys.kind(kind),
    queryFn: ({ signal }) => http.get<Asset[]>(fpath(`/assets?kind=${kind}`), signal),
    refetchInterval: 8000,
  });
}

export function syncAssets(kind: AssetKind) {
  return http.post<{ ok: string }>(fpath(`/assets/sync?kind=${kind}`));
}

function useAssetMutation<TData, TVars>(mutationFn: (vars: TVars) => Promise<TData>) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn,
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: assetKeys.all });
      await qc.invalidateQueries({ queryKey: fsKeys.all });
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
