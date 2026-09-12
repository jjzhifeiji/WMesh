import { useMutation } from "@tanstack/react-query";
import { http } from "@/shared/api/client";

export type ChangePasswordInput = { password: string };

// 改日常密码：稳定身份不变，旧会话继续有效。
export function useChangePassword() {
  return useMutation({
    mutationFn: (input: ChangePasswordInput) => http.post<void>("/v1/me/password", input),
  });
}
