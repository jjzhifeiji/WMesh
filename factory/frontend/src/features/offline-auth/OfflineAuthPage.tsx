import { PlusOutlined } from "@ant-design/icons";
import { App, Button, Card, Form, InputNumber, Modal, Select, Table, Tag, type TableColumnsType } from "antd";
import { useState } from "react";
import { personName, useCatalog } from "@/features/catalog/api";
import { useClients } from "@/features/clients/api";
import { errorMessage } from "@/shared/api/client";
import { formatTime } from "@/shared/format";
import { IdText } from "@/shared/ui/IdText";
import { PageHeader } from "@/shared/ui/PageHeader";
import { useIssuePersonGrant, usePersonGrants, type IssuePersonInput, type PersonGrant } from "./api";

// 给本厂有效账号签发绑定到指定 Client 的离线授权；口令哈希只进授权、不进页面。
export function OfflineAuthPage() {
  const catalog = useCatalog();
  const clients = useClients();
  const grants = usePersonGrants();
  const issue = useIssuePersonGrant();
  const { message } = App.useApp();
  const [open, setOpen] = useState(false);
  const [form] = Form.useForm<IssuePersonInput>();

  const people = (catalog.data?.people ?? []).filter((p) => p.status === "active");
  const bound = (clients.data ?? []).filter((c) => c.status === "bound");

  const columns: TableColumnsType<PersonGrant> = [
    { title: "人员", dataIndex: "personId", render: (id: string) => personName(catalog.data, id) },
    { title: "登录名", dataIndex: "loginName", width: 140 },
    { title: "Client", dataIndex: "clientId", render: (id: string) => <IdText id={id} /> },
    { title: "修订", dataIndex: "revision", width: 80 },
    {
      title: "账号快照",
      dataIndex: "active",
      width: 100,
      render: (ok: boolean) => (ok ? <Tag color="green">有效</Tag> : <Tag>停用</Tag>),
    },
    {
      title: "直属",
      dataIndex: "allowDirect",
      width: 90,
      render: (ok: boolean) => (ok ? "允许" : "—"),
    },
    { title: "生效", dataIndex: "notBefore", width: 170, render: (v: string) => formatTime(v) },
    { title: "失效", dataIndex: "notAfter", width: 170, render: (v: string) => formatTime(v) },
  ];

  return (
    <>
      <PageHeader
        title="人员离线授权"
        description="授权是签发时的角色与组织快照；停用、收权或改分配后要再签更高修订，旧修订不能回滚。"
        extra={
          <Button type="primary" icon={<PlusOutlined />} onClick={() => setOpen(true)}>
            签发授权
          </Button>
        }
      />
      <Card>
        <Table<PersonGrant> rowKey={(r) => `${r.personId}-${r.clientId}-${r.revision}`} columns={columns} dataSource={grants.data ?? []} loading={grants.isLoading} pagination={{ pageSize: 20, hideOnSinglePage: true }} />
      </Card>
      <Modal
        title="签发人员离线授权"
        open={open}
        onCancel={() => setOpen(false)}
        okText="签发"
        confirmLoading={issue.isPending}
        destroyOnHidden
        onOk={() => form.submit()}
      >
        <Form<IssuePersonInput>
          form={form}
          layout="vertical"
          requiredMark={false}
          initialValues={{ days: 30 }}
          onFinish={(values) =>
            issue.mutate(values, {
              onSuccess: () => {
                message.success("已签发");
                form.resetFields();
                setOpen(false);
              },
              onError: (e) => message.error(errorMessage(e)),
            })
          }
        >
          <Form.Item name="personId" label="人员" rules={[{ required: true, message: "请选择人员" }]}>
            <Select
              showSearch
              optionFilterProp="label"
              options={people.map((p) => ({ value: p.id, label: `${p.displayName}（${p.loginName}）` }))}
            />
          </Form.Item>
          <Form.Item name="clientId" label="Client" extra="必须是本厂已接受且未作废的节点。" rules={[{ required: true, message: "请选择 Client" }]}>
            <Select
              showSearch
              optionFilterProp="label"
              options={bound.map((c) => ({ value: c.id, label: c.id }))}
            />
          </Form.Item>
          <Form.Item name="days" label="有效天数" rules={[{ required: true, message: "请输入天数" }]}>
            <InputNumber min={1} max={365} style={{ width: "100%" }} />
          </Form.Item>
        </Form>
      </Modal>
    </>
  );
}
