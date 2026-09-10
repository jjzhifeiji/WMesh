import { LogoutOutlined, UserOutlined } from "@ant-design/icons";
import { Avatar, Breadcrumb, Dropdown, Layout, Menu, Space, Typography } from "antd";
import { useState } from "react";
import { Outlet, useLocation, useNavigate } from "react-router";
import { navItems, pageTitles } from "@/app/navigation";
import { paths } from "@/app/routes";
import { useLogout, useMe } from "@/features/auth/api";

// 后台骨架：左侧菜单、顶部当前位置与账号、中间页面内容。
export function AdminLayout() {
  const { pathname } = useLocation();
  const navigate = useNavigate();
  const [collapsed, setCollapsed] = useState(false);
  const me = useMe();
  const logout = useLogout();

  const selectedKey =
    pathname === paths.dashboard
      ? paths.dashboard
      : (navItems.find((i) => i && i.key !== paths.dashboard && pathname.startsWith(String(i.key)))?.key as string | undefined) ??
        pathname;

  return (
    <Layout className="admin-layout">
      <Layout.Sider collapsible collapsed={collapsed} onCollapse={setCollapsed} width={220}>
        <div className="admin-brand">{collapsed ? "WM" : "WMesh 云端总控"}</div>
        <Menu theme="dark" mode="inline" selectedKeys={[selectedKey]} items={navItems} onClick={({ key }) => navigate(key)} />
      </Layout.Sider>
      <Layout>
        <Layout.Header className="admin-header">
          <Breadcrumb items={[{ title: "WAN" }, { title: pageTitles[selectedKey] ?? "页面" }]} />
          <Space size="middle">
            <Typography.Text>{me.data?.loginName ?? "…"}</Typography.Text>
            <Dropdown
              menu={{
                items: [{ key: "logout", icon: <LogoutOutlined />, label: "退出登录", onClick: () => void logout() }],
              }}
            >
              <Avatar size="small" icon={<UserOutlined />} style={{ cursor: "pointer" }} />
            </Dropdown>
          </Space>
        </Layout.Header>
        <Layout.Content className="admin-content">
          <Outlet />
        </Layout.Content>
      </Layout>
    </Layout>
  );
}
