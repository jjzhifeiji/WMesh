import react from "@vitejs/plugin-react";
import { fileURLToPath, URL } from "node:url";
import { defineConfig } from "vite";

// 开发时 /v1 与 /healthz 代理到本地 Go 服务；生产由 Go 进程直接托管构建产物。
export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) },
  },
  server: {
    port: 5174,
    proxy: {
      "/v1": "http://127.0.0.1:52081",
      "/healthz": "http://127.0.0.1:52081",
    },
  },
  build: {
    // 语法目标对齐样式里用到的 :has()（Chromium 105）；Win7/8 上最后的 Chrome 109 也在内。
    // Vite 只转语法不补 API，源码别用更新的 API（tsconfig 的 lib 卡在 ES2022）。
    target: ["chrome105", "edge105", "firefox121", "safari16"],
    chunkSizeWarningLimit: 1500,
  },
});
