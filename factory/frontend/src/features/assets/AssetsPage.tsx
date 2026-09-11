import { PlusOutlined } from "@ant-design/icons";
import { App, Button, Card, Form, Input, Modal, Popconfirm, Radio, Select, Space, Table, Tag, Typography, type TableColumnsType } from "antd";
import { useMemo, useState } from "react";
import { personName, useCatalog, useIsProcessEngineer } from "@/features/catalog/api";
import { errorMessage } from "@/shared/api/client";
import { formatTime } from "@/shared/format";
import { statusColor, statusLabel } from "@/shared/labels";
import { IdText } from "@/shared/ui/IdText";
import { PageHeader } from "@/shared/ui/PageHeader";
import {
  useAssetContent,
  useAssets,
  useAuthorContext,
  useCreateAsset,
  useDisableAsset,
  useExportSnapshot,
  usePromoteAsset,
  usePublishAsset,
  useRenameAsset,
  useSetAssetCopyable,
  useUpdateAssetContent,
  type Asset,
  type AssetKind,
  type AssetLevel,
  type CreateAssetInput,
} from "./api";

type CreateForm = {
  level: AssetLevel;
  placement: string;
  name: string;
  content: string;
  processIds?: string[];
};

function levelLabel(level: string) {
  if (level === "factory") return "厂级";
  if (level === "personal") return "个人级";
  return level;
}

function pathText(row: Asset) {
  if (!row.orgPath?.length) return "工厂直属";
  return row.orgPath.map((n) => n.name).join(" / ");
}

