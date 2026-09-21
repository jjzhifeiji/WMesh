import { FolderOutlined, PlusOutlined } from "@ant-design/icons";
import { App, Button, Card, Descriptions, Empty, Form, Input, Modal, Radio, Select, Space, Switch, Table, Tag, Typography, type TableColumnsType } from "antd";
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

// canPromote 可用且可复制的个人级可复制为厂级。
function canPromote(row: Asset) {
  return row.level === "personal" && row.status === "available" && row.copyable;
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
  const fs = useFS("process");
  const copy = useCopyAsset();
  const rename = useRenameAsset();
  const updateContent = useUpdateAssetContent();
  const setCopyable = useSetAssetCopyable();
  const setAssetDeps = useSetAssetDeps();
  const publish = usePublishAsset();
  const disable = useDisableAsset();
  const enable = useEnableAsset();
  const remove = useDeleteAsset();
  const promote = usePromoteAsset();
  const qc = useQueryClient();
  const { message, modal } = App.useApp();
  const meId = catalog.data?.me.id;
  const [levelFilter, setLevelFilter] = useState<"all" | AssetLevel>("all");
  const [query, setQuery] = useState("");
  const [fsQuery, setFsQuery] = useState("");
  const [statusFilter, setStatusFilter] = useState("all");
  const [weldFilter, setWeldFilter] = useState("all");
  const [fsWeld, setFsWeld] = useState("all");
  const [sortKey, setSortKey] = useState<"name" | "code" | "createdAt">("createdAt");
  const [sortAsc, setSortAsc] = useState(false);
  const [open, setOpen] = useState(false);
  const [copyFor, setCopyFor] = useState<Asset | null>(null);
  const [detailFor, setDetailFor] = useState<Asset | null>(null);
  const [editFor, setEditFor] = useState<Asset | null>(null);
  const [mkdirOpen, setMkdirOpen] = useState(false);
  const [form] = Form.useForm<CreateForm>();
  const [mkdirForm] = Form.useForm<MkdirForm>();
  const [copyForm] = Form.useForm<{ name: string }>();
  const createLevel = Form.useWatch("level", form) as AssetLevel | undefined;
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

  const rows = useMemo(() => {
    const needle = query.trim().toLowerCase();
    const filtered = (assets.data ?? []).filter((a) => {
      if (levelFilter !== "all" && a.level !== levelFilter) return false;
      if (statusFilter !== "all" && a.status !== statusFilter) return false;
      if (weldFilter !== "all" && (a.weldKind || WELD_SINGLE) !== weldFilter) return false;
      if (!needle) return true;
      if (a.code === query.trim() || a.code === query.trim().toUpperCase()) return true;
      const creator = creatorText(a, catalog.data).toLowerCase();
      return a.name.toLowerCase().includes(needle) || creator.includes(needle);
    });
    const collator = new Intl.Collator("zh");
    return [...filtered].sort((a, b) => {
      let cmp = 0;
      if (sortKey === "name") cmp = collator.compare(a.name || "", b.name || "");
      else if (sortKey === "code") cmp = (a.code || "").localeCompare(b.code || "", "zh", { numeric: true });
      else cmp = Date.parse(a.createdAt || "") - Date.parse(b.createdAt || "");
      return sortAsc ? cmp : -cmp;
    });
  }, [assets.data, levelFilter, statusFilter, weldFilter, query, catalog.data, sortKey, sortAsc]);

  useEffect(() => {
    if (copyFor) copyForm.setFieldsValue({ name: `${copyFor.name}-副本` });
  }, [copyFor, copyForm]);

  const covers = (row: Asset) => {
    if (row.level === "platform") return false;
    if (row.level === "personal") return row.creatorId === meId || coversOrg(catalog.data, row.orgUnitId);
    return true;
  };
  const canMutate = (row: Asset) => covers(row) && row.status !== "disabled";
  // 不可复制的平台级是保密件，厂端不打开正文。
  const canRead = (row: Asset) => {
    if (row.level === "platform" && !row.copyable) return false;
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
    const tplRows = projectTemplatesForWeld(projectTpls.data, WELD_SINGLE);
    const next = isProcess ? schema : tplRows.length ? projectContentSchema(tplRows) : schema;
    const folder = parent ?? (fs.data ?? []).find((n) => n.id === folderOpts[0]?.value);
    const level: AssetLevel = folder?.treeLevel === "personal" ? "personal" : "factory";
    form.setFieldsValue({
      content: next ? JSON.stringify(defaultValue(next)) : "",
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
  const sortOrder = (key: "name" | "code" | "createdAt") => (sortKey === key ? (sortAsc ? "ascend" : "descend") : undefined);

  const columns: TableColumnsType<Asset> = [
    { title: isProcess ? "工艺名称" : "工程名称", dataIndex: "name", width: 180, ellipsis: true, sorter: true, sortOrder: sortOrder("name"), render: (name: string) => <Typography.Text strong>{name}</Typography.Text> },
    { title: "编号", dataIndex: "code", width: 150, sorter: true, sortOrder: sortOrder("code"), render: (code: string) => <Typography.Text copyable={{ text: code }}>{code || "—"}</Typography.Text> },
    {
      title: "级别",
      dataIndex: "level",
      width: 80,
      render: (l: string) => <Tag color={l === "factory" ? "blue" : l === "platform" ? "cyan" : "purple"}>{levelLabel(l)}</Tag>,
    },
    { title: "状态", dataIndex: "status", width: 80, render: (s: string) => <Tag color={statusColor(s)}>{statusLabel(s)}</Tag> },
    { title: "类型", dataIndex: "weldKind", width: 100, render: (v: string) => weldKindLabel(v || WELD_SINGLE) },
    { title: "可复制", dataIndex: "copyable", width: 80, render: (ok: boolean) => (ok ? "是" : "否") },
    { title: "修订", dataIndex: "revision", width: 60 },
    { title: "创建人", key: "creator", width: 160, ellipsis: true, render: (_, row) => creatorText(row, catalog.data) },
    ...(!isProcess
      ? [
          {
            title: "依赖工艺",
            key: "deps",
            width: 220,
            ellipsis: true,
            render: (_: unknown, row: Asset) => {
              if (!row.deps?.length) return "—";
              const all = processes.data ?? [];
              return row.deps.map((d) => all.find((p) => p.id === d.id)?.name || d.id).join("、");
            },
          } satisfies TableColumnsType<Asset>[number],
        ]
      : []),
    { title: "创建时间", dataIndex: "createdAt", width: 160, sorter: true, sortOrder: sortOrder("createdAt"), render: (v: string) => formatTime(v) },
    {
      title: "操作",
      key: "actions",
      width: 360,
      fixed: "right",
      render: (_, row) => (
        <Space size={4} wrap>
          <Button size="small" onClick={() => setDetailFor(row)}>
            详情
          </Button>
          {canCopy(row) ? (
            <Button size="small" onClick={() => setCopyFor(row)}>
              复制
            </Button>
          ) : null}
          {covers(row) ? (
            <Button size="small" type="primary" onClick={() => setEditFor(row)}>
              编辑
            </Button>
          ) : null}
          {canMutate(row) && row.status === "draft" ? (
            <Button
              size="small"
              onClick={() =>
                modal.confirm({
                  title: `发布「${row.name}」？`,
                  content: "发布后可被依赖。可复制仍可改。",
                  onOk: () =>
                    publish.mutate(
                      { id: row.id, expected: row.revision },
                      { onSuccess: () => message.success("已发布"), onError: onErr },
                    ),
                })
              }
            >
              发布
            </Button>
          ) : null}
          {canMutate(row) && row.status === "available" ? (
            <Button
              size="small"
              danger
              onClick={() =>
                modal.confirm({
                  title: `停用「${row.name}」？`,
                  content: "停用后不能改、不能升档、不能被新工程依赖；可以再启用。",
                  okButtonProps: { danger: true },
                  onOk: () =>
                    disable.mutate(
                      { id: row.id, expected: row.revision },
                      { onSuccess: () => message.success("已停用"), onError: onErr },
                    ),
                })
              }
            >
              停用
            </Button>
          ) : null}
          {covers(row) && row.status === "disabled" ? (
            <Button
              size="small"
              onClick={() =>
                enable.mutate(
                  { id: row.id, expected: row.revision },
                  { onSuccess: () => message.success("已启用"), onError: onErr },
                )
              }
            >
              启用
            </Button>
          ) : null}
          {covers(row) ? (
            <Button
              size="small"
              danger
              onClick={() =>
                modal.confirm({
                  title: `删除「${row.name}」？`,
                  content: "删除后不能恢复。若已被工程依赖会拒绝。",
                  okButtonProps: { danger: true },
                  onOk: () => remove.mutate(row.id, { onSuccess: () => message.success("已删除"), onError: onErr }),
                })
              }
            >
              删除
            </Button>
          ) : null}
        </Space>
      ),
    },
  ];

  const emptyText = `还没有${title}`;

  return (
    <>
      <PageHeader
        title={title}
        description={
          isProcess
            ? "本厂账号都能制作；云端不可复制的不显示参数。个人级只有创建人能打开正文。"
            : "工程钉死所依赖工艺的身份和修订；升档工程不会另拆出工艺。"
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
            ) : null}
            {isProcess ? (
              <Button icon={<FolderOutlined />} onClick={() => openMkdir()}>
                新建文件夹
              </Button>
            ) : null}
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
            onSelectFile={(n) => {
              if (n.asset) setDetailFor(n.asset);
            }}
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
      <Card>
        <Space wrap style={{ marginBottom: 12 }}>
          <Input.Search allowClear placeholder={`搜索${title}名称、编号或创建人`} value={query} onChange={(e) => setQuery(e.target.value)} style={{ width: 280 }} />
          <Select
            value={statusFilter}
            onChange={setStatusFilter}
            style={{ width: 132 }}
            options={[
              { value: "all", label: "全部状态" },
              { value: "draft", label: "草稿" },
              { value: "available", label: "可用" },
              { value: "disabled", label: "已停用" },
            ]}
          />
          <Select
            value={weldFilter}
            onChange={setWeldFilter}
            style={{ width: 140 }}
            options={[{ value: "all", label: "全部类型" }, ...WELD_KINDS.map((k) => ({ value: k.value, label: k.label }))]}
          />
          <Radio.Group value={levelFilter} onChange={(e) => setLevelFilter(e.target.value)}>
            <Radio.Button value="all">全部</Radio.Button>
            <Radio.Button value="factory">厂级</Radio.Button>
            <Radio.Button value="personal">个人级</Radio.Button>
            <Radio.Button value="platform">平台级</Radio.Button>
          </Radio.Group>
          <Radio.Group value={sortKey} onChange={(e) => setSortKey(e.target.value)}>
            <Radio.Button value="name">按名称</Radio.Button>
            <Radio.Button value="code">按编号</Radio.Button>
            <Radio.Button value="createdAt">按时间</Radio.Button>
          </Radio.Group>
          <Button onClick={() => setSortAsc((v) => !v)}>{sortAsc ? "升序 ↑" : "降序 ↓"}</Button>
        </Space>
        <Table<Asset>
          rowKey="id"
          columns={columns}
          dataSource={rows}
          loading={assets.isLoading}
          scroll={{ x: 1280 }}
          pagination={{ pageSize: 20, hideOnSinglePage: true }}
          onChange={(_p, _f, sorter) => {
            const s = Array.isArray(sorter) ? sorter[0] : sorter;
            const field = s?.field;
            if (field !== "name" && field !== "code" && field !== "createdAt") return;
            if (!s.order) {
              setSortKey("createdAt");
              setSortAsc(false);
              return;
            }
            setSortKey(field);
            setSortAsc(s.order === "ascend");
          }}
          locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={emptyText} /> }}
        />
      </Card>
      )}
      <Modal title={`新建${title}`} open={open} onCancel={() => setOpen(false)} okText="创建" confirmLoading={create.isPending} destroyOnHidden width={720} styles={{ body: { maxHeight: "50vh", overflow: "auto" } }} onOk={() => form.submit()}>
        <Form<CreateForm>
          form={form}
          layout="vertical"
          size="small"
          requiredMark={false}
          initialValues={{ level: "factory", content: "", weldKind: WELD_SINGLE, copyable: true }}
          onValuesChange={(changed) => {
            if (isProcess && "parentId" in changed) {
              const folder = (fs.data ?? []).find((n) => n.id === changed.parentId);
              if (folder) form.setFieldValue("level", folder.treeLevel === "personal" ? "personal" : "factory");
            }
            if (!isProcess && "weldKind" in changed) {
              const weld = changed.weldKind || WELD_SINGLE;
              const rows = projectTemplatesForWeld(projectTpls.data, weld);
              const next = rows.length ? projectContentSchema(rows) : schema;
              form.setFieldValue("content", next ? JSON.stringify(defaultValue(next)) : "");
            }
          }}
          onFinish={(values) => {
            const deps = isProcess ? undefined : depsFromSelection(undefined, values.content, processes.data ?? [], createSchema);
            const input: CreateAssetInput = {
              kind,
              level: values.level,
              name: values.name,
              content: values.content,
              weldKind: values.weldKind,
              copyable: isProcess ? values.copyable : undefined,
              deps,
              parentId: isProcess ? values.parentId : undefined,
            };
            create.mutate(input, {
              onSuccess: () => {
                message.success("已创建草稿");
                form.resetFields();
                setOpen(false);
              },
              onError: onErr,
            });
          }}
        >
          {isProcess ? (
            <Form.Item name="parentId" label="文件夹" extra="工艺会放到这个目录下，级别随文件夹。" rules={[{ required: true, message: "请选择文件夹" }]}>
              <Select showSearch optionFilterProp="label" options={folderOpts} placeholder="选择文件夹" />
            </Form.Item>
          ) : null}
          <Form.Item name="level" label="级别" extra={isProcess ? "随所选文件夹。" : "个人级只有你能打开正文；厂级本厂有效账号都能维护。"}>
            <Radio.Group disabled={isProcess}>
              <Radio value="factory">厂级</Radio>
              <Radio value="personal">个人级</Radio>
            </Radio.Group>
          </Form.Item>
          <Form.Item name="name" label="显示名" extra="显示名不是身份，改名也不换编号。" rules={[{ required: true, message: "请输入显示名" }]}>
            <Input autoComplete="off" autoFocus />
          </Form.Item>
          {isProcess ? (
            <Form.Item name="copyable" label="可复制" extra="否就不能升档。" valuePropName="checked">
              <Switch size="default" checkedChildren="可复制" unCheckedChildren="不可复制" />
            </Form.Item>
          ) : null}
          <Form.Item name="weldKind" label="类型" extra="创建后不能改。App 同类型工作流才能打开。" rules={[{ required: true, message: "请选择类型" }]}>
            <Select options={WELD_KINDS.map((k) => ({ value: k.value, label: k.label }))} />
          </Form.Item>
          <Form.Item name="content" label="参数">
            <ContentEditor schema={createSchema} weldKind={createWeld} processOptions={isProcess ? undefined : processPickerOptions(processes.data ?? [], (p) => canPinForProject(createLevel ?? "factory", p, meId) && sameWeldKind(p.weldKind, createWeld))} />
          </Form.Item>
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
      <DetailModal
        row={isProcess ? null : detailing}
        processes={processes.data ?? []}
        catalog={catalog.data}
        canRead={isProcess ? false : detailing ? canRead(detailing) : false}
        schema={isProcess ? schema : detailSchema}
        onClose={() => setDetailFor(null)}
      />
      <EditModal
        row={editing}
        canRead={editing ? canRead(editing) : false}
        schema={editSchema}
        processes={processes.data ?? []}
        meId={meId}
        saving={rename.isPending || updateContent.isPending || setAssetDeps.isPending}
        locked={editing ? !canMutate(editing) : true}
        onClose={() => setEditFor(null)}
        onSave={async (name, content, originalContent) => {
          if (!editing) return;
          try {
            let expected = editing.revision;
            if (name !== editing.name) {
              const next = await rename.mutateAsync({ id: editing.id, expected, name });
              expected = next.revision;
            }
            if (editing.kind === "project") {
              const oldIds = (editing.deps ?? []).map((d) => d.id);
              const pickable = pickableProcesses(editing, processes.data ?? [], meId);
              const nextIds = uniqueIds(processIdsFromContent(content, schema));
              const added = nextIds.filter((id) => !oldIds.includes(id));
              const toDep = (id: string): AssetDep => {
                const pinned = (editing.deps ?? []).find((d) => d.id === id);
                if (pinned) return pinned;
                const p = pickable.find((x) => x.id === id) ?? (processes.data ?? []).find((x) => x.id === id);
                if (!p) return { id, revision: 0, digest: "" };
                return { id: p.id, revision: p.revision, digest: p.digest };
              };
              if (added.length) {
                const next = await setAssetDeps.mutateAsync({
                  id: editing.id,
                  expected,
                  deps: [...oldIds, ...added].map(toDep),
                });
                expected = next.revision;
              }
              if (content !== originalContent) {
                const next = await updateContent.mutateAsync({ id: editing.id, expected, content });
                expected = next.revision;
              }
              if (nextIds.join("\0") !== oldIds.join("\0") || added.length) {
                await setAssetDeps.mutateAsync({ id: editing.id, expected, deps: nextIds.map(toDep) });
              }
            } else if (content !== originalContent) {
              await updateContent.mutateAsync({ id: editing.id, expected, content });
            }
            message.success("已保存");
            setEditFor(null);
          } catch (e) {
            onErr(e);
          }
        }}
        extra={
          editing ? (
            <Space wrap>
              {covers(editing) ? (
                <CopyableSwitch
                  checked={editing.copyable}
                  disabled={!canMutate(editing)}
                  loading={setCopyable.isPending && setCopyable.variables?.id === editing.id}
                  onToggle={(next) => toggleCopyable(editing, next)}
                />
              ) : null}
              {canPromote(editing) ? (
                <Button
                  onClick={() => {
                    const dst = (assets.data ?? []).find((a) => a.sourceId === editing.id);
                    const same = Boolean(dst && dst.digest === editing.digest);
                    modal.confirm({
                      title: same ? "正文未变" : dst ? "覆盖已复制的厂级？" : "复制为厂级？",
                      content: same
                        ? "正文没变，不会另开一条。"
                        : dst
                          ? "会覆盖已复制的厂级，个人原件不动。"
                          : "复制出新厂级，个人原件不动；响应不含个人正文。",
                      onOk: () =>
                        promote.mutate(editing.id, {
                          onSuccess: () => message.success(same ? "正文未变，未重复复制" : dst ? "已覆盖厂级" : "已复制为厂级"),
                          onError: onErr,
                        }),
                    });
                  }}
                >
                  {(assets.data ?? []).some((a) => a.sourceId === editing.id)
                    ? (assets.data ?? []).find((a) => a.sourceId === editing.id)?.digest === editing.digest
                      ? "已复制"
                      : "覆盖厂级"
                    : "可复制"}
                </Button>
              ) : null}
            </Space>
          ) : null
        }
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
}: {
  row: Asset;
  canRead: boolean;
  schema: ContentSchema | null;
  processOptions?: { value: string; label: string; disabled?: boolean }[];
}) {
  const q = useAssetContent(canRead ? row.id : null);
  if (!canRead) {
    return (
      <Typography.Text type="secondary">
        {row.level === "platform" && !row.copyable ? "不可复制，不显示工艺参数。" : "无权查看正文。"}
      </Typography.Text>
    );
  }
  if (q.isError && isLeaseError(q.error)) {
    return <Typography.Text type="secondary">租约未就绪，暂时不能显示工艺参数。</Typography.Text>;
  }
  if (q.isError) return <Typography.Text type="danger">{errorMessage(q.error)}</Typography.Text>;
  if (q.isLoading) return <Typography.Text type="secondary">读取中…</Typography.Text>;
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
  children,
}: {
  row: Asset;
  catalog: Catalog | undefined;
  canRead: boolean;
  schema: ContentSchema | null;
  children?: ReactNode;
}) {
  return (
    <Space direction="vertical" size={12} style={{ width: "100%" }}>
      <Typography.Title level={5} style={{ margin: 0 }}>
        {row.name}
      </Typography.Title>
      <Descriptions size="small" column={1}>
        <Descriptions.Item label="编号">
          <Typography.Text copyable={{ text: row.code }}>{row.code || "—"}</Typography.Text>
        </Descriptions.Item>
        <Descriptions.Item label="工艺ID">
          <IdText id={row.id} />
        </Descriptions.Item>
        <Descriptions.Item label="级别">
          <Tag color={row.level === "factory" ? "blue" : row.level === "platform" ? "cyan" : "purple"}>{levelLabel(row.level)}</Tag>
        </Descriptions.Item>
        <Descriptions.Item label="状态">
          <Tag color={statusColor(row.status)}>{statusLabel(row.status)}</Tag>
        </Descriptions.Item>
        <Descriptions.Item label="类型">{weldKindLabel(row.weldKind || WELD_SINGLE)}</Descriptions.Item>
        <Descriptions.Item label="可复制">{row.copyable ? "是" : "否"}</Descriptions.Item>
        <Descriptions.Item label="修订">{row.revision}</Descriptions.Item>
        <Descriptions.Item label="创建人">{creatorText(row, catalog)}</Descriptions.Item>
        <Descriptions.Item label="创建时间">{formatTime(row.createdAt)}</Descriptions.Item>
        <Descriptions.Item label="更新">{formatTime(row.updatedAt)}</Descriptions.Item>
      </Descriptions>
      {children}
      <Typography.Paragraph type="secondary" style={{ marginBottom: 0 }}>
        参数
      </Typography.Paragraph>
      <div className="fs-detail-params">
        <ParamsView row={row} canRead={canRead} schema={schema} />
      </div>
    </Space>
  );
}

