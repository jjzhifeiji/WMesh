import { useMutation } from "@tanstack/react-query";
import { useCatalogMutation } from "@/features/catalog/api";
import { http } from "@/shared/api/client";
import { fpath } from "@/shared/auth/session";

// 改自己的显示名：登录名和稳定身份不变。
export function useSetMyDisplayName() {
  return useCatalogMutation((displayName: string) => http.post<void>(fpath("/me/display-name"), { displayName }));
}

export type ChangePasswordInput = { currentPassword: string; password: string };

// 改日常密码：须带当前密码，稳定身份不变，本人其它会话作废。
export function useChangePassword() {
  return useMutation({
    mutationFn: (input: ChangePasswordInput) => http.post<void>(fpath("/me/password"), input),
  });
}
