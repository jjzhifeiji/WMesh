// 服务端只回英文业务错误码，这里统一翻成给人看的话；没收录的原样显示。
const zh: Record<string, string> = {
  "invalid credentials": "登录名或口令不对",
  unauthorized: "会话已失效，请重新登录",
  forbidden: "没有这项许可",
  "account pending": "账号还待启用，请先用激活口令激活",
  "account disabled": "账号已停用",
  "invalid activation": "登录名或激活口令不对",
  "already activated": "已经激活过了，请直接登录",
  "last factory super admin": "不能拿掉最后一名有效工厂超管",
  "login name already taken": "登录名已被占用",
  "not found": "找不到这家工厂或对象，请核对工厂 ID",
  "org unit is disabled": "组织节点已停用",
  "org unit still has active children": "该节点下还有有效子节点，先停用子节点",
  "active assignment already exists": "已经分配到这个节点了",
  "active role grant already exists": "已经授予过同样的角色",
  "still referenced": "还有引用，不能删除",
  "role and scope do not match": "该角色不能挂这种作用域",
  "org unit parent would create a cycle": "不能把节点挂到自己或自己的下级",
  "org unit cannot have two parents": "一个节点只能有一个上级",
  "invalid work context": "工作上下文无效",
  "invalid id": "标识格式不对",
  "invalid key material": "公钥格式不对，需要 32 字节的 base64 或 hex",
  "revision is not strictly newer": "修订号必须比已接受的更大",
  "revision does not match": "别人已经改过这条，请刷新后再写",
  "asset integrity check failed": "内容与摘要对不上，不能当有效资产用",
  "asset is not available": "草稿或已停用，不能这样用",
  "asset is not copyable": "不可复制，不能升档或放宽",
  "asset dependency missing or mismatched": "依赖的工艺不存在、未发布或修订对不上",
  "client binding is void": "本厂绑定已作废，不能再签发",
  "client public key does not match": "公钥与已登记的不一致",
  "internal error": "服务异常，请稍后再试",
};

export function translateError(code: string) {
  return zh[code] ?? code;
}
