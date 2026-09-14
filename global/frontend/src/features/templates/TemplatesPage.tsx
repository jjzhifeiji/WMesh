import { App, Button, Card, Input, Modal, Space, Spin, Typography, Upload } from "antd";
import { useState } from "react";
import { errorMessage } from "@/shared/api/client";
import { PageHeader } from "@/shared/ui/PageHeader";
import type { AssetKind } from "@/features/assets/api";
import { useTemplate, useUpdateTemplate, useBuiltinTemplate } from "./api";
import { SchemaEditor } from "./SchemaEditor";
import { contentFromSchema, schemaFromJSON, adoptSchema, type ContentSchema } from "./schema";

export function TemplatesPage({ kind }: { kind: AssetKind }) {
  const { message, modal } = App.useApp();
  const isProcess = kind === "process";
  const q = useTemplate(kind);
  const builtin = useBuiltinTemplate(kind, !isProcess);
  const save = useUpdateTemplate();
  const [local, setLocal] = useState<{ kind: AssetKind; revision: number; schema: ContentSchema } | null>(null);
  const [importOpen, setImportOpen] = useState(false);
  const [importText, setImportText] = useState("");
  const [exportOpen, setExportOpen] = useState(false);

  const draft = local && q.data && local.kind === kind && local.revision === q.data.revision ? local.schema : (q.data?.schema ?? null);
  const setDraft = (schema: ContentSchema) => {
    if (!q.data) return;
    setLocal({ kind, revision: q.data.revision, schema });
  };

  const onErr = (e: unknown) => message.error(errorMessage(e));
  const title = isProcess ? "工艺模版" : "工程模版";
  const noun = isProcess ? "工艺" : "工程";
  const exportJSON = draft ? JSON.stringify(contentFromSchema(draft), null, 4) : "";
  const fileName = isProcess ? "工艺.json" : "工程.json";

  const applyImport = (raw: string) => {
    try {
      const schema = adoptSchema(schemaFromJSON(raw), draft);
      if (!q.data) {
        message.error("模版还没加载完");
        return;
      }
      setLocal({ kind, revision: q.data.revision, schema });
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
    a.download = fileName;
    a.click();
    URL.revokeObjectURL(url);
  };

  return (
    <>
      <PageHeader
        title={title}
        description={`全平台一份。保存后下发给在线工厂，不改已有${noun}正文；只有新建才按模版套字段。`}
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
            {!isProcess ? (
              <Button
                disabled={!builtin.data || !q.data}
                onClick={() => {
                  if (!builtin.data || !q.data) return;
                  setLocal({ kind, revision: q.data.revision, schema: builtin.data.schema });
                  message.success("已恢复默认，尚未保存");
                }}
              >
                恢复默认
              </Button>
            ) : null}
            <Button
              type="primary"
              loading={save.isPending}
              disabled={!draft || !q.data}
              onClick={() => {
                if (!draft || !q.data) return;
                modal.confirm({
                  title: `保存${title}？`,
                  content: `已有${noun}正文不变。此后新建才按新字段。`,
                  onOk: () =>
                    save.mutate(
                      { kind, expected: q.data.revision, schema: draft },
                      { onSuccess: () => message.success("已保存"), onError: onErr },
                    ),
                });
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
            <Typography.Text type="secondary" style={{ fontSize: 12, display: "block", marginBottom: 8 }}>
              修订 {q.data?.revision ?? 1}
            </Typography.Text>
            <SchemaEditor value={draft} onChange={setDraft} />
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
          贴{noun}正文，和设备里的 JSON 一样。
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
