import { useCatalogMutation, type Account } from "@/features/catalog/api";
import { http } from "@/shared/api/client";
import { fpath } from "@/shared/auth/session";

export type CreatePersonInput = { loginName: string; displayName: string };

export type CreatedPerson = {
  account: Account;
  activationToken: string; // 一次性激活口令，只在这次响应里出现
};

// 建的是待启用账号；激活口令由超管交给本人自设日常口令。
export function useCreatePerson() {
  return useCatalogMutation((input: CreatePersonInput) => http.post<CreatedPerson>(fpath("/people"), input));
}

// 停用不删：会话上的新操作立刻被拒；最后一名有效超管不能停。
export function useDisablePerson() {
  return useCatalogMutation((personId: string) => http.post<void>(fpath(`/people/${personId}/disable`)));
}
