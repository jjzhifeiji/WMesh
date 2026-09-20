import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { http } from "@/shared/api/client";
import { fpath } from "@/shared/auth/session";

export type PathNode = {
  id: string; // 节点稳定身份
  name: string; // 当时显示名
};

export type WeldGroup = "project" | "person" | "org" | "day" | "person-day";

export type WeldReportRow = {
  personId?: string; // 当时登录人
  loginName?: string; // 登录名
  displayName?: string; // 显示名
  orgUnitId?: string | null; // 发生节点
  orgPath?: PathNode[]; // 发生时路径
  day?: string; // UTC 日期
  projectId?: string | null; // 工程身份
  projectName?: string; // 工程名
  lengthMm: number; // 该组焊长毫米
  durationSec: number; // 该组时长秒
  runCount: number; // 该组次数
};

export type WeldRunRow = {
  id: string; // 事实身份
  personId: string; // 当时登录人
  loginName: string; // 登录名
  displayName: string; // 显示名
  orgUnitId: string | null; // 发生节点
  orgPath: PathNode[]; // 发生时路径
  projectId: string | null; // 工程身份
  projectName: string; // 工程名快照
  weldKind: string; // 焊接模式
  lengthMm: number; // 本段焊长毫米
  durationSec: number; // 本段时长秒
  occurredAt: string; // RFC3339
};

export type WeldDemoResult = {
  created: number; // 本轮新写入
  total: number; // 演示焊次总数
};

export const weldReportKeys = {
  group: (group: WeldGroup, from: string, to: string) => ["weld-reports", group, from, to] as const,
  runs: (from: string, to: string) => ["weld-runs", from, to] as const,
};

export function useWeldReports(group: WeldGroup, from: string, to: string, enabled = true) {
  return useQuery({
    queryKey: weldReportKeys.group(group, from, to),
    queryFn: ({ signal }) =>
      http.get<WeldReportRow[]>(fpath(`/weld-reports?group=${encodeURIComponent(group)}&from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}`), signal),
    enabled,
  });
}

export function useWeldRuns(from: string, to: string) {
  return useQuery({
    queryKey: weldReportKeys.runs(from, to),
    queryFn: ({ signal }) => http.get<WeldRunRow[]>(fpath(`/weld-runs?from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}`), signal),
  });
}

export function useSeedWeldDemo() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => http.post<WeldDemoResult>(fpath("/weld-reports/demo"), {}),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ["weld-reports"] });
      await qc.invalidateQueries({ queryKey: ["weld-runs"] });
    },
  });
}

export function formatLengthM(mm: number) {
  return `${(mm / 1000).toFixed(1)} m`;
}

export function formatDuration(sec: number) {
  const h = Math.floor(sec / 3600);
  const m = Math.floor((sec % 3600) / 60);
  const s = Math.floor(sec % 60);
  return `${h}:${String(m).padStart(2, "0")}:${String(s).padStart(2, "0")}`;
}

export function pathLabel(path: PathNode[] | undefined) {
  if (!path || path.length === 0) return "厂直属";
  return path.map((n) => n.name).join(" / ");
}

export function weldKindLabel(kind: string) {
  if (kind === "single") return "单层";
  if (kind === "multilayer") return "多层";
  if (kind === "tbar") return "T 排";
  return "—";
}

export function personLabel(r: { displayName?: string; loginName?: string }) {
  if (!r.displayName && !r.loginName) return "—";
  if (r.displayName && r.loginName) return `${r.displayName}（${r.loginName}）`;
  return r.displayName || r.loginName || "—";
}

export function avg(total: number, count: number) {
  if (count <= 0) return 0;
  return total / count;
}
