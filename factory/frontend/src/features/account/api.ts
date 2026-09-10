import { useMutation } from "@tanstack/react-query";
import { http } from "@/shared/api/client";
import { fpath } from "@/shared/auth/session";

export type ChangePasswordInput = { password: string };

// 改日常口令：稳定身份不变，旧会话继续有效。
export function useChangePassword() {
  return useMutation({
    mutationFn: (input: ChangePasswordInput) => http.post<void>(fpath("/me/password"), input),
  });
}
