import { InboxOutlined } from "@ant-design/icons";
import { App, Button, Card, Table, Typography, type TableColumnsType } from "antd";
import { useRef, useState } from "react";
import { errorMessage } from "@/shared/api/client";
import { PageHeader } from "@/shared/ui/PageHeader";
import { useImportLegacy, type LegacyFile, type LegacyImportResult, type LegacyReject } from "./api";

type Picked = { processes: LegacyFile[]; projects: LegacyFile[]; skipped: number };

// 选示教器旧目录：工艺先入库，工程把路径换成身份；缺文件的那份工程拒绝。
export function LegacyImportPage() {
  const imp = useImportLegacy();
  const { message } = App.useApp();
  const inputRef = useRef<HTMLInputElement>(null);
  const [picked, setPicked] = useState<Picked | null>(null);
  const [result, setResult] = useState<LegacyImportResult | null>(null);

  const onFiles = async (list: FileList | null) => {
    if (!list || list.length === 0) return;
    const next = await readLegacyTree(list);
    setPicked(next);
    setResult(null);
  };

  return (
    <>
      <PageHeader
        title="旧文件导入"
        description="选示教器上的 processes / projects 目录（或整份 ShiJiaoQi）。只收本厂工艺工程师导入；缺路径的工程整份拒绝，已入工艺保留。"
      />
      <Card style={{ maxWidth: 720 }}>
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
          onClick={() =>
            picked &&
            imp.mutate(
              { processes: picked.processes, projects: picked.projects },
              {
                onSuccess: (data) => {
                  setResult(data);
                  message.success(`已入工艺 ${data.processes.length}，工程 ${data.projects.length}，拒绝 ${data.rejected.length}`);
                },
                onError: (e) => message.error(errorMessage(e)),
              },
            )
          }
        >
          开始导入
        </Button>
        {result ? <ResultTables result={result} /> : null}
      </Card>
    </>
  );
}

function ResultTables({ result }: { result: LegacyImportResult }) {
  const rejectCols: TableColumnsType<LegacyReject> = [
    { title: "路径", dataIndex: "path", ellipsis: true },
    { title: "原因", dataIndex: "reason" },
  ];
  return (
    <div style={{ marginTop: 24 }}>
      <Typography.Text>已入工艺 {result.processes.length}，工程 {result.projects.length}</Typography.Text>
      {result.rejected.length > 0 ? (
        <Table
          style={{ marginTop: 12 }}
          size="small"
          rowKey="path"
          pagination={false}
          columns={rejectCols}
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
