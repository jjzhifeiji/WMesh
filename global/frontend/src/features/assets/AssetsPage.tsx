import { FolderOutlined, PlusOutlined } from "@ant-design/icons";
import { App, Button, Card, Descriptions, Empty, Form, Input, Modal, Select, Space, Switch, Tag, Tooltip, Typography } from "antd";
import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { ApiError, errorMessage } from "@/shared/api/client";
import { formatTime } from "@/shared/format";
import { IdText } from "@/shared/ui/IdText";
import { PageHeader } from "@/shared/ui/PageHeader";
import { useDirectory } from "@/features/factories/api";
import { ContentEditor } from "@/features/templates/ContentFields";
import { badContentJSON, collectProcessIds, defaultValue, projectContentSchema } from "@/features/templates/schema";
import { useProjectTemplates, useTemplate } from "@/features/templates/api";
import { projectTemplatesForWeld, sameWeldKind, weldKindLabel, WELD_KINDS, WELD_SINGLE } from "@/features/templates/projectKinds";
import type { ContentSchema } from "@/features/templates/schema";
import {
  useAssetContent,
  useAssets,
  useCreateAsset,
  useCopyAsset,
  useDisableAsset,
  useEnableAsset,
  useDeleteAsset,
  usePromoteFromFactory,
  usePublishAsset,
  useRenameAsset,
  useCreateFSFolder,
  useFS,
  useSetAssetCopyable,
  useSetAssetWeldKind,
  useSetAssetDeps,
  useUpdateAssetContent,
  assetKeys,
  type Asset,
  type AssetDep,
  type AssetKind,
  type CreateAssetInput,
  type FSNode,
} from "./api";
import { fsFolderOptions, ProcessExplorer } from "./ProcessExplorer";
import { PromotePicker } from "./PromotePicker";
import { emptyWeld, ProjectWeldEditor, type ProcessOption } from "./ProjectWeldEditor";

type CreateForm = { name: string; content: string; weldKind: string; copyable: boolean; parentId?: string };
type MkdirForm = { parentId: string; name: string };

// EditBase 编辑弹窗的基准：打开时记下这一行，正文首次新取成功后并入；保存与比较都以它为准，每成功一步就推进。
type EditBase = {
  id: string; // 资产身份
  revision: number; // 下一步提交用的期望修订
  name: string; // 已生效的名称
  weldKind: string; // 已生效的类型
  deps: AssetDep[]; // 已生效的依赖
  content: string | null; // 已生效的正文；null 为还没新取到
};

function baseOf(row: Asset, content: string | null): EditBase {
  return { id: row.id, revision: row.revision, name: row.name, weldKind: row.weldKind || WELD_SINGLE, deps: row.deps ?? [], content };
}

// editErrText 修订对不上多半是别处在弹窗打开后写过，提示关掉重开。
function editErrText(e: unknown) {
  if (isRevisionConflict(e)) return "这条在弹窗打开后已被改过，请关掉重开再改";
  return errorMessage(e);
}

function isRevisionConflict(e: unknown) {
  return e instanceof ApiError && e.code === "revision does not match";
}

// editFailText 分步保存中途失败：写明哪几步已生效、哪一步没成。
function editFailText(done: string[], step: string, e: unknown) {
  if (!done.length) return editErrText(e);
  return `${done.join("、")}已生效；${step}保存失败：${editErrText(e)}`;
}

// jsonRule 有字段表时挡住语法错的 JSON；和原正文相同的旧正文不拦。
function jsonRule(schema: ContentSchema | null, original: string) {
  return {
    validator: (_: unknown, v: string | undefined) =>
      badContentJSON(v, schema) && v !== original ? Promise.reject(new Error("JSON 格式不对，不能保存")) : Promise.resolve(),
  };
}

function statusLabel(status: string) {
  if (status === "draft") return "未发布";
  if (status === "available") return "可用";
  if (status === "disabled") return "已停用";
  return status;
}

function processListRow(n: FSNode) {
  if (n.nodeKind === "folder") {
    return <Typography.Text>{n.name}</Typography.Text>;
  }
  return (
    <span>
      <Typography.Text strong>{n.asset?.name || n.name}</Typography.Text>
      {n.asset ? (
        <Typography.Text type="secondary" style={{ marginLeft: 8 }}>
          {n.asset.code} · {statusLabel(n.asset.status)} · {weldKindLabel(n.asset.weldKind || WELD_SINGLE)}
        </Typography.Text>
      ) : null}
    </span>
  );
}

function statusColor(status: string) {
  if (status === "draft") return "gold";
  if (status === "available") return "green";
  return "default";
}

function canCopy(row: Asset) {
  return row.status !== "disabled";
}

function CopyableSwitch({
  checked,
  disabled,
  loading,
  onToggle,
}: {
  checked: boolean;
  disabled?: boolean;
  loading?: boolean;
  onToggle: (next: boolean) => void;
}) {
  return (
    <Switch
      checked={checked}
      disabled={disabled}
      loading={loading}
      checkedChildren="可复制"
      unCheckedChildren="不可复制"
      onChange={onToggle}
    />
  );
}

// 平台级工艺或工程：只在 WAN 维护；不能改厂库原件，升档从在线工厂自动拉。
export function AssetsPage({ kind }: { kind: AssetKind }) {
  return <AssetsBody key={kind} kind={kind} />;
}

