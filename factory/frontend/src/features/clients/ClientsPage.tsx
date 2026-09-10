import { PlusOutlined } from "@ant-design/icons";
import { Alert, App, Button, Card, Form, Input, InputNumber, Modal, Popconfirm, Space, Table, Tag, Typography, type TableColumnsType } from "antd";
import { useMemo, useState } from "react";
import { errorMessage } from "@/shared/api/client";
import { formatTime } from "@/shared/format";
import { statusColor, statusLabel } from "@/shared/labels";
import { IdText } from "@/shared/ui/IdText";
import { PageHeader } from "@/shared/ui/PageHeader";
import {
  useAcceptClient,
  useClients,
  useIssueRuntime,
  useRevokeRuntime,
  useRuntimeGrants,
  useSigningKey,
  useVoidClient,
  type AcceptClientInput,
  type Client,
  type RuntimeGrant,
} from "./api";

type IssueValues = { days: number };

// 本厂已接受的现场节点：先登记绑定，再签发或撤销运行许可。
export function ClientsPage() {
  const clients = useClients();
  const grants = useRuntimeGrants();
  const signing = useSigningKey();
  const accept = useAcceptClient();
  const voidBind = useVoidClient();
  const issue = useIssueRuntime();
  const revoke = useRevokeRuntime();
  const { message } = App.useApp();
  const [open, setOpen] = useState(false);
  const [issueFor, setIssueFor] = useState<string | null>(null);
  const [form] = Form.useForm<AcceptClientInput>();
  const [issueForm] = Form.useForm<IssueValues>();

  const grantOf = useMemo(() => {
    const m = new Map<string, RuntimeGrant>();
    for (const g of grants.data ?? []) m.set(g.clientId, g);
    return m;
  }, [grants.data]);

  const columns: TableColumnsType<Client> = [
    { title: "节点身份", dataIndex: "id", render: (id: string) => <IdText id={id} /> },
    {
      title: "公钥",
      dataIndex: "publicKey",
      render: (v: string) => (
        <Typography.Text code copyable={{ text: v, tooltips: ["复制公钥", "已复制"] }} style={{ maxWidth: 180 }} ellipsis>
          {v}
        </Typography.Text>
      ),
    },
    { title: "绑定修订", dataIndex: "bindingRevision", width: 100 },
    {
      title: "状态",
      dataIndex: "status",
      width: 100,
      render: (s: string) => <Tag color={statusColor(s)}>{statusLabel(s)}</Tag>,
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
    { title: "接受时间", dataIndex: "boundAt", width: 170, render: (v: string) => formatTime(v) },
    {
      title: "操作",
      key: "actions",
      width: 240,
      render: (_, row) =>
        row.status === "bound" ? (
          <Space size={4}>
            <Button size="small" onClick={() => setIssueFor(row.id)}>
              签发许可
            </Button>
            <Popconfirm
              title="撤销运行许可？"
              description="已连网节点会按更高修订拒绝新开；离线用到过期。"
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
            <Popconfirm
              title="作废本厂绑定？"
              description="作废后本厂不得再签发；换厂后应升高修订再重新接受。"
              onConfirm={() =>
                voidBind.mutate(row.id, {
                  onSuccess: () => message.success("已作废"),
                  onError: (e) => message.error(errorMessage(e)),
                })
              }
            >
              <Button size="small" danger>
                作废
              </Button>
            </Popconfirm>
          </Space>
        ) : null,
    },
  ];

  return (
    <>
      <PageHeader
        title="Client 节点"
        description="先按 WAN 给出的身份、公钥和绑定修订登记到本厂，再签发运行许可。私钥不进厂库。"
        extra={
          <Button type="primary" icon={<PlusOutlined />} onClick={() => setOpen(true)}>
            接受绑定
          </Button>
        }
      />
      {signing.data?.publicKey ? (
        <Alert
          type="info"
          showIcon
          style={{ marginBottom: 16 }}
          message="本厂签发公钥"
          description={
            <Typography.Text copyable={{ text: signing.data.publicKey }}>
              {signing.data.publicKey}
            </Typography.Text>
          }
        />
      ) : null}
      <Card>
        <Table<Client> rowKey="id" columns={columns} dataSource={clients.data ?? []} loading={clients.isLoading || grants.isLoading} pagination={{ pageSize: 20, hideOnSinglePage: true }} />
      </Card>
      <Modal
        title="接受绑定"
        open={open}
        onCancel={() => setOpen(false)}
        okText="登记"
        confirmLoading={accept.isPending}
        destroyOnHidden
        onOk={() => form.submit()}
      >
        <Form<AcceptClientInput>
          form={form}
          layout="vertical"
          requiredMark={false}
          initialValues={{ bindingRevision: 1 }}
          onFinish={(values) =>
            accept.mutate(values, {
              onSuccess: () => {
                message.success("已接受绑定");
                form.resetFields();
                setOpen(false);
              },
              onError: (e) => message.error(errorMessage(e)),
            })
          }
        >
          <Form.Item name="id" label="Client 稳定身份" extra="与 WAN 名录里的节点身份相同。" rules={[{ required: true, message: "请输入节点身份" }]}>
            <Input autoComplete="off" autoFocus />
          </Form.Item>
          <Form.Item name="publicKey" label="本机公钥" extra="32 字节公钥，base64 或 hex。不要粘贴私钥。" rules={[{ required: true, message: "请输入公钥" }]}>
            <Input.TextArea rows={3} />
          </Form.Item>
          <Form.Item name="bindingRevision" label="绑定修订" rules={[{ required: true, message: "请输入修订号" }]}>
            <InputNumber min={1} style={{ width: "100%" }} />
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
