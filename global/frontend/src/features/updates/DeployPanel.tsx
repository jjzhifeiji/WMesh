import { InboxOutlined } from "@ant-design/icons";
import { useQueryClient } from "@tanstack/react-query";
import { App, Button, Card, Progress, Space, Table, Tag, Typography, type TableColumnsType } from "antd";
import { useEffect, useRef, useState } from "react";
import { ApiError, errorMessage } from "@/shared/api/client";
import { formatBytes, formatSpeed, shortHash } from "@/shared/format";
import { publishSoftware, softwareKindLabel, softwareKeys, type SoftwareKind, type SoftwareRelease } from "./api";
import { planDistPublish, sha256Hex, type DistAction, type DistRow } from "./pack";

type JobPhase = "wait" | "hash" | "upload" | "ok" | "skip" | "fail";

type Job = {
  phase: JobPhase;
  loaded: number;
  total: number;
  speed: number;
  message: string;
};

type Phase = "empty" | "preview" | "uploading" | "done";

function actionTag(action: DistAction) {
  if (action === "publish") return <Tag color="blue">将发布</Tag>;
  if (action === "skip") return <Tag>跳过</Tag>;
  return <Tag color="red">拒绝</Tag>;
}

function jobTag(job?: Job) {
  if (!job) return null;
  if (job.phase === "hash") return <Tag color="processing">校验</Tag>;
  if (job.phase === "upload") return <Tag color="processing">上传</Tag>;
  if (job.phase === "ok") return <Tag color="green">已发布</Tag>;
  if (job.phase === "skip") return <Tag>跳过</Tag>;
  if (job.phase === "fail") return <Tag color="red">失败</Tag>;
  return <Tag>等待</Tag>;
}

function formatEta(remain: number, speed: number) {
  if (speed <= 0 || remain <= 0) return "—";
  const sec = Math.max(1, Math.ceil(remain / speed));
  if (sec < 60) return `剩余约 ${sec} 秒`;
  const min = Math.ceil(sec / 60);
  return `剩余约 ${min} 分钟`;
}

function bindDirectory(el: HTMLInputElement | null) {
  if (!el) return;
  el.setAttribute("webkitdirectory", "");
  el.setAttribute("directory", "");
  el.click();
}

function summarize(rows: DistRow[]) {
  const publish = rows.filter((r) => r.action === "publish");
  const skip = rows.filter((r) => r.action === "skip");
  const reject = rows.filter((r) => r.action === "reject");
  const bytes = publish.reduce((n, r) => n + r.bytes, 0);
  return { publish, skip, reject, bytes };
}

