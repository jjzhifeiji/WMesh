import {
  ApartmentOutlined,
  DashboardOutlined,
  IdcardOutlined,
  SafetyCertificateOutlined,
  TagsOutlined,
  TeamOutlined,
  UserOutlined,
} from "@ant-design/icons";
import type { MenuProps } from "antd";
import { paths } from "./routes";

type NavItem = { key: string; label: string; icon: React.ReactNode; superAdminOnly?: boolean };

// 侧边栏菜单定义；标了 superAdminOnly 的只给工厂超管看。新功能在这里加一项、在 router 里加一条路由。
export const navItems: NavItem[] = [
  { key: paths.dashboard, label: "概览", icon: <DashboardOutlined /> },
  { key: paths.orgTypes, label: "组织类型", icon: <TagsOutlined />, superAdminOnly: true },
  { key: paths.orgUnits, label: "组织节点", icon: <ApartmentOutlined />, superAdminOnly: true },
  { key: paths.people, label: "人员", icon: <TeamOutlined />, superAdminOnly: true },
  { key: paths.grants, label: "角色授予", icon: <SafetyCertificateOutlined />, superAdminOnly: true },
  { key: paths.assignments, label: "组织分配", icon: <IdcardOutlined />, superAdminOnly: true },
  { key: paths.account, label: "我的账号", icon: <UserOutlined /> },
];

export function menuItems(isSuperAdmin: boolean): NonNullable<MenuProps["items"]> {
  return navItems.filter((i) => isSuperAdmin || !i.superAdminOnly).map(({ key, label, icon }) => ({ key, label, icon }));
}

export const pageTitles: Record<string, string> = Object.fromEntries(navItems.map((i) => [i.key, i.label]));
