import { PlusOutlined } from "@ant-design/icons";
import { App, Button, Card, Form, Input, Modal, Select, Table, Typography, type TableColumnsType } from "antd";
import { useMemo, useState } from "react";
import { useDirectory } from "@/features/factories/api";
import { errorMessage } from "@/shared/api/client";
import { formatTime } from "@/shared/format";
import { IdText } from "@/shared/ui/IdText";
import { PageHeader } from "@/shared/ui/PageHeader";
import { useBindClient, useClients, useRebindClient, type BindClientInput, type Client } from "./api";

// WAN 只登记公钥和所属工厂；厂内人员授权从这里查不到。
export function ClientsPage() {
  const clients = useClients();
  const dir = useDirectory();
  const bind = useBindClient();
  const rebind = useRebindClient();
  const { message } = App.useApp();
  const [open, setOpen] = useState(false);
  const [rebindFor, setRebindFor] = useState<Client | null>(null);
  const [newId, setNewId] = useState("");
  const [form] = Form.useForm<BindClientInput>();
  const [rebindForm] = Form.useForm<{ factoryId: string }>();

  const factoryName = useMemo(() => {
    const m = new Map((dir.data?.factories ?? []).map((f) => [f.id, f.name]));
    return (id: string | null) => (id ? (m.get(id) ?? id) : "未绑定");
  }, [dir.data]);

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
    { title: "所属工厂", dataIndex: "factoryId", render: (id: string | null) => factoryName(id) },
    { title: "绑定修订", dataIndex: "bindingRevision", width: 100 },
    { title: "绑定时间", dataIndex: "boundAt", width: 170, render: (v: string | null) => formatTime(v) },
    {
      title: "操作",
      key: "actions",
      width: 100,
      render: (_, row) =>
        row.factoryId ? (
          <Button size="small" onClick={() => setRebindFor(row)}>
            改绑
          </Button>
        ) : null,
    },
  ];

  const openBind = () => {
    setNewId(crypto.randomUUID());
    setOpen(true);
  };

  return (
    <>
      <PageHeader
        title="Client 绑定"
        description="一台节点同一时刻只属一个厂。改绑会升高修订；厂内人员、组织和口令仍不在这里。"
        extra={
          <Button type="primary" icon={<PlusOutlined />} onClick={openBind}>
            绑定节点
          </Button>
        }
      />
      <Card>
        <Table<Client> rowKey="id" columns={columns} dataSource={clients.data ?? []} loading={clients.isLoading} pagination={{ pageSize: 20, hideOnSinglePage: true }} />
      </Card>
      <Modal
        title="绑定节点"
        open={open}
        onCancel={() => setOpen(false)}
        okText="绑定"
        confirmLoading={bind.isPending}
        destroyOnHidden
        onOk={() => form.submit()}
      >
        <Form<BindClientInput>
          key={newId}
          form={form}
          layout="vertical"
          requiredMark={false}
          initialValues={{ id: newId }}
          onFinish={(values) =>
            bind.mutate(values, {
              onSuccess: () => {
                message.success("已绑定");
                setOpen(false);
              },
              onError: (e) => message.error(errorMessage(e)),
            })
          }
        >
          <Form.Item name="id" label="Client 稳定身份" extra="可改成现场已有身份；关闭前请抄走，厂内接受绑定时要用。" rules={[{ required: true, message: "请输入节点身份" }]}>
            <Input autoComplete="off" />
          </Form.Item>
          <Form.Item name="factoryId" label="工厂" rules={[{ required: true, message: "请选择工厂" }]}>
            <Select options={(dir.data?.factories ?? []).map((f) => ({ value: f.id, label: f.name }))} />
          </Form.Item>
          <Form.Item name="publicKey" label="本机公钥" extra="32 字节公钥，base64 或 hex。不要粘贴私钥。" rules={[{ required: true, message: "请输入公钥" }]}>
            <Input.TextArea rows={3} />
          </Form.Item>
        </Form>
      </Modal>
      <Modal
        title="改绑到另一厂"
        open={rebindFor !== null}
        onCancel={() => setRebindFor(null)}
        okText="改绑"
        confirmLoading={rebind.isPending}
        destroyOnHidden
        onOk={() => rebindForm.submit()}
      >
        <Form<{ factoryId: string }>
          form={rebindForm}
          layout="vertical"
          requiredMark={false}
          onFinish={(values) => {
            if (!rebindFor) return;
            rebind.mutate(
              { id: rebindFor.id, factoryId: values.factoryId },
              {
                onSuccess: () => {
                  message.success("已改绑");
                  setRebindFor(null);
                },
                onError: (e) => message.error(errorMessage(e)),
              },
            );
          }}
        >
          <Form.Item name="factoryId" label="新工厂" rules={[{ required: true, message: "请选择工厂" }]}>
            <Select
              options={(dir.data?.factories ?? [])
                .filter((f) => f.id !== rebindFor?.factoryId)
                .map((f) => ({ value: f.id, label: f.name }))}
            />
          </Form.Item>
        </Form>
      </Modal>
    </>
  );
}
