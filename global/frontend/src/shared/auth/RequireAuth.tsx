import type { PropsWithChildren } from "react";
import { Navigate, useLocation } from "react-router";
import { paths } from "@/app/routes";
import { useSessionToken } from "./session";

// 没有会话就送去登录页，并记住来路以便登录后回来。
export function RequireAuth({ children }: PropsWithChildren) {
  const token = useSessionToken();
  const location = useLocation();
  if (!token) {
    return <Navigate to={paths.login} replace state={{ from: location.pathname }} />;
  }
  return children;
}
