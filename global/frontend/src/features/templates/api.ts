import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { http } from "@/shared/api/client";
import type { AssetKind } from "@/features/assets/api";
import type { ContentSchema } from "./schema";

export type ContentTemplate = {
  id: string; // 稳定身份
  kind: AssetKind; // process / project
  name?: string; // 工程模版名称；工艺为空
  revision: number; // 当前修订
  schema: ContentSchema; // 字段表
  digest: string; // SHA-256
};

export const templateKeys = {
  all: ["wan-templates"] as const,
  kind: (kind: AssetKind) => ["wan-templates", kind] as const,
  projects: ["wan-templates", "projects"] as const,
};

export function useTemplate(kind: AssetKind) {
  return useQuery({
    queryKey: templateKeys.kind(kind),
    queryFn: ({ signal }) => http.get<ContentTemplate>(`/v1/templates?kind=${kind}`, signal),
  });
}

export function useProjectTemplates() {
  return useQuery({
    queryKey: templateKeys.projects,
    queryFn: ({ signal }) => http.get<ContentTemplate[]>("/v1/project-templates", signal),
  });
}

export function useCreateProjectTemplate() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { name: string; schema: ContentSchema }) => http.post<ContentTemplate>("/v1/project-templates", input),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: templateKeys.all });
    },
  });
}

export function useUpdateProjectTemplate() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { id: string; expected: number; name: string; schema: ContentSchema }) =>
      http.post<ContentTemplate>(`/v1/project-templates/${input.id}`, {
        expected: input.expected,
        name: input.name,
        schema: input.schema,
      }),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: templateKeys.all });
    },
  });
}

export function useDeleteProjectTemplate() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => http.del<{ status: string }>(`/v1/project-templates/${id}`),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: templateKeys.all });
    },
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
