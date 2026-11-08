import { Alert, Button } from "antd";
import { useState, type CSSProperties } from "react";
import { errorMessage } from "@/shared/api/client";

// 只取提示要用的几项，查询钩子的返回值都满足。
type QueryLike = {
  data?: unknown;
  error: unknown;
  isError: boolean;
  errorUpdatedAt: number;
  refetch: () => Promise<unknown>;
};

// readFailed 读失败且手上没有数据：这时列表和表单都不能画，否则空列表、默认值会被当成真的。
// 没数据的查询每次重取（轮询时每隔几秒）都会退回 pending、清掉 error，只有 errorUpdatedAt 还在；
// 靠它认出「上次读失败、正在重读」，这段时间仍算读失败，页面不在报错和空表之间来回切。
export function readFailed(query: Pick<QueryLike, "data" | "isError" | "errorUpdatedAt">) {
  return query.data === undefined && (query.isError || query.errorUpdatedAt > 0);
}

// 读失败照实说，错误文字已翻成中文（如解包租约已过期）；手上还有旧数据时说明下面是旧的。
export function QueryErrorAlert({ query, what, style }: { query: QueryLike; what: string; style?: CSSProperties }) {
  // 只跟手动重试走，别随页面轮询闪烁。
  const [retrying, setRetrying] = useState(false);
  // 重读期间 error 被清空，记住上一次的错误接着显示。
  const [lastError, setLastError] = useState(query.error);
  if (query.error && query.error !== lastError) setLastError(query.error);
  if (!query.isError && !readFailed(query)) return null;
  const stale = query.data !== undefined;
  return (
    <Alert
      type={stale ? "warning" : "error"}
      showIcon
      message={stale ? `${what}刷新失败，下面是上次读到的内容，可能已过期` : `读不到${what}`}
      description={lastError ? errorMessage(lastError) : "正在重新读取…"}
      style={{ marginBottom: 16, ...style }}
      action={
        <Button
          size="small"
          loading={retrying}
          onClick={() => {
            setRetrying(true);
            void query.refetch().finally(() => setRetrying(false));
          }}
        >
          重试
        </Button>
      }
    />
  );
}
