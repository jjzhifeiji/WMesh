import { Navigate, createBrowserRouter } from "react-router";
import { AssetsPage } from "@/features/assets/AssetsPage";
import { AccountPage } from "@/features/account/AccountPage";
import { AssignmentsPage } from "@/features/assignments/AssignmentsPage";
import { ActivatePage } from "@/features/auth/ActivatePage";
import { ClaimPage } from "@/features/auth/ClaimPage";
import { LoginPage } from "@/features/auth/LoginPage";
import { ClientPolicyPage } from "@/features/client-policy/ClientPolicyPage";
import { ClientsPage } from "@/features/clients/ClientsPage";
import { DashboardPage } from "@/features/dashboard/DashboardPage";
import { LegacyImportPage } from "@/features/legacy-import/LegacyImportPage";
import { OrgUnitsPage } from "@/features/org/OrgUnitsPage";
import { PeoplePage } from "@/features/people/PeoplePage";
import { ReportsPage } from "@/features/reports/ReportsPage";
import { AdminLayout } from "@/layouts/AdminLayout";
import { AuthLayout } from "@/layouts/AuthLayout";
import { RequireAuth } from "@/shared/auth/RequireAuth";
import { RequireSuperAdmin } from "@/shared/auth/RequireSuperAdmin";
import { NotFoundPage } from "@/shared/ui/NotFoundPage";
import { PlaceholderPage } from "@/shared/ui/PlaceholderPage";
import { placeholderPaths } from "./navigation";
import { paths } from "./routes";

const sa = (page: React.ReactNode) => <RequireSuperAdmin>{page}</RequireSuperAdmin>;

// 两棵路由树：公开的登录/激活区，和需要会话的后台区；管理页再套一层超管守卫。
export const router = createBrowserRouter([
  {
    element: <AuthLayout />,
    children: [
      { path: paths.login, element: <LoginPage /> },
      { path: paths.activate, element: <ActivatePage /> },
      { path: paths.claim, element: <ClaimPage /> },
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
      { path: paths.updates, element: <Navigate to={paths.dashboard} replace /> },
      { path: paths.orgUnits, element: sa(<OrgUnitsPage />) },
      { path: paths.people, element: sa(<PeoplePage />) },
      { path: paths.assignments, element: sa(<AssignmentsPage />) },
      { path: paths.clients, element: sa(<ClientsPage />) },
      { path: paths.clientPolicy, element: sa(<ClientPolicyPage />) },
      { path: paths.processes, element: <AssetsPage kind="process" /> },
      { path: paths.projects, element: <AssetsPage kind="project" /> },
      { path: paths.legacyImport, element: <LegacyImportPage /> },
      { path: paths.account, element: <AccountPage /> },
      { path: paths.reports, element: <ReportsPage /> },
      ...placeholderPaths.map((path) => ({ path, element: <PlaceholderPage /> })),
      { path: "*", element: <NotFoundPage /> },
    ],
  },
]);
