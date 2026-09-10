import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { grantWindow } from "@/features/clients/api";
import { http } from "@/shared/api/client";
import { fpath } from "@/shared/auth/session";

export type PersonGrant = {
  personId: string; // 本厂账号
  clientId: string; // 绑定 Client
  loginName: string; // 签发时登录名
  allowDirect: boolean; // 是否允许 Factory 直属
  active: boolean; // 签发时账号是否有效
  revision: number; // 人员授权修订
  notBefore: string; // 生效时间
  notAfter: string; // 失效时间
};

export type IssuePersonInput = {
  personId: string;
  clientId: string;
  days: number;
};

export const personGrantKeys = { all: ["person-offline-grants"] as const };

export function usePersonGrants() {
  return useQuery({
    queryKey: personGrantKeys.all,
    queryFn: ({ signal }) => http.get<PersonGrant[]>(fpath("/person-offline-grants"), signal),
  });
}

export function useIssuePersonGrant() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: IssuePersonInput) =>
      http.post<PersonGrant>(fpath("/person-offline-grants"), {
        personId: input.personId,
        clientId: input.clientId,
        ...grantWindow(input.days),
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: personGrantKeys.all }),
  });
}
