import type { PropsWithChildren } from "react";
import { Navigate, useLocation } from "react-router";
import { paths } from "@/app/routes";
import { useSession } from "./session";

// 没有令牌或没选工厂就送去登录页，并记住来路以便登录后回来。
export function RequireAuth({ children }: PropsWithChildren) {
  const { token, factoryId } = useSession();
  const location = useLocation();
  if (!token || !factoryId) {
    return <Navigate to={paths.login} replace state={{ from: location.pathname }} />;
  }
  return children;
}
