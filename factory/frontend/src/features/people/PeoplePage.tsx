import { PlusOutlined } from "@ant-design/icons";
import { App, Button, Card, Form, Input, Modal, Popconfirm, Space, Table, Tag, type TableColumnsType } from "antd";
import { useState } from "react";
import { useCatalog, type Account } from "@/features/catalog/api";
import { errorMessage } from "@/shared/api/client";
import { roleLabel } from "@/shared/labels";
import { IdText } from "@/shared/ui/IdText";
import { PageHeader } from "@/shared/ui/PageHeader";
import { SecretOnce } from "@/shared/ui/SecretOnce";
import { StatusTag } from "@/shared/ui/StatusTag";
import { useCreatePerson, useDisablePerson, type CreatePersonInput } from "./api";

// 人员账号固定归本厂；这里只建、停、看，角色与分配在各自页面。
export function PeoplePage() {
  const catalog = useCatalog();
  const create = useCreatePerson();
  const disable = useDisablePerson();
  const { message } = App.useApp();
  const [open, setOpen] = useState(false);
  const [form] = Form.useForm<CreatePersonInput>();

  const c = catalog.data;
  const rolesOf = (personId: string) => (c?.roleGrants ?? []).filter((g) => g.personId === personId);

  const columns: TableColumnsType<Account> = [
    { title: "登录名", dataIndex: "loginName", width: 160 },
    { title: "显示名", dataIndex: "displayName", width: 160 },
    { title: "状态", dataIndex: "status", width: 100, render: (s: string) => <StatusTag status={s} /> },
    {
      title: "角色",
      key: "roles",
      render: (_, p) => (
        <Space wrap size={[4, 4]}>
          {rolesOf(p.id).map((g) => (
            <Tag key={g.id}>{roleLabel(g.role)}</Tag>
          ))}
        </Space>
      ),
    },
    { title: "稳定身份", dataIndex: "id", width: 180, render: (id: string) => <IdText id={id} /> },
    {
      title: "操作",
      key: "actions",
      width: 100,
      render: (_, p) =>
        p.status !== "disabled" && p.id !== c?.me.id ? (
          <Popconfirm
            title={`停用 ${p.displayName}？`}
            description="停用后不能登录，已有会话上的新操作立刻被拒；历史归属与审计不变。"
            onConfirm={() =>
              disable.mutate(p.id, {
                onSuccess: () => message.success("已停用"),
                onError: (e) => message.error(errorMessage(e)),
              })
            }
          >
            <Button size="small" danger>
              停用
            </Button>
          </Popconfirm>
        ) : null,
    },
  ];

  const created = create.data;
  const close = () => {
    form.resetFields();
    create.reset();
    setOpen(false);
  };

  return (
    <>
      <PageHeader
        title="人员"
        description="新账号先处于待启用；把激活口令交给本人，由本人自设日常口令后才能登录。"
        extra={
          <Button type="primary" icon={<PlusOutlined />} onClick={() => setOpen(true)}>
            新建账号
          </Button>
        }
      />
      <Card>
        <Table<Account> rowKey="id" columns={columns} dataSource={c?.people ?? []} loading={catalog.isLoading} pagination={{ pageSize: 20, hideOnSinglePage: true }} />
      </Card>
      <Modal
        title={created ? "账号已创建" : "新建账号"}
        open={open}
        onCancel={created ? undefined : close}
        closable={!created}
        maskClosable={false}
        destroyOnHidden
        footer={
          created ? (
            <Button type="primary" onClick={close}>
              我已抄走口令，关闭
            </Button>
          ) : (
            <>
              <Button onClick={close}>取消</Button>
              <Button type="primary" loading={create.isPending} onClick={() => form.submit()}>
                创建
              </Button>
            </>
          )
        }
      >
        {created ? (
          <SecretOnce
            title="激活口令只显示这一次，请立刻交给本人"
            items={[
              { label: "登录名", value: created.account.loginName },
              { label: "激活口令", value: created.activationToken },
            ]}
          />
        ) : (
          <Form<CreatePersonInput>
            form={form}
            layout="vertical"
            requiredMark={false}
            onFinish={(values) => create.mutate(values, { onError: (e) => message.error(errorMessage(e)) })}
          >
            <Form.Item name="loginName" label="登录名" extra="本厂内唯一。" rules={[{ required: true, message: "请输入登录名" }]}>
              <Input maxLength={64} autoComplete="off" autoFocus />
            </Form.Item>
            <Form.Item name="displayName" label="显示名" rules={[{ required: true, message: "请输入显示名" }]}>
              <Input maxLength={64} />
            </Form.Item>
          </Form>
        )}
      </Modal>
    </>
  );
}
