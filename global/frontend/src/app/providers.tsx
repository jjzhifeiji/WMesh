import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { App as AntdApp, ConfigProvider } from "antd";
import zhCN from "antd/locale/zh_CN";
import dayjs from "dayjs";
import "dayjs/locale/zh-cn";
import { useEffect, useState, type PropsWithChildren } from "react";
import { session } from "@/shared/auth/session";

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
  // 查询键不含账号：令牌一变（401 被清、退出、换人登录）就清空缓存，旧会话的数据不留给新会话。
  useEffect(() => {
    let last = session.token;
    return session.subscribe(() => {
      // 令牌没变的通知不清缓存。
      if (session.token === last) return;
      last = session.token;
      queryClient.clear();
    });
  }, [queryClient]);
  return (
    <ConfigProvider locale={zhCN} theme={{ token: { colorPrimary: "#1f4b3a", borderRadius: 6 } }}>
      <AntdApp>
        <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
      </AntdApp>
    </ConfigProvider>
  );
}
