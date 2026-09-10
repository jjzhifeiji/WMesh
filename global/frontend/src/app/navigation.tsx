import { DashboardOutlined, ShopOutlined } from "@ant-design/icons";
import type { MenuProps } from "antd";
import { paths } from "./routes";

// 侧边栏菜单；新功能在这里加一项、在 router 里加一条路由即可。
export const navItems: NonNullable<MenuProps["items"]> = [
  { key: paths.dashboard, icon: <DashboardOutlined />, label: "概览" },
  { key: paths.factories, icon: <ShopOutlined />, label: "工厂名录" },
];

// 顶栏面包屑用的页面标题。
export const pageTitles: Record<string, string> = {
  [paths.dashboard]: "概览",
  [paths.factories]: "工厂名录",
};
