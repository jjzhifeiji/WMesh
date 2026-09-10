import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { http } from "@/shared/api/client";

export type Factory = {
  id: string; // 工厂稳定身份，也是厂端选库的依据
  name: string; // 工厂显示名，不当身份
  createdAt: string; // 名录入库时间
};

export type InitialSuperAdmin = {
  factoryId: string; // 一厂只能绑一名初始超管
  personId: string; // 落在厂库里的账号身份
  loginName: string; // 交付时的登录名，不是秘密
  createdAt: string; // 对账记录写入时间
};

export type Directory = {
  factories: Factory[]; // WAN 工厂名录
  initials: InitialSuperAdmin[]; // 各厂初始超管身份，不含口令
};

export type CreateFactoryInput = {
  name: string; // 工厂显示名
  saLogin: string; // 初始超管登录名
  saDisplay: string; // 初始超管显示名
};

export type CreatedFactory = {
  factory: Factory; // 刚写入名录的工厂
  superAdminId: string; // 厂库里的初始超管身份
  activationToken: string; // 一次性激活口令，只在这次响应里出现
};

export const factoryKeys = { directory: ["directory"] as const };

export function useDirectory() {
  return useQuery({
    queryKey: factoryKeys.directory,
    queryFn: ({ signal }) => http.get<Directory>("/v1/directory", signal),
  });
}

// 建厂成功后名录会变，直接让缓存失效重拉。
export function useCreateFactory() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: CreateFactoryInput) => http.post<CreatedFactory>("/v1/factories", input),
    onSuccess: () => qc.invalidateQueries({ queryKey: factoryKeys.directory }),
  });
}
