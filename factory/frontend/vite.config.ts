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
    chunkSizeWarningLimit: 1500,
  },
});
