import { InboxOutlined } from "@ant-design/icons";
import { App, Button, Card, Table, Typography, type TableColumnsType } from "antd";
import { useMemo, useRef, useState } from "react";
import { useAssets } from "@/features/assets/api";
import { errorMessage } from "@/shared/api/client";
import { PageHeader } from "@/shared/ui/PageHeader";
import { useImportLegacy, type LegacyFile, type LegacyImportResult, type LegacyReject } from "./api";

type Picked = { processes: LegacyFile[]; projects: LegacyFile[]; skipped: number };

// 选示教器旧目录：工艺先入库，工程把路径换成身份；缺文件的那份工程拒绝。
export function LegacyImportPage() {
  const imp = useImportLegacy();
  const procs = useAssets("process");
  const projs = useAssets("project");
  const { message, modal } = App.useApp();
  const inputRef = useRef<HTMLInputElement>(null);
  const [picked, setPicked] = useState<Picked | null>(null);
  const [result, setResult] = useState<LegacyImportResult | null>(null);
  const [acting, setActing] = useState<{ path: string; mode: "overwrite" | "rename" } | null>(null);
  const existing = useMemo(() => {
    const set = new Set<string>();
    for (const a of [...(procs.data ?? []), ...(projs.data ?? [])]) {
      if (a.level !== "factory" || a.status === "disabled") continue;
      set.add(`${a.kind}:${a.name}`);
    }
    return set;
  }, [procs.data, projs.data]);

  const onFiles = async (list: FileList | null) => {
    if (!list || list.length === 0) return;
    const next = await readLegacyTree(list);
    setPicked(next);
    setResult(null);
  };

  const start = async () => {
    if (!picked) return;
    const collisions = sameNames(picked, existing);
    let overwrite = false;
    let rename = false;
    if (collisions.length > 0) {
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
    const payload = retryPayload(picked, row.path, mode);
    if (!payload) {
      message.error("找不到该文件");
      return;
    }
    setActing({ path: row.path, mode });
    imp.mutate(payload, {
      onSettled: () => setActing(null),
      onSuccess: (data) => {
        const merged = mergeRetry(result, row.path, normalizeResult(data));
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
        description="选示教器上的 processes / projects 目录（或整份 ShiJiaoQi）。只收本厂工艺工程师导入；缺路径的工程整份拒绝，已入工艺保留。同名会问覆盖、按路径重命名还是跳过。"
      />
      <Card style={{ maxWidth: 880 }}>
        <input
          ref={inputRef}
          type="file"
          multiple
          style={{ display: "none" }}
          onChange={(e) => void onFiles(e.target.files)}
        />
        <Button icon={<InboxOutlined />} onClick={() => bindDirectory(inputRef.current)}>
          选择文件夹
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
          disabled={!picked || (picked.processes.length === 0 && picked.projects.length === 0)}
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

function normalizeResult(data: LegacyImportResult): LegacyImportResult {
  return {
    processes: data.processes ?? [],
    projects: data.projects ?? [],
    rejected: data.rejected ?? [],
    skipped: data.skipped ?? [],
  };
}

function ResultTables({
  result,
  acting,
  busy,
  onRetry,
}: {
  result: LegacyImportResult;
  acting: { path: string; mode: "overwrite" | "rename" } | null;
  busy: boolean;
  onRetry: (row: LegacyReject, mode: "overwrite" | "rename") => void;
}) {
  const rejectCols: TableColumnsType<LegacyReject> = [
    { title: "路径", dataIndex: "path", ellipsis: true },
    { title: "原因", dataIndex: "reason", width: 120 },
    {
      title: "操作",
      key: "act",
      width: 168,
      render: (_, row) => (
        <>
          <Button
            size="small"
            disabled={busy}
            loading={acting?.path === row.path && acting.mode === "overwrite"}
            onClick={() => onRetry(row, "overwrite")}
          >
            覆盖
          </Button>{" "}
          <Button
            size="small"
            disabled={busy}
            loading={acting?.path === row.path && acting.mode === "rename"}
            onClick={() => onRetry(row, "rename")}
          >
            重命名
          </Button>
        </>
      ),
    },
  ];
  const rejectOnlyCols: TableColumnsType<LegacyReject> = [
    { title: "路径", dataIndex: "path", ellipsis: true },
    { title: "原因", dataIndex: "reason" },
  ];
  return (
    <div style={{ marginTop: 24 }}>
      <Typography.Text>
        已入工艺 {result.processes.length}，工程 {result.projects.length}
        {result.skipped.length ? `，跳过 ${result.skipped.length}` : ""}
      </Typography.Text>
      {result.skipped.length > 0 ? (
        <Table
          style={{ marginTop: 12 }}
          size="small"
          rowKey="path"
          pagination={false}
          columns={rejectCols}
          dataSource={result.skipped}
        />
      ) : null}
      {result.rejected.length > 0 ? (
        <Table
          style={{ marginTop: 12 }}
          size="small"
          rowKey="path"
          pagination={false}
          columns={rejectOnlyCols}
          dataSource={result.rejected}
        />
      ) : null}
    </div>
  );
}

function bindDirectory(el: HTMLInputElement | null) {
  if (!el) return;
  el.setAttribute("webkitdirectory", "");
  el.click();
}

async function readLegacyTree(list: FileList): Promise<Picked> {
  const processes: LegacyFile[] = [];
  const projects: LegacyFile[] = [];
  let skipped = 0;
  for (const file of Array.from(list)) {
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

function retryPayload(picked: Picked, path: string, mode: "overwrite" | "rename") {
  const proc = picked.processes.find((f) => f.path === path);
  if (proc) {
    return {
      processes: [{ ...proc, overwrite: mode === "overwrite", rename: mode === "rename" }],
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

function mergeRetry(prev: LegacyImportResult | null, path: string, next: LegacyImportResult): LegacyImportResult {
  const base = prev ?? { processes: [], projects: [], rejected: [], skipped: [] };
  const gone = new Set([path, ...next.rejected.map((r) => r.path)]);
  return {
    processes: [...base.processes, ...next.processes],
    projects: [...base.projects, ...next.projects],
    rejected: [...base.rejected, ...next.rejected],
    skipped: base.skipped.filter((s) => !gone.has(s.path)),
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

function sameNames(picked: Picked, existing: Set<string>) {
  const out: string[] = [];
  const seen = new Set<string>();
  for (const f of picked.processes) {
    const n = processName(f);
    const k = `process:${n}`;
    if (existing.has(k) && !seen.has(k)) {
      seen.add(k);
      out.push(`工艺 ${n}`);
    }
  }
  for (const f of picked.projects) {
    const n = projectName(f);
    const k = `project:${n}`;
    if (existing.has(k) && !seen.has(k)) {
      seen.add(k);
      out.push(`工程 ${n}`);
    }
  }
  return out;
}

function askSameName(
  modal: ReturnType<typeof App.useApp>["modal"],
  names: string[],
): Promise<"overwrite" | "rename" | "skip" | "cancel"> {
  const preview = names.slice(0, 8).join("、") + (names.length > 8 ? ` 等 ${names.length} 个` : "");
  return new Promise((resolve) => {
    let decided = false;
    const inst = modal.confirm({
      title: "已有同名工艺或工程",
      content: `覆盖会改已有正文并保留身份；重命名按路径另起一份；跳过同名则沿用已有、只导入其余。同名：${preview}`,
      okText: "覆盖",
      cancelText: "取消",
      onOk: () => {
        decided = true;
        resolve("overwrite");
      },
      onCancel: () => {
        if (!decided) resolve("cancel");
      },
      footer: (_, { OkBtn, CancelBtn }) => (
        <>
          <CancelBtn />
          <Button
            onClick={() => {
              decided = true;
              inst.destroy();
              resolve("skip");
            }}
          >
            跳过同名
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
