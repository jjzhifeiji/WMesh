import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { App as AntdApp, ConfigProvider } from "antd";
import zhCN from "antd/locale/zh_CN";
import dayjs from "dayjs";
import "dayjs/locale/zh-cn";
import { useState, type PropsWithChildren } from "react";

dayjs.locale("zh-cn");

// 服务端状态统一走 TanStack Query；失败只重试一次，401 由 api client 清会话。
function makeQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: 1, refetchOnWindowFocus: false, staleTime: 10_000 },
    },
  });
}

// 全局上下文：antd 中文与主题、消息/弹窗上下文、查询缓存。
export function AppProviders({ children }: PropsWithChildren) {
  const [queryClient] = useState(makeQueryClient);
  return (
    <ConfigProvider locale={zhCN} theme={{ token: { colorPrimary: "#1f4b3a", borderRadius: 6 } }}>
      <AntdApp>
        <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
      </AntdApp>
    </ConfigProvider>
  );
}