// 本厂工艺或工程：工艺工程师制作与升档；超管只看元数据，打不开个人正文。
export function AssetsPage({ kind }: { kind: AssetKind }) {
  const isProcess = kind === "process";
  const title = isProcess ? "工艺" : "工程";
  const catalog = useCatalog();
  const isPE = useIsProcessEngineer();
  const assets = useAssets(kind);
  const processes = useAssets("process");
  const author = useAuthorContext();
  const create = useCreateAsset();
  const rename = useRenameAsset();
  const updateContent = useUpdateAssetContent();
  const setCopyable = useSetAssetCopyable();
  const publish = usePublishAsset();
  const disable = useDisableAsset();
  const promote = usePromoteAsset();
  const exportSnap = useExportSnapshot();
  const { message } = App.useApp();
  const meId = catalog.data?.me.id;
  const [levelFilter, setLevelFilter] = useState<"all" | AssetLevel>("all");
  const [open, setOpen] = useState(false);
  const [renameFor, setRenameFor] = useState<Asset | null>(null);
  const [contentFor, setContentFor] = useState<Asset | null>(null);
  const [viewFor, setViewFor] = useState<string | null>(null);
  const [snapText, setSnapText] = useState<string | null>(null);
  const [form] = Form.useForm<CreateForm>();
  const [renameForm] = Form.useForm<{ name: string }>();
  const [contentForm] = Form.useForm<{ content: string }>();
  const createLevel = Form.useWatch("level", form) as AssetLevel | undefined;

  const rows = useMemo(() => {
    const all = assets.data ?? [];
    if (levelFilter === "all") return all;
    return all.filter((a) => a.level === levelFilter);
  }, [assets.data, levelFilter]);

  const availableProcesses = useMemo(() => {
    const all = processes.data ?? [];
    if (createLevel === "personal") {
      return all.filter((p) => p.status === "available" && (p.level === "factory" || p.creatorId === meId));
    }
    return all.filter((p) => p.status === "available" && p.level === "factory");
  }, [processes.data, createLevel, meId]);

  const canMutate = (row: Asset) => isPE && row.status !== "disabled" && (row.level === "factory" || row.creatorId === meId);
  const canRead = (row: Asset) => row.level === "factory" || row.creatorId === meId;
  const onErr = (e: unknown) => message.error(errorMessage(e));

  const columns: TableColumnsType<Asset> = [
    { title: "显示名", dataIndex: "name" },
    {
      title: "级别",
      dataIndex: "level",
      width: 90,
      render: (l: string) => <Tag color={l === "factory" ? "blue" : "purple"}>{levelLabel(l)}</Tag>,
    },
    { title: "状态", dataIndex: "status", width: 90, render: (s: string) => <Tag color={statusColor(s)}>{statusLabel(s)}</Tag> },
    { title: "可升档", dataIndex: "copyable", width: 80, render: (ok: boolean) => (ok ? <Tag color="green">可</Tag> : <Tag>否</Tag>) },
    { title: "修订", dataIndex: "revision", width: 70 },
    { title: "创建人", dataIndex: "creatorId", render: (id: string) => personName(catalog.data, id) },
    { title: "工作位置", key: "path", render: (_, row) => pathText(row) },
    { title: "身份", dataIndex: "id", width: 160, render: (id: string) => <IdText id={id} /> },
    { title: "更新", dataIndex: "updatedAt", width: 160, render: (v: string) => formatTime(v) },
    {
      title: "操作",
      key: "actions",
      width: 280,
      render: (_, row) => (
        <Space size={4} wrap>
          {canRead(row) ? (
            <Button size="small" onClick={() => setViewFor(row.id)}>
              正文
            </Button>
          ) : null}
          {canMutate(row) ? (
            <Button size="small" onClick={() => setRenameFor(row)}>
              改名
            </Button>
          ) : null}
          {canMutate(row) ? (
            <Button size="small" onClick={() => setContentFor(row)}>
              改正文
            </Button>
          ) : null}
          {canMutate(row) && row.status === "draft" ? (
            <Popconfirm
              title={`发布「${row.name}」？`}
              description="发布后可被依赖和升档；可复制只能再收紧。"
              onConfirm={() => publish.mutate({ id: row.id, expected: row.revision }, { onSuccess: () => message.success("已发布"), onError: onErr })}
            >
              <Button size="small" type="primary">
                发布
              </Button>
            </Popconfirm>
          ) : null}
          {canMutate(row) && row.status === "available" ? (
            <Popconfirm
              title={`停用「${row.name}」？`}
              description="停用后不能再改、不能升档、不能被新工程依赖；不能改回可用。"
              onConfirm={() => disable.mutate({ id: row.id, expected: row.revision }, { onSuccess: () => message.success("已停用"), onError: onErr })}
            >
              <Button size="small" danger>
                停用
              </Button>
            </Popconfirm>
          ) : null}
          {canMutate(row) && row.copyable ? (
            <Popconfirm
              title="改为不可复制？"
              description="不可复制后不能升档；可用后不能再改回可复制。"
              onConfirm={() => setCopyable.mutate({ id: row.id, expected: row.revision, copyable: false }, { onSuccess: () => message.success("已收紧"), onError: onErr })}
            >
              <Button size="small">禁止升档</Button>
            </Popconfirm>
          ) : null}
          {isPE && row.level === "personal" && row.status === "available" && row.copyable ? (
            <Popconfirm
              title="升档为厂级？"
              description="复制出新厂级，个人原件不动；升档响应不含个人正文。"
              onConfirm={() => promote.mutate(row.id, { onSuccess: () => message.success("已升档为厂级"), onError: onErr })}
            >
              <Button size="small">升厂级</Button>
            </Popconfirm>
          ) : null}
          {isPE && row.level === "factory" && row.status === "available" && row.copyable ? (
            <Button
              size="small"
              onClick={() =>
                exportSnap.mutate(row.id, {
                  onSuccess: (snap) => setSnapText(JSON.stringify(snap, null, 2)),
                  onError: onErr,
                })
              }
            >
              升平台快照
            </Button>
          ) : null}
        </Space>
      ),
    },
  ];

  const placements = useMemo(() => {
    const opts: { value: string; label: string }[] = [];
    if (author.data?.allowDirect) opts.push({ value: "direct", label: "工厂直属" });
    for (const u of author.data?.orgUnits ?? []) opts.push({ value: u.id, label: u.name });
    return opts;
  }, [author.data]);

  return (
    <>
      <PageHeader
        title={title}
        description={
          isProcess
            ? "厂级由工艺工程师制作；个人级只有创建人能打开正文。升档复制新条目，不改原件。"
            : "工程钉死所依赖工艺的身份和修订；升档工程不会另拆出工艺。"
        }
        extra={
          isPE ? (
            <Button type="primary" icon={<PlusOutlined />} onClick={() => setOpen(true)}>
              新建{title}
            </Button>
          ) : null
        }
      />
      <Card>
        <Radio.Group value={levelFilter} onChange={(e) => setLevelFilter(e.target.value)} style={{ marginBottom: 12 }}>
          <Radio.Button value="all">全部</Radio.Button>
          <Radio.Button value="factory">厂级</Radio.Button>
          <Radio.Button value="personal">个人级</Radio.Button>
        </Radio.Group>
        <Table<Asset> rowKey="id" columns={columns} dataSource={rows} loading={assets.isLoading} pagination={{ pageSize: 20, hideOnSinglePage: true }} scroll={{ x: 1400 }} />
      </Card>
      <Modal title={`新建${title}`} open={open} onCancel={() => setOpen(false)} okText="创建" confirmLoading={create.isPending} destroyOnHidden onOk={() => form.submit()}>
        <Form<CreateForm>
          form={form}
          layout="vertical"
          requiredMark={false}
          initialValues={{ level: "factory", placement: placements[0]?.value, content: "" }}
          onFinish={(values) => {
            const direct = values.placement === "direct";
            const deps = isProcess
              ? undefined
              : (values.processIds ?? []).map((id) => {
                  const p = availableProcesses.find((x) => x.id === id);
                  if (!p) return { id, revision: 0, digest: "" };
                  return { id: p.id, revision: p.revision, digest: p.digest };
                });
            const input: CreateAssetInput = {
              kind,
              level: values.level,
              name: values.name,
              content: values.content,
              direct,
              orgUnitId: direct ? null : values.placement,
              deps,
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
          <Form.Item name="level" label="级别" extra="个人级只有你能打开正文；厂级由本厂工艺工程师维护。">
            <Radio.Group>
              <Radio value="factory">厂级</Radio>
              <Radio value="personal">个人级</Radio>
            </Radio.Group>
          </Form.Item>
          <Form.Item name="placement" label="工作位置" extra="须覆盖你的工艺工程师作用域，且直属须整厂作用域。" rules={[{ required: true, message: "请选择工作位置" }]}>
            <Select options={placements} placeholder={placements.length ? "选择位置" : "没有可写的工作位置，请先授予工艺工程师并分配组织"} />
          </Form.Item>
          <Form.Item name="name" label="显示名" extra="显示名不是身份，改名也不换编号。" rules={[{ required: true, message: "请输入显示名" }]}>
            <Input autoComplete="off" autoFocus />
          </Form.Item>
          {!isProcess ? (
            <Form.Item name="processIds" label="依赖工艺" extra="必须是已发布且你有权使用的工艺；会钉死当前修订。" rules={[{ required: true, message: "请选择依赖工艺" }]}>
              <Select mode="multiple" optionFilterProp="label" options={availableProcesses.map((p) => ({ value: p.id, label: `${p.name} · ${levelLabel(p.level)} · r${p.revision}` }))} />
            </Form.Item>
          ) : null}
          <Form.Item name="content" label="正文">
            <Input.TextArea rows={6} />
          </Form.Item>
        </Form>
      </Modal>
      <Modal title="改显示名" open={renameFor !== null} onCancel={() => setRenameFor(null)} okText="保存" confirmLoading={rename.isPending} destroyOnHidden onOk={() => renameForm.submit()}>
        <Form<{ name: string }>
          key={renameFor?.id}
          form={renameForm}
          layout="vertical"
          requiredMark={false}
          initialValues={{ name: renameFor?.name }}
          onFinish={(values) => {
            if (!renameFor) return;
            rename.mutate(
              { id: renameFor.id, expected: renameFor.revision, name: values.name },
              {
                onSuccess: () => {
                  message.success("已改名");
                  setRenameFor(null);
                },
                onError: onErr,
              },
            );
          }}
        >
          <Form.Item name="name" label="显示名" rules={[{ required: true, message: "请输入显示名" }]}>
            <Input autoComplete="off" />
          </Form.Item>
        </Form>
      </Modal>
      <Modal title="改正文" open={contentFor !== null} onCancel={() => setContentFor(null)} okText="保存" confirmLoading={updateContent.isPending} destroyOnHidden onOk={() => contentForm.submit()}>
        <Form<{ content: string }>
          form={contentForm}
          layout="vertical"
          requiredMark={false}
          onFinish={(values) => {
            if (!contentFor) return;
            updateContent.mutate(
              { id: contentFor.id, expected: contentFor.revision, content: values.content },
              {
                onSuccess: () => {
                  message.success("已改正文");
                  setContentFor(null);
                },
                onError: onErr,
              },
            );
          }}
        >
          <Form.Item name="content" label="正文">
            <Input.TextArea rows={8} />
          </Form.Item>
        </Form>
      </Modal>
      <ContentModal id={viewFor} onClose={() => setViewFor(null)} />
      <Modal title="升平台快照" open={snapText !== null} onCancel={() => setSnapText(null)} footer={null} width={720}>
        <Typography.Paragraph type="secondary">交给 WAN 管理员粘贴升档；原厂级仍只在本厂。</Typography.Paragraph>
        <Typography.Paragraph copyable={{ text: snapText ?? "", tooltips: ["复制快照", "已复制"] }}>
          <pre style={{ maxHeight: 360, overflow: "auto" }}>{snapText}</pre>
        </Typography.Paragraph>
      </Modal>
    </>
  );
}

function ContentModal({ id, onClose }: { id: string | null; onClose: () => void }) {
  const q = useAssetContent(id);
  return (
    <Modal title="正文" open={id !== null} onCancel={onClose} footer={null} width={640}>
      {q.isError ? (
        <Typography.Text type="danger">{errorMessage(q.error)}</Typography.Text>
      ) : (
        <pre style={{ maxHeight: 360, overflow: "auto", whiteSpace: "pre-wrap" }}>{q.data?.content ?? (q.isLoading ? "读取中…" : "")}</pre>
      )}
    </Modal>
  );
}
