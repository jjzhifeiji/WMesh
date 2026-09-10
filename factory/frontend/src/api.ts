const TOKEN_KEY = "wmesh.factory.token";
const FACTORY_KEY = "wmesh.factory.id";

export type Account = {
  id: string;
  loginName: string;
  displayName: string;
  status: string;
};

export type OrgType = { id: string; name: string; status: string };
export type OrgUnit = {
  id: string;
  orgTypeId: string;
  parentId: string | null;
  name: string;
  status: string;
};
export type Assignment = { id: string; personId: string; orgUnitId: string; status: string };
export type RoleGrant = {
  id: string;
  personId: string;
  role: string;
  scopeKind: string;
  orgUnitId: string | null;
  status: string;
};

export type Catalog = {
  me: Account;
  people: Account[];
  orgTypes: OrgType[];
  orgUnits: OrgUnit[];
  assignments: Assignment[];
  roleGrants: RoleGrant[];
};

const zh: Record<string, string> = {
  "invalid credentials": "登录名或口令不对",
  unauthorized: "请先登录",
  forbidden: "没有这项许可",
  "account pending": "账号还待启用",
  "account disabled": "账号已停用",
  "invalid activation": "激活口令不对",
  "already activated": "已经激活过了",
  "last factory super admin": "不能拿掉最后一名有效工厂超管",
  "not found": "找不到这家工厂或对象",
  "internal error": "服务异常",
};

export function translate(msg: string) {
  return zh[msg] ?? msg;
}

export function factoryId() {
  return sessionStorage.getItem(FACTORY_KEY) ?? "";
}

export function setFactoryId(id: string) {
  sessionStorage.setItem(FACTORY_KEY, id);
}

export function setToken(token: string | null) {
  if (token) sessionStorage.setItem(TOKEN_KEY, token);
  else sessionStorage.removeItem(TOKEN_KEY);
}

export function hasToken() {
  return Boolean(sessionStorage.getItem(TOKEN_KEY));
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

export function basePath() {
  return `/v1/factories/${factoryId()}`;
}