function DetailModal({
  row,
  processes,
  catalog,
  canRead,
  schema,
  onClose,
}: {
  row: Asset | null;
  processes: Asset[];
  catalog: Catalog | undefined;
  canRead: boolean;
  schema: ContentSchema | null;
  onClose: () => void;
}) {
  return (
    <Modal title="详情" open={row !== null} onCancel={onClose} footer={null} width={720} styles={{ body: { maxHeight: "50vh", overflow: "auto" } }}>
      {row ? (
        <>
          <Descriptions
            column={1}
            size="small"
            items={[
              { key: "name", label: row.kind === "process" ? "工艺名称" : "工程名称", children: row.name },
              { key: "code", label: "编号", children: <Typography.Text copyable={{ text: row.code }}>{row.code || "—"}</Typography.Text> },
              ...(row.kind === "process" ? [{ key: "id", label: "工艺ID", children: <IdText id={row.id} /> }] : []),
              { key: "level", label: "级别", children: <Tag color={row.level === "factory" ? "blue" : "purple"}>{levelLabel(row.level)}</Tag> },
              { key: "status", label: "状态", children: <Tag color={statusColor(row.status)}>{statusLabel(row.status)}</Tag> },
              { key: "weldKind", label: "类型", children: weldKindLabel(row.weldKind || WELD_SINGLE) },
              { key: "copyable", label: "可复制", children: row.copyable ? "是" : "否" },
              { key: "revision", label: "修订", children: row.revision },
              { key: "creator", label: "创建人", children: creatorText(row, catalog) },
              ...(row.kind === "project" ? [{ key: "deps", label: "依赖工艺", children: depText(row, processes) }] : []),
              { key: "created", label: "创建时间", children: formatTime(row.createdAt) },
              { key: "updated", label: "更新", children: formatTime(row.updatedAt) },
            ]}
          />
          <Typography.Paragraph type="secondary" style={{ marginTop: 16, marginBottom: 8 }}>
            参数
          </Typography.Paragraph>
          <ParamsView
            row={row}
            canRead={canRead}
            schema={schema}
            processOptions={row.kind === "project" ? processSelectOptions((row.deps ?? []).map((d) => d.id), processes) : undefined}
          />
        </>
      ) : null}
    </Modal>
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

function depsFromSelection(selected: string[] | undefined, content: string | undefined, available: Asset[], schema: ContentSchema | null): AssetDep[] {
  const ids = uniqueIds([...(selected ?? []), ...processIdsFromContent(content, schema)]);
  return ids.map((id) => {
    const p = available.find((x) => x.id === id);
    if (!p) return { id, revision: 0, digest: "" };
    return { id: p.id, revision: p.revision, digest: p.digest };
  });
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

function pickableProcesses(row: Asset | null, all: Asset[], meId?: string): Asset[] {
  if (!row || row.kind !== "project") return [];
  return all.filter((p) => canPinForProject(row.level, p, meId) && sameWeldKind(p.weldKind, row.weldKind));
}

function EditModal({
  row,
  canRead,
  schema,
  processes,
  meId,
  saving,
  locked,
  extra,
  onClose,
  onSave,
}: {
  row: Asset | null;
  canRead: boolean;
  schema: ContentSchema | null;
  processes: Asset[];
  meId?: string;
  saving: boolean;
  locked: boolean;
  extra: ReactNode;
  onClose: () => void;
  onSave: (name: string, content: string, originalContent: string) => Promise<void>;
}) {
  const q = useAssetContent(canRead && row ? row.id : null);
  const [form] = Form.useForm<{ name: string; content: string }>();
  useEffect(() => {
    if (!row) return;
    form.setFieldsValue({ name: row.name });
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
      width={720}
      styles={{ body: { maxHeight: "50vh", overflow: "auto" } }}
      onOk={() => form.submit()}
    >
      {q.isError && isLeaseError(q.error) ? (
        <Typography.Text type="secondary">租约未就绪，暂时不能改正文。</Typography.Text>
      ) : q.isError ? (
        <Typography.Text type="danger">{errorMessage(q.error)}</Typography.Text>
      ) : null}
      {extra ? <div style={{ marginBottom: 8 }}>{extra}</div> : null}
      <Form<{ name: string; content: string }>
        form={form}
        layout="vertical"
        size="small"
        requiredMark={false}
        onFinish={(values) => onSave(values.name, values.content, q.data?.content ?? "").catch(() => undefined)}
      >
        {row?.kind === "process" ? (
          <Form.Item label="工艺ID">
            <IdText id={row.id} />
          </Form.Item>
        ) : null}
        <Form.Item name="name" label="显示名" extra="显示名不是身份，改名也不换编号。" rules={[{ required: true, message: "请输入显示名" }]}>
          <Input autoComplete="off" disabled={locked} />
        </Form.Item>
        <Form.Item label="类型">{weldKindLabel(row?.weldKind || WELD_SINGLE)}</Form.Item>
        <Form.Item name="content" label="参数">
          <ContentEditor
            schema={schema}
            disabled={locked}
            weldKind={row?.weldKind || WELD_SINGLE}
            processOptions={row?.kind === "project" ? processPickerOptions(processes, (p) => canPinForProject(row.level, p, meId) && sameWeldKind(p.weldKind, row.weldKind), (row.deps ?? []).map((d) => d.id)) : undefined}
          />
        </Form.Item>
      </Form>
    </Modal>
  );
}
