import { InboxOutlined } from "@ant-design/icons";
import { App, Button, Card, Table, Typography, type TableColumnsType } from "antd";
import { useMemo, useRef, useState } from "react";
import { useAssets } from "@/features/assets/api";
import { weldKindLabel } from "@/features/templates/projectKinds";
import { errorMessage } from "@/shared/api/client";
import { translateError } from "@/shared/api/errors";
import { PageHeader } from "@/shared/ui/PageHeader";
import { useImportLegacy, type LegacyFile, type LegacyImportResult, type LegacyReject } from "./api";

type Picked = { processes: LegacyFile[]; projects: LegacyFile[]; skipped: number };

function normalizeResult(data: LegacyImportResult): LegacyImportResult {
  return {
    processes: data.processes ?? [],
    projects: data.projects ?? [],
    rejected: uniqueRows(data.rejected ?? []),
    skipped: uniqueRows(data.skipped ?? []),
  };
}

// copyKey 同一路径的工艺按作业类型拆成几份时，靠类型和后缀分开。
function copyKey(r: LegacyReject) {
  return `${r.path}\n${r.weldKind ?? ""}\n${r.suffix ?? ""}`;
}

// rowKey 表格行键：哪一份加原因，同一份同原因只算一行。
function rowKey(r: LegacyReject) {
  return `${copyKey(r)}\n${r.reason}`;
}

// uniqueRows 同一行键只留最后一条，表格行键不重复。
function uniqueRows(rows: LegacyReject[]) {
  return [...new Map(rows.map((r) => [rowKey(r), r])).values()];
}

// 选示教器旧目录：工艺先入库，工程把路径换成身份；缺文件的那份工程拒绝。
export function LegacyImportPage() {
  const imp = useImportLegacy();
  const procs = useAssets("process");
  const projs = useAssets("project");
  const { message, modal } = App.useApp();
  const inputRef = useRef<HTMLInputElement>(null);
  const [picked, setPicked] = useState<Picked | null>(null);
  const [reading, setReading] = useState(false);
  const [result, setResult] = useState<LegacyImportResult | null>(null);
  const [acting, setActing] = useState<{ key: string; mode: "overwrite" | "rename" } | null>(null);
  const existing = useMemo(() => {
    const set = new Set<string>();
    for (const a of [...(procs.data ?? []), ...(projs.data ?? [])]) {
      if (a.status === "disabled") continue;
      set.add(`${a.kind}:${a.name}`);
    }
    return set;
  }, [procs.data, projs.data]);

  const onFiles = async (list: FileList | null) => {
    if (!list || list.length === 0) return;
    // FileList 跟着 input.value 走，先拷出来再清空；不清空的话改完文件再选同一目录不触发 onChange。
    const files = Array.from(list);
    if (inputRef.current) inputRef.current.value = "";
    setReading(true);
    try {
      const next = await readLegacyTree(files);
      setPicked(next);
      setResult(null);
    } catch (e) {
      message.error(errorMessage(e));
    } finally {
      setReading(false);
    }
  };

  const start = async () => {
    if (!picked) return;
    const collisions = sameNames(picked, existing);
    let overwrite = false;
    let rename = false;
    if (collisions.existing.length > 0 || collisions.batch.length > 0) {
      const choice = await askSameName(modal, collisions);
      if (choice === "cancel") return;
      overwrite = choice === "overwrite";
      rename = choice === "rename";
    }
    imp.mutate(
      { processes: picked.processes, projects: picked.projects, overwrite, rename },
      {
        onSuccess: (data) => {
          const next = normalizeResult(data);
          setResult(next);
          message.success(
            `已入工艺 ${next.processes.length}，工程 ${next.projects.length}，跳过 ${next.skipped.length}，拒绝 ${next.rejected.length}`,
          );
        },
        onError: (e) => message.error(errorMessage(e)),
      },
    );
  };

  const retryOne = (row: LegacyReject, mode: "overwrite" | "rename") => {
    if (!picked) {
      message.error("请重新选择文件夹");
      return;
    }
    const payload = retryPayload(picked, row, mode);
    if (!payload) {
      message.error("找不到该文件");
      return;
    }
    setActing({ key: rowKey(row), mode });
    imp.mutate(payload, {
      onSettled: () => setActing(null),
      onSuccess: (data) => {
        const merged = mergeRetry(result, row, normalizeResult(data));
        setResult(merged);
        message.success(
          `已入工艺 ${merged.processes.length}，工程 ${merged.projects.length}，跳过 ${merged.skipped.length}，拒绝 ${merged.rejected.length}`,
        );
      },
      onError: (e) => message.error(errorMessage(e)),
    });
  };

  return (
    <>
      <PageHeader
        title="旧文件导入"
        description="选示教器上的 processes / projects 目录（或整份 ShiJiaoQi）。收入平台级工艺/工程；缺路径的工程整份拒绝，已入工艺保留。同名会问覆盖、按路径重命名还是跳过。"
      />
      <Card style={{ maxWidth: 880 }}>
        <input
          ref={inputRef}
          type="file"
          multiple
          style={{ display: "none" }}
          onChange={(e) => void onFiles(e.target.files)}
        />
        <Button icon={<InboxOutlined />} loading={reading} disabled={imp.isPending} onClick={() => bindDirectory(inputRef.current)}>
          {reading ? "读取中…" : "选择文件夹"}
        </Button>
        {picked ? (
          <Typography.Paragraph type="secondary" style={{ marginTop: 12 }}>
            工艺 {picked.processes.length} 份，工程 {picked.projects.length} 份
            {picked.skipped ? `，跳过 ${picked.skipped} 个非 JSON` : ""}
          </Typography.Paragraph>
        ) : null}
        <Button
          type="primary"
          style={{ marginTop: 8 }}
          disabled={reading || !picked || (picked.processes.length === 0 && picked.projects.length === 0)}
          loading={imp.isPending}
          onClick={() => void start()}
        >
          开始导入
        </Button>
        {result ? (
          <ResultTables
            result={result}
            acting={acting}
            busy={imp.isPending}
            onRetry={retryOne}
          />
        ) : null}
      </Card>
    </>
  );
}

