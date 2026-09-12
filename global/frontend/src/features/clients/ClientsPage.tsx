import { PlusOutlined } from "@ant-design/icons";
import { App, Button, Card, Form, Input, Modal, Select, Space, Table, type TableColumnsType } from "antd";
import { useMemo, useState } from "react";
import { useDirectory } from "@/features/factories/api";
import { errorMessage } from "@/shared/api/client";
import { formatTime } from "@/shared/format";
import { IdText } from "@/shared/ui/IdText";
import { PageHeader } from "@/shared/ui/PageHeader";
import {
  useAssignClient,
  useClients,
  useRebindClient,
  useRegisterClient,
  useRenameClient,
  type Client,
  type RegisterClientInput,
} from "./api";

// WAN 名录里的自有设备：起名、分给工厂；厂端通道自动落库。
export function ClientsPage() {
  const clients = useClients();
  const dir = useDirectory();
  const register = useRegisterClient();
  const rename = useRenameClient();
  const assign = useAssignClient();
  const rebind = useRebindClient();
  const { message } = App.useApp();
  const [open, setOpen] = useState(false);
  const [renameFor, setRenameFor] = useState<Client | null>(null);
  const [moveFor, setMoveFor] = useState<Client | null>(null);
  const [form] = Form.useForm<RegisterClientInput>();
  const [renameForm] = Form.useForm<{ name: string }>();
  const [moveForm] = Form.useForm<{ factoryId: string }>();

  const factories = dir.data?.factories ?? [];
  const factoryName = useMemo(() => {
    const m = new Map(factories.map((f) => [f.id, f.name]));
    return (id: string | null) => (id ? (m.get(id) ?? id) : "未分配");
  }, [factories]);

  const columns: TableColumnsType<Client> = [
    { title: "名称", dataIndex: "name" },
    { title: "识别号", dataIndex: "id", width: 280, render: (id: string) => <IdText id={id} /> },
    { title: "所属工厂", dataIndex: "factoryId", render: (id: string | null) => factoryName(id) },
    { title: "创建时间", dataIndex: "createdAt", width: 170, render: (v: string) => formatTime(v) },
    { title: "分配时间", dataIndex: "boundAt", width: 170, render: (v: string | null) => formatTime(v) },
    {
      title: "操作",
      key: "actions",
      width: 180,
      render: (_, row) => (
        <Space size={4}>
          <Button size="small" onClick={() => { setRenameFor(row); renameForm.setFieldsValue({ name: row.name }); }}>
            改名
          </Button>
          {row.factoryId ? (
            <Button size="small" onClick={() => { setMoveFor(row); moveForm.resetFields(); }}>
              改分
            </Button>
          ) : (
            <Button size="small" onClick={() => { setMoveFor(row); moveForm.resetFields(); }}>
              分配
            </Button>
          )}
        </Space>
      ),
    },
  ];

  return (
    <>
      <PageHeader
        title="设备"
        description="先起一个给人看的名字，再分给工厂。识别号由系统给出，不能改。"
        extra={
          <Button type="primary" icon={<PlusOutlined />} onClick={() => setOpen(true)}>
            登记设备
          </Button>
        }
      />
      <Card>
        <Table<Client> rowKey="id" columns={columns} dataSource={clients.data ?? []} loading={clients.isLoading} pagination={{ pageSize: 20, hideOnSinglePage: true }} />
      </Card>
      <Modal
        title="登记设备"
        open={open}
        onCancel={() => setOpen(false)}
        okText="登记"
        confirmLoading={register.isPending}
        destroyOnHidden
        onOk={() => form.submit()}
      >
        <Form<RegisterClientInput>
          form={form}
          layout="vertical"
          requiredMark={false}
          onFinish={(values) =>
            register.mutate(
              { name: values.name, factoryId: values.factoryId || undefined },
              {
                onSuccess: () => {
                  message.success(values.factoryId ? "已登记并分配到工厂" : "已登记，稍后分配");
                  setOpen(false);
                  form.resetFields();
                },
                onError: (e) => message.error(errorMessage(e)),
              },
            )
          }
        >
          <Form.Item name="name" label="名称" extra="给现场看的名字，可随时改；识别号登记后自动给出。" rules={[{ required: true, message: "请输入名称" }, { max: 64, message: "最多 64 个字" }]}>
            <Input autoFocus maxLength={64} placeholder="例如 焊机-12" />
          </Form.Item>
          <Form.Item name="factoryId" label="分给工厂" extra="不选则先进入名录。">
            <Select allowClear placeholder="稍后分配" options={factories.filter((f) => f.status === "active").map((f) => ({ value: f.id, label: f.name }))} />
          </Form.Item>
        </Form>
      </Modal>
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
          form={renameForm}
          layout="vertical"
          requiredMark={false}
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
          <Form.Item name="name" label="名称" rules={[{ required: true, message: "请输入名称" }, { max: 64, message: "最多 64 个字" }]}>
            <Input maxLength={64} />
          </Form.Item>
        </Form>
      </Modal>
      <Modal
        title={moveFor?.factoryId ? "改分到另一厂" : "分配到工厂"}
        open={moveFor !== null}
        onCancel={() => setMoveFor(null)}
        okText={moveFor?.factoryId ? "改分" : "分配"}
        confirmLoading={assign.isPending || rebind.isPending}
        destroyOnHidden
        onOk={() => moveForm.submit()}
      >
        <Form<{ factoryId: string }>
          form={moveForm}
          layout="vertical"
          requiredMark={false}
          onFinish={(values) => {
            if (!moveFor) return;
            const mut = moveFor.factoryId ? rebind : assign;
            mut.mutate(
              { id: moveFor.id, factoryId: values.factoryId },
              {
                onSuccess: () => {
                  message.success(moveFor.factoryId ? "已改分" : "已分配");
                  setMoveFor(null);
                },
                onError: (e) => message.error(errorMessage(e)),
              },
            );
          }}
        >
          <Form.Item name="factoryId" label="工厂" rules={[{ required: true, message: "请选择工厂" }]}>
            <Select
              options={factories
                .filter((f) => f.status === "active" && f.id !== moveFor?.factoryId)
                .map((f) => ({ value: f.id, label: f.name }))}
            />
          </Form.Item>
        </Form>
      </Modal>
    </>
  );
}
