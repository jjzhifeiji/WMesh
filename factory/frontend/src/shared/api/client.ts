// 所有 HTTP 请求的唯一出口：带令牌、解析 JSON、把错误码翻成 ApiError。
// 收到 401，或本厂被 WAN 停用/注销，清掉令牌让路由守卫送回登录页。
import { session } from "@/shared/auth/session";
import { translateError } from "./errors";

export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

type Method = "GET" | "POST" | "PUT" | "PATCH" | "DELETE";

type RequestInitLite = {
  method?: Method;
  body?: unknown;
  signal?: AbortSignal;
};

export async function request<T>(path: string, init: RequestInitLite = {}): Promise<T> {
  const headers = new Headers();
  if (init.body !== undefined) headers.set("Content-Type", "application/json");
  const token = session.token;
  if (token) headers.set("Authorization", `Bearer ${token}`);

  let res: Response;
  try {
    res = await fetch(path, {
      method: init.method ?? "GET",
      headers,
      body: init.body === undefined ? undefined : JSON.stringify(init.body),
      signal: init.signal,
    });
  } catch (err) {
    // 主动取消原样抛，查询库靠它识别；其余都是连不上（断网、服务没起），浏览器只给英文 Failed to fetch。
    if (init.signal?.aborted) throw err;
    throw new ApiError(0, "network error", translateError("network error"));
  }
  if (res.status === 204) return undefined as T;

  const text = await res.text();
  let data: unknown;
  try {
    data = text ? JSON.parse(text) : undefined;
  } catch {
    data = undefined;
  }
  if (!res.ok) {
    const code = (data as { error?: string } | undefined)?.error ?? (res.statusText.toLowerCase() || "request failed");
    if (res.status === 401 && token) session.clear();
    if (res.status === 403 && token && (code === "factory is disabled" || code === "factory is retired")) session.clear();
    throw new ApiError(res.status, code, translateError(code));
  }
  return data as T;
}

export const http = {
  get: <T>(path: string, signal?: AbortSignal) => request<T>(path, { signal }),
  post: <T>(path: string, body?: unknown) => request<T>(path, { method: "POST", body }),
  put: <T>(path: string, body?: unknown) => request<T>(path, { method: "PUT", body }),
  patch: <T>(path: string, body?: unknown) => request<T>(path, { method: "PATCH", body }),
  del: <T>(path: string) => request<T>(path, { method: "DELETE" }),
};

export function errorMessage(err: unknown) {
  return err instanceof Error ? err.message : String(err);
}