// 服务端只给英文原因，导入自己的几条先翻，其余走通用错误表。
const reasonZh: Record<string, string> = {
  "same content": "内容相同，沿用已有",
  "name exists": "已有同名",
  "invalid path": "路径不对",
  "invalid process json": "工艺文件读不出来（不是有效 JSON）",
  "tbar gap bands missing: import gap folder processes together with the project": "T排缺间隙带：请把缝隙目录的工艺和工程一起导入",
};

function reasonText(reason: string) {
  return reasonZh[reason] ?? translateError(reason);
}

function ResultTables({
  result,
  acting,
  busy,
  onRetry,
}: {
  result: LegacyImportResult;
  acting: { key: string; mode: "overwrite" | "rename" } | null;
  busy: boolean;
  onRetry: (row: LegacyReject, mode: "overwrite" | "rename") => void;
}) {
  const pathCol: TableColumnsType<LegacyReject>[number] = {
    title: "路径",
    dataIndex: "path",
    ellipsis: true,
    // 拆成几份的工艺带上作业类型，同一路径的几行才分得清。
    render: (_, row) => (row.weldKind ? `${row.path}（${weldKindLabel(row.weldKind)}）` : row.path),
  };
  const rejectCols: TableColumnsType<LegacyReject> = [
    pathCol,
    { title: "原因", dataIndex: "reason", width: 160, render: (r: string) => reasonText(r) },
    {
      title: "操作",
      key: "act",
      width: 168,
      // 内容相同的已沿用原条目，覆盖或重命名都没有意义。
      render: (_, row) =>
        row.reason === "same content" ? null : (
          <>
            <Button
              size="small"
              disabled={busy}
              loading={acting?.key === rowKey(row) && acting.mode === "overwrite"}
              onClick={() => onRetry(row, "overwrite")}
            >
              覆盖
            </Button>{" "}
            <Button
              size="small"
              disabled={busy}
              loading={acting?.key === rowKey(row) && acting.mode === "rename"}
              onClick={() => onRetry(row, "rename")}
            >
              重命名
            </Button>
          </>
        ),
    },
  ];
  const rejectOnlyCols: TableColumnsType<LegacyReject> = [
    pathCol,
    { title: "原因", dataIndex: "reason", render: (r: string) => reasonText(r) },
  ];
  return (
    <div style={{ marginTop: 24 }}>
      <Typography.Text>
        已入工艺 {result.processes.length}，工程 {result.projects.length}
        {result.skipped.length ? `，跳过 ${result.skipped.length}` : ""}
        {result.rejected.length ? `，拒绝 ${result.rejected.length}` : ""}
      </Typography.Text>
      {result.skipped.length > 0 ? (
        <>
          <Typography.Title level={5} style={{ marginTop: 16 }}>
            跳过（可逐条覆盖或重命名后再导）
          </Typography.Title>
          <Table
            size="small"
            rowKey={rowKey}
            pagination={false}
            columns={rejectCols}
            dataSource={result.skipped}
          />
        </>
      ) : null}
      {result.rejected.length > 0 ? (
        <>
          <Typography.Title level={5} type="danger" style={{ marginTop: 16 }}>
            拒绝（先改好文件，再重新选择文件夹导入）
          </Typography.Title>
          <Table
            size="small"
            rowKey={rowKey}
            pagination={false}
            columns={rejectOnlyCols}
            dataSource={result.rejected}
          />
        </>
      ) : null}
    </div>
  );
}

