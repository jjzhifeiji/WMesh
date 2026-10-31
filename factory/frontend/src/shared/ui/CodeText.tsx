import { Tooltip, Typography } from "antd";

// 给人看的短码（F01、C0001、编号）；完整 UUID 收进悬停提示，可复制。没有短码才退回 UUID。
export function CodeText({ code, id }: { code?: string | null; id: string }) {
  const text = code?.trim();
  return (
    <Tooltip title={id}>
      <Typography.Text copyable={{ text: id, tooltips: ["复制完整 ID", "已复制"] }}>{text || id}</Typography.Text>
    </Tooltip>
  );
}
