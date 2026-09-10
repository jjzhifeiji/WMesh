import { createBrowserRouter } from "react-router";
import { LoginPage } from "@/features/auth/LoginPage";
import { DashboardPage } from "@/features/dashboard/DashboardPage";
import { FactoriesPage } from "@/features/factories/FactoriesPage";
import { AdminLayout } from "@/layouts/AdminLayout";
import { AuthLayout } from "@/layouts/AuthLayout";
import { RequireAuth } from "@/shared/auth/RequireAuth";
import { NotFoundPage } from "@/shared/ui/NotFoundPage";
import { paths } from "./routes";

// 两棵路由树：公开的登录区，和需要会话的后台区（侧栏 + 顶栏布局）。
export const router = createBrowserRouter([
  {
    element: <AuthLayout />,
    children: [{ path: paths.login, element: <LoginPage /> }],
  },
  {
    element: (
      <RequireAuth>
        <AdminLayout />
      </RequireAuth>
    ),
    children: [
      { index: true, element: <DashboardPage /> },
      { path: paths.factories, element: <FactoriesPage /> },
      { path: "*", element: <NotFoundPage /> },
    ],
  },
]);
