import { FolderOutlined, PlusOutlined } from "@ant-design/icons";
import { App, Button, Card, Descriptions, Empty, Form, Input, Modal, Select, Space, Tag, Tooltip, Typography } from "antd";
import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { isAssetAdmin, isSuperAdmin, personName, useCatalog, type Catalog } from "@/features/catalog/api";
import { errorMessage, ApiError } from "@/shared/api/client";
import { formatTime } from "@/shared/format";
import { statusColor, statusLabel } from "@/shared/labels";
import { IdText } from "@/shared/ui/IdText";
import { useConfirmDiscard } from "@/shared/ui/confirmDiscard";
import { PageHeader } from "@/shared/ui/PageHeader";
import { ContentEditor } from "@/features/templates/ContentFields";
import { badContentJSON, collectProcessIds, defaultValue, projectContentSchema } from "@/features/templates/schema";
import { useProjectTemplates, useTemplate, syncTemplates, templateKeys } from "@/features/templates/api";
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
  usePromoteAsset,
  usePublishAsset,
  useRenameAsset,
  useCreateFSFolder,
  useFS,
  useSetAssetWeldKind,
  useSetAssetDeps,
  useUpdateAssetContent,
  syncAssets,
  assetKeys,
  type Asset,
  type AssetDep,
  type AssetKind,
  type CreateAssetInput,
  type FSNode,
} from "./api";
import { fsFolderOptions, ProcessExplorer } from "./ProcessExplorer";
import { badGapBands, emptyWeld, ProjectWeldEditor, type ProcessOption } from "./ProjectWeldEditor";

type CreateForm = {
  name: string;
  content: string;
  weldKind: string;
  parentId?: string;
};
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

// editErrText 修订对不上多半是示教器或别人在弹窗打开后写过，提示关掉重开。
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

function levelLabel(level: string) {
  if (level === "factory") return "厂级";
  if (level === "personal") return "个人级";
  if (level === "platform") return "平台级";
  return level;
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
          {n.asset.code} · {statusLabel(n.asset.status)} · {levelLabel(n.asset.level)} · {weldKindLabel(n.asset.weldKind || WELD_SINGLE)}
        </Typography.Text>
      ) : null}
    </span>
  );
}

function creatorText(row: Asset, catalog: Catalog | undefined) {
  if (row.level === "platform") return "云端";
  if (row.creatorDisplay && row.creatorLogin) return `${row.creatorDisplay}（${row.creatorLogin}）`;
  if (row.creatorDisplay) return row.creatorDisplay;
  if (row.creatorLogin) return row.creatorLogin;
  return personName(catalog, row.creatorId);
}

// canPromote 未停用的个人级可复制一份到厂级。
function canPromote(row: Asset) {
  return row.level === "personal" && row.status !== "disabled";
}

// 本厂工艺或工程：厂级归超管和管理员，个人级人人可建；别人的个人级只有超管看得到。
export function AssetsPage({ kind }: { kind: AssetKind }) {
  return <AssetsBody key={kind} kind={kind} />;
}

