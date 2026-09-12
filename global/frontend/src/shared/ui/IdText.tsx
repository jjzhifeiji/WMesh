import { Typography } from "antd";

// 能换行就全显示；容器仍溢出才省略，完整值可复制。
export function IdText({ id }: { id: string }) {
  return (
    <span className="id-text" title={id}>
      <code>{id}</code>
      <Typography.Text copyable={{ text: id, tooltips: ["复制完整 ID", "已复制"] }} />
    </span>
  );
}
