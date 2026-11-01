import { LogoutOutlined, UserOutlined } from "@ant-design/icons";
import { Avatar, Breadcrumb, Dropdown, Layout, Menu, Space, Typography } from "antd";
import { useState } from "react";
import { Outlet, useLocation, useNavigate } from "react-router";
import { breadcrumbItems, menuItems, openGroupFor } from "@/app/navigation";
import { paths } from "@/app/routes";
import { useLogout, useMe } from "@/features/auth/api";
import { ApplyOverlay } from "@/features/updates/ApplyOverlay";
import { WANUpdatePrompt } from "@/features/updates/WANUpdatePrompt";

// 后台骨架：左侧两级菜单、顶部当前位置与账号、中间页面内容。
export function AdminLayout() {
  const { pathname } = useLocation();
  const navigate = useNavigate();
  const [collapsed, setCollapsed] = useState(false);
  // 菜单组展开状态受控：换页时把当前页所在的组并进去，从别处跳进折叠组也能看到当前项。
  const [openKeys, setOpenKeys] = useState<string[]>(() => openGroupFor(pathname));
  const [openedFor, setOpenedFor] = useState(pathname);
  if (openedFor !== pathname) {
    setOpenedFor(pathname);
    const group = openGroupFor(pathname);
    if (group.some((k) => !openKeys.includes(k))) setOpenKeys([...openKeys, ...group]);
  }
  const me = useMe();
  const logout = useLogout();

  return (
    <Layout className="admin-layout">
      <Layout.Sider collapsible collapsed={collapsed} onCollapse={setCollapsed} breakpoint="lg" width={232}>
        <div className="admin-brand">{collapsed ? "WM" : "WMesh 云端总控"}</div>
        <Menu
          theme="dark"
          mode="inline"
          selectedKeys={[pathname]}
          items={menuItems()}
          onClick={({ key }) => {
            if (key.startsWith("/")) navigate(key);
          }}
          openKeys={openKeys}
          onOpenChange={setOpenKeys}
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
          {pathname === paths.dashboard ? <WANUpdatePrompt /> : null}
          <ApplyOverlay />
        </Layout.Content>
      </Layout>
    </Layout>
  );
}
