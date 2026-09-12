import { PlusOutlined } from "@ant-design/icons";
import { App, Button, Card, Descriptions, Empty, Form, Input, Modal, Select, Space, Switch, Table, Tag, Typography, type TableColumnsType } from "antd";
import { useEffect, useMemo, useState, type ReactNode } from "react";
import { errorMessage } from "@/shared/api/client";
import { formatTime } from "@/shared/format";
import { PageHeader } from "@/shared/ui/PageHeader";
import { useDirectory } from "@/features/factories/api";
import { ContentEditor } from "@/features/templates/ContentFields";
import { defaultValue } from "@/features/templates/schema";
import { useTemplate } from "@/features/templates/api";
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
  useSetAssetCopyable,
  useUpdateAssetContent,
  type Asset,
  type AssetKind,
  type CreateAssetInput,
  type PromotableAsset,
} from "./api";

type CreateForm = { name: string; content: string; processIds?: string[] };

function statusLabel(status: string) {
  if (status === "draft") return "未发布";
  if (status === "available") return "可用";
  if (status === "disabled") return "已停用";
  return status;
}

function statusColor(status: string) {
  if (status === "draft") return "gold";
  if (status === "available") return "green";
  return "default";
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
  const template = useTemplate(kind);
  const schema = template.data?.schema ?? null;
  const create = useCreateAsset();
  const copy = useCopyAsset();
  const rename = useRenameAsset();
  const updateContent = useUpdateAssetContent();
  const publish = usePublishAsset();
  const disable = useDisableAsset();
  const enable = useEnableAsset();
  const remove = useDeleteAsset();
  const setCopyable = useSetAssetCopyable();
  const promote = usePromoteFromFactory();
  const directory = useDirectory();
  const { message, modal } = App.useApp();
  const [open, setOpen] = useState(false);
  const [copyFor, setCopyFor] = useState<Asset | null>(null);
  const [promoteOpen, setPromoteOpen] = useState(false);
  const [detailFor, setDetailFor] = useState<Asset | null>(null);
  const [editFor, setEditFor] = useState<Asset | null>(null);
  const [factoryId, setFactoryId] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const [statusFilter, setStatusFilter] = useState("all");
  const [sourceFilter, setSourceFilter] = useState("all");
  const [form] = Form.useForm<CreateForm>();
  const [copyForm] = Form.useForm<{ name: string }>();
  const promotable = usePromotableAssets(promoteOpen ? factoryId : null, kind);

  const factories = useMemo(() => (directory.data?.factories ?? []).filter((f) => (f.status ?? "active") === "active"), [directory.data]);
  const availableProcesses = useMemo(() => (processes.data ?? []).filter((p) => p.status === "available"), [processes.data]);
  const onErr = (e: unknown) => message.error(errorMessage(e));
  const canCopy = (row: Asset) => isProcess && row.copyable && row.status !== "disabled";
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
    return (assets.data ?? []).filter((a) => {
      if (statusFilter !== "all" && a.status !== statusFilter) return false;
      if (sourceFilter !== "all" && (a.sourceFactory || "本端新建") !== sourceFilter) return false;
      if (!needle) return true;
      const source = (a.sourceFactory || "本端新建").toLowerCase();
      return a.name.toLowerCase().includes(needle) || source.includes(needle);
    });
  }, [assets.data, query, statusFilter, sourceFilter]);
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

  const openPromote = () => {
    const online = factories.find((f) => f.channelOnline);
    setFactoryId(online?.id ?? null);
    setPromoteOpen(true);
  };
  const closePromote = () => {
    setPromoteOpen(false);
    setFactoryId(null);
  };

  const columns: TableColumnsType<Asset> = [
    {
      title: isProcess ? "工艺名称" : "工程名称",
      dataIndex: "name",
      ellipsis: true,
      render: (name: string) => <Typography.Text strong>{name}</Typography.Text>,
    },
    { title: "状态", dataIndex: "status", width: 90, render: (s: string) => <Tag color={statusColor(s)}>{statusLabel(s)}</Tag> },
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
    { title: "创建时间", dataIndex: "createdAt", width: 160, render: (v: string) => formatTime(v) },
    {
      title: "操作",
      key: "actions",
      width: 300,
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
          <Space>
            <Button onClick={openPromote}>从工厂升档</Button>
            <Button
              type="primary"
              icon={<PlusOutlined />}
              onClick={() => {
                form.setFieldsValue({ content: schema ? JSON.stringify(defaultValue(schema)) : "" });
                setOpen(true);
              }}
            >
              新建{isProcess ? "工艺" : "工程"}
            </Button>
          </Space>
        }
      />
      <Card>
        <Space wrap style={{ marginBottom: 12 }}>
          <Input.Search allowClear placeholder={`搜索${isProcess ? "工艺" : "工程"}名称或来源厂`} value={query} onChange={(e) => setQuery(e.target.value)} style={{ width: 280 }} />
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
          <Select value={sourceFilter} onChange={setSourceFilter} style={{ width: 200 }} options={sourceOptions} />
        </Space>
        <Table<Asset>
          rowKey="id"
          columns={columns}
          dataSource={rows}
          loading={assets.isLoading}
          scroll={{ x: 960 }}
          pagination={{ pageSize: 20, hideOnSinglePage: true }}
          locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={`还没有${title}。可在本页新建，或从在线工厂升档。`} /> }}
        />
      </Card>
      <Modal title={`新建${isProcess ? "工艺" : "工程"}`} open={open} onCancel={() => setOpen(false)} okText="创建" confirmLoading={create.isPending} destroyOnHidden width={720} styles={{ body: { maxHeight: "50vh", overflow: "auto" } }} onOk={() => form.submit()}>
        <Form<CreateForm>
          form={form}
          layout="vertical"
          size="small"
          requiredMark={false}
          initialValues={{ content: "" }}
          onFinish={(values) => {
            const input: CreateAssetInput = {
              kind,
              name: values.name,
              content: values.content,
              deps: isProcess
                ? undefined
                : (values.processIds ?? []).map((id) => {
                    const p = availableProcesses.find((x) => x.id === id);
                    if (!p) return { id, revision: 0, digest: "" };
                    return { id: p.id, revision: p.revision, digest: p.digest };
                  }),
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
          <Form.Item name="name" label="显示名" extra="显示名不是身份。" rules={[{ required: true, message: "请输入显示名" }]}>
            <Input autoComplete="off" autoFocus />
          </Form.Item>
          {!isProcess ? (
            <Form.Item
              name="processIds"
              label="依赖工艺"
              extra={availableProcesses.length ? "必须已是平台级可用工艺。" : "还没有已发布的平台工艺，请先发布工艺。"}
              rules={[{ required: true, message: "请选择依赖工艺" }]}
            >
              <Select mode="multiple" optionFilterProp="label" options={availableProcesses.map((p) => ({ value: p.id, label: `${p.name} · r${p.revision}` }))} />
            </Form.Item>
          ) : null}
          <Form.Item name="content" label="参数">
            <ContentEditor schema={schema} />
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
      <Modal title="从工厂升档" open={promoteOpen} onCancel={closePromote} footer={null} width={720} destroyOnHidden>
        <Typography.Paragraph type="secondary">向在线工厂要可升档的厂级清单。正文没变则跳过；有变更则覆盖已升档的那条，不另开身份。</Typography.Paragraph>
        <Select
          style={{ width: "100%", marginBottom: 12 }}
          placeholder="选择工厂"
          value={factoryId ?? undefined}
          onChange={(v) => setFactoryId(v)}
          options={factories.map((f) => ({
            value: f.id,
            label: `${f.name}${f.channelOnline ? " · 在线" : " · 离线"}`,
            disabled: !f.channelOnline,
          }))}
        />
        <Table<PromotableAsset>
          rowKey="id"
          size="small"
          loading={promotable.isFetching}
          dataSource={promotable.data ?? []}
          pagination={false}
          locale={{
            emptyText: (
              <Empty
                image={Empty.PRESENTED_IMAGE_SIMPLE}
                description={promotable.isError ? errorMessage(promotable.error) : "该厂没有可升档的已发布厂级，或厂端不在线。"}
              />
            ),
          }}
          columns={[
            { title: isProcess ? "工艺名称" : "工程名称", dataIndex: "name", ellipsis: true },
            { title: "修订", dataIndex: "revision", width: 70 },
            {
              title: "",
              key: "go",
              width: 110,
              render: (_, row) => {
                const plat = (assets.data ?? []).find((a) => a.sourceId === row.id);
                const same = Boolean(plat && plat.digest === row.digest);
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
      <DetailModal row={detailing} processes={processes.data ?? []} schema={schema} onClose={() => setDetailFor(null)} />
      <EditModal
        row={editing}
        schema={schema}
        saving={rename.isPending || updateContent.isPending}
        onClose={() => setEditFor(null)}
        onSave={async (name, content, originalContent) => {
          if (!editing) return;
          try {
            let expected = editing.revision;
            if (name !== editing.name) {
              const next = await rename.mutateAsync({ id: editing.id, expected, name });
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
          editing ? (
            <Space wrap>
              <CopyableSwitch
                checked={editing.copyable}
                disabled={editing.status === "disabled"}
                loading={setCopyable.isPending && setCopyable.variables?.id === editing.id}
                onToggle={(next) => toggleCopyable(editing, next)}
              />
              {editing.status === "draft" ? (
                <Button
                  onClick={() =>
                    modal.confirm({
                      title: `发布「${editing.name}」？`,
                      content: "发布后可被平台级工程依赖，并下到在线工厂。可复制仍可改。",
                      onOk: () =>
                        publish.mutate(
                          { id: editing.id, expected: editing.revision },
                          { onSuccess: () => message.success("已发布"), onError: onErr },
                        ),
                    })
                  }
                >
                  发布
                </Button>
              ) : null}
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

function DetailModal({ row, processes, schema, onClose }: { row: Asset | null; processes: Asset[]; schema: ContentSchema | null; onClose: () => void }) {
  const q = useAssetContent(row?.id ?? null);
  return (
    <Modal title="详情" open={row !== null} onCancel={onClose} footer={null} width={720} styles={{ body: { maxHeight: "50vh", overflow: "auto" } }}>
      {row ? (
        <>
          <Descriptions
            column={1}
            size="small"
            items={[
              { key: "name", label: row.kind === "process" ? "工艺名称" : "工程名称", children: row.name },
              { key: "status", label: "状态", children: <Tag color={statusColor(row.status)}>{statusLabel(row.status)}</Tag> },
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
          {q.isError ? (
            <Typography.Text type="danger">{errorMessage(q.error)}</Typography.Text>
          ) : q.isLoading ? (
            "读取中…"
          ) : (
            <ContentEditor schema={schema} value={q.data?.content ?? ""} disabled />
          )}
        </>
      ) : null}
    </Modal>
  );
}

function EditModal({
  row,
  schema,
  saving,
  extra,
  onClose,
  onSave,
}: {
  row: Asset | null;
  schema: ContentSchema | null;
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
        <Form.Item name="name" label="显示名" extra="显示名不是身份。" rules={[{ required: true, message: "请输入显示名" }]}>
          <Input autoComplete="off" disabled={locked} />
        </Form.Item>
        <Form.Item name="content" label="参数">
          <ContentEditor schema={schema} disabled={locked} />
        </Form.Item>
      </Form>
    </Modal>
  );
}
