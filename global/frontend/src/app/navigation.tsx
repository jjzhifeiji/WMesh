import {
  AppstoreOutlined,
  AuditOutlined,
  ClusterOutlined,
  DashboardOutlined,
  ShopOutlined,
} from "@ant-design/icons";
import type { MenuProps } from "antd";
import type { ReactNode } from "react";
import { paths } from "./routes";

type NavLeaf = {
  key: string;
  label: string;
  placeholder?: boolean;
  description?: string;
};

type NavGroup = {
  key: string;
  label: string;
  icon: ReactNode;
  children: NavLeaf[];
};

type NavEntry = (NavLeaf & { icon: ReactNode; children?: undefined }) | NavGroup;

// 侧栏两级菜单：现有页按工厂 / 设备 / 资产拆组；后续组先占位。账号只在顶栏。
export const navTree: NavEntry[] = [
  { key: paths.dashboard, label: "概览", icon: <DashboardOutlined /> },
  {
    key: "factories",
    label: "工厂",
    icon: <ShopOutlined />,
    children: [{ key: paths.factories, label: "工厂名录" }],
  },
  {
    key: "devices",
    label: "设备",
    icon: <ClusterOutlined />,
    children: [{ key: paths.clients, label: "设备" }],
  },
  {
    key: "assets",
    label: "资产",
    icon: <AppstoreOutlined />,
    children: [
      { key: paths.processes, label: "平台工艺", description: "平台级工艺的制作、维护与升档。" },
      { key: paths.projects, label: "平台工程", description: "平台级工程及其依赖工艺的治理。" },
      { key: paths.templates, label: "工艺模版", description: "平台工艺当前字段表。" },
      { key: paths.projectTemplates, label: "工程模版", description: "平台工程当前字段表。" },
    ],
  },
  {
    key: "audit",
    label: "审计",
    icon: <AuditOutlined />,
    children: [
      { key: paths.auditEvents, label: "操作审计", placeholder: true, description: "WAN 操作留痕查询，不含认证秘密。" },
      { key: paths.auditStats, label: "汇总", placeholder: true, description: "跨厂汇总，不含厂内人员明细与点云原件。" },
    ],
  },
];

export function openGroupFor(pathname: string): string[] {
  for (const entry of navTree) {
    if (entry.children?.some((c) => c.key === pathname)) return [entry.key];
  }
  return [];
}

export const placeholderPaths = navTree.flatMap((entry) =>
  (entry.children ?? []).filter((c) => c.placeholder).map((c) => c.key),
);

export function menuItems(): NonNullable<MenuProps["items"]> {
  return navTree.map((entry) => {
    if (entry.children) {
      return {
        key: entry.key,
        icon: entry.icon,
        label: entry.label,
        children: entry.children.map((c) => ({ key: c.key, label: c.label })),
      };
    }
    return { key: entry.key, icon: entry.icon, label: entry.label };
  });
}

export function breadcrumbItems(pathname: string): { title: string }[] {
  for (const entry of navTree) {
    if (entry.key === pathname) return [{ title: entry.label }];
    const leaf = entry.children?.find((c) => c.key === pathname);
    if (leaf) return [{ title: entry.label }, { title: leaf.label }];
  }
  if (pathname === paths.account) return [{ title: "我的账号" }];
  return [{ title: "页面" }];
}

export function placeholderMeta(pathname: string): { title: string; description?: string } | undefined {
  for (const entry of navTree) {
    const leaf = entry.children?.find((c) => c.key === pathname && c.placeholder);
    if (leaf) return { title: leaf.label, description: leaf.description };
  }
  return undefined;
}
