import { PlusOutlined } from "@ant-design/icons";
import { App, Button, Card, Form, Input, Modal, Popconfirm, Select, Space, Table, Tag, Typography, type TableColumnsType } from "antd";
import { useMemo, useState } from "react";
import { errorMessage } from "@/shared/api/client";
import { formatTime } from "@/shared/format";
import { IdText } from "@/shared/ui/IdText";
import { PageHeader } from "@/shared/ui/PageHeader";
import { useAssetContent, useAssets, useCreateAsset, usePromoteSnapshot, usePublishAsset, useRenameAsset, type Asset, type AssetKind, type CreateAssetInput } from "./api";

type CreateForm = { name: string; content: string; processIds?: string[] };

function statusLabel(status: string) {
  if (status === "draft") return "草稿";
  if (status === "available") return "可用";
  if (status === "disabled") return "已停用";
  return status;
}

function statusColor(status: string) {
  if (status === "draft") return "gold";
  if (status === "available") return "green";
  return "default";
}

// 平台级工艺或工程：只在 WAN 维护；不能改厂库原件，升档只收厂端快照。
export function AssetsPage({ kind }: { kind: AssetKind }) {
  const isProcess = kind === "process";
  const title = isProcess ? "平台工艺" : "平台工程";
  const assets = useAssets(kind);
  const processes = useAssets("process");
  const create = useCreateAsset();
  const rename = useRenameAsset();
  const publish = usePublishAsset();
  const promote = usePromoteSnapshot();
  const { message } = App.useApp();
  const [open, setOpen] = useState(false);
  const [promoteOpen, setPromoteOpen] = useState(false);
  const [renameFor, setRenameFor] = useState<Asset | null>(null);
  const [viewFor, setViewFor] = useState<string | null>(null);
  const [form] = Form.useForm<CreateForm>();
  const [renameForm] = Form.useForm<{ name: string }>();
  const [promoteForm] = Form.useForm<{ snapshot: string }>();

  const availableProcesses = useMemo(() => (processes.data ?? []).filter((p) => p.status === "available"), [processes.data]);
  const onErr = (e: unknown) => message.error(errorMessage(e));

  const columns: TableColumnsType<Asset> = [
    { title: "显示名", dataIndex: "name" },
    { title: "状态", dataIndex: "status", width: 90, render: (s: string) => <Tag color={statusColor(s)}>{statusLabel(s)}</Tag> },
    { title: "修订", dataIndex: "revision", width: 70 },
    { title: "来源厂", dataIndex: "sourceFactoryId", width: 160, render: (id: string | null) => (id ? <IdText id={id} /> : "本端新建") },
    { title: "身份", dataIndex: "id", width: 160, render: (id: string) => <IdText id={id} /> },
    { title: "更新", dataIndex: "updatedAt", width: 160, render: (v: string) => formatTime(v) },
    {
      title: "操作",
      key: "actions",
      width: 220,
      render: (_, row) => (
        <Space size={4} wrap>
          <Button size="small" onClick={() => setViewFor(row.id)}>
            正文
          </Button>
          {row.status !== "disabled" ? (
            <Button size="small" onClick={() => setRenameFor(row)}>
              改名
            </Button>
          ) : null}
          {row.status === "draft" ? (
            <Popconfirm
              title={`发布「${row.name}」？`}
              description="发布后可被平台级工程依赖；平台级不可再改为可复制。"
              onConfirm={() => publish.mutate({ id: row.id, expected: row.revision }, { onSuccess: () => message.success("已发布"), onError: onErr })}
            >
              <Button size="small" type="primary">
                发布
              </Button>
            </Popconfirm>
          ) : null}
        </Space>
      ),
    },
  ];

  return (
    <>
      <PageHeader
        title={title}
        description={isProcess ? "只做平台级。厂级原件仍在各厂；升档只留下新的平台级副本。" : "只能依赖已发布的平台级工艺；升档工程不会另生成工艺。"}
        extra={
          <Space>
            <Button onClick={() => setPromoteOpen(true)}>用厂级快照升档</Button>
            <Button type="primary" icon={<PlusOutlined />} onClick={() => setOpen(true)}>
              新建{isProcess ? "工艺" : "工程"}
            </Button>
          </Space>
        }
      />
      <Card>
        <Table<Asset> rowKey="id" columns={columns} dataSource={assets.data ?? []} loading={assets.isLoading} pagination={{ pageSize: 20, hideOnSinglePage: true }} />
      </Card>
      <Modal title={`新建${isProcess ? "工艺" : "工程"}`} open={open} onCancel={() => setOpen(false)} okText="创建" confirmLoading={create.isPending} destroyOnHidden onOk={() => form.submit()}>
        <Form<CreateForm>
          form={form}
          layout="vertical"
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
            <Form.Item name="processIds" label="依赖工艺" extra="必须已是平台级可用工艺。" rules={[{ required: true, message: "请选择依赖工艺" }]}>
              <Select mode="multiple" optionFilterProp="label" options={availableProcesses.map((p) => ({ value: p.id, label: `${p.name} · r${p.revision}` }))} />
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
      <Modal title="用厂级快照升档" open={promoteOpen} onCancel={() => setPromoteOpen(false)} okText="升档" confirmLoading={promote.isPending} destroyOnHidden onOk={() => promoteForm.submit()} width={720}>
        <Form<{ snapshot: string }>
          form={promoteForm}
          layout="vertical"
          requiredMark={false}
          onFinish={(values) => {
            let snap: unknown;
            try {
              snap = JSON.parse(values.snapshot);
            } catch {
              message.error("快照不是合法 JSON");
              return;
            }
            promote.mutate(snap, {
              onSuccess: () => {
                message.success("已升为平台级");
                promoteForm.resetFields();
                setPromoteOpen(false);
              },
              onError: onErr,
            });
          }}
        >
          <Form.Item name="snapshot" label="厂端导出的快照" extra="从厂内管理端「升平台快照」复制；WAN 不保存厂级原件。" rules={[{ required: true, message: "请粘贴快照" }]}>
            <Input.TextArea rows={12} />
          </Form.Item>
        </Form>
      </Modal>
      <ContentModal id={viewFor} onClose={() => setViewFor(null)} />
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
