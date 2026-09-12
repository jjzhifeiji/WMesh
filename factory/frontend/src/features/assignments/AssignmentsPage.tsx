import { PlusOutlined } from "@ant-design/icons";
import { App, Button, Card, Form, Modal, Popconfirm, Select, Table, TreeSelect, type TableColumnsType } from "antd";
import { useMemo, useState } from "react";
import { personName, unitName, useCatalog, type Assignment } from "@/features/catalog/api";
import { buildUnitTree, type UnitTreeNode } from "@/features/org/api";
import { errorMessage } from "@/shared/api/client";
import { formatTime } from "@/shared/format";
import { PageHeader } from "@/shared/ui/PageHeader";
import { useAssign, useUnassign, type AssignInput } from "./api";

type TreeOption = { value: string; title: string; children?: TreeOption[] };

function toTreeOptions(nodes: UnitTreeNode[]): TreeOption[] {
  return nodes
    .filter((n) => n.status === "active")
    .map((n) => ({ value: n.id, title: n.name, children: n.children ? toTreeOptions(n.children) : undefined }));
}

// 人员至多挂到一个组织节点；挂哪儿不改变归厂，也不自动带权限。
export function AssignmentsPage() {
  const catalog = useCatalog();
  const assign = useAssign();
  const unassign = useUnassign();
  const { message } = App.useApp();
  const [open, setOpen] = useState(false);
  const [form] = Form.useForm<AssignInput>();

  const c = catalog.data;
  const tree = useMemo(() => buildUnitTree(c?.orgUnits ?? []), [c?.orgUnits]);
  const people = (c?.people ?? []).filter((p) => p.status !== "disabled");
  const assigned = new Set((c?.assignments ?? []).map((a) => a.personId));
  const freePeople = people.filter((p) => !assigned.has(p.id));
  const hasUnits = (c?.orgUnits ?? []).some((u) => u.status === "active");

  const columns: TableColumnsType<Assignment> = [
    { title: "人员", dataIndex: "personId", render: (id: string) => personName(c, id) },
    { title: "组织节点", dataIndex: "orgUnitId", render: (id: string) => unitName(c, id) },
    { title: "分配时间", dataIndex: "createdAt", width: 170, render: (v: string) => formatTime(v) },
    {
      title: "操作",
      key: "actions",
      width: 120,
      render: (_, a) => (
        <Popconfirm
          title="取消该分配？"
          description="之后不能再以该节点为工作上下文；历史事实不变。"
          onConfirm={() =>
            unassign.mutate(
              { personId: a.personId, orgUnitId: a.orgUnitId },
              {
                onSuccess: () => message.success("已取消"),
                onError: (e) => message.error(errorMessage(e)),
              },
            )
          }
        >
          <Button size="small" danger>
            取消分配
          </Button>
        </Popconfirm>
      ),
    },
  ];

  return (
    <>
      <PageHeader
        title="组织分配"
        description="每人最多挂到一个节点；要换节点先取消再分。权限仍由人员页的角色决定。"
        extra={
          <Button type="primary" icon={<PlusOutlined />} disabled={!hasUnits || freePeople.length === 0} onClick={() => setOpen(true)}>
            新建分配
          </Button>
        }
      />
      <Card>
        <Table<Assignment>
          rowKey="id"
          columns={columns}
          dataSource={c?.assignments ?? []}
          loading={catalog.isLoading}
          pagination={{ pageSize: 20, hideOnSinglePage: true }}
          locale={{ emptyText: hasUnits ? "还没有分配" : "先去「组织节点」建节点" }}
        />
      </Card>
      <Modal
        title="新建分配"
        open={open}
        onCancel={() => setOpen(false)}
        okText="分配"
        confirmLoading={assign.isPending}
        destroyOnHidden
        onOk={() => form.submit()}
      >
        <Form<AssignInput>
          form={form}
          layout="vertical"
          requiredMark={false}
          onFinish={(values) =>
            assign.mutate(values, {
              onSuccess: () => {
                message.success("已分配");
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
              options={freePeople.map((p) => ({ value: p.id, label: `${p.displayName}（${p.loginName}）` }))}
              placeholder="选择人员"
            />
          </Form.Item>
          <Form.Item name="orgUnitId" label="组织节点" rules={[{ required: true, message: "请选择节点" }]}>
            <TreeSelect treeDefaultExpandAll treeData={toTreeOptions(tree)} placeholder="选择组织节点" />
          </Form.Item>
        </Form>
      </Modal>
    </>
  );
}
