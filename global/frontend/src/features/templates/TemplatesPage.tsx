import { PlusOutlined } from "@ant-design/icons";
import { Alert, App, Button, Input, Modal, Segmented, Space, Spin, Tag, Typography, Upload } from "antd";
import { useMemo, useState } from "react";
import { copyText } from "@/shared/clipboard";
import { errorMessage } from "@/shared/api/client";
import { useConfirmDiscard } from "@/shared/ui/confirmDiscard";
import { PageHeader } from "@/shared/ui/PageHeader";
import { useCreateProcessTemplate, useDeleteProcessTemplate, useProcessTemplates, useUpdateProcessTemplate, type ContentTemplate } from "./api";
import { ContentFields } from "./ContentFields";
import { PROCESS_SEEDS, SEED_TPL_CORNER, SEED_TPL_WELD } from "./processKinds";
import { SchemaEditor } from "./SchemaEditor";
import { adoptSchema, canonicalizeSchema, contentFromSchema, defaultValue, schemaFromJSON, type ContentSchema } from "./schema";

type View = "form" | "json";

// 还没保存的新模版在清单里占一行，用这个假身份选中它。
const NEW_ID = "__new__";

// Draft 当前选中那份的未保存改动；revision 记打开时的修订，列表刷新后修订变了就作废。
type Draft = { id: string; revision: number; name: string; schema: ContentSchema };

function objectSchema(schema: ContentSchema | undefined): ContentSchema {
  return { root: "object", fields: schema?.fields ?? [] };
}

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
  if ((parsed as ContentSchema).root !== "object") {
    throw new Error("工艺模版的 root 须是 object");
  }
  return canonicalizeSchema(parsed as ContentSchema);
}

function isSeed(id: string) {
  return id === SEED_TPL_WELD || id === SEED_TPL_CORNER;
}

function seedRank(id: string) {
  return id === SEED_TPL_WELD ? 0 : id === SEED_TPL_CORNER ? 1 : 2;
}

// 预置的焊接参数、包角排最前，其余按名称。
function sortTemplates(rows: ContentTemplate[]): ContentTemplate[] {
  return [...rows].sort((a, b) => seedRank(a.id) - seedRank(b.id) || (a.name ?? "").localeCompare(b.name ?? "", "zh"));
}

