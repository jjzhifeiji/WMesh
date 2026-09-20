import { LogoutOutlined, UserOutlined } from "@ant-design/icons";
import { Avatar, Breadcrumb, Dropdown, Layout, Menu, Space, Typography } from "antd";
import { useState } from "react";
import { Outlet, useLocation, useNavigate } from "react-router";
import { breadcrumbItems, menuItems, openGroupFor } from "@/app/navigation";
import { paths } from "@/app/routes";
import { useLogout } from "@/features/auth/api";
import { useCatalog, useIsSuperAdmin } from "@/features/catalog/api";
import { ApplyOverlay } from "@/features/updates/ApplyOverlay";
import { FactoryUpdatePrompt } from "@/features/updates/FactoryUpdatePrompt";
import { useSession } from "@/shared/auth/session";
import { IdText } from "@/shared/ui/IdText";

// 后台骨架：左侧菜单（按角色裁剪）、顶部当前位置与账号、中间页面内容。
export function AdminLayout() {
  const { pathname } = useLocation();
  const navigate = useNavigate();
  const [collapsed, setCollapsed] = useState(false);
  const { factoryId } = useSession();
  const catalog = useCatalog();
  const isSA = useIsSuperAdmin();
  const logout = useLogout();
  const me = catalog.data?.me;

  return (
    <Layout className="admin-layout">
      <Layout.Sider collapsible collapsed={collapsed} onCollapse={setCollapsed} width={232}>
        <div className="admin-brand">{collapsed ? "WM" : "WMesh 厂内管理"}</div>
        <Menu
          theme="dark"
          mode="inline"
          selectedKeys={[pathname]}
          items={menuItems(isSA)}
          onClick={({ key }) => {
            if (key.startsWith("/")) navigate(key);
          }}
          defaultOpenKeys={collapsed ? undefined : openGroupFor(pathname)}
        />
      </Layout.Sider>
      <Layout>
        <Layout.Header className="admin-header">
          <Breadcrumb items={[{ title: <>工厂 <IdText id={factoryId} /></> }, ...breadcrumbItems(pathname)]} />
          <Space size="middle">
            <Typography.Text>{me ? `${me.displayName}（${me.loginName}）` : "…"}</Typography.Text>
            <Dropdown
              menu={{
                items: [
                  { key: "account", icon: <UserOutlined />, label: "我的账号", onClick: () => navigate(paths.account) },
                  { type: "divider" },
                  { key: "logout", icon: <LogoutOutlined />, label: "退出登录", onClick: () => void logout() },
                ],
              }}
            >
              <Avatar size="small" icon={<UserOutlined />} style={{ cursor: "pointer" }} />
            </Dropdown>
          </Space>
        </Layout.Header>
        <Layout.Content className="admin-content">
          <Outlet />
          {pathname === paths.dashboard ? <FactoryUpdatePrompt enabled={isSA} /> : null}
          <ApplyOverlay />
        </Layout.Content>
      </Layout>
    </Layout>
  );
}