function AssetsBody({ kind }: { kind: AssetKind }) {
  const isProcess = kind === "process";
  const title = isProcess ? "平台工艺" : "平台工程";
  const assets = useAssets(kind);
  const processes = useAssets("process");
  const processTpl = useTemplate("process");
  const projectTpls = useProjectTemplates();
  const schema = isProcess ? (processTpl.data?.schema ?? null) : projectTpls.data?.length ? projectContentSchema(projectTpls.data) : null;
  const create = useCreateAsset();
  const mkdir = useCreateFSFolder();
  const fs = useFS(kind);
  const copy = useCopyAsset();
  const rename = useRenameAsset();
  const updateContent = useUpdateAssetContent();
  const publish = usePublishAsset();
  const disable = useDisableAsset();
  const enable = useEnableAsset();
  const remove = useDeleteAsset();
  const setCopyable = useSetAssetCopyable();
  const setWeldKind = useSetAssetWeldKind();
  const setAssetDeps = useSetAssetDeps();
  const promote = usePromoteFromFactory();
  const directory = useDirectory();
  const qc = useQueryClient();
  const { message, modal } = App.useApp();
  const [open, setOpen] = useState(false);
  const [copyFor, setCopyFor] = useState<Asset | null>(null);
  const [promoteOpen, setPromoteOpen] = useState(false);
  const [detailFor, setDetailFor] = useState<Asset | null>(null);
  const [editFor, setEditFor] = useState<Asset | null>(null);
  const [editBase, setEditBase] = useState<EditBase | null>(null);
  const [factoryId, setFactoryId] = useState<string | null>(null);
  const [browseFactory, setBrowseFactory] = useState<string | null>(null);
  const [mkdirOpen, setMkdirOpen] = useState(false);
  const [fsQuery, setFsQuery] = useState("");
  const [fsWeld, setFsWeld] = useState("all");
  const [form] = Form.useForm<CreateForm>();
  const [mkdirForm] = Form.useForm<MkdirForm>();
  const [copyForm] = Form.useForm<{ name: string }>();
  const createWeld = Form.useWatch("weldKind", form) ?? WELD_SINGLE;
  const createSchema = useMemo(() => {
    if (isProcess) return schema;
    const rows = projectTemplatesForWeld(projectTpls.data, createWeld);
    return rows.length ? projectContentSchema(rows) : schema;
  }, [isProcess, schema, projectTpls.data, createWeld]);
  const factories = useMemo(() => (directory.data?.factories ?? []).filter((f) => (f.status ?? "active") === "active"), [directory.data]);
  const folderOpts = useMemo(() => fsFolderOptions(fs.data ?? [], (n) => n.treeLevel === "platform"), [fs.data]);
  const availableProcesses = useMemo(() => (processes.data ?? []).filter((p) => p.status === "available"), [processes.data]);
  const onErr = (e: unknown) => message.error(errorMessage(e));
  useEffect(() => {
    if (copyFor) copyForm.setFieldsValue({ name: `${copyFor.name}-副本` });
  }, [copyFor, copyForm]);
  // 打开编辑先记下这一行当基准；修订号取在读正文之前，只会比正文旧。
  const openEdit = (row: Asset) => {
    setEditFor(row);
    setEditBase(baseOf(row, null));
  };
  const closeEdit = () => {
    setEditFor(null);
    setEditBase(null);
  };
  // advanceBase 某一步写成功后把基准推进到服务端返回的新行。
  const advanceBase = (next: Asset, content?: string) =>
    setEditBase((b) => (b && b.id === next.id ? baseOf(next, content ?? b.content) : b));
  // fillBaseContent 正文首次新取成功才并入基准，之后的重取不再改它。
  const fillBaseContent = (id: string, content: string) =>
    setEditBase((b) => (b && b.id === id && b.content === null ? { ...b, content } : b));
  const finishEdit = (done: string[]) => {
    if (done.length) message.success("已保存");
    else message.info("没有改动");
    closeEdit();
  };
  // editFail 报编辑失败；修订冲突时顺手刷新列表，关掉重开就能拿到新修订。
  const editFail = (e: unknown, done: string[] = [], step = "") => {
    if (isRevisionConflict(e)) void qc.invalidateQueries({ queryKey: assetKeys.all });
    message.error(editFailText(done, step, e));
  };
  // 只在编辑弹窗里切；用基准修订号提交，成功后推进基准。
  const toggleCopyable = (row: Asset, copyable: boolean) => {
    if (row.copyable === copyable) return;
    const expected = editBase?.id === row.id ? editBase.revision : row.revision;
    setCopyable.mutate(
      { id: row.id, expected, copyable },
      {
        onSuccess: (next) => {
          advanceBase(next);
          message.success(copyable ? "已设为可复制" : "已设为不可复制");
        },
        onError: (e) => editFail(e),
      },
    );
  };

  const detailing = detailFor ? (assets.data?.find((a) => a.id === detailFor.id) ?? detailFor) : null;
  const editing = editFor ? (assets.data?.find((a) => a.id === editFor.id) ?? editFor) : null;
  const editSchema = useMemo(() => {
    if (!editing || isProcess) return schema;
    const rows = projectTemplatesForWeld(projectTpls.data, editing.weldKind || WELD_SINGLE);
    return rows.length ? projectContentSchema(rows) : schema;
  }, [editing, isProcess, schema, projectTpls.data]);
  const detailSchema = useMemo(() => {
    if (!detailing || isProcess) return schema;
    const rows = projectTemplatesForWeld(projectTpls.data, detailing.weldKind || WELD_SINGLE);
    return rows.length ? projectContentSchema(rows) : schema;
  }, [detailing, isProcess, schema, projectTpls.data]);

  const openCreate = (parentId?: string) => {
    form.setFieldsValue({
      content: isProcess && schema ? JSON.stringify(defaultValue(schema)) : "[]",
      weldKind: WELD_SINGLE,
      copyable: false,
      parentId: parentId ?? folderOpts[0]?.value,
    });
    setOpen(true);
  };
  const openMkdir = (parentId?: string) => {
    mkdirForm.setFieldsValue({ parentId: parentId ?? folderOpts[0]?.value, name: "新建文件夹" });
    setMkdirOpen(true);
  };
  const openPromote = () => {
    const online = factories.find((f) => f.channelOnline);
    setFactoryId(online?.id ?? null);
    setPromoteOpen(true);
  };
  const closePromote = () => {
    setPromoteOpen(false);
    setFactoryId(null);
  };
  const pickFactory = (id: string) => {
    setFactoryId(id);
  };

  return (
    <>
      <PageHeader
        title={title}
        description={isProcess ? "只做平台级。厂级原件仍在各厂；正文没变则跳过升档，有变更则覆盖。" : "新建只填名称和类型，再按 App 那样加焊道、绑工艺；点列到平板上采集。"}
        extra={
          <Space wrap>
            {isProcess ? (
              <>
                <Input.Search
                  allowClear
                  placeholder="搜索工艺名称、编号"
                  value={fsQuery}
                  onChange={(e) => setFsQuery(e.target.value)}
                  style={{ width: 240 }}
                />
                <Select
                  value={fsWeld}
                  onChange={setFsWeld}
                  style={{ width: 128 }}
                  options={[{ value: "all", label: "全部类型" }, ...WELD_KINDS.map((k) => ({ value: k.value, label: k.label }))]}
                />
              </>
            ) : (
              <>
                <Input.Search
                  allowClear
                  placeholder="搜索工程名称、编号"
                  value={fsQuery}
                  onChange={(e) => setFsQuery(e.target.value)}
                  style={{ width: 240 }}
                />
                <Select
                  value={fsWeld}
                  onChange={setFsWeld}
                  style={{ width: 128 }}
                  options={[{ value: "all", label: "全部类型" }, ...WELD_KINDS.map((k) => ({ value: k.value, label: k.label }))]}
                />
              </>
            )}
            {!browseFactory ? (
              <Button icon={<FolderOutlined />} onClick={() => openMkdir()}>
                新建文件夹
              </Button>
            ) : null}
            <Button onClick={openPromote}>从工厂升档</Button>
            <Button type="primary" icon={<PlusOutlined />} onClick={() => openCreate()}>
              新建{isProcess ? "工艺" : "工程"}
            </Button>
          </Space>
        }
      />
      {isProcess ? (
        <Card styles={{ body: { padding: 0 } }}>
          <div style={{ padding: 12, borderBottom: "1px solid #f0f0f0" }}>
            <Select
              value={browseFactory ?? ""}
              onChange={(v) => {
                setBrowseFactory(v || null);
                setDetailFor(null);
              }}
              style={{ width: 280 }}
              options={[
                { value: "", label: "本平台目录" },
                ...factories.map((f) => ({
                  value: f.id,
                  label: `看厂目录：${f.name}${f.channelOnline ? " · 在线" : " · 离线"}`,
                  disabled: !f.channelOnline,
                })),
              ]}
            />
          </div>
          <ProcessExplorer
            key={browseFactory ?? "platform"}
            factoryId={browseFactory}
            readOnly={Boolean(browseFactory)}
            writable={() => !browseFactory}
            selectedAssetId={detailFor?.id ?? editFor?.id ?? null}
            query={fsQuery}
            weldFilter={fsWeld}
            onNewProcess={(folder) => openCreate(folder.id)}
            onSelectFile={(n) => {
              if (!browseFactory) setDetailFor(n?.asset ?? null);
            }}
            listRow={processListRow}
            detail={
              browseFactory ? (
                <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="云端只看各厂目录结构，不打开厂级/个人正文。" />
              ) : detailing ? (
                <FileDetail row={detailing} schema={schema}>
                  <Space wrap>
                    <Button size="small" type="primary" onClick={() => openEdit(detailing)}>
                      编辑
                    </Button>
                    {canCopy(detailing) ? (
                      <Button size="small" onClick={() => setCopyFor(detailing)}>
                        复制
                      </Button>
                    ) : null}
                    {detailing.status === "draft" ? (
                      <Button
                        size="small"
                        onClick={() =>
                          modal.confirm({
                            title: `发布「${detailing.name}」？`,
                            content: "发布后可被平台级工程依赖，并下到在线工厂。可复制仍可改。",
                            onOk: () =>
                              publish.mutate(
                                { id: detailing.id, expected: detailing.revision },
                                { onSuccess: () => message.success("已发布"), onError: onErr },
                              ),
                          })
                        }
                      >
                        发布
                      </Button>
                    ) : null}
                    {detailing.status === "available" ? (
                      <Button
                        size="small"
                        danger
                        onClick={() =>
                          modal.confirm({
                            title: `停用「${detailing.name}」？`,
                            content: "停用后不能改、不能升档、不能被新工程依赖；可以再启用。",
                            okButtonProps: { danger: true },
                            onOk: () =>
                              disable.mutate(
                                { id: detailing.id, expected: detailing.revision },
                                { onSuccess: () => message.success("已停用"), onError: onErr },
                              ),
                          })
                        }
                      >
                        停用
                      </Button>
                    ) : null}
                    {detailing.status === "disabled" ? (
                      <Button
                        size="small"
                        onClick={() =>
                          enable.mutate(
                            { id: detailing.id, expected: detailing.revision },
                            { onSuccess: () => message.success("已启用"), onError: onErr },
                          )
                        }
                      >
                        启用
                      </Button>
                    ) : null}
                    <Button
                      size="small"
                      danger
                      onClick={() =>
                        modal.confirm({
                          title: `删除「${detailing.name}」？`,
                          content: "删除后不能恢复。若已被工程依赖会拒绝。",
                          okButtonProps: { danger: true },
                          onOk: () =>
                            remove.mutate(detailing.id, {
                              onSuccess: () => {
                                message.success("已删除");
                                setDetailFor(null);
                              },
                              onError: onErr,
                            }),
                        })
                      }
                    >
                      删除
                    </Button>
                  </Space>
                </FileDetail>
              ) : (
                <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="点一份工艺看详情" />
              )
            }
          />
        </Card>
      ) : (
        <Card styles={{ body: { padding: 0 } }}>
          <div style={{ padding: 12, borderBottom: "1px solid #f0f0f0" }}>
            <Select
              value={browseFactory ?? ""}
              onChange={(v) => {
                setBrowseFactory(v || null);
                setDetailFor(null);
              }}
              style={{ width: 280 }}
              options={[
                { value: "", label: "本平台目录" },
                ...factories.map((f) => ({
                  value: f.id,
                  label: `看厂目录：${f.name}${f.channelOnline ? " · 在线" : " · 离线"}`,
                  disabled: !f.channelOnline,
                })),
              ]}
            />
          </div>
          <ProcessExplorer
            key={browseFactory ?? "platform"}
            kind="project"
            noun="工程"
            factoryId={browseFactory}
            readOnly={Boolean(browseFactory)}
            writable={() => !browseFactory}
            selectedAssetId={detailFor?.id ?? editFor?.id ?? null}
            query={fsQuery}
            weldFilter={fsWeld}
            onNewProcess={(folder) => openCreate(folder.id)}
            onSelectFile={(n) => {
              if (!browseFactory) setDetailFor(n?.asset ?? null);
            }}
            listRow={processListRow}
            detail={
              browseFactory ? (
                <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="云端只看各厂目录结构，不打开厂级/个人正文。" />
              ) : detailing ? (
                <FileDetail row={detailing} schema={detailSchema} processes={processes.data ?? []} templates={projectTpls.data ?? []}>
                  <Space wrap>
                    <Button size="small" type="primary" onClick={() => openEdit(detailing)}>
                      编辑
                    </Button>
                    {canCopy(detailing) ? (
                      <Button size="small" onClick={() => setCopyFor(detailing)}>
                        复制
                      </Button>
                    ) : null}
                    {detailing.status === "draft" ? (
                      <Button
                        size="small"
                        onClick={() =>
                          modal.confirm({
                            title: `发布「${detailing.name}」？`,
                            content: "发布后可被平台级工程依赖，并下到在线工厂。可复制仍可改。",
                            onOk: () =>
                              publish.mutate(
                                { id: detailing.id, expected: detailing.revision },
                                { onSuccess: () => message.success("已发布"), onError: onErr },
                              ),
                          })
                        }
                      >
                        发布
                      </Button>
                    ) : null}
                    {detailing.status === "available" ? (
                      <Button
                        size="small"
                        danger
                        onClick={() =>
                          modal.confirm({
                            title: `停用「${detailing.name}」？`,
                            content: "停用后不能改、不能升档、不能被新工程依赖；可以再启用。",
                            okButtonProps: { danger: true },
                            onOk: () =>
                              disable.mutate(
                                { id: detailing.id, expected: detailing.revision },
                                { onSuccess: () => message.success("已停用"), onError: onErr },
                              ),
                          })
                        }
                      >
                        停用
                      </Button>
                    ) : null}
                    {detailing.status === "disabled" ? (
                      <Button
                        size="small"
                        onClick={() =>
                          enable.mutate(
                            { id: detailing.id, expected: detailing.revision },
                            { onSuccess: () => message.success("已启用"), onError: onErr },
                          )
                        }
                      >
                        启用
                      </Button>
                    ) : null}
                    <Button
                      size="small"
                      danger
                      onClick={() =>
                        modal.confirm({
                          title: `删除「${detailing.name}」？`,
                          content: "删除后不能恢复。",
                          okButtonProps: { danger: true },
                          onOk: () =>
                            remove.mutate(detailing.id, {
                              onSuccess: () => {
                                message.success("已删除");
                                setDetailFor(null);
                              },
                              onError: onErr,
                            }),
                        })
                      }
                    >
                      删除
                    </Button>
                  </Space>
                </FileDetail>
              ) : (
                <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="点一份工程看详情" />
              )
            }
          />
        </Card>
      )}
      <Modal title={`新建${isProcess ? "工艺" : "工程"}`} open={open} onCancel={() => setOpen(false)} okText="创建" confirmLoading={create.isPending} destroyOnHidden width={isProcess ? 1100 : 560} styles={{ body: { maxHeight: isProcess ? "82vh" : "50vh", overflow: "auto" } }} onOk={() => form.submit()}>
        <Form<CreateForm>
          className={isProcess ? "project-edit-form" : undefined}
          form={form}
          layout="vertical"
          size="small"
          requiredMark={false}
          initialValues={{ content: "", weldKind: WELD_SINGLE, copyable: false }}
          onFinish={(values) => {
            const weld = values.weldKind || WELD_SINGLE;
            const content = isProcess ? values.content : JSON.stringify([emptyWeld(weld, projectTpls.data ?? [], 0)]);
            const input: CreateAssetInput = {
              kind,
              name: values.name,
              content,
              weldKind: weld,
              copyable: isProcess ? values.copyable : undefined,
              parentId: values.parentId,
            };
            create.mutate(input, {
              onSuccess: (row) => {
                message.success("已创建草稿");
                form.resetFields();
                setOpen(false);
                if (!isProcess) openEdit(row);
              },
              onError: onErr,
            });
          }}
        >
          {isProcess ? (
            <Form.Item name="parentId" label="文件夹" extra="工艺会放到这个目录下。" rules={[{ required: true, message: "请选择文件夹" }]}>
              <Select showSearch optionFilterProp="label" options={folderOpts} placeholder="选择文件夹" />
            </Form.Item>
          ) : (
            <Form.Item name="parentId" label="文件夹" extra="工程会放到这个目录下。" rules={[{ required: true, message: "请选择文件夹" }]}>
              <Select showSearch optionFilterProp="label" options={folderOpts} placeholder="选择文件夹" />
            </Form.Item>
          )}
          <div className="project-edit-meta">
            <Form.Item
              name="name"
              label={<Tooltip title="显示名不是身份。">{isProcess ? "显示名" : "工程名称"}</Tooltip>}
              rules={[{ required: true, message: isProcess ? "请输入显示名" : "请输入工程名称" }]}
            >
              <Input autoComplete="off" autoFocus />
            </Form.Item>
            <Form.Item name="weldKind" label={<Tooltip title="App 同类型工作流才能打开。">类型</Tooltip>} rules={[{ required: true, message: "请选择类型" }]}>
              <Select options={WELD_KINDS.map((k) => ({ value: k.value, label: k.label }))} />
            </Form.Item>
            {isProcess ? (
              <Form.Item name="copyable" label="可复制" extra="否则厂端看不到正文。默认否。" valuePropName="checked">
                <Switch size="default" checkedChildren="可复制" unCheckedChildren="不可复制" />
              </Form.Item>
            ) : null}
          </div>
          {isProcess ? (
            <Form.Item name="content" label="参数" rules={[jsonRule(createSchema, "")]}>
              <ContentEditor schema={createSchema} weldKind={createWeld} />
            </Form.Item>
          ) : (
            <Typography.Text type="secondary">创建后按 App 那样加焊道、选工艺。点列到平板上采集。</Typography.Text>
          )}
        </Form>
      </Modal>
      <Modal
        title="新建文件夹"
        open={mkdirOpen}
        onCancel={() => setMkdirOpen(false)}
        okText="创建"
        confirmLoading={mkdir.isPending}
        destroyOnHidden
        onOk={() => mkdirForm.submit()}
      >
        <Form<MkdirForm>
          form={mkdirForm}
          layout="vertical"
          requiredMark={false}
          onFinish={(values) => {
            mkdir.mutate(
              { parentId: values.parentId, name: values.name.trim() },
              {
                onSuccess: () => {
                  message.success("已创建");
                  mkdirForm.resetFields();
                  setMkdirOpen(false);
                },
                onError: onErr,
              },
            );
          }}
        >
          <Form.Item name="parentId" label="位置" rules={[{ required: true, message: "请选择文件夹" }]}>
            <Select showSearch optionFilterProp="label" options={folderOpts} placeholder="选择上级文件夹" />
          </Form.Item>
          <Form.Item name="name" label="名称" rules={[{ required: true, message: "请输入名称" }]}>
            <Input autoComplete="off" autoFocus />
          </Form.Item>
        </Form>
      </Modal>
      <Modal
        title={`复制${isProcess ? "工艺" : "工程"}`}
        open={copyFor != null}
        onCancel={() => setCopyFor(null)}
        okText="确定"
        confirmLoading={copy.isPending}
        destroyOnHidden
        onOk={() => copyForm.submit()}
      >
        <Form
          form={copyForm}
          layout="vertical"
          requiredMark={false}
          onFinish={(values) => {
            if (!copyFor) return;
            copy.mutate(
              { id: copyFor.id, name: values.name.trim() },
              {
                onSuccess: () => {
                  message.success("已创建草稿");
                  copyForm.resetFields();
                  setCopyFor(null);
                },
                onError: onErr,
              },
            );
          }}
        >
          <Form.Item name="name" label={`新${isProcess ? "工艺" : "工程"}名称`} extra="另存为新草稿，原件不动；之后同新建。" rules={[{ required: true, message: `请输入新${isProcess ? "工艺" : "工程"}名称` }]}>
            <Input autoComplete="off" autoFocus />
          </Form.Item>
        </Form>
      </Modal>
      <Modal title="从工厂升档" open={promoteOpen} onCancel={closePromote} footer={null} width={960} destroyOnHidden>
        <Typography.Paragraph type="secondary">
          按该厂文件夹逐级看{isProcess ? "工艺" : "工程"}：厂级、个人级、已下发的平台级，不分状态。正文没变则跳过；有变更则覆盖已升档的那条，不另开身份。
        </Typography.Paragraph>
        <PromotePicker
          key={factoryId ?? "none"}
          kind={kind}
          noun={isProcess ? "工艺" : "工程"}
          factoryId={factoryId}
          factories={factories}
          platformAssets={assets.data ?? []}
          promoting={promote.isPending}
          onPickFactory={pickFactory}
          onPromote={(assetId, outcome) => {
            if (!factoryId) return;
            promote.mutate(
              { factoryId, assetId },
              {
                onSuccess: () => {
                  if (outcome === "same") message.success("正文未变，未重复升档");
                  else if (outcome === "cover") message.success("已覆盖，未发布");
                  else message.success("已升档，未发布");
                  closePromote();
                },
                onError: onErr,
              },
            );
          }}
        />
      </Modal>
      <EditModal
        row={editing?.kind === "process" ? editing : null}
        base={editing?.kind === "process" ? editBase : null}
        schema={schema}
        saving={rename.isPending || updateContent.isPending || setWeldKind.isPending}
        onBaseContent={fillBaseContent}
        onClose={closeEdit}
        onSave={async (name, content, weldKind) => {
          // 一律和基准比、用基准修订号提交；content 为 null 表示正文没读到，只改名。
          const base = editBase;
          if (!base || editing?.id !== base.id) return;
          const done: string[] = [];
          let step = "改名";
          try {
            let expected = base.revision;
            if (name !== base.name) {
              const next = await rename.mutateAsync({ id: base.id, expected, name });
              expected = next.revision;
              advanceBase(next);
              done.push("改名");
            }
            if (content !== null && weldKind && weldKind !== base.weldKind) {
              step = "类型";
              const next = await setWeldKind.mutateAsync({ id: base.id, expected, weldKind });
              expected = next.revision;
              advanceBase(next);
              done.push("类型");
            }
            if (content !== null && content !== base.content) {
              step = "参数";
              const next = await updateContent.mutateAsync({ id: base.id, expected, content });
              advanceBase(next, content);
              done.push("参数");
            }
            finishEdit(done);
          } catch (e) {
            editFail(e, done, step);
          }
        }}
        extra={
          editing?.kind === "process" ? (
            <Space wrap>
              <CopyableSwitch
                checked={editing.copyable}
                disabled={editing.status === "disabled"}
                loading={setCopyable.isPending && setCopyable.variables?.id === editing.id}
                onToggle={(next) => toggleCopyable(editing, next)}
              />
            </Space>
          ) : null
        }
      />
      <ProjectEditModal
        row={editing?.kind === "project" ? editing : null}
        base={editing?.kind === "project" ? editBase : null}
        templates={projectTpls.data ?? []}
        processes={availableProcesses}
        saving={rename.isPending || updateContent.isPending || setAssetDeps.isPending || setWeldKind.isPending}
        onBaseContent={fillBaseContent}
        onClose={closeEdit}
        onSave={async (name, content, weldKind) => {
          // 一律和基准比、用基准修订号提交；content 为 null 表示正文没读到，只改名，不碰类型、依赖和焊道。
          const base = editBase;
          if (!base || editing?.id !== base.id) return;
          const done: string[] = [];
          let step = "改名";
          try {
            let expected = base.revision;
            if (name !== base.name) {
              const next = await rename.mutateAsync({ id: base.id, expected, name });
              expected = next.revision;
              advanceBase(next);
              done.push("改名");
            }
            if (content === null) {
              finishEdit(done);
              return;
            }
            const nextKind = weldKind || base.weldKind;
            let depIds = base.deps.map((d) => d.id);
            if (nextKind !== base.weldKind) {
              step = "类型";
              const next = await setWeldKind.mutateAsync({ id: base.id, expected, weldKind: nextKind });
              expected = next.revision;
              depIds = (next.deps ?? []).map((d) => d.id);
              advanceBase(next);
              done.push("类型");
            }
            const nextIds = uniqueIds(
              processIdsFromContent(content, (() => {
                const rows = projectTemplatesForWeld(projectTpls.data, nextKind);
                return rows.length ? projectContentSchema(rows) : editSchema;
              })()),
            );
            const added = nextIds.filter((id) => !depIds.includes(id));
            const toDep = (id: string): AssetDep => {
              const p = availableProcesses.find((x) => x.id === id && x.status === "available");
              if (p) return { id: p.id, revision: p.revision, digest: p.digest };
              const pinned = base.deps.find((d) => d.id === id);
              if (pinned) return pinned;
              return { id, revision: 0, digest: "" };
            };
            if (added.length) {
              step = "依赖";
              const next = await setAssetDeps.mutateAsync({
                id: base.id,
                expected,
                deps: [...depIds, ...added].map(toDep),
              });
              expected = next.revision;
              advanceBase(next);
              done.push("依赖");
            }
            if (content !== base.content) {
              step = "焊道";
              const next = await updateContent.mutateAsync({ id: base.id, expected, content });
              expected = next.revision;
              advanceBase(next, content);
              done.push("焊道");
            }
            if (nextIds.join("\0") !== depIds.join("\0") || added.length) {
              step = "依赖";
              const next = await setAssetDeps.mutateAsync({ id: base.id, expected, deps: nextIds.map(toDep) });
              advanceBase(next);
              if (!done.includes("依赖")) done.push("依赖");
            }
            finishEdit(done);
          } catch (e) {
            editFail(e, done, step);
          }
        }}
      />
    </>
  );
}

