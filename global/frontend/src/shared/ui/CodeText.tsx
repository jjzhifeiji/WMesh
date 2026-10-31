import { Tooltip } from "antd";

// 列表里给人看短码；完整 UUID 只在悬停提示里，排障时再看。
export function CodeText({ code, id }: { code?: string; id: string }) {
  return (
    <Tooltip title={id}>
      <code>{code || "—"}</code>
    </Tooltip>
  );
}
