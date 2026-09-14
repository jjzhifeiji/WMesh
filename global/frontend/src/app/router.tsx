import { createBrowserRouter } from "react-router";
import { LoginPage } from "@/features/auth/LoginPage";
import { DashboardPage } from "@/features/dashboard/DashboardPage";
import { FactoriesPage } from "@/features/factories/FactoriesPage";
import { ClientsPage } from "@/features/clients/ClientsPage";
import { AssetsPage } from "@/features/assets/AssetsPage";
import { TemplatesPage } from "@/features/templates/TemplatesPage";
import { ProjectTemplatesPage } from "@/features/templates/ProjectTemplatesPage";
import { AccountPage } from "@/features/account/AccountPage";
import { AdminLayout } from "@/layouts/AdminLayout";
import { AuthLayout } from "@/layouts/AuthLayout";
import { RequireAuth } from "@/shared/auth/RequireAuth";
import { NotFoundPage } from "@/shared/ui/NotFoundPage";
import { PlaceholderPage } from "@/shared/ui/PlaceholderPage";
import { placeholderPaths } from "./navigation";
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
      { path: paths.clients, element: <ClientsPage /> },
      { path: paths.processes, element: <AssetsPage kind="process" /> },
      { path: paths.projects, element: <AssetsPage kind="project" /> },
      { path: paths.templates, element: <TemplatesPage /> },
      { path: paths.projectTemplates, element: <ProjectTemplatesPage /> },
      { path: paths.account, element: <AccountPage /> },
      ...placeholderPaths.map((path) => ({ path, element: <PlaceholderPage /> })),
      { path: "*", element: <NotFoundPage /> },
    ],
  },
]);
