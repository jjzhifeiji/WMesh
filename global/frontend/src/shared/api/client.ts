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

type Method = "GET" | "POST" | "PUT" | "PATCH" | "DELETE";

type RequestInitLite = {
  method?: Method;
  body?: unknown;
  form?: FormData;
  signal?: AbortSignal;
};

export async function request<T>(path: string, init: RequestInitLite = {}): Promise<T> {
  const headers = new Headers();
  let body: BodyInit | undefined;
  if (init.form) {
    body = init.form;
  } else if (init.body !== undefined) {
    headers.set("Content-Type", "application/json");
    body = JSON.stringify(init.body);
  }
  const token = session.token;
  if (token) headers.set("Authorization", `Bearer ${token}`);

  const res = await fetch(path, {
    method: init.method ?? "GET",
    headers,
    body,
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

export type FormProgress = { loaded: number; total: number };

function parseApiBody(status: number, text: string, token: string | null): unknown {
  let data: unknown;
  try {
    data = text ? JSON.parse(text) : undefined;
  } catch {
    data = undefined;
  }
  if (status === 204) return undefined;
  if (status >= 200 && status < 300) return data;
  const code = (data as { error?: string } | undefined)?.error ?? "request failed";
  if (status === 401 && token) session.clear();
  throw new ApiError(status, code, translateError(code));
}

// 带上传进度的表单；大包才走这条，普通 JSON 仍用 request。
export function postFormProgress<T>(
  path: string,
  form: FormData,
  onProgress?: (ev: FormProgress) => void,
  signal?: AbortSignal,
): Promise<T> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    const token = session.token;
    xhr.open("POST", path);
    if (token) xhr.setRequestHeader("Authorization", `Bearer ${token}`);
    const onAbort = () => xhr.abort();
    signal?.addEventListener("abort", onAbort);
    xhr.upload.addEventListener("progress", (e) => {
      onProgress?.({ loaded: e.loaded, total: e.lengthComputable ? e.total : 0 });
    });
    xhr.addEventListener("load", () => {
      signal?.removeEventListener("abort", onAbort);
      try {
        resolve(parseApiBody(xhr.status, xhr.responseText, token) as T);
      } catch (err) {
        reject(err);
      }
    });
    xhr.addEventListener("error", () => {
      signal?.removeEventListener("abort", onAbort);
      reject(new ApiError(0, "network error", "网络中断，请重试"));
    });
    xhr.addEventListener("abort", () => {
      signal?.removeEventListener("abort", onAbort);
      reject(new DOMException("Aborted", "AbortError"));
    });
    xhr.send(form);
  });
}

export const http = {
  get: <T>(path: string, signal?: AbortSignal) => request<T>(path, { signal }),
  post: <T>(path: string, body?: unknown) => request<T>(path, { method: "POST", body }),
  postForm: <T>(path: string, form: FormData, signal?: AbortSignal) => request<T>(path, { method: "POST", form, signal }),
  postFormProgress: <T>(path: string, form: FormData, onProgress?: (ev: FormProgress) => void, signal?: AbortSignal) =>
    postFormProgress<T>(path, form, onProgress, signal),
  patch: <T>(path: string, body?: unknown) => request<T>(path, { method: "PATCH", body }),
  del: <T>(path: string) => request<T>(path, { method: "DELETE" }),
};

export function errorMessage(err: unknown) {
  return err instanceof Error ? err.message : String(err);
}
