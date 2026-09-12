import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback } from "react";
import { useNavigate } from "react-router";
import { paths } from "@/app/routes";
import { http } from "@/shared/api/client";
import { session } from "@/shared/auth/session";

export type Me = {
  id: string; // WAN 管理员稳定身份
  loginName: string;
};

export type LoginInput = { loginName: string; password: string };

export const authKeys = { me: ["me"] as const };

export function useMe() {
  return useQuery({ queryKey: authKeys.me, queryFn: ({ signal }) => http.get<Me>("/v1/me", signal) });
}

// 登录成功即持有令牌；密码只在这一次请求里出现。
export function useLogin() {
  return useMutation({
    mutationFn: (input: LoginInput) => http.post<{ token: string }>("/v1/login", input),
    onSuccess: (out) => session.set(out.token),
  });
}

// 退出：服务端删会话，本地清令牌与所有缓存，再回登录页。
export function useLogout() {
  const qc = useQueryClient();
  const navigate = useNavigate();
  return useCallback(async () => {
    try {
      await http.post("/v1/logout");
    } catch {
      /* 会话本来就没了也照样清本地 */
    }
    session.clear();
    qc.clear();
    navigate(paths.login, { replace: true });
  }, [qc, navigate]);
}
