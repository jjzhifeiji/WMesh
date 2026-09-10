import { PlusOutlined } from "@ant-design/icons";
import { App, Button, Card, Form, Input, Modal, Popconfirm, Select, Table, TreeSelect, type TableColumnsType } from "antd";
import { useMemo, useState } from "react";
import { typeName, useCatalog } from "@/features/catalog/api";
import { errorMessage } from "@/shared/api/client";
import { formatTime } from "@/shared/format";
import { PageHeader } from "@/shared/ui/PageHeader";
import { StatusTag } from "@/shared/ui/StatusTag";
import { buildUnitTree, useCreateOrgUnit, useDisableOrgUnit, type CreateOrgUnitInput, type UnitTreeNode } from "./api";

type TreeOption = { value: string; title: string; children?: TreeOption[] };

function toTreeOptions(nodes: UnitTreeNode[]): TreeOption[] {
  return nodes
    .filter((n) => n.status === "active")
    .map((n) => ({ value: n.id, title: n.name, children: n.children ? toTreeOptions(n.children) : undefined }));
}

// 组织节点是一棵树：至多一个上级，空上级表示直挂工厂；停用不删，历史路径不受影响。
export function OrgUnitsPage() {
  const catalog = useCatalog();
  const create = useCreateOrgUnit();
  const disable = useDisableOrgUnit();
  const { message } = App.useApp();
  const [open, setOpen] = useState(false);
  const [form] = Form.useForm<CreateOrgUnitInput>();

  const c = catalog.data;
  const tree = useMemo(() => buildUnitTree(c?.orgUnits ?? []), [c?.orgUnits]);
  const activeTypes = (c?.orgTypes ?? []).filter((t) => t.status === "active");
  // 默认全部展开（含新建的父节点），只记住用户手动收起过的。
  const [collapsedIds, setCollapsedIds] = useState<ReadonlySet<string>>(new Set());
  const expandedRowKeys = useMemo(() => {
    const units = c?.orgUnits ?? [];
    return units.filter((u) => units.some((x) => x.parentId === u.id) && !collapsedIds.has(u.id)).map((u) => u.id);
  }, [c?.orgUnits, collapsedIds]);

  const columns: TableColumnsType<UnitTreeNode> = [
    { title: "名称", dataIndex: "name" },
    { title: "类型", dataIndex: "orgTypeId", width: 140, render: (id: string) => typeName(c, id) },
    { title: "状态", dataIndex: "status", width: 100, render: (s: string) => <StatusTag status={s} /> },
    { title: "创建时间", dataIndex: "createdAt", width: 170, render: (v: string) => formatTime(v) },
    {
      title: "操作",
      key: "actions",
      width: 100,
      render: (_, u) =>
        u.status === "active" ? (
          <Popconfirm
            title="停用该节点？"
            description="其下还有有效子节点时会被拒绝；已发生的事实仍记在原路径上。"
            onConfirm={() =>
              disable.mutate(u.id, {
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
        title="组织节点"
        description="任意层级、任意组合；每个节点只属一个上级，不能成环。"
        extra={
          <Button type="primary" icon={<PlusOutlined />} disabled={activeTypes.length === 0} onClick={() => setOpen(true)}>
            新建节点
          </Button>
        }
      />
      <Card>
        <Table<UnitTreeNode>
          rowKey="id"
          columns={columns}
          dataSource={tree}
          loading={catalog.isLoading}
          pagination={false}
          expandable={{
            expandedRowKeys,
            onExpand: (expanded, record) =>
              setCollapsedIds((prev) => {
                const next = new Set(prev);
                if (expanded) next.delete(record.id);
                else next.add(record.id);
                return next;
              }),
          }}
          locale={{ emptyText: activeTypes.length === 0 ? "先去「组织类型」建一个类型" : "还没有组织节点" }}
        />
      </Card>
      <Modal
        title="新建组织节点"
        open={open}
        onCancel={() => setOpen(false)}
        okText="创建"
        confirmLoading={create.isPending}
        destroyOnHidden
        onOk={() => form.submit()}
      >
        <Form<CreateOrgUnitInput>
          form={form}
          layout="vertical"
          requiredMark={false}
          initialValues={{ parentId: null }}
          onFinish={(values) =>
            create.mutate(
              { ...values, parentId: values.parentId || null },
              {
                onSuccess: () => {
                  message.success("已创建");
                  form.resetFields();
                  setOpen(false);
                },
                onError: (e) => message.error(errorMessage(e)),
              },
            )
          }
        >
          <Form.Item name="typeId" label="类型" rules={[{ required: true, message: "请选择类型" }]}>
            <Select options={activeTypes.map((t) => ({ value: t.id, label: t.name }))} placeholder="选择组织类型" />
          </Form.Item>
          <Form.Item name="parentId" label="上级节点" extra="留空表示直挂工厂。">
            <TreeSelect allowClear treeDefaultExpandAll treeData={toTreeOptions(tree)} placeholder="直挂工厂" />
          </Form.Item>
          <Form.Item name="name" label="名称" rules={[{ required: true, message: "请输入名称" }]}>
            <Input maxLength={64} placeholder="如：一车间" />
          </Form.Item>
        </Form>
      </Modal>
    </>
  );
}
