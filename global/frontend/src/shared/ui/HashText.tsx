import { Typography } from "antd";
import { shortHash } from "@/shared/format";

// 摘要列表里只露前几位，完整值点复制拿，和云端或包里的核对。
export function HashText({ value }: { value?: string | null }) {
  if (!value) return <>—</>;
  return <Typography.Text copyable={{ text: value, tooltips: ["复制完整摘要", "已复制"] }}>{shortHash(value)}</Typography.Text>;
}
