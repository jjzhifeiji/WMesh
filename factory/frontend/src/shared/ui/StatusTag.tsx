import { Tag } from "antd";
import { statusColor, statusLabel } from "@/shared/labels";

export function StatusTag({ status }: { status: string }) {
  return <Tag color={statusColor(status)}>{statusLabel(status)}</Tag>;
}
