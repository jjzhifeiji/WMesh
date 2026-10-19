import { useMutation } from "@tanstack/react-query";
import { http } from "@/shared/api/client";

export type ChangePasswordInput = { currentPassword: string; password: string };

// 改日常密码：须带当前密码，稳定身份不变，其它会话作废。
export function useChangePassword() {
  return useMutation({
    mutationFn: (input: ChangePasswordInput) => http.post<void>("/v1/me/password", input),
  });
}