function depText(row: Asset, processes: Asset[]) {
  if (!row.deps?.length) return "—";
  return row.deps.map((d) => processes.find((p) => p.id === d.id)?.name || "未知工艺").join("、");
}

function ParamsView({
  row,
  schema,
  processOptions,
  processes = [],
  templates = [],
}: {
  row: Asset;
  schema: ContentSchema | null;
  processOptions?: { value: string; label: string; disabled?: boolean }[];
  processes?: Asset[];
  templates?: { id: string; name?: string; schema?: ContentSchema }[];
}) {
  const q = useAssetContent(row.id);
  if (q.isError) return <Typography.Text type="danger">{errorMessage(q.error)}</Typography.Text>;
  if (q.isLoading) return <Typography.Text type="secondary">读取中…</Typography.Text>;
  if (row.kind === "project") {
    return (
      <ProjectWeldEditor
        value={q.data?.content ?? ""}
        disabled
        weldKind={row.weldKind || WELD_SINGLE}
        templates={templates}
        processOptions={processOptions ?? processPickerOptions(processes, (p) => p.status === "available" && sameWeldKind(p.weldKind, row.weldKind), (row.deps ?? []).map((d) => d.id))}
      />
    );
  }
  return (
    <ContentEditor
      schema={schema}
      value={q.data?.content ?? ""}
      disabled
      weldKind={row.weldKind || WELD_SINGLE}
      processOptions={processOptions}
    />
  );
}

