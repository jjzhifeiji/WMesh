import { Tooltip } from "antd";
import { formatAgo, formatTime } from "@/shared/format";

// 显示多久以前，悬停看具体时间。
export function AgoText({ value }: { value?: string | null }) {
  if (!value) return <>—</>;
  return <Tooltip title={formatTime(value)}>{formatAgo(value)}</Tooltip>;
}
