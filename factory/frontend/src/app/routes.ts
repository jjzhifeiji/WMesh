// 路径常量：路由表、菜单、跳转都只引用这里，不写裸字符串。
export const paths = {
  login: "/login",
  activate: "/activate",
  claim: "/claim",
  dashboard: "/",
  orgUnits: "/org/units",
  people: "/people",
  assignments: "/assignments",
  account: "/account",
  clients: "/devices",
  processes: "/assets/processes",
  projects: "/assets/projects",
  fieldFiles: "/assets/files",
  distribute: "/delivery/distribute",
  sync: "/delivery/sync",
  auditEvents: "/audit/events",
  auditStats: "/audit/stats",
} as const;
