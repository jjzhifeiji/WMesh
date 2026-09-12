import { App, Button, Card, Form, Input, InputNumber, Modal, Popconfirm, Space, Table, Tag, Typography, type TableColumnsType } from "antd";
import { useMemo, useState } from "react";
import { errorMessage } from "@/shared/api/client";
import { formatTime } from "@/shared/format";
import { statusColor, statusLabel } from "@/shared/labels";
import { IdText } from "@/shared/ui/IdText";
import { PageHeader } from "@/shared/ui/PageHeader";
import {
  useClients,
  useIssueRuntime,
  useRenameClient,
  useRevokeRuntime,
  useRuntimeGrants,
  type Client,
  type RuntimeGrant,
} from "./api";

type IssueValues = { days: number };

// 云端分来的现场设备：按名字辨认，签发或撤销运行许可。
export function ClientsPage() {
  const clients = useClients();
  const grants = useRuntimeGrants();
  const rename = useRenameClient();
  const issue = useIssueRuntime();
  const revoke = useRevokeRuntime();
  const { message } = App.useApp();
  const [renameFor, setRenameFor] = useState<Client | null>(null);
  const [issueFor, setIssueFor] = useState<string | null>(null);
  const [renameForm] = Form.useForm<{ name: string }>();
  const [issueForm] = Form.useForm<IssueValues>();

  const grantOf = useMemo(() => {
    const m = new Map<string, RuntimeGrant>();
    for (const g of grants.data ?? []) m.set(g.clientId, g);
    return m;
  }, [grants.data]);

  const columns: TableColumnsType<Client> = [
    { title: "名称", dataIndex: "name", width: 140, ellipsis: true, render: (name: string) => <Typography.Text strong>{name}</Typography.Text> },
    { title: "识别号", dataIndex: "id", width: 280, render: (id: string) => <IdText id={id} /> },
    {
      title: "状态",
      dataIndex: "status",
      width: 100,
      render: (_, row) => {
        if (row.status === "void") return <Tag>{statusLabel("void")}</Tag>;
        if (!row.publicKey) return <Tag>待上线</Tag>;
        return <Tag color={statusColor("bound")}>{statusLabel("bound")}</Tag>;
      },
    },
    {
      title: "使用人",
      key: "operator",
      width: 180,
      render: (_, row) => {
        if (!row.operatorDisplay && !row.operatorLogin) return "—";
        if (row.operatorDisplay && row.operatorLogin) {
          return `${row.operatorDisplay}（${row.operatorLogin}）`;
        }
        return row.operatorDisplay || row.operatorLogin;
      },
    },
    {
      title: "运行许可",
      key: "runtime",
      width: 140,
      render: (_, row) => {
        const g = grantOf.get(row.id);
        if (!g) return "—";
        return g.canRun ? <Tag color="green">可运行 · r{g.revision}</Tag> : <Tag>已撤销 · r{g.revision}</Tag>;
      },
    },
    { title: "分配时间", dataIndex: "boundAt", width: 170, render: (v: string) => formatTime(v) },
    {
      title: "操作",
      key: "actions",
      width: 220,
      render: (_, row) =>
        row.status === "bound" ? (
          <Space size={4}>
            <Button size="small" onClick={() => setRenameFor(row)}>
              改名
            </Button>
            {row.publicKey ? (
              <>
                <Button size="small" onClick={() => setIssueFor(row.id)}>
                  签发许可
                </Button>
                <Popconfirm
                  title="撤销运行许可？"
                  description="已连网设备会按更高修订拒绝新开；离线用到过期。"
                  onConfirm={() =>
                    revoke.mutate(
                      { clientId: row.id, days: 30 },
                      {
                        onSuccess: () => message.success("已撤销"),
                        onError: (e) => message.error(errorMessage(e)),
                      },
                    )
                  }
                >
                  <Button size="small" danger>
                    撤销许可
                  </Button>
                </Popconfirm>
              </>
            ) : null}
          </Space>
        ) : (
          <Button size="small" onClick={() => setRenameFor(row)}>
            改名
          </Button>
        ),
    },
  ];

  return (
    <>
      <PageHeader title="设备" description="云端把设备分到本厂后会自动出现。识别号固定；运行许可仍由本厂签发或撤销。" />
      <Card>
        <Table<Client> rowKey="id" columns={columns} dataSource={clients.data ?? []} loading={clients.isLoading || grants.isLoading} pagination={{ pageSize: 20, hideOnSinglePage: true }} />
      </Card>
      <Modal
        title="改名"
        open={renameFor !== null}
        onCancel={() => setRenameFor(null)}
        okText="保存"
        confirmLoading={rename.isPending}
        destroyOnHidden
        onOk={() => renameForm.submit()}
      >
        <Form<{ name: string }>
          key={renameFor?.id}
          form={renameForm}
          layout="vertical"
          requiredMark={false}
          initialValues={{ name: renameFor?.name }}
          onFinish={(values) => {
            if (!renameFor) return;
            rename.mutate(
              { id: renameFor.id, name: values.name },
              {
                onSuccess: () => {
                  message.success("已改名");
                  setRenameFor(null);
                },
                onError: (e) => message.error(errorMessage(e)),
              },
            );
          }}
        >
          <Form.Item name="name" label="设备名称" rules={[{ required: true, message: "请输入名称" }, { max: 64, message: "最多 64 字" }]}>
            <Input maxLength={64} autoFocus />
          </Form.Item>
        </Form>
      </Modal>
      <Modal
        title="签发运行许可"
        open={issueFor !== null}
        onCancel={() => setIssueFor(null)}
        okText="签发"
        confirmLoading={issue.isPending}
        destroyOnHidden
        onOk={() => issueForm.submit()}
      >
        <Form<IssueValues>
          form={issueForm}
          layout="vertical"
          requiredMark={false}
          initialValues={{ days: 30 }}
          onFinish={(values) => {
            if (!issueFor) return;
            issue.mutate(
              { clientId: issueFor, days: values.days },
              {
                onSuccess: () => {
                  message.success("已签发");
                  setIssueFor(null);
                },
                onError: (e) => message.error(errorMessage(e)),
              },
            );
          }}
        >
          <Form.Item name="days" label="有效天数" extra="从现在起算；签发修订只向前。" rules={[{ required: true, message: "请输入天数" }]}>
            <InputNumber min={1} max={365} style={{ width: "100%" }} />
          </Form.Item>
        </Form>
      </Modal>
    </>
  );
}
