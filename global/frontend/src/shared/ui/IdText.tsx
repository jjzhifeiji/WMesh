import { Typography } from "antd";
import { shortId } from "@/shared/format";

// 稳定身份的统一展示：缩略 + 复制完整值。
export function IdText({ id }: { id: string }) {
  return (
    <Typography.Text code copyable={{ text: id, tooltips: ["复制完整 ID", "已复制"] }}>
      {shortId(id)}
    </Typography.Text>
  );
}
