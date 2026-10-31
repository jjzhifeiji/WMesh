import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
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
  orgPath?: PathNode[]; // 焊工现在所属组织的路径
  day?: string; // 按浏览器时区切的日期
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
  orgPath: PathNode[]; // 焊工现在所属组织的路径
  projectId: string | null; // 工程身份
  projectName: string; // 工程名快照
  weldKind: string; // 焊接模式
  lengthMm: number; // 本段焊长毫米
  durationSec: number; // 本段时长秒
  occurredAt: string; // RFC3339
};

export type WeldRunPage = {
  items: WeldRunRow[]; // 本页起停，新的在前
  total: number; // 同条件总条数
  lengthMm: number; // 同条件焊长合计毫米
  durationSec: number; // 同条件时长合计秒
};

// 下钻筛选；projectId 传 none 表示未关联工程，orgUnitId 传 direct 表示厂直属。
export type WeldRunFilter = {
  personId?: string; // 当时登录人
  projectId?: string; // 工程身份或 none
  orgUnitId?: string; // 发生节点或 direct
};

export type WeldDemoResult = {
  created: number; // 本轮新写入
  total: number; // 演示焊次总数
};

export const weldReportKeys = {
  group: (group: WeldGroup, from: string, to: string, tz: string) => ["weld-reports", group, from, to, tz] as const,
  runs: (from: string, to: string, f: WeldRunFilter, limit: number, offset: number) =>
    ["weld-runs", from, to, f.personId ?? "", f.projectId ?? "", f.orgUnitId ?? "", limit, offset] as const,
};

// 浏览器时区的 IANA 名，后端按它切日；取不到就不带，后端按 UTC。
function browserTimeZone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || "";
  } catch {
    return "";
  }
}

export function useWeldReports(group: WeldGroup, from: string, to: string, enabled = true) {
  const tz = browserTimeZone();
  return useQuery({
    queryKey: weldReportKeys.group(group, from, to, tz),
    queryFn: ({ signal }) =>
      http.get<WeldReportRow[]>(
        fpath(
          `/weld-reports?group=${encodeURIComponent(group)}&from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}${tz ? `&tz=${encodeURIComponent(tz)}` : ""}`,
        ),
        signal,
      ),
    enabled,
  });
}

// 服务端分页取下钻，合计按同一筛选在后端算；翻页时先留着上一页免得闪。
export function useWeldRuns(from: string, to: string, filter: WeldRunFilter, limit: number, offset: number) {
  return useQuery({
    queryKey: weldReportKeys.runs(from, to, filter, limit, offset),
    queryFn: ({ signal }) => {
      const qs = new URLSearchParams({ from, to, limit: String(limit), offset: String(offset) });
      if (filter.personId) qs.set("personId", filter.personId);
      if (filter.projectId) qs.set("projectId", filter.projectId);
      if (filter.orgUnitId) qs.set("orgUnitId", filter.orgUnitId);
      return http.get<WeldRunPage>(fpath(`/weld-runs?${qs.toString()}`), signal);
    },
    placeholderData: keepPreviousData,
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
