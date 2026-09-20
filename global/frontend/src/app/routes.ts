// 路径常量：路由表、菜单、跳转都只引用这里，不写裸字符串。
export const paths = {
  login: "/login",
  dashboard: "/",
  factories: "/factories",
  clients: "/devices",
  processes: "/assets/processes",
  projects: "/assets/projects",
  templates: "/assets/templates",
  projectTemplates: "/assets/project-templates",
  legacyImport: "/assets/import",
  updates: "/updates",
  auditEvents: "/audit/events",
  auditStats: "/audit/stats",
  reports: "/reports",
  account: "/account",
} as const;
