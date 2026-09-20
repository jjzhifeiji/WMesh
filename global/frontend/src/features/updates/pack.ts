import { softwareKindLabel, type SoftwareKind, type SoftwareRelease } from "./api";

export type DistAction = "publish" | "skip" | "reject";

export type DistRow = {
  key: string; // kind:version:file
  kind: SoftwareKind;
  version: number;
  versionName: string;
  fileName: string;
  file?: File;
  bytes: number; // 包文件大小；缺文件为 0
  sha256: string;
  currentVersion?: number; // 现网该种类最高版本
  currentVersionName?: string; // 现网版本名
  action: DistAction;
  reason: string;
};

type PackMeta = {
  kind: SoftwareKind;
  version: number;
  versionName: string;
  file: string;
  sha256: string;
};

const kinds = new Set<string>(["wan_service", "factory_service", "client_apk"]);

function isKind(v: string): v is SoftwareKind {
  return kinds.has(v);
}

function basename(path: string) {
  const parts = path.split(/[/\\]/);
  return parts[parts.length - 1] ?? path;
}

function parseMeta(text: string): PackMeta | null {
  let raw: unknown;
  try {
    raw = JSON.parse(text);
  } catch {
    return null;
  }
  if (!raw || typeof raw !== "object") return null;
  const o = raw as Record<string, unknown>;
  if (typeof o.kind !== "string" || !isKind(o.kind)) return null;
  if (typeof o.version !== "number" || !Number.isInteger(o.version) || o.version < 1) return null;
  if (typeof o.versionName !== "string" || !o.versionName.trim()) return null;
  if (typeof o.file !== "string" || !o.file.trim()) return null;
  if (typeof o.sha256 !== "string" || !/^[0-9a-fA-F]{64}$/.test(o.sha256.trim())) return null;
  return {
    kind: o.kind,
    version: o.version,
    versionName: o.versionName.trim(),
    file: o.file.trim(),
    sha256: o.sha256.trim().toLowerCase(),
  };
}

function hexToBytes(hex: string): Uint8Array | null {
  if (hex.length !== 64) return null;
  const out = new Uint8Array(32);
  for (let i = 0; i < 32; i++) {
    const n = Number.parseInt(hex.slice(i * 2, i * 2 + 2), 16);
    if (Number.isNaN(n)) return null;
    out[i] = n;
  }
  return out;
}

// pack.sh 记 hex，列表接口记 base64，对上才算同一份。
export function sameDigest(hexSha256: string, apiDigest: string) {
  const want = hexToBytes(hexSha256.trim().toLowerCase());
  if (!want) return false;
  try {
    const got = Uint8Array.from(atob(apiDigest), (c) => c.charCodeAt(0));
    if (got.length !== want.length) return false;
    return got.every((b, i) => b === want[i]);
  } catch {
    return false;
  }
}

function maxPublished(rows: SoftwareRelease[]) {
  const out = new Map<SoftwareKind, SoftwareRelease>();
  for (const row of rows) {
    const cur = out.get(row.kind);
    if (!cur || row.version > cur.version) out.set(row.kind, row);
  }
  return out;
}

function decideAgainstPublished(meta: Pick<PackMeta, "kind" | "version" | "sha256">, published: Map<SoftwareKind, SoftwareRelease>): Pick<DistRow, "action" | "reason"> {
  const cur = published.get(meta.kind);
  if (!cur) return { action: "publish", reason: "新版本，将上传" };
  if (meta.version < cur.version) {
    return { action: "skip", reason: `已有 ${softwareKindLabel[meta.kind]} v${cur.version}，跳过旧版` };
  }
  if (meta.version === cur.version) {
    if (sameDigest(meta.sha256, cur.digest)) {
      return { action: "publish", reason: "同版本同摘要，将核对补传" };
    }
    return { action: "reject", reason: "同版本摘要不同，原件不动" };
  }
  return { action: "publish", reason: "新版本，将上传" };
}

// 读 pack.sh 打出的 json + 包文件；每种只留最高版本，再对照已发布决定跳过或拒绝。
export async function planDistPublish(files: Iterable<File>, published: SoftwareRelease[]): Promise<DistRow[]> {
  const byName = new Map<string, File>();
  const jsonFiles: File[] = [];
  for (const file of files) {
    const name = basename(file.webkitRelativePath || file.name);
    byName.set(name, file);
    if (name.endsWith(".json")) jsonFiles.push(file);
  }

  const found: DistRow[] = [];
  for (const json of jsonFiles) {
    const meta = parseMeta(await json.text());
    if (!meta) continue;
    const pack = byName.get(meta.file);
    const key = `${meta.kind}:${meta.version}:${meta.file}`;
    found.push({
      key,
      kind: meta.kind,
      version: meta.version,
      versionName: meta.versionName,
      fileName: meta.file,
      file: pack,
      bytes: pack?.size ?? 0,
      sha256: meta.sha256,
      action: pack ? "publish" : "reject",
      reason: pack ? "" : `找不到包文件 ${meta.file}`,
    });
  }

  const latest = new Map<SoftwareKind, DistRow>();
  const rows: DistRow[] = [];
  const sorted = found.toSorted((a, b) => a.kind.localeCompare(b.kind) || b.version - a.version);
  for (const row of sorted) {
    if (row.action === "reject") {
      rows.push(row);
      continue;
    }
    const keep = latest.get(row.kind);
    if (keep) {
      rows.push({ ...row, action: "skip", reason: `目录里已有更高的 v${keep.version}，跳过` });
      continue;
    }
    latest.set(row.kind, row);
  }

  const publishedMax = maxPublished(published);
  for (const row of latest.values()) {
    if (!row.file) {
      rows.push({ ...row, action: "reject", reason: `找不到包文件 ${row.fileName}` });
      continue;
    }
    const decided = decideAgainstPublished(row, publishedMax);
    const cur = publishedMax.get(row.kind);
    rows.push({
      ...row,
      ...decided,
      currentVersion: cur?.version,
      currentVersionName: cur?.versionName,
    });
  }

  return rows.toSorted((a, b) => a.kind.localeCompare(b.kind) || b.version - a.version);
}

export async function sha256Hex(file: File) {
  const buf = await file.arrayBuffer();
  const hash = await crypto.subtle.digest("SHA-256", buf);
  return [...new Uint8Array(hash)].map((b) => b.toString(16).padStart(2, "0")).join("");
}
