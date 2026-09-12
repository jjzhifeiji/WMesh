import { useQuery } from "@tanstack/react-query";
import { ApiError, http } from "@/shared/api/client";
import { fpath } from "@/shared/auth/session";
import type { AssetKind } from "@/features/assets/api";
import type { ContentSchema } from "./schema";

export type ContentTemplate = {
  id: string; // 与云端相同
  kind: AssetKind; // process / project
  revision: number; // 已收修订
  schema: ContentSchema; // 字段表
  digest: string; // SHA-256
};

export const templateKeys = {
  all: ["factory-templates"] as const,
  kind: (kind: AssetKind) => ["factory-templates", kind] as const,
};

export function useTemplate(kind: AssetKind) {
  return useQuery({
    queryKey: templateKeys.kind(kind),
    queryFn: async ({ signal }) => {
      try {
        return await http.get<ContentTemplate>(fpath(`/templates?kind=${kind}`), signal);
      } catch (e) {
        if (e instanceof ApiError && e.status === 404) return null;
        throw e;
      }
    },
    retry: false,
  });
}
