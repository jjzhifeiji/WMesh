import { App, Button, Card, Input, Modal, Segmented, Space, Spin, Typography, Upload } from "antd";
import { useState } from "react";
import { errorMessage } from "@/shared/api/client";
import { PageHeader } from "@/shared/ui/PageHeader";
import { useTemplate, useUpdateTemplate } from "./api";
import { SchemaEditor } from "./SchemaEditor";
import { contentFromSchema, schemaFromJSON, adoptSchema, canonicalizeSchema, type ContentSchema } from "./schema";

type View = "form" | "json";

function schemaJSON(schema: ContentSchema): string {
  return JSON.stringify(canonicalizeSchema(schema), null, 4);
}

function parseSchemaJSON(raw: string): ContentSchema {
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    throw new Error("不是合法 JSON");
  }
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
    throw new Error("根必须是对象");
  }
  const root = (parsed as ContentSchema).root;
  if (root !== "object" && root !== "array") {
    throw new Error("root 须是 object 或 array");
  }
  return canonicalizeSchema(parsed as ContentSchema);
}

export function TemplatesPage() {
  const { message, modal } = App.useApp();
  const q = useTemplate("process");
  const save = useUpdateTemplate();
  const [local, setLocal] = useState<{ revision: number; schema: ContentSchema } | null>(null);
  const [importOpen, setImportOpen] = useState(false);
  const [importText, setImportText] = useState("");
  const [exportOpen, setExportOpen] = useState(false);
  const [view, setView] = useState<View>("form");
  const [jsonText, setJsonText] = useState("");
  const [jsonErr, setJsonErr] = useState("");

  const draft = local && q.data && local.revision === q.data.revision ? local.schema : (q.data?.schema ?? null);
  const setDraft = (schema: ContentSchema) => {
    if (!q.data) return;
    setLocal({ revision: q.data.revision, schema });
  };

  const onErr = (e: unknown) => message.error(errorMessage(e));
  const exportJSON = draft ? JSON.stringify(contentFromSchema(draft), null, 4) : "";

  const applyImport = (raw: string) => {
    try {
      const schema = adoptSchema(schemaFromJSON(raw), draft);
      if (!q.data) {
        message.error("模版还没加载完");
        return;
      }
      setLocal({ revision: q.data.revision, schema });
      setImportOpen(false);
      setImportText("");
      message.success("已导入，尚未保存");
    } catch (e) {
      message.error(e instanceof Error ? e.message : "导入失败");
    }
  };

  const downloadExport = () => {
    const blob = new Blob([exportJSON], { type: "application/json" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = "工艺.json";
    a.click();
    URL.revokeObjectURL(url);
  };

  const applyJSON = (): ContentSchema | null => {
    try {
      const schema = parseSchemaJSON(jsonText);
      setJsonErr("");
      return schema;
    } catch (e) {
      setJsonErr(e instanceof Error ? e.message : "不是合法 JSON");
      return null;
    }
  };

  const switchView = (next: View) => {
    if (next === view) return;
    if (next === "json") {
      if (!draft) return;
      setJsonText(schemaJSON(draft));
      setJsonErr("");
      setView("json");
      return;
    }
    const schema = applyJSON();
    if (!schema) return;
    setDraft(schema);
    setView("form");
  };

  const persist = (schema: ContentSchema) => {
    if (!q.data) return;
    modal.confirm({
      title: "保存工艺模版？",
      content: "已有工艺正文不变。此后新建才按新字段。",
      onOk: () =>
        save.mutate(
          { kind: "process", expected: q.data.revision, schema },
          { onSuccess: () => message.success("已保存"), onError: onErr },
        ),
    });
  };

  return (
    <>
      <PageHeader
        title="工艺模版"
        description="全平台一份。保存后下发给在线工厂，不改已有工艺正文；只有新建才按模版套字段。"
        extra={
          <Space wrap>
            <Button disabled={!draft} onClick={() => setExportOpen(true)}>
              导出 JSON
            </Button>
            <Button
              onClick={() => {
                setImportText("");
                setImportOpen(true);
              }}
            >
              导入 JSON
            </Button>
            <Button
              type="primary"
              loading={save.isPending}
              disabled={!draft || !q.data}
              onClick={() => {
                if (!draft || !q.data) return;
                if (view === "form") {
                  persist(draft);
                  return;
                }
                const schema = applyJSON();
                if (!schema) return;
                setDraft(schema);
                persist(schema);
              }}
            >
              保存
            </Button>
          </Space>
        }
      />
      <Card size="small" styles={{ body: { padding: 12 } }}>
        {q.isLoading ? (
          <Spin />
        ) : q.isError ? (
          <Typography.Text type="danger">{errorMessage(q.error)}</Typography.Text>
        ) : draft ? (
          <>
            <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 12, marginBottom: 8 }}>
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                修订 {q.data?.revision ?? 1}
              </Typography.Text>
              <Segmented
                value={view}
                onChange={(v) => switchView(v as View)}
                options={[
                  { label: "表单", value: "form" },
                  { label: "JSON", value: "json" },
                ]}
              />
            </div>
            {view === "form" ? (
              <SchemaEditor value={draft} onChange={setDraft} />
            ) : (
              <>
                {jsonErr ? (
                  <Typography.Text type="danger" style={{ display: "block", marginBottom: 8 }}>
                    {jsonErr}
                  </Typography.Text>
                ) : null}
                <Input.TextArea
                  className="schema-json"
                  rows={22}
                  value={jsonText}
                  onChange={(e) => {
                    setJsonText(e.target.value);
                    setJsonErr("");
                  }}
                />
              </>
            )}
          </>
        ) : (
          <Typography.Text type="secondary">没有模版。</Typography.Text>
        )}
      </Card>
      <Modal
        title="导入 JSON"
        open={importOpen}
        onCancel={() => setImportOpen(false)}
        okText="导入"
        onOk={() => applyImport(importText)}
        destroyOnHidden
        width={720}
      >
        <Typography.Text type="secondary" style={{ display: "block", marginBottom: 8 }}>
          贴工艺正文，和设备里的 JSON 一样。
        </Typography.Text>
        <Upload
          accept="application/json,.json"
          showUploadList={false}
          beforeUpload={(file) => {
            void file.text().then((text) => setImportText(text)).catch(() => message.error("读文件失败"));
            return false;
          }}
        >
          <Button size="small" style={{ marginBottom: 8 }}>
            选择文件
          </Button>
        </Upload>
        <Input.TextArea className="schema-json" rows={16} value={importText} onChange={(e) => setImportText(e.target.value)} placeholder={'{\n    "name": "示例",\n    "current": 170.5\n}'} />
      </Modal>
      <Modal
        title="导出 JSON"
        open={exportOpen}
        onCancel={() => setExportOpen(false)}
        width={720}
        destroyOnHidden
        footer={
          <Space>
            <Button
              onClick={() => {
                void navigator.clipboard.writeText(exportJSON).then(
                  () => message.success("已复制"),
                  () => message.error("复制失败"),
                );
              }}
            >
              复制
            </Button>
            <Button type="primary" onClick={downloadExport}>
              下载
            </Button>
          </Space>
        }
      >
        <Input.TextArea className="schema-json" rows={20} value={exportJSON} readOnly />
      </Modal>
    </>
  );
}
