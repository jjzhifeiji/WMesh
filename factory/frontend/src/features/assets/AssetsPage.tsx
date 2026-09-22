import { FolderOutlined, PlusOutlined } from "@ant-design/icons";
import { App, Button, Card, Descriptions, Empty, Form, Input, Modal, Radio, Select, Space, Switch, Tag, Tooltip, Typography } from "antd";
import { useEffect, useMemo, useState, type ReactNode } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { personName, useCatalog, coversOrg, type Catalog } from "@/features/catalog/api";
import { errorMessage, ApiError } from "@/shared/api/client";
import { formatTime } from "@/shared/format";
import { statusColor, statusLabel } from "@/shared/labels";
import { IdText } from "@/shared/ui/IdText";
import { PageHeader } from "@/shared/ui/PageHeader";
import { ContentEditor } from "@/features/templates/ContentFields";
import { collectProcessIds, defaultValue, projectContentSchema } from "@/features/templates/schema";
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
  useSetAssetCopyable,
  useSetAssetWeldKind,
  useSetAssetDeps,
  useUpdateAssetContent,
  syncAssets,
  assetKeys,
  type Asset,
  type AssetDep,
  type AssetKind,
  type AssetLevel,
  type CreateAssetInput,
  type FSNode,
} from "./api";
import { fsFolderOptions, ProcessExplorer } from "./ProcessExplorer";
import { emptyWeld, ProjectWeldEditor } from "./ProjectWeldEditor";

type CreateForm = {
  level: AssetLevel;
  name: string;
  content: string;
  weldKind: string;
  copyable: boolean;
  parentId?: string;
};
type MkdirForm = { parentId: string; name: string };

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

