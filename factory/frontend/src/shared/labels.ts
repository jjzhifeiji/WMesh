// 固定枚举的中文展示名与颜色；枚举值以服务端为准。
// 授予页可出这五种；组织负责人服务端仍认，只是不再新授。
export const ROLES = [
  { value: "factory_super_admin", label: "工厂超管", scopes: ["factory"] },
  { value: "org_admin", label: "管理员", scopes: ["org_unit"] },
  { value: "process_engineer", label: "工艺工程师", scopes: ["factory", "org_unit"] },
  { value: "operator", label: "操作员", scopes: ["factory", "org_unit"] },
  { value: "auditor", label: "审计员", scopes: ["factory", "org_unit"] },
] as const;

const ROLE_LABELS: Record<string, string> = {
  factory_super_admin: "工厂超管",
  org_admin: "管理员",
  org_lead: "组织负责人",
  process_engineer: "工艺工程师",
  operator: "操作员",
  auditor: "审计员",
};

export type Role = (typeof ROLES)[number]["value"];
export type ScopeKind = "factory" | "org_unit";

export function roleLabel(role: string) {
  return ROLE_LABELS[role] ?? role;
}

// 该角色允许挂的作用域；服务端也会再校验一次。
export function roleScopes(role: string): readonly ScopeKind[] {
  return ROLES.find((r) => r.value === role)?.scopes ?? [];
}

export function scopeLabel(scopeKind: string) {
  return scopeKind === "factory" ? "整厂" : "组织节点";
}

export function statusLabel(status: string) {
  switch (status) {
    case "pending":
      return "待启用";
    case "active":
      return "有效";
    case "disabled":
      return "已停用";
    case "bound":
      return "已绑定";
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
    case "pending":
      return "gold";
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
