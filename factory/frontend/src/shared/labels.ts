// 固定枚举的中文展示名与颜色；枚举值以服务端为准。
// 三种固定角色，都覆盖整厂；超管拥有全部权限。
export const ROLES = [
  { value: "factory_super_admin", label: "工厂超管", hint: "全部权限" },
  { value: "org_admin", label: "管理员", hint: "厂级工艺工程、报表与审计" },
  { value: "operator", label: "操作员", hint: "示教器作业" },
] as const;

const ROLE_LABELS: Record<string, string> = Object.fromEntries(ROLES.map((r) => [r.value, r.label]));

export type Role = (typeof ROLES)[number]["value"];

export function roleLabel(role: string) {
  return ROLE_LABELS[role] ?? role;
}

export function statusLabel(status: string) {
  switch (status) {
    case "active":
      return "有效";
    case "disabled":
      return "已停用";
    case "bound":
      return "已分配";
    case "void":
      return "已作废";
    case "draft":
      return "草稿";
    case "available":
      return "可用";
    default:
      return status;
  }
}

export function statusColor(status: string) {
  switch (status) {
    case "active":
      return "green";
    case "disabled":
      return "default";
    case "bound":
      return "green";
    case "void":
      return "default";
    case "draft":
      return "gold";
    case "available":
      return "green";
    default:
      return "default";
  }
}
