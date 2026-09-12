// 会话的唯一存放处：工厂 ID 记在 localStorage（不是秘密，下次免填），令牌只放 sessionStorage。
// 只管「有没有会话」，登录/激活/退出的请求在 features/auth 里。
import { useSyncExternalStore } from "react";

const TOKEN_KEY = "wmesh.factory.token";
const FACTORY_KEY = "wmesh.factory.id";

type Snapshot = { token: string | null; factoryId: string };
type Listener = () => void;

const listeners = new Set<Listener>();
let snapshot: Snapshot = {
  token: sessionStorage.getItem(TOKEN_KEY),
  factoryId: localStorage.getItem(FACTORY_KEY) ?? "",
};

function update(next: Partial<Snapshot>) {
  snapshot = { ...snapshot, ...next };
  listeners.forEach((l) => l());
}

export const session = {
  get token() {
    return snapshot.token;
  },
  get factoryId() {
    return snapshot.factoryId;
  },
  setFactoryId(id: string | undefined) {
    const v = id?.trim() ?? "";
    if (!v) return;
    localStorage.setItem(FACTORY_KEY, v);
    update({ factoryId: v });
  },
  setToken(token: string) {
    sessionStorage.setItem(TOKEN_KEY, token);
    update({ token });
  },
  clear() {
    if (snapshot.token === null) return;
    sessionStorage.removeItem(TOKEN_KEY);
    update({ token: null });
  },
  subscribe(listener: Listener) {
    listeners.add(listener);
    return () => {
      listeners.delete(listener);
    };
  },
};

// 组件里用它读会话；变了会自动重渲染。
export function useSession() {
  return useSyncExternalStore(session.subscribe, () => snapshot);
}

// 本厂 API 前缀；所有厂内请求都从这里拼路径。
export function fpath(rel: string) {
  return `/v1/factories/${session.factoryId}${rel}`;
}
