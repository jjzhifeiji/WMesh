import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { http } from "@/shared/api/client";
import type { AssetKind } from "@/features/assets/api";
import type { ContentSchema } from "./schema";

export type ContentTemplate = {
  id: string; // 稳定身份
  kind: AssetKind; // process / project
  name?: string; // 模版名称
  revision: number; // 当前修订
  schema: ContentSchema; // 字段表
  digest: string; // SHA-256
};

export const templateKeys = {
  all: ["wan-templates"] as const,
  processes: ["wan-templates", "processes"] as const,
  projects: ["wan-templates", "projects"] as const,
};

export function useProcessTemplates() {
  return useQuery({
    queryKey: templateKeys.processes,
    queryFn: ({ signal }) => http.get<ContentTemplate[]>("/v1/process-templates", signal),
  });
}

// 新建工艺模版；带 id 只用于按预置身份补建，名称和字段表留空就用预置的。
export function useCreateProcessTemplate() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { id?: string; name: string; schema?: ContentSchema }) => http.post<ContentTemplate>("/v1/process-templates", input),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: templateKeys.all });
    },
  });
}

export function useUpdateProcessTemplate() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { id: string; expected: number; name: string; schema: ContentSchema }) =>
      http.post<ContentTemplate>(`/v1/process-templates/${input.id}`, {
        expected: input.expected,
        name: input.name,
        schema: input.schema,
      }),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: templateKeys.all });
    },
  });
}

export function useDeleteProcessTemplate() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => http.del<{ status: string }>(`/v1/process-templates/${id}`),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: templateKeys.all });
    },
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
    mutationFn: (input: { id?: string; name: string; schema: ContentSchema }) => http.post<ContentTemplate>("/v1/project-templates", input),
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