// canPromote 未停用的个人级可升厂级；工艺还须可复制。
function canPromote(row: Asset) {
	return row.level === "personal" && row.status !== "disabled" && (row.kind !== "process" || row.copyable);
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

// 本厂工艺或工程：本厂有效账号都能制作；个人级正文只创建人能打开。
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
  const setCopyable = useSetAssetCopyable();
  const setWeldKind = useSetAssetWeldKind();
  const setAssetDeps = useSetAssetDeps();
  const publish = usePublishAsset();
  const disable = useDisableAsset();
  const enable = useEnableAsset();
  const remove = useDeleteAsset();
  const promote = usePromoteAsset();
  const qc = useQueryClient();
  const { message, modal } = App.useApp();
  const meId = catalog.data?.me.id;
  const [fsQuery, setFsQuery] = useState("");
  const [fsWeld, setFsWeld] = useState("all");
  const [open, setOpen] = useState(false);
  const [copyFor, setCopyFor] = useState<Asset | null>(null);
  const [detailFor, setDetailFor] = useState<Asset | null>(null);
  const [editFor, setEditFor] = useState<Asset | null>(null);
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

  const covers = (row: Asset) => {
    if (row.level === "platform") return false;
    if (row.level === "personal") return row.creatorId === meId || coversOrg(catalog.data, row.orgUnitId);
    return true;
  };
  const canMutate = (row: Asset) => covers(row) && row.status !== "disabled";
  // 不可复制的平台级工艺是保密件，厂端不打开正文；工程不保密。
  const canRead = (row: Asset) => {
    if (row.kind === "process" && row.level === "platform" && !row.copyable) return false;
    if (row.level === "personal") return row.creatorId === meId || coversOrg(catalog.data, row.orgUnitId);
    return true;
  };
  const canCopy = (row: Asset) => isProcess && row.copyable && row.status !== "disabled" && canRead(row);
  const isFactoryAdmin = coversOrg(catalog.data, null);
  const folderOpts = useMemo(
    () =>
      fsFolderOptions(fs.data ?? [], (n) => {
        if (n.treeLevel === "platform") return false;
        if (n.treeLevel === "factory") return true;
        return n.ownerId === meId || isFactoryAdmin;
      }),
    [fs.data, meId, isFactoryAdmin],
  );
  const openCreate = (parent?: FSNode) => {
    const folder = parent ?? (fs.data ?? []).find((n) => n.id === folderOpts[0]?.value);
    const level: AssetLevel = folder?.treeLevel === "personal" ? "personal" : "factory";
    form.setFieldsValue({
      content: isProcess && schema ? JSON.stringify(defaultValue(schema)) : "[]",
      weldKind: WELD_SINGLE,
      level,
      copyable: true,
      parentId: folder?.id,
    });
    setOpen(true);
  };
  const openMkdir = (parentId?: string) => {
    mkdirForm.setFieldsValue({ parentId: parentId ?? folderOpts[0]?.value, name: "新建文件夹" });
    setMkdirOpen(true);
  };
  const onErr = (e: unknown) => message.error(errorMessage(e));
  const toggleCopyable = (row: Asset, copyable: boolean) => {
    if (row.copyable === copyable) return;
    setCopyable.mutate(
      { id: row.id, expected: row.revision, copyable },
      { onSuccess: () => message.success(copyable ? "已设为可复制" : "已设为不可复制"), onError: onErr },
    );
  };
  const promotedOf = (row: Asset) => (assets.data ?? []).find((a) => a.sourceId === row.id);
  const promoteLabel = (row: Asset) => {
    const dst = promotedOf(row);
    if (!dst) return "升档为厂级";
    return dst.digest === row.digest ? "已升档" : "覆盖厂级";
  };
  const askPromote = (row: Asset) => {
    const dst = promotedOf(row);
    const same = Boolean(dst && dst.digest === row.digest);
    modal.confirm({
      title: same ? "正文未变" : dst ? "覆盖已升档的厂级？" : "升档为厂级？",
      content: same
        ? "正文没变，不会另开一条。"
        : dst
          ? "会覆盖已升档的厂级，个人原件不动。"
          : "复制出新厂级，个人原件不动。",
      onOk: () =>
        promote.mutate(row.id, {
          onSuccess: () => message.success(same ? "正文未变，未重复升档" : dst ? "已覆盖厂级" : "已升档为厂级"),
          onError: onErr,
        }),
    });
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
                      <Button size="small" type="primary" onClick={() => setEditFor(detailing)}>
                        编辑
                      </Button>
                    ) : null}
                    {canPromote(detailing) && covers(detailing) ? (
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
                      <Button size="small" type="primary" onClick={() => setEditFor(detailing)}>
                        编辑
                      </Button>
                    ) : null}
                    {canPromote(detailing) && covers(detailing) ? (
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
      <Modal title={`新建${title}`} open={open} onCancel={() => setOpen(false)} okText="创建" confirmLoading={create.isPending} destroyOnHidden width={isProcess ? 1100 : 560} styles={{ body: { maxHeight: isProcess ? "82vh" : "50vh", overflow: "auto" } }} onOk={() => form.submit()}>
        <Form<CreateForm>
          className={isProcess ? "project-edit-form" : undefined}
          form={form}
          layout="vertical"
          size="small"
          requiredMark={false}
          initialValues={{ level: "factory", content: "", weldKind: WELD_SINGLE, copyable: true }}
          onValuesChange={(changed) => {
            if ("parentId" in changed) {
              const folder = (fs.data ?? []).find((n) => n.id === changed.parentId);
              if (folder) form.setFieldValue("level", folder.treeLevel === "personal" ? "personal" : "factory");
            }
          }}
          onFinish={(values) => {
            const weld = values.weldKind || WELD_SINGLE;
            const content = isProcess ? values.content : JSON.stringify([emptyWeld(weld, projectTpls.data ?? [], 0)]);
            const input: CreateAssetInput = {
              kind,
              level: values.level,
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
                if (!isProcess) setEditFor(row);
              },
              onError: onErr,
            });
          }}
        >
          {isProcess ? (
            <Form.Item name="parentId" label="文件夹" extra="工艺会放到这个目录下，级别随文件夹。" rules={[{ required: true, message: "请选择文件夹" }]}>
              <Select showSearch optionFilterProp="label" options={folderOpts} placeholder="选择文件夹" />
            </Form.Item>
          ) : (
            <Form.Item name="parentId" label="文件夹" extra="工程会放到这个目录下，级别随文件夹。" rules={[{ required: true, message: "请选择文件夹" }]}>
              <Select showSearch optionFilterProp="label" options={folderOpts} placeholder="选择文件夹" />
            </Form.Item>
          )}
          <Form.Item name="level" label="级别" extra="随所选文件夹。">
            <Radio.Group disabled>
              <Radio value="factory">厂级</Radio>
              <Radio value="personal">个人级</Radio>
            </Radio.Group>
          </Form.Item>
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
            {isProcess ? (
              <Form.Item name="copyable" label="可复制" extra="否就不能升档。" valuePropName="checked">
                <Switch size="default" checkedChildren="可复制" unCheckedChildren="不可复制" />
              </Form.Item>
            ) : null}
          </div>
          {isProcess ? (
            <Form.Item name="content" label="参数">
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
        canRead={editing?.kind === "process" ? canRead(editing) : false}
        schema={schema}
        saving={rename.isPending || updateContent.isPending || setWeldKind.isPending}
        locked={editing ? !canMutate(editing) : true}
        onClose={() => setEditFor(null)}
        onSave={async (name, content, originalContent, weldKind) => {
          if (!editing || editing.kind !== "process") return;
          try {
            let expected = editing.revision;
            if (name !== editing.name) {
              const next = await rename.mutateAsync({ id: editing.id, expected, name });
              expected = next.revision;
            }
            if (weldKind && weldKind !== (editing.weldKind || WELD_SINGLE)) {
              const next = await setWeldKind.mutateAsync({ id: editing.id, expected, weldKind });
              expected = next.revision;
            }
            if (content !== originalContent) {
              await updateContent.mutateAsync({ id: editing.id, expected, content });
            }
            message.success("已保存");
            setEditFor(null);
          } catch (e) {
            onErr(e);
          }
        }}
        extra={
          editing?.kind === "process" ? (
            <Space wrap>
              {covers(editing) ? (
                <CopyableSwitch
                  checked={editing.copyable}
                  disabled={!canMutate(editing)}
                  loading={setCopyable.isPending && setCopyable.variables?.id === editing.id}
                  onToggle={(next) => toggleCopyable(editing, next)}
                />
              ) : null}
              {canPromote(editing) && covers(editing) ? (
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
        canRead={editing?.kind === "project" ? canRead(editing) : false}
        templates={projectTpls.data ?? []}
        processes={processes.data ?? []}
        meId={meId}
        saving={rename.isPending || updateContent.isPending || setAssetDeps.isPending || setWeldKind.isPending}
        locked={editing ? !canMutate(editing) : true}
        onClose={() => setEditFor(null)}
        onSave={async (name, content, originalContent, weldKind) => {
          if (!editing || editing.kind !== "project") return;
          try {
            let expected = editing.revision;
            if (name !== editing.name) {
              const next = await rename.mutateAsync({ id: editing.id, expected, name });
              expected = next.revision;
            }
            const nextKind = weldKind || editing.weldKind || WELD_SINGLE;
            let depIds = (editing.deps ?? []).map((d) => d.id);
            if (nextKind !== (editing.weldKind || WELD_SINGLE)) {
              const next = await setWeldKind.mutateAsync({ id: editing.id, expected, weldKind: nextKind });
              expected = next.revision;
              depIds = (next.deps ?? []).map((d) => d.id);
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
              const pinned = (editing.deps ?? []).find((d) => d.id === id);
              if (pinned) return pinned;
              return { id, revision: 0, digest: "" };
            };
            if (added.length) {
              const next = await setAssetDeps.mutateAsync({
                id: editing.id,
                expected,
                deps: [...depIds, ...added].map(toDep),
              });
              expected = next.revision;
            }
            if (content !== originalContent) {
              const next = await updateContent.mutateAsync({ id: editing.id, expected, content });
              expected = next.revision;
            }
            if (nextIds.join("\0") !== depIds.join("\0") || added.length) {
              await setAssetDeps.mutateAsync({ id: editing.id, expected, deps: nextIds.map(toDep) });
            }
            message.success("已保存");
            setEditFor(null);
          } catch (e) {
            onErr(e);
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
  const opts: { value: string; label: string; disabled?: boolean }[] = [];
  for (const p of all) {
    seen.add(p.id);
    const ok = pin(p);
    if (!ok && !selectedSet.has(p.id)) continue;
    const extra = ok ? "" : ` · ${statusLabel(p.status)}`;
    opts.push({ value: p.id, label: processLabel(p) + extra, disabled: !ok });
  }
  for (const id of selected ?? []) {
    if (seen.has(id)) continue;
    opts.push({ value: id, label: id, disabled: true });
  }
  return opts;
}

function processSelectOptions(selected: string[] | undefined, pickable: Asset[], catalog: Asset[] = pickable) {
  const byId = new Map(catalog.map((p) => [p.id, p]));
  const opts = pickable.map((p) => ({ value: p.id, label: processLabel(p) }));
  const seen = new Set(pickable.map((p) => p.id));
  for (const id of selected ?? []) {
    if (seen.has(id)) continue;
    const p = byId.get(id);
    opts.push({ value: id, label: p ? processLabel(p) : id });
    seen.add(id);
  }
  return opts;
}

function EditModal({
  row,
  canRead,
  schema,
  saving,
  locked,
  extra,
  onClose,
  onSave,
}: {
  row: Asset | null;
  canRead: boolean;
  schema: ContentSchema | null;
  saving: boolean;
  locked: boolean;
  extra?: ReactNode;
  onClose: () => void;
  onSave: (name: string, content: string, originalContent: string, weldKind: string) => Promise<void>;
}) {
  const q = useAssetContent(canRead && row ? row.id : null);
  const [form] = Form.useForm<{ name: string; content: string; weldKind: string }>();
  const editWeld = Form.useWatch("weldKind", form) ?? row?.weldKind ?? WELD_SINGLE;
  useEffect(() => {
    if (!row) return;
    form.setFieldsValue({ name: row.name, weldKind: row.weldKind || WELD_SINGLE });
    if (!q.isFetching) form.setFieldsValue({ content: q.data?.content ?? "" });
  }, [row, q.data, q.isFetching, form]);
  return (
    <Modal
      title="编辑"
      open={row !== null}
      onCancel={onClose}
      okText="保存"
      okButtonProps={{ disabled: locked }}
      confirmLoading={saving || q.isLoading}
      width={1100}
      styles={{ body: { maxHeight: "82vh", overflow: "auto" } }}
      onOk={() => form.submit()}
    >
      {q.isError && isLeaseError(q.error) ? (
        <Typography.Text type="secondary">租约未就绪，暂时不能改正文。</Typography.Text>
      ) : q.isError ? (
        <Typography.Text type="danger">{errorMessage(q.error)}</Typography.Text>
      ) : null}
      <Form<{ name: string; content: string; weldKind: string }>
        className="project-edit-form"
        form={form}
        layout="vertical"
        size="small"
        requiredMark={false}
        onFinish={(values) => onSave(values.name, values.content, q.data?.content ?? "", values.weldKind || row?.weldKind || WELD_SINGLE).catch(() => undefined)}
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
              <Select disabled={locked} options={WELD_KINDS.map((k) => ({ value: k.value, label: k.label }))} />
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
        <Form.Item name="content" label="参数">
          <ContentEditor
            schema={schema}
            disabled={locked}
            weldKind={editWeld}
          />
        </Form.Item>
      </Form>
    </Modal>
  );
}

function ProjectEditModal({
  row,
  canRead,
  templates,
  processes,
  meId,
  saving,
  locked,
  onClose,
  onSave,
}: {
  row: Asset | null;
  canRead: boolean;
  templates: { id: string; name?: string; schema?: ContentSchema }[];
  processes: Asset[];
  meId?: string;
  saving: boolean;
  locked: boolean;
  onClose: () => void;
  onSave: (name: string, content: string, originalContent: string, weldKind: string) => Promise<void>;
}) {
  const q = useAssetContent(canRead && row ? row.id : null);
  const [form] = Form.useForm<{ name: string; content: string; weldKind: string }>();
  const editWeld = Form.useWatch("weldKind", form) ?? row?.weldKind ?? WELD_SINGLE;
  useEffect(() => {
    if (!row) return;
    form.setFieldsValue({ name: row.name, weldKind: row.weldKind || WELD_SINGLE });
    if (!q.isFetching) form.setFieldsValue({ content: q.data?.content ?? "[]" });
  }, [row, q.data, q.isFetching, form]);
  return (
    <Modal
      title="编辑工程"
      open={row !== null}
      onCancel={onClose}
      okText="保存"
      okButtonProps={{ disabled: locked }}
      confirmLoading={saving || q.isLoading}
      width={1100}
      styles={{ body: { maxHeight: "82vh", overflow: "auto" } }}
      onOk={() => form.submit()}
    >
      {q.isError && isLeaseError(q.error) ? (
        <Typography.Text type="secondary">租约未就绪，暂时不能改正文。</Typography.Text>
      ) : q.isError ? (
        <Typography.Text type="danger">{errorMessage(q.error)}</Typography.Text>
      ) : null}
      <Form<{ name: string; content: string; weldKind: string }>
        className="project-edit-form"
        form={form}
        layout="vertical"
        size="small"
        requiredMark={false}
        onValuesChange={(changed) => {
          if (changed.weldKind) form.setFieldValue("content", JSON.stringify([emptyWeld(changed.weldKind, templates, 0)]));
        }}
        onFinish={(values) => {
          const weld = values.weldKind || row?.weldKind || WELD_SINGLE;
          onSave(values.name, values.content ?? "", q.data?.content ?? "", weld).catch(() => undefined);
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
            <Select disabled={locked} options={WELD_KINDS.map((k) => ({ value: k.value, label: k.label }))} />
          </Form.Item>
        </div>
        <Form.Item name="content" label="焊道">
          <ProjectWeldEditor
            disabled={locked}
            weldKind={editWeld}
            templates={templates}
            processOptions={row ? processPickerOptions(processes, (p) => canPinForProject(row.level, p, meId) && sameWeldKind(p.weldKind, editWeld), (row.deps ?? []).map((d) => d.id)) : undefined}
          />
        </Form.Item>
      </Form>
    </Modal>
  );
}
