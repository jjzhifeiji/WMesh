import { FolderOutlined, PlusOutlined } from "@ant-design/icons";
import { App, Button, Card, Descriptions, Empty, Form, Input, Modal, Radio, Select, Space, Switch, Table, Tag, Typography, type TableColumnsType } from "antd";
import { useEffect, useMemo, useState, type ReactNode } from "react";
import { errorMessage } from "@/shared/api/client";
import { formatTime } from "@/shared/format";
import { IdText } from "@/shared/ui/IdText";
import { PageHeader } from "@/shared/ui/PageHeader";
import { useDirectory } from "@/features/factories/api";
import { ContentEditor } from "@/features/templates/ContentFields";
import { collectProcessIds, defaultValue, projectContentSchema } from "@/features/templates/schema";
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
  usePromotableAssets,
  usePublishAsset,
  useRenameAsset,
  useCreateFSFolder,
  useFS,
  useSetAssetCopyable,
  useSetAssetDeps,
  useUpdateAssetContent,
  type Asset,
  type AssetDep,
  type AssetKind,
  type CreateAssetInput,
  type FSNode,
  type PromotableAsset,
} from "./api";
import { fsFolderOptions, ProcessExplorer } from "./ProcessExplorer";

type CreateForm = { name: string; content: string; weldKind: string; copyable: boolean; parentId?: string };
type MkdirForm = { parentId: string; name: string };

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

