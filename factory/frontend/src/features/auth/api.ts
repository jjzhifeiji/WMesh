import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback } from "react";
import { useNavigate } from "react-router";
import { paths } from "@/app/routes";
import { http } from "@/shared/api/client";
import { fpath, session } from "@/shared/auth/session";

export type LoginInput = { factoryId: string; loginName: string; password: string };
export type ActivateInput = { factoryId: string; loginName: string; activationToken: string; password: string };
export type SiteFactory = { id: string; name?: string; shortCode?: string; saLogin: string; status?: string };
export type Site = { wanConfigured: boolean; factories: SiteFactory[] };
export type ClaimInput = { enrollmentCode: string; password: string };
export type Claimed = { factoryId: string; saLogin: string };

export function useSite() {
  return useQuery({
    queryKey: ["site"],
    queryFn: ({ signal }) => http.get<Site>("/v1/site", signal),
  });
}

// 登录前先把工厂 ID 记下来，路径才拼得出来；成功即持有令牌。
export function useLogin() {
  return useMutation({
    mutationFn: ({ factoryId, ...body }: LoginInput) => {
      session.setFactoryId(factoryId);
      return http.post<{ token: string }>(fpath("/login"), body);
    },
    onSuccess: (out) => session.setToken(out.token),
  });
}

// 认领：贴上 WAN 建厂码并当场设密码，工厂 ID 由通道带回。
export function useClaim() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: ClaimInput) => http.post<Claimed>("/v1/site/claim", input),
    onSuccess: (out) => {
      session.setFactoryId(out.factoryId);
      void qc.invalidateQueries({ queryKey: ["site"] });
    },
  });
}

// 激活：持有者用 8 位激活码自设日常密码，账号转为有效；WAN 不参与。
export function useActivate() {
  return useMutation({
    mutationFn: ({ factoryId, ...body }: ActivateInput) => {
      session.setFactoryId(factoryId);
      return http.post<void>(fpath("/activate"), body);
    },
  });
}

// 退出：服务端删会话，本地清令牌与所有缓存，再回登录页。
export function useLogout() {
  const qc = useQueryClient();
  const navigate = useNavigate();
  return useCallback(async () => {
    try {
      await http.post(fpath("/logout"));
    } catch {
      /* 会话本来就没了也照样清本地 */
    }
    session.clear();
    qc.clear();
    navigate(paths.login, { replace: true });
  }, [qc, navigate]);
}
