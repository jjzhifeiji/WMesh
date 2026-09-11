// 路径常量：路由表、菜单、跳转都只引用这里，不写裸字符串。
export const paths = {
  login: "/login",
  dashboard: "/",
  factories: "/factories",
  clients: "/clients",
  processes: "/assets/processes",
  projects: "/assets/projects",
} as const;
