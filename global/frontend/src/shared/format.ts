import dayjs from "dayjs";

// 服务端时间都是 UTC ISO 串，展示统一到本地分钟。
export function formatTime(iso?: string | null) {
  if (!iso) return "—";
  return dayjs(iso).format("YYYY-MM-DD HH:mm");
}

// 通道监控要看到秒，心跳才看得出来。
export function formatDateTime(iso?: string | null) {
  if (!iso) return "—";
  return dayjs(iso).format("YYYY-MM-DD HH:mm:ss");
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

// 包大小、上传进度用人话写。
export function formatBytes(n: number) {
  if (!Number.isFinite(n) || n < 0) return "—";
  if (n < 1024) return `${Math.round(n)} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  if (n < 1024 * 1024 * 1024) return `${(n / (1024 * 1024)).toFixed(1)} MB`;
  return `${(n / (1024 * 1024 * 1024)).toFixed(2)} GB`;
}

// 上传速率，按每秒字节。
export function formatSpeed(bytesPerSec: number) {
  if (!Number.isFinite(bytesPerSec) || bytesPerSec <= 0) return "—";
  return `${formatBytes(bytesPerSec)}/s`;
}

// 在线才展示厂端自报版本；离线不当正在跑。
export function formatRelease(online: boolean, code?: number, name?: string) {
  if (!online || !code) return "—";
  return name ? `${name}（${code}）` : String(code);
}