function bindDirectory(el: HTMLInputElement | null) {
  if (!el) return;
  el.setAttribute("webkitdirectory", "");
  el.click();
}

async function readLegacyTree(list: File[]): Promise<Picked> {
  const processes: LegacyFile[] = [];
  const projects: LegacyFile[] = [];
  let skipped = 0;
  for (const file of list) {
    const rel = (file.webkitRelativePath || file.name).replaceAll("\\", "/");
    const base = rel.split("/").pop() ?? file.name;
    if (!base.toLowerCase().endsWith(".json") || base === "app_settings.json") {
      skipped++;
      continue;
    }
    const content = await file.text();
    if (base === "project_data.json" || base === "multilayer_data.json") {
      projects.push({ path: rel, content });
      continue;
    }
    processes.push({ path: stripRootPrefix(rel), content });
  }
  return { processes, projects: projects.map((p) => ({ ...p, path: stripRootPrefix(p.path) })), skipped };
}

function stripRootPrefix(rel: string) {
  const parts = rel.split("/").filter(Boolean);
  const i = parts.findIndex((p) => p === "processes" || p === "projects");
  if (i >= 0) return parts.slice(i + 1).join("/");
  return parts.join("/");
}

// retryPayload 工艺只重发这一份并带回作业类型和后缀，拆出的那份才盖得到；工程连同全部工艺重发。
function retryPayload(picked: Picked, row: LegacyReject, mode: "overwrite" | "rename") {
  const path = row.path;
  const proc = picked.processes.find((f) => f.path === path);
  if (proc) {
    return {
      processes: [{ ...proc, overwrite: mode === "overwrite", rename: mode === "rename", weldKind: row.weldKind, suffix: row.suffix }],
      projects: [] as LegacyFile[],
      overwrite: false,
      rename: false,
    };
  }
  const proj = picked.projects.find((f) => f.path === path);
  if (!proj) return null;
  return {
    processes: picked.processes,
    projects: [{ ...proj, overwrite: mode === "overwrite", rename: mode === "rename" }],
    overwrite: false,
    rename: false,
  };
}

// mergeRetry 只去掉重试的那一份和新拒绝的那几份，同一路径别的作业类型那行留着。
function mergeRetry(prev: LegacyImportResult | null, row: LegacyReject, next: LegacyImportResult): LegacyImportResult {
  const base = prev ?? { processes: [], projects: [], rejected: [], skipped: [] };
  const gone = new Set([copyKey(row), ...next.rejected.map(copyKey)]);
  return {
    processes: [...base.processes, ...next.processes],
    projects: [...base.projects, ...next.projects],
    rejected: uniqueRows([...base.rejected, ...next.rejected]),
    skipped: base.skipped.filter((s) => !gone.has(copyKey(s))),
  };
}

