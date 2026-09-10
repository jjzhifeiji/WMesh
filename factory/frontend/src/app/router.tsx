import { createBrowserRouter } from "react-router";
import { AccountPage } from "@/features/account/AccountPage";
import { AssignmentsPage } from "@/features/assignments/AssignmentsPage";
import { ActivatePage } from "@/features/auth/ActivatePage";
import { LoginPage } from "@/features/auth/LoginPage";
import { DashboardPage } from "@/features/dashboard/DashboardPage";
import { GrantsPage } from "@/features/grants/GrantsPage";
import { OrgTypesPage } from "@/features/org/OrgTypesPage";
import { OrgUnitsPage } from "@/features/org/OrgUnitsPage";
import { PeoplePage } from "@/features/people/PeoplePage";
import { AdminLayout } from "@/layouts/AdminLayout";
import { AuthLayout } from "@/layouts/AuthLayout";
import { RequireAuth } from "@/shared/auth/RequireAuth";
import { RequireSuperAdmin } from "@/shared/auth/RequireSuperAdmin";
import { NotFoundPage } from "@/shared/ui/NotFoundPage";
import { paths } from "./routes";

const sa = (page: React.ReactNode) => <RequireSuperAdmin>{page}</RequireSuperAdmin>;

// 两棵路由树：公开的登录/激活区，和需要会话的后台区；管理页再套一层超管守卫。
export const router = createBrowserRouter([
  {
    element: <AuthLayout />,
    children: [
      { path: paths.login, element: <LoginPage /> },
      { path: paths.activate, element: <ActivatePage /> },
    ],
  },
  {
    element: (
      <RequireAuth>
        <AdminLayout />
      </RequireAuth>
    ),
    children: [
      { index: true, element: <DashboardPage /> },
      { path: paths.orgTypes, element: sa(<OrgTypesPage />) },
      { path: paths.orgUnits, element: sa(<OrgUnitsPage />) },
      { path: paths.people, element: sa(<PeoplePage />) },
      { path: paths.grants, element: sa(<GrantsPage />) },
      { path: paths.assignments, element: sa(<AssignmentsPage />) },
      { path: paths.account, element: <AccountPage /> },
      { path: "*", element: <NotFoundPage /> },
    ],
  },
]);
