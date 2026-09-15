// 服务端只回英文业务错误码，这里统一翻成给人看的话；没收录的原样显示。
const zh: Record<string, string> = {
  "invalid credentials": "登录名或密码不对",
  unauthorized: "会话已失效，请重新登录",
  forbidden: "没有这项许可：WAN 不代管厂内人员、组织和角色",
  "not found": "找不到这个对象",
  "factory bootstrap failed": "厂端引导失败：厂内服务不可达，或两侧引导密码不一致",
  "invalid enrollment": "建厂码不对或已经用过",
  "factory is disabled": "该厂已停用，不能认领或操作",
  "factory is retired": "该厂已注销，不能再启用",
  "still referenced": "还有引用，不能从名录拿掉",
  "factory public key already registered": "该厂签发公钥已登记",
  "initial super admin already bound": "该厂已经下发过初始超管",
  "wan admin already exists": "WAN 只能有一名管理员",
  "invalid id": "标识格式不对",
  "invalid name": "名字不能为空，最多 64 个字",
  "invalid key material": "公钥格式不对，需要 32 字节的 base64 或 hex",
  "client already bound to a factory": "这台设备已经分给一家工厂，请改分而不是再分一次",
  "client is not bound": "设备还没有分给工厂",
  "client public key already registered": "这把公钥已经登记过了",
  "revision does not match": "别人已经改过这条，请刷新后再写",
  "revision is not strictly newer": "不能下发更低的版本",
  "asset integrity check failed": "内容与摘要对不上，不能当有效资产用",
  "asset is not available": "草稿或已停用，不能这样用",
  "asset is not copyable": "不可复制，不能升档或放宽",
  "asset dependency missing or mismatched": "依赖的工艺不存在、未发布、修订对不上，或焊道引用了未声明的工艺",
	"factory channel is offline": "厂端通道不在线，连不上不能拉升档",
  "content template is invalid": "内容模版字段不合法",
  "asset origin code is not assigned": "还没有本端编号短码，不能新建",
  "asset code already exists": "编号已占用或与身份不一致",
  "origin code exhausted": "短码或编号已用尽",
};

export function translateError(code: string) {
  return zh[code] ?? code;
}
