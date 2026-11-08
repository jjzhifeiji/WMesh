import { Result, Spin } from "antd";
import type { PropsWithChildren } from "react";
import { useCatalog, useIsSuperAdmin } from "@/features/catalog/api";
import { errorMessage } from "@/shared/api/client";

// 管理页只给工厂超管；其他角色进来看到 403，而不是空表格。
export function RequireSuperAdmin({ children }: PropsWithChildren) {
  const catalog = useCatalog();
  const isSA = useIsSuperAdmin();
  if (catalog.isLoading) return <Spin />;
  // 名册读不到（如内容租约没了、服务端出错）不等于不是超管，照实报错，不显示「需要工厂超管」。
  if (!catalog.data && catalog.isError) {
    return <Result status="warning" title="读不到本厂名册，暂时无法确认账号权限" subTitle={errorMessage(catalog.error)} />;
  }
  if (!isSA) {
    return <Result status="403" title="需要工厂超管" subTitle="当前账号没有整厂管理权限，只能查看自己的账号与角色。" />;
  }
  return children;
}
