// 所有 HTTP 请求的唯一出口：带令牌、解析 JSON、把错误码翻成 ApiError。
// 收到 401 且当前持有令牌，说明会话已失效，清掉令牌让路由守卫送回登录页。
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

type Method = "GET" | "POST" | "PUT" | "DELETE";

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

  const res = await fetch(path, {
    method: init.method ?? "GET",
    headers,
    body: init.body === undefined ? undefined : JSON.stringify(init.body),
    signal: init.signal,
  });
  if (res.status === 204) return undefined as T;

  const text = await res.text();
  let data: unknown;
  try {
    data = text ? JSON.parse(text) : undefined;
  } catch {
    data = undefined;
  }
  if (!res.ok) {
    const code = (data as { error?: string } | undefined)?.error ?? res.statusText.toLowerCase();
    if (res.status === 401 && token) session.clear();
    throw new ApiError(res.status, code, translateError(code));
  }
  return data as T;
}

export const http = {
  get: <T>(path: string, signal?: AbortSignal) => request<T>(path, { signal }),
  post: <T>(path: string, body?: unknown) => request<T>(path, { method: "POST", body }),
  del: <T>(path: string) => request<T>(path, { method: "DELETE" }),
};

export function errorMessage(err: unknown) {
  return err instanceof Error ? err.message : String(err);
}
