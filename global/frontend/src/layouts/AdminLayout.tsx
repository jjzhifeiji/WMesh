import { LogoutOutlined, UserOutlined } from "@ant-design/icons";
import { Avatar, Breadcrumb, Dropdown, Layout, Menu, Space, Typography } from "antd";
import { useState } from "react";
import { Outlet, useLocation, useNavigate } from "react-router";
import { breadcrumbItems, menuItems, openGroupFor } from "@/app/navigation";
import { paths } from "@/app/routes";
import { useLogout, useMe } from "@/features/auth/api";

// 后台骨架：左侧两级菜单、顶部当前位置与账号、中间页面内容。
export function AdminLayout() {
  const { pathname } = useLocation();
  const navigate = useNavigate();
  const [collapsed, setCollapsed] = useState(false);
  const me = useMe();
  const logout = useLogout();

  return (
    <Layout className="admin-layout">
      <Layout.Sider collapsible collapsed={collapsed} onCollapse={setCollapsed} width={232}>
        <div className="admin-brand">{collapsed ? "WM" : "WMesh 云端总控"}</div>
        <Menu
          theme="dark"
          mode="inline"
          selectedKeys={[pathname]}
          items={menuItems()}
          onClick={({ key }) => {
            if (key.startsWith("/")) navigate(key);
          }}
          defaultOpenKeys={collapsed ? undefined : openGroupFor(pathname)}
        />
      </Layout.Sider>
      <Layout>
        <Layout.Header className="admin-header">
          <Breadcrumb items={[{ title: "WAN" }, ...breadcrumbItems(pathname)]} />
          <Space size="middle">
            <Typography.Text>{me.data?.loginName ?? "…"}</Typography.Text>
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
        </Layout.Content>
      </Layout>
    </Layout>
  );
}