function FileDetail({
  row,
  schema,
  processes = [],
  templates = [],
  children,
}: {
  row: Asset;
  schema: ContentSchema | null;
  processes?: Asset[];
  templates?: { id: string; name?: string; schema?: ContentSchema }[];
  children?: ReactNode;
}) {
  return (
    <div className="fs-detail-card">
      <div className="fs-detail-top">
        <Typography.Title level={5} style={{ margin: 0 }} ellipsis>
          {row.name}
        </Typography.Title>
        {children ? <div className="fs-detail-actions">{children}</div> : null}
      </div>
      <Descriptions size="small" column={2} bordered className="fs-detail-meta">
        <Descriptions.Item label="编号" span={2}>
          <Typography.Text copyable={{ text: row.code }}>{row.code}</Typography.Text>
        </Descriptions.Item>
        {row.kind === "process" ? (
          <Descriptions.Item label="工艺ID" span={2}>
            <IdText id={row.id} />
          </Descriptions.Item>
        ) : (
          <Descriptions.Item label="依赖工艺" span={2}>
            {depText(row, processes)}
          </Descriptions.Item>
        )}
        <Descriptions.Item label="状态">
          <Tag color={statusColor(row.status)}>{statusLabel(row.status)}</Tag>
        </Descriptions.Item>
        <Descriptions.Item label="类型">{weldKindLabel(row.weldKind || WELD_SINGLE)}</Descriptions.Item>
        {row.kind === "process" ? <Descriptions.Item label="可复制">{row.copyable ? "是" : "否"}</Descriptions.Item> : null}
        <Descriptions.Item label="修订">{row.revision}</Descriptions.Item>
        <Descriptions.Item label="来源厂" span={2}>
          {row.sourceFactory || "本端新建"}
        </Descriptions.Item>
        <Descriptions.Item label="创建时间">{formatTime(row.createdAt)}</Descriptions.Item>
        <Descriptions.Item label="更新">{formatTime(row.updatedAt)}</Descriptions.Item>
      </Descriptions>
      <div>
        <Typography.Text type="secondary" className="fs-detail-section-title">
          {row.kind === "process" ? "参数" : "焊道"}
        </Typography.Text>
        <div className="fs-detail-params">
          <ParamsView row={row} schema={schema} processes={processes} templates={templates} />
        </div>
      </div>
    </div>
  );
}