function processName(f: LegacyFile) {
  const named = f.name?.trim();
  if (named) return named;
  try {
    const obj = JSON.parse(f.content) as { name?: unknown };
    if (typeof obj.name === "string" && obj.name.trim()) return obj.name.trim();
  } catch {
    /* 坏 JSON 交给服务端拒绝 */
  }
  const base = f.path.split("/").pop() ?? "";
  return base.replace(/\.json$/i, "");
}

function projectName(f: LegacyFile) {
  const named = f.name?.trim();
  if (named) return named;
  const parts = f.path.replaceAll("\\", "/").split("/").filter(Boolean);
  if (parts.length >= 2) return parts[parts.length - 2];
  return "imported-project";
}

type SameNames = { existing: string[]; batch: string[] };

// 库里已有的同名，另记本批内同名不同路径的（服务端按路径另起名，不互相覆盖）。
function sameNames(picked: Picked, existing: Set<string>): SameNames {
  const out: string[] = [];
  const seen = new Set<string>();
  const paths = new Map<string, { label: string; paths: Set<string> }>();
  const note = (k: string, label: string, path: string) => {
    const hit = paths.get(k) ?? { label, paths: new Set<string>() };
    hit.paths.add(path);
    paths.set(k, hit);
  };
  for (const f of picked.processes) {
    const n = processName(f);
    const k = `process:${n}`;
    note(k, `工艺 ${n}`, f.path);
    if (existing.has(k) && !seen.has(k)) {
      seen.add(k);
      out.push(`工艺 ${n}`);
    }
  }
  for (const f of picked.projects) {
    const n = projectName(f);
    const k = `project:${n}`;
    note(k, `工程 ${n}`, f.path);
    if (existing.has(k) && !seen.has(k)) {
      seen.add(k);
      out.push(`工程 ${n}`);
    }
  }
  const batch = [...paths.values()].filter((v) => v.paths.size > 1).map((v) => v.label);
  return { existing: out, batch };
}

function previewNames(list: string[]) {
  return list.slice(0, 8).join("、") + (list.length > 8 ? ` 等 ${list.length} 个` : "");
}

function askSameName(
  modal: ReturnType<typeof App.useApp>["modal"],
  names: SameNames,
): Promise<"overwrite" | "rename" | "skip" | "cancel"> {
  const lines: string[] = [];
  if (names.existing.length > 0) {
    lines.push(`覆盖会改已有正文并保留身份；重命名按路径另起一份；跳过同名则沿用已有、只导入其余。同名：${previewNames(names.existing)}`);
  }
  if (names.batch.length > 0) {
    lines.push(`本批内同名不同路径的会按路径另起名，互不覆盖：${previewNames(names.batch)}`);
  }
  return new Promise((resolve) => {
    let decided = false;
    const inst = modal.confirm({
      title: names.existing.length > 0 ? "已有同名工艺或工程" : "本批有同名工艺或工程",
      content: (
        <>
          {lines.map((line) => (
            <Typography.Paragraph key={line} style={{ marginBottom: 8 }}>
              {line}
            </Typography.Paragraph>
          ))}
        </>
      ),
      okText: "跳过同名",
      cancelText: "取消",
      // 回车走跳过同名，不改已有正文；覆盖要单独点。
      onOk: () => {
        decided = true;
        resolve("skip");
      },
      onCancel: () => {
        if (!decided) resolve("cancel");
      },
      footer: (_, { OkBtn, CancelBtn }) => (
        <>
          <CancelBtn />
          <Button
            danger
            onClick={() => {
              decided = true;
              inst.destroy();
              resolve("overwrite");
            }}
          >
            覆盖
          </Button>
          <Button
            onClick={() => {
              decided = true;
              inst.destroy();
              resolve("rename");
            }}
          >
            重命名
          </Button>
          <OkBtn />
        </>
      ),
    });
  });
}
