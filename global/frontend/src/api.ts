const TOKEN_KEY = "wmesh.wan.token";

export type Factory = {
  id: string;
  name: string;
  createdAt: string;
};

export type Initial = {
  factoryId: string;
  personId: string;
  loginName: string;
};

export type Directory = {
  factories: Factory[];
  initials: Initial[];
};

export type CreatedFactory = {
  factory: Factory;
  superAdminId: string;
  activationToken: string;
};

const zh: Record<string, string> = {
  "invalid credentials": "登录名或口令不对",
  unauthorized: "请先登录",
  forbidden: "没有这项许可",
  "internal error": "服务异常",
};

export function translate(msg: string) {
  return zh[msg] ?? msg;
}

export async function api<T>(path: string, init?: RequestInit): Promise<T> {
  const token = sessionStorage.getItem(TOKEN_KEY);
  const headers = new Headers(init?.headers);
  if (init?.body) headers.set("Content-Type", "application/json");
  if (token) headers.set("Authorization", `Bearer ${token}`);
  const res = await fetch(path, { ...init, headers });
  if (res.status === 204) return undefined as T;
  const text = await res.text();
  const data = text ? JSON.parse(text) : {};
  if (!res.ok) {
    throw new Error(translate(data.error ?? res.statusText));
  }
  return data as T;
}

export function setToken(token: string | null) {
  if (token) sessionStorage.setItem(TOKEN_KEY, token);
  else sessionStorage.removeItem(TOKEN_KEY);
}

export function hasToken() {
  return Boolean(sessionStorage.getItem(TOKEN_KEY));
}
