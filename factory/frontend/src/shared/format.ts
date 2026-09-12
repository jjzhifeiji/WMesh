import dayjs from "dayjs";

// 服务端时间都是 UTC ISO 串，展示统一到本地分钟。
export function formatTime(iso?: string | null) {
  if (!iso) return "—";
  return dayjs(iso).format("YYYY-MM-DD HH:mm");
}

// 稳定身份很长，列表里只露前后各 4 位，完整值靠复制按钮拿。
export function shortId(id: string) {
  return id.length > 12 ? `${id.slice(0, 8)}…${id.slice(-4)}` : id;
}

// 正文 SHA-256（接口里是 base64），列表里缩短，完整值靠复制。
export function shortHash(h?: string | null) {
  if (!h) return "—";
  return h.length > 16 ? `${h.slice(0, 10)}…` : h;
}