function levelLabel(level: string) {
  if (level === "factory") return "厂级";
  if (level === "personal") return "个人级";
  if (level === "platform") return "平台级";
  return level;
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
  const isProcess = kind === "process";
  const title = isProcess ? "平台工艺" : "平台工程";
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
  const publish = usePublishAsset();
  const disable = useDisableAsset();
  const enable = useEnableAsset();
  const remove = useDeleteAsset();
  const setCopyable = useSetAssetCopyable();
  const setAssetDeps = useSetAssetDeps();
  const promote = usePromoteFromFactory();
  const directory = useDirectory();
  const { message, modal } = App.useApp();
  const [open, setOpen] = useState(false);
  const [copyFor, setCopyFor] = useState<Asset | null>(null);
  const [promoteOpen, setPromoteOpen] = useState(false);
  const [detailFor, setDetailFor] = useState<Asset | null>(null);
  const [editFor, setEditFor] = useState<Asset | null>(null);
  const [factoryId, setFactoryId] = useState<string | null>(null);
  const [browseFactory, setBrowseFactory] = useState<string | null>(null);
  const [mkdirOpen, setMkdirOpen] = useState(false);
  const [promoteQuery, setPromoteQuery] = useState("");
  const [promotePage, setPromotePage] = useState(1);
  const [promotePageSize, setPromotePageSize] = useState(10);
  const [query, setQuery] = useState("");
  const [fsQuery, setFsQuery] = useState("");
  const [statusFilter, setStatusFilter] = useState("all");
  const [sourceFilter, setSourceFilter] = useState("all");
  const [weldFilter, setWeldFilter] = useState("all");
  const [fsWeld, setFsWeld] = useState("all");
  const [sortKey, setSortKey] = useState<"name" | "code" | "createdAt">("createdAt");
  const [sortAsc, setSortAsc] = useState(false);
  const [form] = Form.useForm<CreateForm>();
  const [mkdirForm] = Form.useForm<MkdirForm>();
  const [copyForm] = Form.useForm<{ name: string }>();
  const createWeld = Form.useWatch("weldKind", form) ?? WELD_SINGLE;
  const createSchema = useMemo(() => {
    if (isProcess) return schema;
    const rows = projectTemplatesForWeld(projectTpls.data, createWeld);
    return rows.length ? projectContentSchema(rows) : schema;
  }, [isProcess, schema, projectTpls.data, createWeld]);
  const promotable = usePromotableAssets(promoteOpen ? factoryId : null, kind);

  const factories = useMemo(() => (directory.data?.factories ?? []).filter((f) => (f.status ?? "active") === "active"), [directory.data]);
  const folderOpts = useMemo(() => fsFolderOptions(fs.data ?? [], (n) => n.treeLevel === "platform"), [fs.data]);
  const availableProcesses = useMemo(() => (processes.data ?? []).filter((p) => p.status === "available"), [processes.data]);
  const onErr = (e: unknown) => message.error(errorMessage(e));
  const canCopy = (row: Asset) => row.status !== "disabled";
  useEffect(() => {
    if (copyFor) copyForm.setFieldsValue({ name: `${copyFor.name}-副本` });
  }, [copyFor, copyForm]);
  const toggleCopyable = (row: Asset, copyable: boolean) => {
    if (row.copyable === copyable) return;
    setCopyable.mutate(
      { id: row.id, expected: row.revision, copyable },
      { onSuccess: () => message.success(copyable ? "已设为可复制" : "已设为不可复制"), onError: onErr },
    );
  };
  const rows = useMemo(() => {
    const needle = query.trim().toLowerCase();
    const filtered = (assets.data ?? []).filter((a) => {
      if (statusFilter !== "all" && a.status !== statusFilter) return false;
      if (weldFilter !== "all" && (a.weldKind || WELD_SINGLE) !== weldFilter) return false;
      if (sourceFilter !== "all" && (a.sourceFactory || "本端新建") !== sourceFilter) return false;
      if (!needle) return true;
      if (a.code === query.trim() || a.code === query.trim().toUpperCase()) return true;
      const source = (a.sourceFactory || "本端新建").toLowerCase();
      return a.name.toLowerCase().includes(needle) || source.includes(needle);
    });
    const collator = new Intl.Collator("zh");
    return [...filtered].sort((a, b) => {
      let cmp = 0;
      if (sortKey === "name") cmp = collator.compare(a.name || "", b.name || "");
      else if (sortKey === "code") cmp = (a.code || "").localeCompare(b.code || "", "zh", { numeric: true });
      else cmp = Date.parse(a.createdAt || "") - Date.parse(b.createdAt || "");
      return sortAsc ? cmp : -cmp;
    });
  }, [assets.data, query, statusFilter, sourceFilter, weldFilter, sortKey, sortAsc]);
  const sourceOptions = useMemo(() => {
    const opts = [{ value: "all", label: "全部来源" }];
    const seen = new Set<string>();
    for (const a of assets.data ?? []) {
      const name = a.sourceFactory || "本端新建";
      if (seen.has(name)) continue;
      seen.add(name);
      opts.push({ value: name, label: name });
    }
    return opts;
  }, [assets.data]);

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

  const openCreate = (parentId?: string) => {
    const tplRows = projectTemplatesForWeld(projectTpls.data, WELD_SINGLE);
    const next = isProcess ? schema : tplRows.length ? projectContentSchema(tplRows) : schema;
    form.setFieldsValue({
      content: next ? JSON.stringify(defaultValue(next)) : "",
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
    setPromoteQuery("");
    setPromotePage(1);
    setPromotePageSize(10);
  };
  const pickFactory = (id: string) => {
    setFactoryId(id);
    setPromoteQuery("");
    setPromotePage(1);
  };
  const promoteRows = useMemo(() => {
    const needle = promoteQuery.trim().toLowerCase();
    const exact = promoteQuery.trim();
    return (promotable.data ?? []).filter((a) => {
      if (!needle) return true;
      if (a.code && (a.code === exact || a.code === exact.toUpperCase())) return true;
      return a.name.toLowerCase().includes(needle) || (a.code ?? "").toLowerCase().includes(needle);
    });
  }, [promotable.data, promoteQuery]);

  const columns: TableColumnsType<Asset> = [
    {
      title: isProcess ? "工艺名称" : "工程名称",
      dataIndex: "name",
      width: 180,
      ellipsis: true,
      sorter: true,
      sortOrder: sortOrder("name"),
      render: (name: string) => <Typography.Text strong>{name}</Typography.Text>,
    },
    { title: "编号", dataIndex: "code", width: 150, sorter: true, sortOrder: sortOrder("code"), render: (code: string) => <Typography.Text copyable={{ text: code }}>{code}</Typography.Text> },
    { title: "状态", dataIndex: "status", width: 90, render: (s: string) => <Tag color={statusColor(s)}>{statusLabel(s)}</Tag> },
    { title: "类型", dataIndex: "weldKind", width: 100, render: (v: string) => weldKindLabel(v || WELD_SINGLE) },
    { title: "可复制", dataIndex: "copyable", width: 80, render: (ok: boolean) => (ok ? "是" : "否") },
    { title: "修订", dataIndex: "revision", width: 70 },
    { title: "来源厂", dataIndex: "sourceFactory", width: 180, ellipsis: true, render: (name: string) => name || "本端新建" },
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
      width: 340,
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
          <Button size="small" type="primary" onClick={() => setEditFor(row)}>
            编辑
          </Button>
          {row.status === "draft" ? (
            <Button
              size="small"
              onClick={() =>
                modal.confirm({
                  title: `发布「${row.name}」？`,
                  content: "发布后可被平台级工程依赖，并下到在线工厂。可复制仍可改。",
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
          {row.status === "available" ? (
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
          {row.status === "disabled" ? (
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
        </Space>
      ),
    },
  ];

  return (
    <>
      <PageHeader
        title={title}
        description={isProcess ? "只做平台级。厂级原件仍在各厂；正文没变则跳过升档，有变更则覆盖。" : "只能依赖已发布的平台级工艺；升档工程不会另生成工艺。"}
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
            {isProcess && !browseFactory ? (
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
                ...factories.map((f) => ({ value: f.id, label: `看厂目录：${f.name}` })),
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
              if (!browseFactory && n.asset) setDetailFor(n.asset);
            }}
            listRow={processListRow}
            detail={
              browseFactory ? (
                <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="云端只看各厂目录结构，不打开厂级/个人正文。" />
              ) : detailing ? (
                <FileDetail row={detailing} schema={schema}>
                  <Space wrap>
                    <Button size="small" type="primary" onClick={() => setEditFor(detailing)}>
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
      <Card>
        <Space wrap style={{ marginBottom: 12 }}>
          <Input.Search allowClear placeholder={`搜索${isProcess ? "工艺" : "工程"}名称、编号或来源厂`} value={query} onChange={(e) => setQuery(e.target.value)} style={{ width: 280 }} />
          <Select
            value={statusFilter}
            onChange={setStatusFilter}
            style={{ width: 132 }}
            options={[
              { value: "all", label: "全部状态" },
              { value: "draft", label: "未发布" },
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
          <Select value={sourceFilter} onChange={setSourceFilter} style={{ width: 200 }} options={sourceOptions} />
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
          scroll={{ x: 1200 }}
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
          locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={`还没有${title}。可在本页新建，或从在线工厂升档。`} /> }}
        />
      </Card>
      )}
      <Modal title={`新建${isProcess ? "工艺" : "工程"}`} open={open} onCancel={() => setOpen(false)} okText="创建" confirmLoading={create.isPending} destroyOnHidden width={720} styles={{ body: { maxHeight: "50vh", overflow: "auto" } }} onOk={() => form.submit()}>
        <Form<CreateForm>
          form={form}
          layout="vertical"
          size="small"
          requiredMark={false}
          initialValues={{ content: "", weldKind: WELD_SINGLE, copyable: false }}
          onValuesChange={(changed) => {
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
            <Form.Item name="parentId" label="文件夹" extra="工艺会放到这个目录下。" rules={[{ required: true, message: "请选择文件夹" }]}>
              <Select showSearch optionFilterProp="label" options={folderOpts} placeholder="选择文件夹" />
            </Form.Item>
          ) : null}
          <Form.Item name="name" label="显示名" extra="显示名不是身份。" rules={[{ required: true, message: "请输入显示名" }]}>
            <Input autoComplete="off" autoFocus />
          </Form.Item>
          {isProcess ? (
            <Form.Item name="copyable" label="可复制" extra="否则厂端看不到正文。默认否。" valuePropName="checked">
              <Switch size="default" checkedChildren="可复制" unCheckedChildren="不可复制" />
            </Form.Item>
          ) : null}
          <Form.Item name="weldKind" label="类型" extra="创建后不能改。App 同类型工作流才能打开。" rules={[{ required: true, message: "请选择类型" }]}>
            <Select options={WELD_KINDS.map((k) => ({ value: k.value, label: k.label }))} />
          </Form.Item>
          <Form.Item name="content" label="参数">
            <ContentEditor schema={createSchema} weldKind={createWeld} processOptions={isProcess ? undefined : processPickerOptions(processes.data ?? [], (p) => p.status === "available" && sameWeldKind(p.weldKind, createWeld))} />
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
      <Modal title="从工厂升档" open={promoteOpen} onCancel={closePromote} footer={null} width={720} destroyOnHidden>
        <Typography.Paragraph type="secondary">
          列出该厂全部{isProcess ? "工艺" : "工程"}：厂级、个人级、已下发的平台级，不分状态。正文没变则跳过；有变更则覆盖已升档的那条，不另开身份。
        </Typography.Paragraph>
        <Select
          style={{ width: "100%", marginBottom: 12 }}
          placeholder="选择工厂"
          value={factoryId ?? undefined}
          onChange={pickFactory}
          options={factories.map((f) => ({
            value: f.id,
            label: `${f.name}${f.channelOnline ? " · 在线" : " · 离线"}`,
            disabled: !f.channelOnline,
          }))}
        />
        <Input.Search
          allowClear
          placeholder={`搜索${isProcess ? "工艺" : "工程"}名称或编号`}
          value={promoteQuery}
          onChange={(e) => {
            setPromoteQuery(e.target.value);
            setPromotePage(1);
          }}
          style={{ width: "100%", marginBottom: 12 }}
        />
        <Table<PromotableAsset>
          rowKey="id"
          size="small"
          loading={promotable.isFetching}
          dataSource={promoteRows}
          pagination={{
            current: promotePage,
            pageSize: promotePageSize,
            total: promoteRows.length,
            showSizeChanger: true,
            pageSizeOptions: [10, 20, 50],
            showTotal: (n) => `共 ${n} 条`,
            onChange: (page, size) => {
              setPromotePage(page);
              setPromotePageSize(size);
            },
          }}
          locale={{
            emptyText: (
              <Empty
                image={Empty.PRESENTED_IMAGE_SIMPLE}
                description={
                  promotable.isError
                    ? errorMessage(promotable.error)
                    : promoteQuery.trim()
                      ? `没有匹配的${isProcess ? "工艺" : "工程"}。`
                      : `该厂没有${isProcess ? "工艺" : "工程"}，或厂端不在线。`
                }
              />
            ),
          }}
          columns={[
            { title: isProcess ? "工艺名称" : "工程名称", dataIndex: "name", ellipsis: true, sorter: (a, b) => a.name.localeCompare(b.name, "zh") },
            { title: "编号", dataIndex: "code", width: 150, sorter: (a, b) => (a.code || "").localeCompare(b.code || "", "zh", { numeric: true }) },
            {
              title: "级别",
              dataIndex: "level",
              width: 80,
              render: (l: string) => <Tag color={l === "factory" ? "blue" : l === "platform" ? "cyan" : "purple"}>{levelLabel(l)}</Tag>,
            },
            {
              title: "类型",
              dataIndex: "weldKind",
              width: 100,
              render: (v: string) => weldKindLabel(v || WELD_SINGLE),
            },
            {
              title: "状态",
              dataIndex: "status",
              width: 90,
              render: (s: string) => <Tag color={statusColor(s)}>{statusLabel(s)}</Tag>,
            },
            { title: "修订", dataIndex: "revision", width: 70 },
            {
              title: "",
              key: "go",
              width: 110,
              render: (_, row) => {
                const plat = (assets.data ?? []).find((a) => a.id === row.id || a.sourceId === row.id);
                const same = Boolean(plat && plat.digest === row.digest);
                if (row.level === "platform") {
                  return (
                    <Button size="small" disabled>
                      {plat ? (same ? "已是最新" : "已在云端") : "已在云端"}
                    </Button>
                  );
                }
                return (
                  <Button
                    size="small"
                    type="primary"
                    loading={promote.isPending}
                    onClick={() => {
                      if (!factoryId) return;
                      promote.mutate(
                        { factoryId, assetId: row.id },
                        {
                          onSuccess: () => {
                            if (same) message.success("正文未变，未重复升档");
                            else if (plat) message.success("已覆盖，未发布");
                            else message.success("已升档，未发布");
                            closePromote();
                          },
                          onError: onErr,
                        },
                      );
                    }}
                  >
                    {same ? "已是最新" : plat ? "覆盖" : "升档"}
                  </Button>
                );
              },
            },
          ]}
        />
      </Modal>
      <DetailModal row={isProcess ? null : detailing} processes={processes.data ?? []} schema={isProcess ? schema : detailSchema} onClose={() => setDetailFor(null)} />
      <EditModal
        row={editing}
        schema={editSchema}
        processes={processes.data ?? []}
        saving={rename.isPending || updateContent.isPending || setAssetDeps.isPending}
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
              const nextIds = uniqueIds(processIdsFromContent(content, schema));
              const added = nextIds.filter((id) => !oldIds.includes(id));
              const toDep = (id: string): AssetDep => {
                const pinned = (editing.deps ?? []).find((d) => d.id === id);
                if (pinned) return pinned;
                const p = availableProcesses.find((x) => x.id === id);
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
}: {
  row: Asset;
  schema: ContentSchema | null;
  processOptions?: { value: string; label: string; disabled?: boolean }[];
}) {
  const q = useAssetContent(row.id);
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

function FileDetail({ row, schema, children }: { row: Asset; schema: ContentSchema | null; children?: ReactNode }) {
  return (
    <Space direction="vertical" size={12} style={{ width: "100%" }}>
      <Typography.Title level={5} style={{ margin: 0 }}>
        {row.name}
      </Typography.Title>
      <Descriptions size="small" column={1}>
        <Descriptions.Item label="编号">
          <Typography.Text copyable={{ text: row.code }}>{row.code}</Typography.Text>
        </Descriptions.Item>
        <Descriptions.Item label="工艺ID">
          <IdText id={row.id} />
        </Descriptions.Item>
        <Descriptions.Item label="状态">
          <Tag color={statusColor(row.status)}>{statusLabel(row.status)}</Tag>
        </Descriptions.Item>
        <Descriptions.Item label="类型">{weldKindLabel(row.weldKind || WELD_SINGLE)}</Descriptions.Item>
        <Descriptions.Item label="可复制">{row.copyable ? "是" : "否"}</Descriptions.Item>
        <Descriptions.Item label="修订">{row.revision}</Descriptions.Item>
        <Descriptions.Item label="来源厂">{row.sourceFactory || "本端新建"}</Descriptions.Item>
        <Descriptions.Item label="创建时间">{formatTime(row.createdAt)}</Descriptions.Item>
        <Descriptions.Item label="更新">{formatTime(row.updatedAt)}</Descriptions.Item>
      </Descriptions>
      {children}
      <Typography.Paragraph type="secondary" style={{ marginBottom: 0 }}>
        参数
      </Typography.Paragraph>
      <div className="fs-detail-params">
        <ParamsView row={row} schema={schema} />
      </div>
    </Space>
  );
}

function DetailModal({ row, processes, schema, onClose }: { row: Asset | null; processes: Asset[]; schema: ContentSchema | null; onClose: () => void }) {
  return (
    <Modal title="详情" open={row !== null} onCancel={onClose} footer={null} width={720} styles={{ body: { maxHeight: "50vh", overflow: "auto" } }}>
      {row ? (
        <>
          <Descriptions
            column={1}
            size="small"
            items={[
              { key: "name", label: row.kind === "process" ? "工艺名称" : "工程名称", children: row.name },
              { key: "code", label: "编号", children: <Typography.Text copyable={{ text: row.code }}>{row.code}</Typography.Text> },
              ...(row.kind === "process" ? [{ key: "id", label: "工艺ID", children: <IdText id={row.id} /> }] : []),
              { key: "status", label: "状态", children: <Tag color={statusColor(row.status)}>{statusLabel(row.status)}</Tag> },
              { key: "weldKind", label: "类型", children: weldKindLabel(row.weldKind || WELD_SINGLE) },
              { key: "copyable", label: "可复制", children: row.copyable ? "是" : "否" },
              { key: "revision", label: "修订", children: row.revision },
              { key: "source", label: "来源厂", children: row.sourceFactory || "本端新建" },
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

function processPickerOptions(all: Asset[], pin: (p: Asset) => boolean, selected?: string[]) {
  const seen = new Set<string>();
  const selectedSet = new Set(selected ?? []);
  const opts: { value: string; label: string; disabled?: boolean }[] = [];
  for (const p of all) {
    seen.add(p.id);
    const ok = pin(p);
    if (!ok && !selectedSet.has(p.id)) continue;
    const extra = ok ? "" : ` · ${statusLabel(p.status)}`;
    opts.push({ value: p.id, label: `${p.name} · r${p.revision}${extra}`, disabled: !ok });
  }
  for (const id of selected ?? []) {
    if (seen.has(id)) continue;
    opts.push({ value: id, label: id, disabled: true });
  }
  return opts;
}

function processSelectOptions(selected: string[] | undefined, pickable: Asset[], catalog: Asset[] = pickable) {
  const byId = new Map(catalog.map((p) => [p.id, p]));
  const opts = pickable.map((p) => ({ value: p.id, label: `${p.name} · r${p.revision}` }));
  const seen = new Set(pickable.map((p) => p.id));
  for (const id of selected ?? []) {
    if (seen.has(id)) continue;
    const p = byId.get(id);
    opts.push({ value: id, label: p ? `${p.name} · r${p.revision}` : id });
    seen.add(id);
  }
  return opts;
}

function EditModal({
  row,
  schema,
  processes,
  saving,
  extra,
  onClose,
  onSave,
}: {
  row: Asset | null;
  schema: ContentSchema | null;
  processes: Asset[];
  saving: boolean;
  extra: ReactNode;
  onClose: () => void;
  onSave: (name: string, content: string, originalContent: string) => Promise<void>;
}) {
  const q = useAssetContent(row?.id ?? null);
  const [form] = Form.useForm<{ name: string; content: string }>();
  const locked = row?.status === "disabled";
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
      {q.isError ? <Typography.Text type="danger">{errorMessage(q.error)}</Typography.Text> : null}
      {extra ? <div style={{ marginBottom: 8 }}>{extra}</div> : null}
      <Form<{ name: string; content: string }>
        form={form}
        layout="vertical"
        size="small"
        requiredMark={false}
        onFinish={(values) =>
          onSave(values.name, values.content, q.data?.content ?? "").catch(() => undefined)
        }
      >
        {row?.kind === "process" ? (
          <Form.Item label="工艺ID">
            <IdText id={row.id} />
          </Form.Item>
        ) : null}
        <Form.Item name="name" label="显示名" extra="显示名不是身份。" rules={[{ required: true, message: "请输入显示名" }]}>
          <Input autoComplete="off" disabled={locked} />
        </Form.Item>
        <Form.Item label="类型">{weldKindLabel(row?.weldKind || WELD_SINGLE)}</Form.Item>
        <Form.Item name="content" label="参数">
          <ContentEditor
            schema={schema}
            disabled={locked}
            weldKind={row?.weldKind || WELD_SINGLE}
            processOptions={row?.kind === "project" ? processPickerOptions(processes, (p) => p.status === "available" && sameWeldKind(p.weldKind, row.weldKind), (row.deps ?? []).map((d) => d.id)) : undefined}
          />
        </Form.Item>
      </Form>
    </Modal>
  );
}
