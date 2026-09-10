import { PlusOutlined } from "@ant-design/icons";
import { App, Button, Card, Form, Input, Modal, Popconfirm, Table, type TableColumnsType } from "antd";
import { useState } from "react";
import { useCatalog, type OrgType } from "@/features/catalog/api";
import { errorMessage } from "@/shared/api/client";
import { formatTime } from "@/shared/format";
import { PageHeader } from "@/shared/ui/PageHeader";
import { StatusTag } from "@/shared/ui/StatusTag";
import { useCreateOrgType, useDisableOrgType, type CreateOrgTypeInput } from "./api";

// 自定义组织类型（场地、车间、班组…）：先有类型，才能建节点。
export function OrgTypesPage() {
  const catalog = useCatalog();
  const create = useCreateOrgType();
  const disable = useDisableOrgType();
  const { message } = App.useApp();
  const [open, setOpen] = useState(false);
  const [form] = Form.useForm<CreateOrgTypeInput>();

  const units = catalog.data?.orgUnits ?? [];
  const activeUnitsOf = (typeId: string) => units.filter((u) => u.orgTypeId === typeId && u.status === "active").length;

  const columns: TableColumnsType<OrgType> = [
    { title: "名称", dataIndex: "name" },
    { title: "有效节点数", key: "units", width: 120, render: (_, t) => activeUnitsOf(t.id) },
    { title: "状态", dataIndex: "status", width: 100, render: (s: string) => <StatusTag status={s} /> },
    { title: "创建时间", dataIndex: "createdAt", width: 170, render: (v: string) => formatTime(v) },
    {
      title: "操作",
      key: "actions",
      width: 100,
      render: (_, t) =>
        t.status === "active" ? (
          <Popconfirm
            title="停用该类型？"
            description="停用后不能再用它建节点；类型下还有有效节点时会被拒绝。"
            onConfirm={() =>
              disable.mutate(t.id, {
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

  return (
    <>
      <PageHeader
        title="组织类型"
        description="层级不预设：场地、车间、产线、班组都由本厂自定义。"
        extra={
          <Button type="primary" icon={<PlusOutlined />} onClick={() => setOpen(true)}>
            新建类型
          </Button>
        }
      />
      <Card>
        <Table<OrgType> rowKey="id" columns={columns} dataSource={catalog.data?.orgTypes ?? []} loading={catalog.isLoading} pagination={false} />
      </Card>
      <Modal
        title="新建组织类型"
        open={open}
        onCancel={() => setOpen(false)}
        okText="创建"
        confirmLoading={create.isPending}
        destroyOnHidden
        onOk={() => form.submit()}
      >
        <Form<CreateOrgTypeInput>
          form={form}
          layout="vertical"
          requiredMark={false}
          onFinish={(values) =>
            create.mutate(values, {
              onSuccess: () => {
                message.success("已创建");
                form.resetFields();
                setOpen(false);
              },
              onError: (e) => message.error(errorMessage(e)),
            })
          }
        >
          <Form.Item name="name" label="名称" rules={[{ required: true, message: "请输入名称" }]}>
            <Input maxLength={32} autoFocus placeholder="如：车间" />
          </Form.Item>
        </Form>
      </Modal>
    </>
  );
}