function uniqueIds(ids: string[]): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const id of ids) {
    if (!id || seen.has(id)) continue;
    seen.add(id);
    out.push(id);
  }
  return out;
}

function processIdsFromContent(content: string | undefined, schema: ContentSchema | null): string[] {
  try {
    return collectProcessIds(JSON.parse(content || "[]"), schema);
  } catch {
    return [];
  }
}

function processPickerOptions(all: Asset[], pin: (p: Asset) => boolean, selected?: string[]) {
  const seen = new Set<string>();
  const selectedSet = new Set(selected ?? []);
  const opts: ProcessOption[] = [];
  for (const p of all) {
    seen.add(p.id);
    const ok = pin(p);
    if (!ok && !selectedSet.has(p.id)) continue;
    const extra = ok ? "" : ` · ${statusLabel(p.status)}`;
    opts.push({ value: p.id, label: `${p.name} · r${p.revision}${extra}`, disabled: !ok, corner: Boolean(p.corner) });
  }
  for (const id of selected ?? []) {
    if (seen.has(id)) continue;
    opts.push({ value: id, label: id, disabled: true });
  }
  return opts;
}

type EditForm = { name: string; content: string; weldKind: string };

// useEditBody 两个编辑弹窗共用：打开时清掉上一条只填名称和类型；正文首次新取成功才写进表单并入基准，之后的失效重取都不再改表单。
function useEditBody(row: Asset | null, base: EditBase | null, onBaseContent: (id: string, content: string) => void) {
  const q = useAssetContent(row?.id ?? null, { fresh: true });
  const [form] = Form.useForm<EditForm>();
  const initFor = useRef<string | null>(null);
  const rowId = row?.id ?? null;
  useEffect(() => {
    if (!base) {
      initFor.current = null;
      return;
    }
    if (initFor.current === base.id) return;
    initFor.current = base.id;
    form.resetFields();
    form.setFieldsValue({ name: base.name, weldKind: base.weldKind });
  }, [base, form]);
  const fresh = q.isFetchedAfterMount && q.isSuccess ? q.data : undefined;
  useEffect(() => {
    if (!base || base.content !== null || rowId !== base.id || !fresh) return;
    form.setFieldsValue({ content: fresh.content });
    onBaseContent(base.id, fresh.content);
  }, [base, rowId, fresh, form, onBaseContent]);
  const ready = base !== null && rowId === base.id && base.content !== null;
  // 打开后新取失败：只能改名，不提交正文、类型和依赖。
  const failed = !ready && base !== null && q.isFetchedAfterMount && q.isError;
  return { q, form, ready, failed };
}

