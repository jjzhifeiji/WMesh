// 会话令牌的唯一存放处：sessionStorage + 可订阅的内存快照。
// 只管「有没有令牌」，登录/退出的请求在 features/auth 里。
import { useSyncExternalStore } from "react";

const TOKEN_KEY = "wmesh.wan.token";

type Listener = () => void;
const listeners = new Set<Listener>();
let token: string | null = sessionStorage.getItem(TOKEN_KEY);

function emit() {
  listeners.forEach((l) => l());
}

export const session = {
  get token() {
    return token;
  },
  set(next: string) {
    token = next;
    sessionStorage.setItem(TOKEN_KEY, next);
    emit();
  },
  clear() {
    if (token === null) return;
    token = null;
    sessionStorage.removeItem(TOKEN_KEY);
    emit();
  },
  subscribe(listener: Listener) {
    listeners.add(listener);
    return () => {
      listeners.delete(listener);
    };
  },
};

// 组件里用它读令牌；令牌变了会自动重渲染。
export function useSessionToken() {
  return useSyncExternalStore(session.subscribe, () => session.token);
}
