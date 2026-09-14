import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { http } from "@/shared/api/client";
import type { AssetKind } from "@/features/assets/api";
import type { ContentSchema } from "./schema";

export type ContentTemplate = {
  id: string; // 稳定身份
  kind: AssetKind; // process / project
  revision: number; // 当前修订
  schema: ContentSchema; // 字段表
  digest: string; // SHA-256
};

export const templateKeys = {
  all: ["wan-templates"] as const,
  kind: (kind: AssetKind) => ["wan-templates", kind] as const,
};

export function useTemplate(kind: AssetKind) {
  return useQuery({
    queryKey: templateKeys.kind(kind),
    queryFn: ({ signal }) => http.get<ContentTemplate>(`/v1/templates?kind=${kind}`, signal),
  });
}

export function useBuiltinTemplate(kind: AssetKind, enabled = true) {
  return useQuery({
    queryKey: ["wan-template-builtin", kind],
    queryFn: ({ signal }) => http.get<{ kind: AssetKind; schema: ContentSchema }>(`/v1/templates/builtin?kind=${kind}`, signal),
    enabled,
  });
}

export function useUpdateTemplate() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { kind: AssetKind; expected: number; schema: ContentSchema }) =>
      http.post<ContentTemplate>("/v1/templates", input),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: templateKeys.all });
    },
  });
}
