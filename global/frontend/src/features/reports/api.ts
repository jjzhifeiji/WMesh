import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { http } from "@/shared/api/client";

export type WeldGroup = "factory" | "project" | "day" | "grain";

export type WeldSummaryRow = {
  factoryId: string; // 上送工厂
  factoryName: string; // 名录显示名
  day: string; // UTC 日期
  projectName: string; // 工程名快照或未关联
  weldKind: string; // 焊接模式
  lengthMm: number; // 该粒焊长毫米
  durationSec: number; // 该粒时长秒
  runCount: number; // 该粒次数
};

export const weldReportKeys = {
  list: (from: string, to: string, factoryId: string) => ["weld-reports", from, to, factoryId] as const,
};

export function useWeldReports(from: string, to: string, factoryId = "") {
  const qs = new URLSearchParams({ from, to });
  if (factoryId) qs.set("factoryId", factoryId);
  return useQuery({
    queryKey: weldReportKeys.list(from, to, factoryId),
    queryFn: ({ signal }) => http.get<WeldSummaryRow[]>(`/v1/weld-reports?${qs}`, signal),
    placeholderData: keepPreviousData,
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

export function weldKindLabel(kind: string) {
  if (kind === "single") return "单层";
  if (kind === "multilayer") return "多层";
  if (kind === "tbar") return "T 排";
  return "—";
}

export function avg(total: number, count: number) {
  if (count <= 0) return 0;
  return total / count;
}

export function groupWeldRows(rows: WeldSummaryRow[], group: WeldGroup): WeldSummaryRow[] {
  if (group === "grain") return rows;
  const map = new Map<string, WeldSummaryRow>();
  for (const r of rows) {
    const key = group === "factory" ? r.factoryId : group === "project" ? `${r.factoryId}|${r.projectName}` : `${r.factoryId}|${r.day}`;
    const cur = map.get(key);
    if (!cur) {
      map.set(key, { ...r, weldKind: "" });
      continue;
    }
    cur.runCount += r.runCount;
    cur.lengthMm += r.lengthMm;
    cur.durationSec += r.durationSec;
  }
  return [...map.values()];
}