function AssetsBody({ kind }: { kind: AssetKind }) {
  const isProcess = kind === "process";
  const title = isProcess ? "工艺" : "工程";
  const catalog = useCatalog();
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
  const setWeldKind = useSetAssetWeldKind();
  const setAssetDeps = useSetAssetDeps();
  const publish = usePublishAsset();
  const disable = useDisableAsset();
  const enable = useEnableAsset();
  const remove = useDeleteAsset();
  const promote = usePromoteAsset();
  const qc = useQueryClient();
  const { message, modal } = App.useApp();
  const confirmDiscard = useConfirmDiscard();
  const meId = catalog.data?.me.id;
  const [fsQuery, setFsQuery] = useState("");
  const [fsWeld, setFsWeld] = useState("all");
  const [open, setOpen] = useState(false);
  const [copyFor, setCopyFor] = useState<Asset | null>(null);
  const [promoteFor, setPromoteFor] = useState<Asset | null>(null);
  const [promoteFolder, setPromoteFolder] = useState<string | undefined>(undefined);
  const [detailFor, setDetailFor] = useState<Asset | null>(null);
  const [editFor, setEditFor] = useState<Asset | null>(null);
  const [editBase, setEditBase] = useState<EditBase | null>(null);
  const [mkdirOpen, setMkdirOpen] = useState(false);
  const [form] = Form.useForm<CreateForm>();
  const [mkdirForm] = Form.useForm<MkdirForm>();
  const [copyForm] = Form.useForm<{ name: string }>();
  const createWeld = Form.useWatch("weldKind", form) ?? WELD_SINGLE;
  const createSchema = useMemo(() => {
    if (isProcess) return schema;
    const rows = projectTemplatesForWeld(projectTpls.data, createWeld);
    return rows.length ? projectContentSchema(rows) : schema;
  }, [isProcess, schema, projectTpls.data, createWeld]);

  useEffect(() => {
    let cancelled = false;
    void Promise.all([syncAssets(kind).catch(() => undefined), syncTemplates(kind).catch(() => undefined)]).then(() => {
      if (cancelled) return;
      void qc.invalidateQueries({ queryKey: assetKeys.all });
      void qc.invalidateQueries({ queryKey: templateKeys.all });
    });
    return () => {
      cancelled = true;
    };
  }, [kind, qc]);

  useEffect(() => {
    if (copyFor) copyForm.setFieldsValue({ name: `${copyFor.name}-副本` });
  }, [copyFor, copyForm]);

  const isSA = isSuperAdmin(catalog.data);
  const isAdmin = isAssetAdmin(catalog.data);
  const covers = (row: Asset) => {
    if (row.level === "platform") return false;
    if (row.level === "personal") return row.creatorId === meId || isSA;
    return isAdmin;
  };
  const canMutate = (row: Asset) => covers(row) && row.status !== "disabled";
  // 不可复制的平台级工艺是保密件，厂端不打开正文；工程不保密。
  const canRead = (row: Asset) => {
    if (row.kind === "process" && row.level === "platform" && !row.copyable) return false;
    if (row.level === "personal") return row.creatorId === meId || isSA;
    return true;
  };
  const canCopy = (row: Asset) => isProcess && row.copyable && row.status !== "disabled" && canRead(row);
  const isFactoryAdmin = isSA;
  const folderOpts = fsFolderOptions(fs.data ?? [], (n) => {
    if (n.treeLevel === "platform") return false;
    if (n.treeLevel === "factory") return isAdmin;
    return n.ownerId === meId || isSA;
  });
  const factoryFolderOpts = fsFolderOptions(fs.data ?? [], (n) => n.treeLevel === "factory");
  const openCreate = (parent?: FSNode) => {
    const folder = parent ?? (fs.data ?? []).find((n) => n.id === folderOpts[0]?.value);
    form.setFieldsValue({
      content: isProcess && schema ? JSON.stringify(defaultValue(schema)) : "[]",
      weldKind: WELD_SINGLE,
      parentId: folder?.id,
    });
    setOpen(true);
  };
  const openMkdir = (parentId?: string) => {
    mkdirForm.setFieldsValue({ parentId: parentId ?? folderOpts[0]?.value, name: "新建文件夹" });
    setMkdirOpen(true);
  };
  const onErr = (e: unknown) => message.error(errorMessage(e));
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
  // 升档先选厂级文件夹；每次都另复制一份厂级草稿，改好再发布。
  const askPromote = (row: Asset) => {
    setPromoteFolder(factoryFolderOpts[0]?.value);
    setPromoteFor(row);
  };
  const promotedCount = (row: Asset) => (assets.data ?? []).filter((a) => a.sourceId === row.id).length;
  const promoteLabel = (row: Asset) => (promotedCount(row) ? `再升一份厂级（已升 ${promotedCount(row)} 份）` : "升档为厂级");
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

  return (
    <>
      <PageHeader
        title={title}
        description={
          isProcess
            ? "本厂账号都能制作；云端不可复制的不显示参数。管理员可把未停用且可复制的个人级升为厂级，草稿也可升。"
            : "新建只填名称和类型，再按 App 那样加焊道、绑工艺；点列到平板上采集。"
        }
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
            <Button icon={<FolderOutlined />} onClick={() => openMkdir()}>
              新建文件夹
            </Button>
            <Button type="primary" icon={<PlusOutlined />} onClick={() => openCreate()}>
              新建{title}
            </Button>
          </Space>
        }
      />
      {isProcess ? (
        <Card styles={{ body: { padding: 0 } }}>
          <ProcessExplorer
            writable={(n) => {
              if (n.treeLevel === "platform") return false;
              if (n.treeLevel === "factory") return true;
              return n.ownerId === meId || isFactoryAdmin;
            }}
            canCreateFile={(n) => n.treeLevel === "factory" || (n.treeLevel === "personal" && n.ownerId === meId)}
            selectedAssetId={detailFor?.id ?? editFor?.id ?? null}
            query={fsQuery}
            weldFilter={fsWeld}
            onNewProcess={(folder) => openCreate(folder)}
            onSelectFile={(n) => setDetailFor(n?.asset ?? null)}
            listRow={processListRow}
            detail={
              detailing ? (
                <FileDetail row={detailing} catalog={catalog.data} canRead={canRead(detailing)} schema={schema}>
                  <Space wrap>
                    {canCopy(detailing) ? (
                      <Button size="small" onClick={() => setCopyFor(detailing)}>
                        复制
                      </Button>
                    ) : null}
                    {covers(detailing) ? (
                      <Button size="small" type="primary" onClick={() => openEdit(detailing)}>
                        编辑
                      </Button>
                    ) : null}
                    {canPromote(detailing) && covers(detailing) && isAdmin ? (
                      <Button size="small" loading={promote.isPending} onClick={() => askPromote(detailing)}>
                        {promoteLabel(detailing)}
                      </Button>
                    ) : null}
                    {canMutate(detailing) && detailing.status === "draft" ? (
                      <Button
                        size="small"
                        onClick={() =>
                          modal.confirm({
                            title: `发布「${detailing.name}」？`,
                            content: "发布后可被依赖。可复制仍可改。",
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
                    {canMutate(detailing) && detailing.status === "available" ? (
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
                    {covers(detailing) && detailing.status === "disabled" ? (
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
                    {covers(detailing) ? (
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
                    ) : null}
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
          <ProcessExplorer
            kind="project"
            noun="工程"
            writable={(n) => {
              if (n.treeLevel === "platform") return false;
              if (n.treeLevel === "factory") return true;
              return n.ownerId === meId || isFactoryAdmin;
            }}
            canCreateFile={(n) => n.treeLevel === "factory" || (n.treeLevel === "personal" && n.ownerId === meId)}
            selectedAssetId={detailFor?.id ?? editFor?.id ?? null}
            query={fsQuery}
            weldFilter={fsWeld}
            onNewProcess={(folder) => openCreate(folder)}
            onSelectFile={(n) => setDetailFor(n?.asset ?? null)}
            listRow={processListRow}
            detail={
              detailing ? (
                <FileDetail row={detailing} catalog={catalog.data} canRead={canRead(detailing)} schema={detailSchema} processes={processes.data ?? []} templates={projectTpls.data ?? []}>
                  <Space wrap>
                    {covers(detailing) ? (
                      <Button size="small" type="primary" onClick={() => openEdit(detailing)}>
                        编辑
                      </Button>
                    ) : null}
                    {canPromote(detailing) && covers(detailing) && isAdmin ? (
                      <Button size="small" loading={promote.isPending} onClick={() => askPromote(detailing)}>
                        {promoteLabel(detailing)}
                      </Button>
                    ) : null}
                    {canMutate(detailing) && detailing.status === "draft" ? (
                      <Button
                        size="small"
                        onClick={() =>
                          modal.confirm({
                            title: `发布「${detailing.name}」？`,
                            content: "发布后可被依赖。可复制仍可改。",
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
                    {canMutate(detailing) && detailing.status === "available" ? (
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
                    {covers(detailing) && detailing.status === "disabled" ? (
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
                    {covers(detailing) ? (
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
                    ) : null}
                  </Space>
                </FileDetail>
              ) : (
                <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="点一份工程看详情" />
              )
            }
          />
        </Card>
      )}
      <Modal title={`新建${title}`} open={open} onCancel={() => confirmDiscard(form.isFieldsTouched(), () => setOpen(false))} mask={{ closable: false }} okText="创建" confirmLoading={create.isPending} destroyOnHidden width={isProcess ? 1100 : 560} styles={{ body: { maxHeight: isProcess ? "82vh" : "50vh", overflow: "auto" } }} onOk={() => form.submit()}>
        <Form<CreateForm>
          className={isProcess ? "project-edit-form" : undefined}
          form={form}
          layout="vertical"
          size="small"
          requiredMark={false}
          initialValues={{ content: "", weldKind: WELD_SINGLE }}
          onFinish={(values) => {
            const weld = values.weldKind || WELD_SINGLE;
            const content = isProcess ? values.content : JSON.stringify([emptyWeld(weld, projectTpls.data ?? [], 0)]);
            const input: CreateAssetInput = {
              kind,
              name: values.name,
              content,
              weldKind: weld,
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
            <Form.Item name="parentId" label="文件夹" extra="放进厂级文件夹就是厂级，放进个人文件夹就是个人级。" rules={[{ required: true, message: "请选择文件夹" }]}>
              <Select showSearch optionFilterProp="label" options={folderOpts} placeholder="选择文件夹" />
            </Form.Item>
          ) : (
            <Form.Item name="parentId" label="文件夹" extra="放进厂级文件夹就是厂级，放进个人文件夹就是个人级。" rules={[{ required: true, message: "请选择文件夹" }]}>
              <Select showSearch optionFilterProp="label" options={folderOpts} placeholder="选择文件夹" />
            </Form.Item>
          )}
          <div className="project-edit-meta">
            <Form.Item
              name="name"
              label={<Tooltip title="显示名不是身份，改名也不换编号。">{isProcess ? "显示名" : "工程名称"}</Tooltip>}
              rules={[{ required: true, message: isProcess ? "请输入显示名" : "请输入工程名称" }]}
            >
              <Input autoComplete="off" autoFocus />
            </Form.Item>
            <Form.Item name="weldKind" label={<Tooltip title="App 同类型工作流才能打开。">类型</Tooltip>} rules={[{ required: true, message: "请选择类型" }]}>
              <Select options={WELD_KINDS.map((k) => ({ value: k.value, label: k.label }))} />
            </Form.Item>
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
        title={promoteFor ? `把「${promoteFor.name}」升档为厂级` : "升档为厂级"}
        open={Boolean(promoteFor)}
        onCancel={() => setPromoteFor(null)}
        okText="升档"
        confirmLoading={promote.isPending}
        destroyOnHidden
        onOk={() =>
          promoteFor &&
          promote.mutate(
            { id: promoteFor.id, folderId: promoteFolder },
            {
              onSuccess: () => {
                message.success("已复制一份厂级草稿，改好后发布即可使用");
                setPromoteFor(null);
              },
              onError: onErr,
            },
          )
        }
      >
        <Typography.Paragraph type="secondary">复制一份到所选厂级文件夹，落成草稿；个人原件不动。每升一次都另起一份。</Typography.Paragraph>
        <Select style={{ width: "100%" }} showSearch optionFilterProp="label" options={factoryFolderOpts} value={promoteFolder} onChange={setPromoteFolder} placeholder="厂级文件夹" />
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
        title="复制工艺"
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
          <Form.Item name="name" label="新工艺名称" extra="另存为新草稿，原件不动；之后同新建。" rules={[{ required: true, message: "请输入新工艺名称" }]}>
            <Input autoComplete="off" autoFocus />
          </Form.Item>
        </Form>
      </Modal>
      <EditModal
        row={editing?.kind === "process" ? editing : null}
        base={editing?.kind === "process" ? editBase : null}
        canRead={editing?.kind === "process" ? canRead(editing) : false}
        schema={schema}
        saving={rename.isPending || updateContent.isPending || setWeldKind.isPending}
        locked={editing ? !canMutate(editing) : true}
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
              {canPromote(editing) && covers(editing) && isAdmin ? (
                <Button loading={promote.isPending} onClick={() => askPromote(editing)}>
                  {promoteLabel(editing)}
                </Button>
              ) : null}
            </Space>
          ) : null
        }
      />
      <ProjectEditModal
        row={editing?.kind === "project" ? editing : null}
        base={editing?.kind === "project" ? editBase : null}
        canRead={editing?.kind === "project" ? canRead(editing) : false}
        templates={projectTpls.data ?? []}
        processes={processes.data ?? []}
        meId={meId}
        saving={rename.isPending || updateContent.isPending || setAssetDeps.isPending || setWeldKind.isPending}
        locked={editing ? !canMutate(editing) : true}
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
            const kindSchema = (() => {
              const rows = projectTemplatesForWeld(projectTpls.data, nextKind);
              return rows.length ? projectContentSchema(rows) : editSchema;
            })();
            const nextIds = uniqueIds(processIdsFromContent(content, kindSchema));
            const added = nextIds.filter((id) => !depIds.includes(id));
            const catalog = processes.data ?? [];
            const toDep = (id: string): AssetDep => {
              const p = catalog.find((x) => x.id === id && x.status === "available");
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

function isLeaseError(e: unknown) {
  return e instanceof ApiError && e.code === "content lease expired";
}

function depText(row: Asset, processes: Asset[]) {
  if (!row.deps?.length) return "—";
  return row.deps.map((d) => processes.find((p) => p.id === d.id)?.name || "未知工艺").join("、");
}

function ParamsView({
  row,
  canRead,
  schema,
  processOptions,
  processes = [],
  templates = [],
}: {
  row: Asset;
  canRead: boolean;
  schema: ContentSchema | null;
  processOptions?: { value: string; label: string; disabled?: boolean }[];
  processes?: Asset[];
  templates?: { id: string; name?: string; schema?: ContentSchema }[];
}) {
  const q = useAssetContent(canRead ? row.id : null);
  if (!canRead) {
    return (
      <Typography.Text type="secondary">
        {row.kind === "process" && row.level === "platform" && !row.copyable ? "不可复制，不显示工艺参数。" : "无权查看正文。"}
      </Typography.Text>
    );
  }
  if (q.isError && isLeaseError(q.error)) {
    return <Typography.Text type="secondary">租约未就绪，暂时不能显示工艺参数。</Typography.Text>;
  }
  if (q.isError) return <Typography.Text type="danger">{errorMessage(q.error)}</Typography.Text>;
  if (q.isLoading) return <Typography.Text type="secondary">读取中…</Typography.Text>;
  if (row.kind === "project") {
    return (
      <ProjectWeldEditor
        value={q.data?.content ?? ""}
        disabled
        weldKind={row.weldKind || WELD_SINGLE}
        templates={templates}
        processOptions={processOptions ?? processSelectOptions((row.deps ?? []).map((d) => d.id), processes)}
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
  catalog,
  canRead,
  schema,
  processes = [],
  templates = [],
  children,
}: {
  row: Asset;
  catalog: Catalog | undefined;
  canRead: boolean;
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
          <Typography.Text copyable={{ text: row.code }}>{row.code || "—"}</Typography.Text>
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
        <Descriptions.Item label="级别">
          <Tag color={row.level === "factory" ? "blue" : row.level === "platform" ? "cyan" : "purple"}>{levelLabel(row.level)}</Tag>
        </Descriptions.Item>
        <Descriptions.Item label="创建人">{creatorText(row, catalog)}</Descriptions.Item>
        <Descriptions.Item label="创建时间">{formatTime(row.createdAt)}</Descriptions.Item>
        <Descriptions.Item label="更新">{formatTime(row.updatedAt)}</Descriptions.Item>
      </Descriptions>
      <div>
        <Typography.Text type="secondary" className="fs-detail-section-title">
          {row.kind === "process" ? "参数" : "焊道"}
        </Typography.Text>
        <div className="fs-detail-params">
          <ParamsView row={row} canRead={canRead} schema={schema} processes={processes} templates={templates} />
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

function processLabel(p: Asset) {
  return `${p.name} · ${levelLabel(p.level)} · r${p.revision}`;
}

function canPinForProject(projectLevel: string | undefined, p: Asset, meId?: string) {
  if (p.status !== "available") return false;
  if (p.level === "factory" || p.level === "platform") return true;
  if (p.level !== "personal") return false;
  return projectLevel === "factory" || p.creatorId === meId;
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
    opts.push({ value: p.id, label: processLabel(p) + extra, disabled: !ok, corner: Boolean(p.corner) });
  }
  for (const id of selected ?? []) {
    if (seen.has(id)) continue;
    opts.push({ value: id, label: id, disabled: true });
  }
  return opts;
}

function processSelectOptions(selected: string[] | undefined, pickable: Asset[], catalog: Asset[] = pickable) {
  const byId = new Map(catalog.map((p) => [p.id, p]));
  const opts: ProcessOption[] = pickable.map((p) => ({ value: p.id, label: processLabel(p), corner: Boolean(p.corner) }));
  const seen = new Set(pickable.map((p) => p.id));
  for (const id of selected ?? []) {
    if (seen.has(id)) continue;
    const p = byId.get(id);
    opts.push({ value: id, label: p ? processLabel(p) : id, corner: Boolean(p?.corner) });
    seen.add(id);
  }
  return opts;
}

type EditForm = { name: string; content: string; weldKind: string };

// useEditBody 两个编辑弹窗共用：打开时清掉上一条只填名称和类型；正文首次新取成功才写进表单并入基准，之后的列表刷新和失效重取都不再改表单。
function useEditBody(row: Asset | null, base: EditBase | null, canRead: boolean, onBaseContent: (id: string, content: string) => void) {
  const q = useAssetContent(canRead && row ? row.id : null, { fresh: true });
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
  // 打开后新取失败或无权读正文：只能改名，不提交正文、类型和依赖。
  const failed = !ready && base !== null && (!canRead || (q.isFetchedAfterMount && q.isError));
  return { q, form, ready, failed };
}

// ContentPending 正文区还没就绪：要么读取中，要么没读到只能改名、可点重读。
function ContentPending({
  failed,
  canRead,
  error,
  retrying,
  onRetry,
}: {
  failed: boolean;
  canRead: boolean;
  error: unknown;
  retrying: boolean;
  onRetry: () => void;
}) {
  if (!failed) return <Typography.Text type="secondary">读取中…</Typography.Text>;
  if (!canRead) return <Typography.Text type="secondary">无权查看正文，只能改名。</Typography.Text>;
  return (
    <Space wrap>
      <Typography.Text type="warning">正文没读到，只能改名。</Typography.Text>
      <Typography.Text type="secondary">{isLeaseError(error) ? "租约未就绪" : errorMessage(error)}</Typography.Text>
      <Button size="small" loading={retrying} onClick={onRetry}>
        重读
      </Button>
    </Space>
  );
}

function EditModal({
  row,
  base,
  canRead,
  schema,
  saving,
  locked,
  extra,
  onBaseContent,
  onClose,
  onSave,
}: {
  row: Asset | null;
  base: EditBase | null;
  canRead: boolean;
  schema: ContentSchema | null;
  saving: boolean;
  locked: boolean;
  extra?: ReactNode;
  onBaseContent: (id: string, content: string) => void;
  onClose: () => void;
  onSave: (name: string, content: string | null, weldKind: string) => Promise<void>;
}) {
  const { q, form, ready, failed } = useEditBody(row, base, canRead, onBaseContent);
  const confirmDiscard = useConfirmDiscard();
  const editWeld = Form.useWatch("weldKind", form) ?? base?.weldKind ?? WELD_SINGLE;
  const pending = row !== null && !ready;
  return (
    <Modal
      title="编辑"
      open={row !== null}
      onCancel={() => confirmDiscard(form.isFieldsTouched(), onClose)}
      mask={{ closable: false }}
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
            label={<Tooltip title="显示名不是身份，改名也不换编号。">显示名</Tooltip>}
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
            <ContentPending failed={failed} canRead={canRead} error={q.error} retrying={q.isFetching} onRetry={() => void q.refetch()} />
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

// 工程换类型会把焊道换成一条空焊道，先确认再换。
function WeldKindSwitch({ value, onChange, disabled }: { value?: string; onChange?: (v: string) => void; disabled?: boolean }) {
  const { modal } = App.useApp();
  return (
    <Select
      value={value}
      disabled={disabled}
      options={WELD_KINDS.map((k) => ({ value: k.value, label: k.label }))}
      onChange={(next: string) => {
        if (next === value) return;
        modal.confirm({
          title: `改成「${weldKindLabel(next)}」？`,
          content: "现有焊道、点位和工艺绑定会全部清空，换成一条空焊道；不保存就不生效。",
          okText: "清空并改类型",
          okButtonProps: { danger: true },
          onOk: () => onChange?.(next),
        });
      }}
    />
  );
}

function ProjectEditModal({
  row,
  base,
  canRead,
  templates,
  processes,
  meId,
  saving,
  locked,
  onBaseContent,
  onClose,
  onSave,
}: {
  row: Asset | null;
  base: EditBase | null;
  canRead: boolean;
  templates: { id: string; name?: string; schema?: ContentSchema }[];
  processes: Asset[];
  meId?: string;
  saving: boolean;
  locked: boolean;
  onBaseContent: (id: string, content: string) => void;
  onClose: () => void;
  onSave: (name: string, content: string | null, weldKind: string) => Promise<void>;
}) {
  const { q, form, ready, failed } = useEditBody(row, base, canRead, onBaseContent);
  const confirmDiscard = useConfirmDiscard();
  const { message } = App.useApp();
  const editWeld = Form.useWatch("weldKind", form) ?? base?.weldKind ?? WELD_SINGLE;
  const pending = row !== null && !ready;
  return (
    <Modal
      title="编辑工程"
      open={row !== null}
      onCancel={() => confirmDiscard(form.isFieldsTouched(), onClose)}
      mask={{ closable: false }}
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
          // 间隙带填反在保存时拦下；不挂成表单规则，免得整块焊道编辑器的输入框都标红。
          const bad = ready ? badGapBands(values.content) : null;
          if (bad) {
            message.error(bad);
            return;
          }
          onSave(values.name, ready ? (values.content ?? "") : null, weld).catch(() => undefined);
        }}
      >
        <div className="project-edit-meta">
          <Form.Item
            name="name"
            label={<Tooltip title="显示名不是身份，改名也不换编号。">工程名称</Tooltip>}
            rules={[{ required: true, message: "请输入工程名称" }]}
          >
            <Input autoComplete="off" disabled={locked} />
          </Form.Item>
          <Form.Item
            name="weldKind"
            label={<Tooltip title="焊道换成该类型空焊道。">类型</Tooltip>}
            rules={[{ required: true, message: "请选择类型" }]}
          >
            <WeldKindSwitch disabled={locked || !ready} />
          </Form.Item>
        </div>
        {pending ? (
          <Form.Item label="焊道">
            <ContentPending failed={failed} canRead={canRead} error={q.error} retrying={q.isFetching} onRetry={() => void q.refetch()} />
          </Form.Item>
        ) : (
          <Form.Item name="content" label="焊道">
            <ProjectWeldEditor
              key={row?.id}
              disabled={locked}
              weldKind={editWeld}
              templates={templates}
              processOptions={row ? processPickerOptions(processes, (p) => canPinForProject(row.level, p, meId) && sameWeldKind(p.weldKind, editWeld), (row.deps ?? []).map((d) => d.id)) : undefined}
            />
          </Form.Item>
        )}
      </Form>
    </Modal>
  );
}
