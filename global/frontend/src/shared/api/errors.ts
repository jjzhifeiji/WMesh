// 服务端只回英文业务错误码，这里统一翻成给人看的话；没收录的原样显示。
const zh: Record<string, string> = {
  "invalid credentials": "登录名或口令不对",
  unauthorized: "会话已失效，请重新登录",
  forbidden: "没有这项许可：WAN 不代管厂内人员、组织和角色",
  "not found": "找不到这个对象",
  "factory bootstrap failed": "厂端引导失败：厂内服务不可达，或两侧引导口令不一致",
  "initial super admin already bound": "该厂已经下发过初始超管",
  "wan admin already exists": "WAN 只能有一名管理员",
  "invalid id": "标识格式不对",
  "internal error": "服务异常，请稍后再试",
};

export function translateError(code: string) {
  return zh[code] ?? code;
}
