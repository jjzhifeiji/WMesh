import { useQuery, useQueryClient, type Query, type QueryClient } from "@tanstack/react-query";
import { Alert } from "antd";
import dayjs from "dayjs";
import { useEffect, useRef } from "react";
import { ApiError, http } from "@/shared/api/client";
import { fpath } from "@/shared/auth/session";
import { formatDateTime } from "@/shared/format";

type WANStatus = {
  configured: boolean; // 本进程配了 WAN 地址；没配不提示
  connected: boolean; // MQTT 通道当前是否连着
  lastConnectedAt?: string; // 最近一次连上；本进程还没连上过为空
  leaseExpiresAt?: string; // 内容租约到期；还没领到为空
};

// 只重读读失败的查询：读得好好的不动。没数据的重读期间 readFailed 仍认它读失败，页面不会闪回空表或默认值（D-105）。
function rereadFailed(qc: QueryClient, match: (q: Query) => boolean) {
  void qc.invalidateQueries({ predicate: (q) => q.queryKey[0] !== "wan-status" && q.state.status === "error" && match(q) });
}

// 租约还没到手时读失败的那些页，等连回云端后自动重读，不用人点重试（F-36）。
function useRereadOnReconnect(st: WANStatus | undefined, updatedAt: number) {
  const qc = useQueryClient();
  const connected = st?.connected ?? false;
  const leaseLive = st?.leaseExpiresAt ? dayjs(st.leaseExpiresAt).isAfter(dayjs()) : false;
  const prev = useRef<{ connected: boolean; leaseLive: boolean } | null>(null);

  // 连回或领到新租约的那一刻，读失败的页都重读一遍；刚进页面第一次读到状态不算连回。
  useEffect(() => {
    if (!st) return;
    const was = prev.current;
    prev.current = { connected, leaseLive };
    if (was && connected && (!was.connected || (leaseLive && !was.leaseLive))) rereadFailed(qc, () => true);
  }, [st, connected, leaseLive, qc]);

  // 连回那一刻抢在租约前重读过、或进页前就失败的：只要租约还活着，每轮状态再试一次因租约失败的查询。
  useEffect(() => {
    if (updatedAt > 0 && connected && leaseLive) rereadFailed(qc, (q) => q.state.error instanceof ApiError && q.state.error.code === "content lease expired");
  }, [connected, leaseLive, updatedAt, qc]);
}

// 与云端通道断开时，每页顶上常驻提示；连着或没配云端就不显示。
export function WANStatusBanner() {
  const q = useQuery({
    queryKey: ["wan-status"],
    queryFn: ({ signal }) => http.get<WANStatus>(fpath("/wan-status"), signal),
    refetchInterval: 15000,
  });
  const st = q.data;
  useRereadOnReconnect(st, q.dataUpdatedAt);
  if (!st || !st.configured || st.connected) return null;

  const last = st.lastConnectedAt ? `最近连上 ${formatDateTime(st.lastConnectedAt)}` : "本次启动后还没连上过";
  let lease = "还没从云端领到内容租约，工艺参数等加密内容暂时读不了。";
  if (st.leaseExpiresAt) {
    lease = dayjs(st.leaseExpiresAt).isAfter(dayjs())
      ? `内容租约 ${formatDateTime(st.leaseExpiresAt)} 到期，到期前连回云端会自动续上；过期后工艺参数等加密内容将读不了。`
      : `内容租约已于 ${formatDateTime(st.leaseExpiresAt)} 到期，工艺参数等加密内容读不了，连回云端后自动恢复。`;
  }

  return (
    <Alert
      type="error"
      showIcon
      banner
      message={`与云端的连接已断开（${last}）`}
      description={`${lease}云端下发、设备分配和软件更新要等连回后才会同步。`}
      style={{ marginBottom: 16 }}
    />
  );
}
