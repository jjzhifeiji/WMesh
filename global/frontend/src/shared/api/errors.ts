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
  "invalid key material": "公钥格式不对，需要 32 字节的 base64 或 hex",
  "client already bound to a factory": "这台节点已经绑到一家工厂，请改绑而不是再绑一次",
  "client is not bound": "节点还没有绑定工厂",
  "client public key already registered": "这把公钥已经登记过了",
  "revision does not match": "别人已经改过这条，请刷新后再写",
  "asset integrity check failed": "内容与摘要对不上，不能当有效资产用",
  "asset is not available": "草稿或已停用，不能这样用",
  "asset is not copyable": "不可复制，不能升档",
  "asset dependency missing or mismatched": "依赖的工艺不存在、未发布或修订对不上",
  "internal error": "服务异常，请稍后再试",
};

export function translateError(code: string) {
  return zh[code] ?? code;
}