// ContentPending 正文区还没就绪：要么读取中，要么没读到只能改名、可点重读。
function ContentPending({ failed, error, retrying, onRetry }: { failed: boolean; error: unknown; retrying: boolean; onRetry: () => void }) {
  if (!failed) return <Typography.Text type="secondary">读取中…</Typography.Text>;
  return (
    <Space wrap>
      <Typography.Text type="warning">正文没读到，只能改名。</Typography.Text>
      <Typography.Text type="secondary">{errorMessage(error)}</Typography.Text>
      <Button size="small" loading={retrying} onClick={onRetry}>
        重读
      </Button>
    </Space>
  );
}

function EditModal({
  row,
  base,
  schema,
  saving,
  extra,
  onBaseContent,
  onClose,
  onSave,
}: {
  row: Asset | null;
  base: EditBase | null;
  schema: ContentSchema | null;
  saving: boolean;
  extra: ReactNode;
  onBaseContent: (id: string, content: string) => void;
  onClose: () => void;
  onSave: (name: string, content: string | null, weldKind: string) => Promise<void>;
}) {
  const { q, form, ready, failed } = useEditBody(row, base, onBaseContent);
  const locked = row?.status === "disabled";
  const editWeld = Form.useWatch("weldKind", form) ?? base?.weldKind ?? WELD_SINGLE;
  const pending = row !== null && !ready;
  return (
    <Modal
      title="编辑"
      open={row !== null}
      onCancel={onClose}
      okText="保存"
      okButtonProps={{ disabled: locked || (pending && !failed) }}
      confirmLoading={saving}
      destroyOnHidden
      width={1100}
      styles={{ body: { maxHeight: "82vh", overflow: "auto" } }}
      onOk={() => form.submit()}
    >
      <Form<EditForm>
        className="project-edit-form"
        form={form}
        layout="vertical"
        size="small"
        requiredMark={false}
        onFinish={(values) => {
          // 基准没建好也没确认读失败时不提交，挡住回车。
          if (!base || (!ready && !failed)) return;
          onSave(values.name, ready ? (values.content ?? "") : null, values.weldKind || base.weldKind).catch(() => undefined);
        }}
      >
        <div className="project-edit-meta">
          <Form.Item
            name="name"
            label={<Tooltip title="显示名不是身份。">显示名</Tooltip>}
            rules={[{ required: true, message: "请输入显示名" }]}
          >
            <Input autoComplete="off" disabled={locked} />
          </Form.Item>
          {row?.kind === "process" ? (
            <Form.Item name="weldKind" label="类型" rules={[{ required: true, message: "请选择类型" }]}>
              <Select disabled={locked || !ready} options={WELD_KINDS.map((k) => ({ value: k.value, label: k.label }))} />
            </Form.Item>
          ) : (
            <Form.Item label="类型">{weldKindLabel(row?.weldKind || WELD_SINGLE)}</Form.Item>
          )}
        </div>
        {row?.kind === "process" ? (
          <div className="process-edit-id">
            <Typography.Text type="secondary">工艺ID</Typography.Text>
            <IdText id={row.id} />
          </div>
        ) : null}
        {extra ? <div className="process-edit-extra">{extra}</div> : null}
        {pending ? (
          <Form.Item label="参数">
            <ContentPending failed={failed} error={q.error} retrying={q.isFetching} onRetry={() => void q.refetch()} />
          </Form.Item>
        ) : (
          <Form.Item name="content" label="参数" rules={[jsonRule(schema, base?.content ?? "")]}>
            <ContentEditor key={row?.id} schema={schema} disabled={locked} weldKind={editWeld} />
          </Form.Item>
        )}
      </Form>
    </Modal>
  );
}

