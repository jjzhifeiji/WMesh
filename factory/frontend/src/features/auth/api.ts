import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback } from "react";
import { useNavigate } from "react-router";
import { paths } from "@/app/routes";
import { http } from "@/shared/api/client";
import { fpath, session } from "@/shared/auth/session";

export type LoginInput = { factoryId: string; loginName: string; password: string };
// leaseReady：内容租约眼下有效；wanConnected：与云端的通道连着（没配平台地址也为 true）。旧服务端不带这两个字段，按有效处理。
export type SiteFactory = { id: string; name?: string; shortCode?: string; saLogin: string; status?: string; leaseReady?: boolean; wanConnected?: boolean };
export type Site = { wanConfigured: boolean; factories: SiteFactory[] };
export type ClaimInput = { enrollmentCode: string; password: string };
export type Claimed = { factoryId: string; saLogin: string };
export type ApplyInput = { factoryName: string; saLogin: string; saDisplay: string };
// 云端回的申请凭据；secret 只回一次，靠它查进度和完成操作。
export type Ticket = { requestId: string; secret: string; verifyCode: string; expiresAt: string };
export type Progress = { status: "pending" | "approved" | "rejected" | "expired"; factoryName: string };

// 等批准可能要几小时，凭据放 localStorage，关掉页面回来还能接着等。
export function loadTicket<T>(key: string): T | null {
  try {
    const raw = localStorage.getItem(key);
    return raw ? (JSON.parse(raw) as T) : null;
  } catch {
    return null;
  }
}

export function saveTicket(key: string, ticket: unknown) {
  localStorage.setItem(key, JSON.stringify(ticket));
}

export function clearTicket(key: string) {
  localStorage.removeItem(key);
}

export function useSite() {
  return useQuery({
    queryKey: ["site"],
    queryFn: ({ signal }) => http.get<Site>("/v1/site", signal),
    // 有厂租约没领到时每 5 秒再取一次，连上云端领到租约后登录页的租约提示自己消失，不用手动刷新（D-91）。
    refetchInterval: (q) => (q.state.data?.factories.some((f) => f.leaseReady === false) ? 5000 : false),
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

export function useApplyOnboarding() {
  return useMutation({
    mutationFn: (input: ApplyInput) => http.post<Ticket>("/v1/site/onboarding", input),
  });
}

// 等待期间每 5 秒查一次，有结果就停；批准之后还要留着结果给设密码页用。
export function useOnboardingStatus(secret: string | undefined) {
  return useQuery({
    queryKey: ["onboarding-status", secret],
    queryFn: () => http.post<Progress>("/v1/site/onboarding/status", { secret }),
    enabled: Boolean(secret),
    refetchInterval: (q) => (!q.state.data || q.state.data.status === "pending" ? 5000 : false),
    retry: false,
  });
}

export function useStartSAReset() {
  return useMutation({
    mutationFn: (factoryId: string) => http.post<Ticket>("/v1/site/sa-reset", { factoryId }),
  });
}

export function useCompleteSAReset() {
  return useMutation({
    mutationFn: (input: { factoryId: string; secret: string; password: string }) => http.post<{ loginName: string }>("/v1/site/sa-reset/complete", input),
  });
}

// 完成接入：云端批准后用申请密钥走认领并当场设密码，工厂 ID 由通道带回。
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
