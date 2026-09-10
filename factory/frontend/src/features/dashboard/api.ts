import { useQuery } from "@tanstack/react-query";
import { http } from "@/shared/api/client";

export type Health = {
  status: "ok" | "degraded";
  version: string;
  db: "ok" | "down";
  oss: "ok" | "down" | "off";
};

// 探活不需要会话；每 30 秒刷一次给概览页看。
export function useHealth() {
  return useQuery({
    queryKey: ["health"],
    queryFn: ({ signal }) => http.get<Health>("/healthz", signal),
    refetchInterval: 30_000,
  });
}
