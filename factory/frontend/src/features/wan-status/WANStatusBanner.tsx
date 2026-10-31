import { useQuery } from "@tanstack/react-query";
import { Alert } from "antd";
import dayjs from "dayjs";
import { http } from "@/shared/api/client";
import { fpath } from "@/shared/auth/session";
import { formatDateTime } from "@/shared/format";

type WANStatus = {
  configured: boolean; // 本进程配了 WAN 地址；没配不提示
  connected: boolean; // MQTT 通道当前是否连着
  lastConnectedAt?: string; // 最近一次连上；本进程还没连上过为空
  leaseExpiresAt?: string; // 内容租约到期；还没领到为空
};

// 与云端通道断开时，每页顶上常驻提示；连着或没配云端就不显示。
export function WANStatusBanner() {
  const q = useQuery({
    queryKey: ["wan-status"],
    queryFn: ({ signal }) => http.get<WANStatus>(fpath("/wan-status"), signal),
    refetchInterval: 15000,
  });
  const st = q.data;
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
