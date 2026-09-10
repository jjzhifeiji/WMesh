// 路径常量：路由表、菜单、跳转都只引用这里，不写裸字符串。
export const paths = {
  login: "/login",
  activate: "/activate",
  dashboard: "/",
  orgTypes: "/org/types",
  orgUnits: "/org/units",
  people: "/people",
  grants: "/grants",
  assignments: "/assignments",
  account: "/account",
} as const;