// 工艺模版页：左边模版清单，中间在页面里直接改字段表，右边实时预览新建这种工艺时的表单。
export function TemplatesPage() {
  const { message, modal } = App.useApp();
  const q = useProcessTemplates();
  const create = useCreateProcessTemplate();
  const update = useUpdateProcessTemplate();
  const remove = useDeleteProcessTemplate();
  const confirmDiscard = useConfirmDiscard();
  const [selId, setSelId] = useState<string | null>(null);
  const [draft, setDraft] = useState<Draft | null>(null);
  const [view, setView] = useState<View>("form");
  const [jsonText, setJsonText] = useState("");
  const [jsonErr, setJsonErr] = useState("");
  const [importOpen, setImportOpen] = useState(false);
  const [importText, setImportText] = useState("");
  const [exportOpen, setExportOpen] = useState(false);

  const rows = useMemo(() => sortTemplates(q.data ?? []), [q.data]);
  const busy = create.isPending || update.isPending || remove.isPending;
  const missingSeeds = q.isLoading || q.isError ? [] : PROCESS_SEEDS.filter((s) => !rows.some((r) => r.id === s.id));

  // 没选过就默认第一份；选中的那份被删了也退回第一份。新建草稿只要还在就保持选中。
  const firstId = rows[0]?.id ?? null;
  const curId = selId === NEW_ID ? (draft?.id === NEW_ID ? NEW_ID : firstId) : selId && rows.some((r) => r.id === selId) ? selId : firstId;
  const row = curId && curId !== NEW_ID ? (rows.find((r) => r.id === curId) ?? null) : null;
  // 草稿只对打开时的那一修订有效：别处保存过、列表刷新后就以服务端为准。
  const liveDraft = draft && draft.id === curId && (curId === NEW_ID || draft.revision === row?.revision) ? draft : null;
  const name = liveDraft?.name ?? row?.name ?? "";
  const schema = liveDraft?.schema ?? (row ? objectSchema(row.schema) : null);
  const dirty = liveDraft !== null;

  const edit = (patch: Partial<Pick<Draft, "name" | "schema">>) => {
    if (!curId || !schema) return;
    setDraft({ id: curId, revision: row?.revision ?? 0, name, schema, ...patch });
  };

  const resetView = () => {
    setView("form");
    setJsonText("");
    setJsonErr("");
  };

  // 换一份或新建前，有没保存的改动先问一句。
  const select = (id: string) => {
    if (id === curId) return;
    confirmDiscard(dirty, () => {
      setDraft(null);
      setSelId(id);
      resetView();
    });
  };

  const startNew = () => {
    confirmDiscard(dirty, () => {
      setDraft({ id: NEW_ID, revision: 0, name: "", schema: { root: "object", fields: [] } });
      setSelId(NEW_ID);
      resetView();
    });
  };

  const applyJSON = (): ContentSchema | null => {
    try {
      const next = parseSchemaJSON(jsonText);
      setJsonErr("");
      return next;
    } catch (e) {
      setJsonErr(e instanceof Error ? e.message : "不是合法 JSON");
      return null;
    }
  };

  const switchView = (next: View) => {
    if (next === view || !schema) return;
    if (next === "json") {
      setJsonText(schemaJSON(schema));
      setJsonErr("");
      setView("json");
      return;
    }
    const parsed = applyJSON();
    if (!parsed) return;
    if (schemaJSON(parsed) !== schemaJSON(schema)) edit({ schema: parsed });
    setView("form");
  };

  const save = async () => {
    if (!curId || !schema) return;
    // JSON 视图里改的先收回成字段表再存。
    let body = schema;
    if (view === "json") {
      const parsed = applyJSON();
      if (!parsed) return;
      body = parsed;
    }
    const cleanName = name.trim();
    if (!cleanName) {
      message.error("请填模版名称");
      return;
    }
    if (rows.some((r) => r.id !== curId && (r.name ?? "").trim() === cleanName)) {
      message.error("名称已用");
      return;
    }
    const canon = canonicalizeSchema({ root: "object", fields: body.fields ?? [] });
    try {
      if (curId === NEW_ID) {
        const made = await create.mutateAsync({ name: cleanName, schema: canon });
        setDraft(null);
        setSelId(made.id);
        message.success("已新建");
      } else if (row) {
        await update.mutateAsync({ id: row.id, expected: row.revision, name: cleanName, schema: canon });
        setDraft(null);
        message.success("已保存");
      }
      resetView();
    } catch (e) {
      // 多半是别处改过修订；重取列表，草稿留着让人自己决定。
      void q.refetch();
      message.error(errorMessage(e));
    }
  };

  const discard = () => {
    setDraft(null);
    if (curId === NEW_ID) setSelId(null);
    resetView();
  };

  const del = () => {
    if (!row) return;
    modal.confirm({
      title: `删除「${row.name}」？`,
      content: "已有工艺正文不变，照常能用；此后不能再按这个种类新建，示教器上送的这类工艺也不再套模版。",
      okText: "删除",
      okButtonProps: { danger: true },
      onOk: async () => {
        try {
          await remove.mutateAsync(row.id);
          setDraft(null);
          setSelId(null);
          resetView();
          message.success("已删除");
        } catch (e) {
          message.error(errorMessage(e));
        }
      },
    });
  };

  // 名称和字段表留空，服务端按预置的补。
  const rebuildSeed = async (seed: { id: string; name: string }) => {
    try {
      const made = await create.mutateAsync({ id: seed.id, name: "" });
      message.success(`已补建 ${seed.name}`);
      // 手头没有未保存的改动才跳过去，免得把正在改的那份冲掉。
      if (!dirty) {
        setSelId(made.id);
        resetView();
      }
    } catch (e) {
      message.error(errorMessage(e));
    }
  };

  // 导入：贴一份工艺正文，按它的键推出字段表；已有字段的名称、单位尽量沿用。
  const applyImport = () => {
    if (!schema) return;
    try {
      const next = adoptSchema(schemaFromJSON(importText), schema);
      if (next.root !== "object") throw new Error("工艺正文的根必须是对象");
      edit({ schema: next });
      resetView();
      setImportOpen(false);
      setImportText("");
      message.success("已导入，尚未保存");
    } catch (e) {
      message.error(e instanceof Error ? e.message : "导入失败");
    }
  };

  const exportJSON = schema ? JSON.stringify(contentFromSchema(schema), null, 4) : "";
  const downloadExport = () => {
    const blob = new Blob([exportJSON], { type: "application/json" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `${name.trim() || "工艺"}.json`;
    a.click();
    URL.revokeObjectURL(url);
  };

  // 预览跟着字段表走；JSON 视图里还没收回的改动不算。字段表一变就按新默认值重画。
  const previewKey = schema ? schemaJSON(schema) : "";

  return (
    <>
      <PageHeader
        title="工艺模版"
        description="每份模版是一个工艺种类：自己的名称、字段表、修订。新建工艺时先选种类再按它的字段填。保存后下发给在线工厂，不改已有工艺正文。"
        extra={
          <Space wrap>
            <Button disabled={!schema || view === "json"} onClick={() => setImportOpen(true)}>
              导入 JSON
            </Button>
            <Button disabled={!schema} onClick={() => setExportOpen(true)}>
              导出 JSON
            </Button>
            {row ? (
              <Button danger disabled={busy} onClick={del}>
                删除
              </Button>
            ) : null}
            {dirty ? (
              <Button disabled={busy} onClick={discard}>
                放弃修改
              </Button>
            ) : null}
            <Button type="primary" loading={create.isPending || update.isPending} disabled={!schema || (!dirty && view === "form")} onClick={() => void save()}>
              保存
            </Button>
          </Space>
        }
      />
      {q.isLoading ? (
        <Spin />
      ) : q.isError ? (
        <Typography.Text type="danger">{errorMessage(q.error)}</Typography.Text>
      ) : (
        <div className="tpl-page">
          <div className="tpl-list">
            <div className="tpl-col-head">模版</div>
            <div className="tpl-list-body">
              {rows.map((r) => (
                <div key={r.id} className={`tpl-list-row${r.id === curId ? " tpl-list-row-on" : ""}`} onClick={() => select(r.id)}>
                  <div className="tpl-list-name">
                    <Typography.Text strong={r.id === curId} ellipsis>
                      {r.name}
                    </Typography.Text>
                    {r.id === curId && dirty ? <Tag color="gold">未保存</Tag> : null}
                  </div>
                  <Typography.Text type="secondary" className="tpl-list-meta">
                    {isSeed(r.id) ? "预置 · " : ""}修订 {r.revision} · {r.schema?.fields?.length ?? 0} 个字段
                  </Typography.Text>
                </div>
              ))}
              {draft?.id === NEW_ID ? (
                <div className={`tpl-list-row${curId === NEW_ID ? " tpl-list-row-on" : ""}`} onClick={() => select(NEW_ID)}>
                  <div className="tpl-list-name">
                    <Typography.Text strong={curId === NEW_ID} ellipsis>
                      {draft.name.trim() || "新模版"}
                    </Typography.Text>
                    <Tag color="gold">未保存</Tag>
                  </div>
                  <Typography.Text type="secondary" className="tpl-list-meta">
                    还没创建
                  </Typography.Text>
                </div>
              ) : null}
              {rows.length === 0 && draft?.id !== NEW_ID ? (
                <Typography.Text type="secondary" className="tpl-list-empty">
                  还没有模版。
                </Typography.Text>
              ) : null}
            </div>
            <div className="tpl-list-foot">
              <Button block icon={<PlusOutlined />} disabled={busy || draft?.id === NEW_ID} onClick={startNew}>
                新建模版
              </Button>
              {missingSeeds.map((seed) => (
                <Button key={seed.id} block disabled={busy} onClick={() => void rebuildSeed(seed)}>
                  补建 {seed.name}
                </Button>
              ))}
            </div>
          </div>
          <div className="tpl-edit">
            <div className="tpl-col-head tpl-edit-head">
              <span>字段表{row ? ` · 修订 ${row.revision}` : ""}</span>
              <Segmented
                size="small"
                value={view}
                disabled={!schema}
                onChange={(v) => switchView(v as View)}
                options={[
                  { label: "表单", value: "form" },
                  { label: "JSON", value: "json" },
                ]}
              />
            </div>
            {schema ? (
              <div className="tpl-edit-body">
                <div className="tpl-name-row">
                  <span className="tpl-name-label">名称</span>
                  <Input value={name} maxLength={80} placeholder="比如 摆动参数" onChange={(e) => edit({ name: e.target.value })} />
                </div>
                {curId === SEED_TPL_CORNER ? (
                  <Alert
                    type="warning"
                    showIcon
                    className="tpl-hint"
                    message="示教器要读 name、passes 和每层的 length、flatOffset、upwardOffset：这几项不能删、不能改键名和类型，其余可以改，也可以加字段。"
                  />
                ) : null}
                {view === "form" ? (
                  <SchemaEditor value={schema} onChange={(next) => edit({ schema: next })} />
                ) : (
                  <>
                    {jsonErr ? (
                      <Typography.Text type="danger" style={{ display: "block", marginBottom: 8 }}>
                        {jsonErr}
                      </Typography.Text>
                    ) : null}
                    <Input.TextArea
                      className="schema-json"
                      autoSize={{ minRows: 22 }}
                      value={jsonText}
                      onChange={(e) => {
                        setJsonText(e.target.value);
                        setJsonErr("");
                      }}
                    />
                  </>
                )}
              </div>
            ) : (
              <div className="tpl-edit-body">
                <Typography.Text type="secondary">左边选一份模版，或新建一份。</Typography.Text>
              </div>
            )}
          </div>
          <div className="tpl-preview">
            <div className="tpl-col-head">新建这种工艺时看到的表单</div>
            <div className="tpl-preview-body">
              {schema ? (
                <>
                  <Preview key={previewKey} schema={schema} />
                  <Typography.Text type="secondary" className="tpl-preview-note">
                    {view === "json" ? "切回「表单」后预览才会跟上 JSON 里的改动。" : "这里可以随便填，只是看效果，不会保存。"}
                  </Typography.Text>
                </>
              ) : (
                <Typography.Text type="secondary">没有可预览的模版。</Typography.Text>
              )}
            </div>
          </div>
        </div>
      )}
      <Modal title="导入 JSON" open={importOpen} onCancel={() => setImportOpen(false)} okText="导入" onOk={applyImport} destroyOnHidden width={720}>
        <Typography.Text type="secondary" style={{ display: "block", marginBottom: 8 }}>
          贴一份这种工艺的正文，和设备里的 JSON 一样；按它的键生成字段表，导入后还要点保存。
        </Typography.Text>
        <Upload
          accept="application/json,.json"
          showUploadList={false}
          beforeUpload={(file) => {
            void file
              .text()
              .then((text) => setImportText(text))
              .catch(() => message.error("读文件失败"));
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
                void copyText(exportJSON).then((ok) => {
                  if (ok) message.success("已复制");
                  else message.error("复制失败，请手动选中上面的内容复制，或点「下载」");
                });
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

// Preview 用资产页新建工艺的同一个表单组件渲染，看到的就是实际效果；填的值只留在这里。
function Preview({ schema }: { schema: ContentSchema }) {
  const [value, setValue] = useState<unknown>(() => defaultValue(schema));
  if (!(schema.fields ?? []).length) {
    return <Typography.Text type="secondary">还没有字段，加了字段这里就会出现对应的输入框。</Typography.Text>;
  }
  return <ContentFields schema={schema} value={value} onChange={setValue} />;
}
