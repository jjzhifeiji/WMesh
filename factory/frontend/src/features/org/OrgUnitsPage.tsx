import { PlusOutlined } from "@ant-design/icons";
import { App, Button, Card, Form, Input, Modal, Popconfirm, Space, Table, TreeSelect, type TableColumnsType } from "antd";
import { useMemo, useState } from "react";
import { useCatalog } from "@/features/catalog/api";
import { errorMessage } from "@/shared/api/client";
import { formatTime } from "@/shared/format";
import { PageHeader } from "@/shared/ui/PageHeader";
import { StatusTag } from "@/shared/ui/StatusTag";
import { buildUnitTree, useCreateOrgUnit, useDeleteOrgUnit, useDisableOrgUnit, useEnableOrgUnit, type CreateOrgUnitInput, type UnitTreeNode } from "./api";

type TreeOption = { value: string; title: string; children?: TreeOption[] };

function toTreeOptions(nodes: UnitTreeNode[]): TreeOption[] {
  return nodes
    .filter((n) => n.status === "active")
    .map((n) => ({ value: n.id, title: n.name, children: n.children ? toTreeOptions(n.children) : undefined }));
}

// 工厂下一棵树：不选上级就直挂工厂；停用不删，历史路径不受影响。
export function OrgUnitsPage() {
  const catalog = useCatalog();
  const create = useCreateOrgUnit();
  const disable = useDisableOrgUnit();
  const enable = useEnableOrgUnit();
  const remove = useDeleteOrgUnit();
  const { message } = App.useApp();
  const [open, setOpen] = useState(false);
  const [form] = Form.useForm<CreateOrgUnitInput>();

  const c = catalog.data;
  const tree = useMemo(() => buildUnitTree(c?.orgUnits ?? []), [c?.orgUnits]);
  const peopleCount = useMemo(() => {
    const m = new Map<string, number>();
    for (const a of c?.assignments ?? []) {
      m.set(a.orgUnitId, (m.get(a.orgUnitId) ?? 0) + 1);
    }
    return m;
  }, [c?.assignments]);
  const [collapsedIds, setCollapsedIds] = useState<ReadonlySet<string>>(new Set());
  const expandedRowKeys = useMemo(() => {
    const units = c?.orgUnits ?? [];
    return units.filter((u) => units.some((x) => x.parentId === u.id) && !collapsedIds.has(u.id)).map((u) => u.id);
  }, [c?.orgUnits, collapsedIds]);

  const columns: TableColumnsType<UnitTreeNode> = [
    { title: "名称", dataIndex: "name" },
    {
      title: "人数",
      width: 80,
      render: (_, u) => peopleCount.get(u.id) ?? 0,
    },
    { title: "状态", dataIndex: "status", width: 100, render: (s: string) => <StatusTag status={s} /> },
    { title: "创建时间", dataIndex: "createdAt", width: 170, render: (v: string) => formatTime(v) },
    {
      title: "操作",
      key: "actions",
      width: 180,
      render: (_, u) => (
        <Space>
          {u.status === "active" ? (
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
              <Button size="small">停用</Button>
            </Popconfirm>
          ) : (
            <Button
              size="small"
              onClick={() =>
                enable.mutate(u.id, {
                  onSuccess: () => message.success("已启用"),
                  onError: (e) => message.error(errorMessage(e)),
                })
              }
            >
              启用
            </Button>
          )}
          <Popconfirm
            title={`删除「${u.name}」？`}
            description="有下级、当前人员、有效角色或历史事实时会拒绝。删除后不能恢复。"
            onConfirm={() =>
              remove.mutate(u.id, {
                onSuccess: () => message.success("已删除"),
                onError: (e) => message.error(errorMessage(e)),
              })
            }
          >
            <Button size="small" danger>
              删除
            </Button>
          </Popconfirm>
        </Space>
      ),
    },
  ];

  return (
    <>
      <PageHeader
        title="组织节点"
        description="整厂一棵树：不选上级则直接挂在工厂下；每个节点只有一个上级，不能成环。"
        extra={
          <Button type="primary" icon={<PlusOutlined />} onClick={() => setOpen(true)}>
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
          locale={{ emptyText: "还没有组织节点" }}
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
