import { Result, Spin } from "antd";
import type { PropsWithChildren } from "react";
import { useCatalog, useIsSuperAdmin } from "@/features/catalog/api";

// 管理页只给工厂超管；其他角色进来看到 403，而不是空表格。
export function RequireSuperAdmin({ children }: PropsWithChildren) {
  const catalog = useCatalog();
  const isSA = useIsSuperAdmin();
  if (catalog.isLoading) return <Spin />;
  if (!isSA) {
    return <Result status="403" title="需要工厂超管" subTitle="当前账号没有整厂管理权限，只能查看自己的账号与角色。" />;
  }
  return children;
}