// 选 pack.sh 的 dist：先预览确认，再按进度上传。
export function DeployPanel({ published }: { published: SoftwareRelease[] }) {
  const qc = useQueryClient();
  const { message, modal } = App.useApp();
  const picker = useRef<HTMLInputElement>(null);
  const abortRef = useRef<AbortController | null>(null);
  const [phase, setPhase] = useState<Phase>("empty");
  const [parsing, setParsing] = useState(false);
  const [rows, setRows] = useState<DistRow[]>([]);
  const [jobs, setJobs] = useState<Record<string, Job>>({});
  const [overall, setOverall] = useState({ loaded: 0, total: 0, speed: 0 });
  const [folderHint, setFolderHint] = useState("");
  const [previewStats, setPreviewStats] = useState({ publish: 0, skip: 0, reject: 0, bytes: 0 });

  const stats = previewStats;
  const busy = phase === "uploading";

  useEffect(() => {
    const el = picker.current;
    if (!el) return;
    el.setAttribute("webkitdirectory", "");
    el.setAttribute("directory", "");
  }, []);

  const patchJob = (key: string, patch: Partial<Job>) => {
    setJobs((cur) => ({ ...cur, [key]: { ...(cur[key] ?? { phase: "wait", loaded: 0, total: 0, speed: 0, message: "" }), ...patch } }));
  };

  const onFiles = async (list: FileList | null) => {
    if (!list?.length || busy) return;
    // FileList 跟着 input.value 走，必须先拷出来再清空，否则本机解析会读到空目录。
    const files = Array.from(list);
    if (picker.current) picker.current.value = "";
    setParsing(true);
    try {
      const planned = await planDistPublish(files, published);
      if (planned.length === 0) {
        message.error("这个目录里没有 pack.sh 打出的元数据");
        return;
      }
      const first = files[0].webkitRelativePath || files[0].name;
      const root = first.split(/[/\\]/)[0] ?? "dist";
      setFolderHint(root);
      setRows(planned);
      const plan = summarize(planned);
      setPreviewStats({ publish: plan.publish.length, skip: plan.skip.length, reject: plan.reject.length, bytes: plan.bytes });
      setJobs({});
      setOverall({ loaded: 0, total: 0, speed: 0 });
      setPhase("preview");
    } catch (e) {
      message.error(errorMessage(e));
    } finally {
      setParsing(false);
    }
  };

  const confirmAndStart = () => {
    const plan = summarize(rows);
    if (plan.publish.length === 0) {
      message.info("没有需要上传的新包，旧版和重复已跳过");
      return;
    }
    const lines = plan.publish.map((r) => `${softwareKindLabel[r.kind]} ${r.versionName}（v${r.version}） ${formatBytes(r.bytes)}`);
    let decided = false;
    modal.confirm({
      title: "确认上传这些包？",
      content: (
        <div>
          <p>
            将上传 {plan.publish.length} 个包，共 {formatBytes(plan.bytes)}
            {plan.skip.length ? `；跳过 ${plan.skip.length} 个` : ""}
            {plan.reject.length ? `；拒绝 ${plan.reject.length} 个` : ""}。
          </p>
          <ul style={{ paddingLeft: 20, marginBottom: 0 }}>
            {lines.map((t) => (
              <li key={t}>{t}</li>
            ))}
          </ul>
        </div>
      ),
      okText: "开始上传",
      cancelText: "返回",
      onOk: () => {
        decided = true;
        void runUpload();
      },
      onCancel: () => {
        if (!decided) return;
      },
    });
  };

  const runUpload = async () => {
    const todo = rows.filter((r) => r.action === "publish" && r.file);
    const total = todo.reduce((n, r) => n + r.bytes, 0);
    const ac = new AbortController();
    abortRef.current = ac;
    setPhase("uploading");
    setOverall({ loaded: 0, total, speed: 0 });
    setJobs(Object.fromEntries(todo.map((r) => [r.key, { phase: "wait" as const, loaded: 0, total: r.bytes, speed: 0, message: "等待" }])));

    let doneBytes = 0;
    let tickAt = Date.now();
    let tickBytes = 0;
    let speed = 0;
    const sample = (loaded: number) => {
      const now = Date.now();
      const dt = (now - tickAt) / 1000;
      if (dt >= 0.25) {
        const inst = Math.max(0, (loaded - tickBytes) / dt);
        speed = speed <= 0 ? inst : speed * 0.65 + inst * 0.35;
        tickAt = now;
        tickBytes = loaded;
      }
      setOverall({ loaded, total, speed });
      return speed;
    };

    const next = [...rows];
    try {
      for (let i = 0; i < next.length; i++) {
        if (ac.signal.aborted) break;
        const row = next[i];
        if (row.action !== "publish" || !row.file) continue;
        patchJob(row.key, { phase: "hash", loaded: 0, total: row.bytes, message: "校验摘要" });
        const sum = await sha256Hex(row.file);
        if (ac.signal.aborted) break;
        if (sum !== row.sha256) {
          next[i] = { ...row, action: "reject", reason: "文件摘要与 json 对不上" };
          patchJob(row.key, { phase: "fail", message: "文件摘要与 json 对不上" });
          continue;
        }
        const form = new FormData();
        form.append("kind", row.kind);
        form.append("version", String(row.version));
        form.append("versionName", row.versionName);
        form.append("file", row.file, row.fileName);
        patchJob(row.key, { phase: "upload", loaded: 0, total: row.bytes, message: "上传中" });
        try {
          await publishSoftware(
            form,
            (ev) => {
              const loaded = doneBytes + ev.loaded;
              const sp = sample(loaded);
              patchJob(row.key, { phase: "upload", loaded: ev.loaded, total: ev.total || row.bytes, speed: sp, message: formatSpeed(sp) });
            },
            ac.signal,
          );
          doneBytes += row.bytes;
          sample(doneBytes);
          next[i] = { ...row, action: "skip", reason: "已发布" };
          patchJob(row.key, { phase: "ok", loaded: row.bytes, total: row.bytes, message: "已发布" });
        } catch (e) {
          if (e instanceof DOMException && e.name === "AbortError") {
            patchJob(row.key, { phase: "fail", message: "已取消" });
            break;
          }
          const code = e instanceof ApiError ? e.code : "";
          if (code === "revision is not strictly newer") {
            next[i] = { ...row, action: "skip", reason: "已有更高版本，跳过" };
            patchJob(row.key, { phase: "skip", message: "已有更高版本，跳过" });
          } else if (code === "asset integrity check failed") {
            next[i] = { ...row, action: "reject", reason: "同版本摘要不同，原件不动" };
            patchJob(row.key, { phase: "fail", message: "同版本摘要不同，原件不动" });
          } else {
            next[i] = { ...row, action: "reject", reason: errorMessage(e) };
            patchJob(row.key, { phase: "fail", message: errorMessage(e) });
          }
        }
      }
      setRows(next);
      await qc.invalidateQueries({ queryKey: softwareKeys.all });
      await qc.invalidateQueries({ queryKey: softwareKeys.pending });
      const uploaded = next.filter((r) => r.reason === "已发布").length;
      const failed = next.filter((r) => r.action === "reject").length;
      if (ac.signal.aborted) {
        message.warning("已取消后续上传");
      } else if (failed === 0) {
        message.success(uploaded > 0 ? `已发布 ${uploaded} 个包` : "没有需要新发的包");
      } else {
        message.error(`${failed} 个包没有发出去`);
      }
    } finally {
      abortRef.current = null;
      setPhase("done");
    }
  };

  const columns: TableColumnsType<DistRow> = [
    { title: "种类", dataIndex: "kind", width: 110, render: (k: SoftwareKind) => softwareKindLabel[k] ?? k },
    { title: "版本号", dataIndex: "version", width: 80 },
    { title: "版本名", dataIndex: "versionName", width: 100 },
    {
      title: "现网",
      width: 120,
      render: (_, r) => (r.currentVersion ? `${r.currentVersionName ?? "—"}（v${r.currentVersion}）` : "未发布"),
    },
    { title: "文件", dataIndex: "fileName", ellipsis: true },
    { title: "大小", dataIndex: "bytes", width: 90, render: (n: number) => formatBytes(n) },
    { title: "摘要", dataIndex: "sha256", width: 140, render: (v: string) => shortHash(v) },
    { title: "处理", width: 90, render: (_, r) => (phase === "empty" || phase === "preview" ? actionTag(r.action) : jobTag(jobs[r.key]) ?? actionTag(r.action)) },
    {
      title: "进度 / 说明",
      render: (_, r) => {
        const job = jobs[r.key];
        if (job && (job.phase === "upload" || job.phase === "hash")) {
          const pct = job.phase === "hash" || job.total <= 0 ? 0 : Math.round((job.loaded / job.total) * 100);
          return (
            <div>
              <Progress percent={pct} status="active" size="small" />
              <Typography.Text type="secondary">{job.message}</Typography.Text>
            </div>
          );
        }
        return job?.message || r.reason;
      },
    },
  ];

  const overallPct = overall.total > 0 ? Math.min(100, Math.round((overall.loaded / overall.total) * 100)) : 0;

  return (
    <Card>
      <input ref={picker} type="file" multiple style={{ display: "none" }} onChange={(e) => void onFiles(e.target.files)} />
      <Space wrap>
        <Button icon={<InboxOutlined />} disabled={busy} loading={parsing} onClick={() => bindDirectory(picker.current)}>
          选择文件夹
        </Button>
        {phase === "preview" ? (
          <Button type="primary" disabled={stats.publish === 0} onClick={confirmAndStart}>
            开始发布
          </Button>
        ) : null}
        {phase === "uploading" ? (
          <Button danger onClick={() => abortRef.current?.abort()}>
            取消上传
          </Button>
        ) : null}
        {phase === "done" ? (
          <Button
            onClick={() => {
              setPhase("empty");
              setRows([]);
              setJobs({});
              setFolderHint("");
              setPreviewStats({ publish: 0, skip: 0, reject: 0, bytes: 0 });
            }}
          >
            重新选择
          </Button>
        ) : null}
      </Space>
      {phase === "empty" ? (
        <Typography.Paragraph type="secondary" style={{ marginTop: 12, marginBottom: 0 }}>
          选 pack.sh 打出的 dist 目录。这一步只在本机读 json 做预览，不会上传。
        </Typography.Paragraph>
      ) : (
        <Typography.Paragraph type="secondary" style={{ marginTop: 12 }}>
          {phase === "preview"
            ? `目录 ${folderHint}：将发布 ${stats.publish} 个（${formatBytes(stats.bytes)}），跳过 ${stats.skip} 个，拒绝 ${stats.reject} 个。确认后再上传。`
            : `目录 ${folderHint}：已发布 ${Object.values(jobs).filter((j) => j.phase === "ok").length} 个，跳过 ${stats.skip + Object.values(jobs).filter((j) => j.phase === "skip").length} 个，失败 ${Object.values(jobs).filter((j) => j.phase === "fail").length} 个。`}
        </Typography.Paragraph>
      )}
      {phase === "uploading" || phase === "done" ? (
        <div style={{ marginBottom: 12 }}>
          <Progress percent={phase === "done" ? 100 : overallPct} status={phase === "uploading" ? "active" : undefined} />
          <Typography.Text type="secondary">
            {formatBytes(overall.loaded)} / {formatBytes(overall.total)}
            {phase === "uploading" ? ` · ${formatSpeed(overall.speed)} · ${formatEta(overall.total - overall.loaded, overall.speed)}` : ""}
          </Typography.Text>
        </div>
      ) : null}
      {rows.length > 0 ? (
        <Table<DistRow> rowKey={(r) => r.key} size="small" pagination={false} columns={columns} dataSource={rows} />
      ) : null}
    </Card>
  );
}
