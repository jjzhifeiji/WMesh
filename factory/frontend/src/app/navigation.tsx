import {
  ApartmentOutlined,
  AppstoreOutlined,
  AuditOutlined,
  CloudSyncOutlined,
  ClusterOutlined,
  DashboardOutlined,
  TeamOutlined,
} from "@ant-design/icons";
import type { MenuProps } from "antd";
import type { ReactNode } from "react";
import { paths } from "./routes";

type NavLeaf = {
  key: string;
  label: string;
  superAdminOnly?: boolean;
  placeholder?: boolean;
  description?: string;
};

type NavGroup = {
  key: string;
  label: string;
  icon: ReactNode;
  superAdminOnly?: boolean;
  children: NavLeaf[];
};

type NavEntry = (NavLeaf & { icon: ReactNode; children?: undefined }) | NavGroup;

function allowed(isSuperAdmin: boolean, item: { superAdminOnly?: boolean }) {
  return isSuperAdmin || !item.superAdminOnly;
}

// 侧栏两级菜单：现有页按组织 / 人员拆组；后续组先占位。我的账号只在顶栏。
export const navTree: NavEntry[] = [
  { key: paths.dashboard, label: "概览", icon: <DashboardOutlined /> },
  {
    key: "org",
    label: "组织",
    icon: <ApartmentOutlined />,
    superAdminOnly: true,
    children: [
      { key: paths.orgUnits, label: "组织节点" },
    ],
  },
  {
    key: "access",
    label: "人员与权限",
    icon: <TeamOutlined />,
    superAdminOnly: true,
    children: [
      { key: paths.people, label: "人员" },
      { key: paths.assignments, label: "组织分配" },
    ],
  },
  {
    key: "devices",
    label: "设备",
    icon: <ClusterOutlined />,
    superAdminOnly: true,
    children: [
      { key: paths.clients, label: "设备" },
    ],
  },
  {
    key: "assets",
    label: "资产",
    icon: <AppstoreOutlined />,
    children: [
      { key: paths.processes, label: "工艺", description: "厂级工艺的制作、维护与升档。" },
      { key: paths.projects, label: "工程", description: "工程及其依赖工艺的治理。" },
      { key: paths.fieldFiles, label: "现场文件", placeholder: true, description: "Client 上传的点云/图片记录，本体在本厂对象存储。" },
    ],
  },
  {
    key: "delivery",
    label: "下发与同步",
    icon: <CloudSyncOutlined />,
    children: [
      { key: paths.distribute, label: "下发与缓存", placeholder: true, description: "向本厂 Client 下发资产并调控缓存。" },
      { key: paths.sync, label: "汇聚与 Intent", placeholder: true, description: "弱网汇聚、幂等合并与意图收敛。" },
    ],
  },
  {
    key: "audit",
    label: "审计",
    icon: <AuditOutlined />,
    children: [
      { key: paths.auditEvents, label: "操作审计", placeholder: true, description: "本厂操作留痕查询，不含认证秘密。" },
      { key: paths.auditStats, label: "统计", placeholder: true, description: "按事实发生时的组织路径归集，历史不随调动改写。" },
    ],
  },
];

export const accountTitle = "我的账号";

export function openGroupFor(pathname: string): string[] {
  for (const entry of navTree) {
    if (entry.children?.some((c) => c.key === pathname)) return [entry.key];
  }
  return [];
}

export const placeholderPaths = navTree.flatMap((entry) =>
  (entry.children ?? []).filter((c) => c.placeholder).map((c) => c.key),
);

export function menuItems(isSuperAdmin: boolean): NonNullable<MenuProps["items"]> {
  return navTree.flatMap((entry) => {
    if (!allowed(isSuperAdmin, entry)) return [];
    if (entry.children) {
      const children = entry.children.filter((c) => allowed(isSuperAdmin, c)).map((c) => ({ key: c.key, label: c.label }));
      if (children.length === 0) return [];
      return [{ key: entry.key, icon: entry.icon, label: entry.label, children }];
    }
    return [{ key: entry.key, icon: entry.icon, label: entry.label }];
  });
}

export function breadcrumbItems(pathname: string): { title: string }[] {
  if (pathname === paths.account) return [{ title: accountTitle }];
  for (const entry of navTree) {
    if (entry.key === pathname) return [{ title: entry.label }];
    const leaf = entry.children?.find((c) => c.key === pathname);
    if (leaf) return [{ title: entry.label }, { title: leaf.label }];
  }
  return [{ title: "页面" }];
}

export function placeholderMeta(pathname: string): { title: string; description?: string } | undefined {
  for (const entry of navTree) {
    const leaf = entry.children?.find((c) => c.key === pathname && c.placeholder);
    if (leaf) return { title: leaf.label, description: leaf.description };
  }
  return undefined;
}