function ProjectEditModal({
  row,
  base,
  templates,
  processes,
  saving,
  onBaseContent,
  onClose,
  onSave,
}: {
  row: Asset | null;
  base: EditBase | null;
  templates: { id: string; name?: string; schema?: ContentSchema }[];
  processes: Asset[];
  saving: boolean;
  onBaseContent: (id: string, content: string) => void;
  onClose: () => void;
  onSave: (name: string, content: string | null, weldKind: string) => Promise<void>;
}) {
  const { q, form, ready, failed } = useEditBody(row, base, onBaseContent);
  const locked = row?.status === "disabled";
  const editWeld = Form.useWatch("weldKind", form) ?? base?.weldKind ?? WELD_SINGLE;
  const pending = row !== null && !ready;
  return (
    <Modal
      title="编辑工程"
      open={row !== null}
      onCancel={onClose}
      okText="保存"
      okButtonProps={{ disabled: locked || (pending && !failed) }}
      confirmLoading={saving}
      destroyOnHidden
      width={1100}
      styles={{ body: { maxHeight: "82vh", overflow: "auto" } }}
      onOk={() => form.submit()}
    >
      <Form<EditForm>
        className="project-edit-form"
        form={form}
        layout="vertical"
        size="small"
        requiredMark={false}
        onValuesChange={(changed) => {
          if (changed.weldKind) form.setFieldValue("content", JSON.stringify([emptyWeld(changed.weldKind, templates, 0)]));
        }}
        onFinish={(values) => {
          // 基准没建好也没确认读失败时不提交，挡住回车。
          if (!base || (!ready && !failed)) return;
          const weld = values.weldKind || base.weldKind;
          onSave(values.name, ready ? (values.content ?? "") : null, weld).catch(() => undefined);
        }}
      >
        <div className="project-edit-meta">
          <Form.Item
            name="name"
            label={<Tooltip title="显示名不是身份。">工程名称</Tooltip>}
            rules={[{ required: true, message: "请输入工程名称" }]}
          >
            <Input autoComplete="off" disabled={locked} />
          </Form.Item>
          <Form.Item
            name="weldKind"
            label={<Tooltip title="焊道换成该类型空焊道。">类型</Tooltip>}
            rules={[{ required: true, message: "请选择类型" }]}
          >
            <Select disabled={locked || !ready} options={WELD_KINDS.map((k) => ({ value: k.value, label: k.label }))} />
          </Form.Item>
        </div>
        {pending ? (
          <Form.Item label="焊道">
            <ContentPending failed={failed} error={q.error} retrying={q.isFetching} onRetry={() => void q.refetch()} />
          </Form.Item>
        ) : (
          <Form.Item name="content" label="焊道">
            <ProjectWeldEditor
              key={row?.id}
              disabled={locked}
              weldKind={editWeld}
              templates={templates}
              processOptions={row ? processPickerOptions(processes, (p) => p.status === "available" && sameWeldKind(p.weldKind, editWeld), (row.deps ?? []).map((d) => d.id)) : undefined}
            />
          </Form.Item>
        )}
      </Form>
    </Modal>
  );
}
