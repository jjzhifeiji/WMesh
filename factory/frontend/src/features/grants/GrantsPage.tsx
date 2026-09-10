import { PlusOutlined } from "@ant-design/icons";
import { App, Button, Card, Form, Modal, Popconfirm, Select, Table, Tag, TreeSelect, type TableColumnsType } from "antd";
import { useMemo, useState } from "react";
import { personName, unitName, useCatalog, type RoleGrant } from "@/features/catalog/api";
import { buildUnitTree, type UnitTreeNode } from "@/features/org/api";
import { errorMessage } from "@/shared/api/client";
import { formatTime } from "@/shared/format";
import { ROLES, roleLabel, roleScopes, scopeLabel, type Role, type ScopeKind } from "@/shared/labels";
import { PageHeader } from "@/shared/ui/PageHeader";
import { useGrantRole, useRevokeRole, type GrantRoleInput } from "./api";

type TreeOption = { value: string; title: string; children?: TreeOption[] };

function toTreeOptions(nodes: UnitTreeNode[]): TreeOption[] {
  return nodes
    .filter((n) => n.status === "active")
    .map((n) => ({ value: n.id, title: n.name, children: n.children ? toTreeOptions(n.children) : undefined }));
}

// 角色 = 账号 + 作用域（整厂 / 某节点子树）+ 固定角色；默认拒绝，没授就没有。
export function GrantsPage() {
  const catalog = useCatalog();
  const grant = useGrantRole();
  const revoke = useRevokeRole();
  const { message } = App.useApp();
  const [open, setOpen] = useState(false);
  const [form] = Form.useForm<GrantRoleInput>();
  const role = Form.useWatch("role", form) as Role | undefined;
  const scopeKind = Form.useWatch("scopeKind", form) as ScopeKind | undefined;

  const c = catalog.data;
  const tree = useMemo(() => buildUnitTree(c?.orgUnits ?? []), [c?.orgUnits]);
  const allowedScopes = role ? roleScopes(role) : [];
  const people = (c?.people ?? []).filter((p) => p.status !== "disabled");

  const columns: TableColumnsType<RoleGrant> = [
    { title: "人员", dataIndex: "personId", render: (id: string) => personName(c, id) },
    { title: "角色", dataIndex: "role", width: 140, render: (r: string) => <Tag color="blue">{roleLabel(r)}</Tag> },
    {
      title: "作用域",
      key: "scope",
      render: (_, g) => (g.scopeKind === "factory" ? scopeLabel(g.scopeKind) : `${scopeLabel(g.scopeKind)}：${unitName(c, g.orgUnitId)}`),
    },
    { title: "授予时间", dataIndex: "createdAt", width: 170, render: (v: string) => formatTime(v) },
    {
      title: "操作",
      key: "actions",
      width: 100,
      render: (_, g) => (
        <Popconfirm
          title="收回该角色？"
          description="收回后该账号的新操作立刻按剩余角色判定。"
          onConfirm={() =>
            revoke.mutate(g.id, {
              onSuccess: () => message.success("已收回"),
              onError: (e) => message.error(errorMessage(e)),
            })
          }
        >
          <Button size="small" danger>
            收回
          </Button>
        </Popconfirm>
      ),
    },
  ];

  return (
    <>
      <PageHeader
        title="角色授予"
        description="工厂超管只能整厂；管理员只能挂节点；操作员、审计员两者皆可。"
        extra={
          <Button type="primary" icon={<PlusOutlined />} onClick={() => setOpen(true)}>
            授予角色
          </Button>
        }
      />
      <Card>
        <Table<RoleGrant> rowKey="id" columns={columns} dataSource={c?.roleGrants ?? []} loading={catalog.isLoading} pagination={{ pageSize: 20, hideOnSinglePage: true }} />
      </Card>
      <Modal
        title="授予角色"
        open={open}
        onCancel={() => setOpen(false)}
        okText="授予"
        confirmLoading={grant.isPending}
        destroyOnHidden
        onOk={() => form.submit()}
      >
        <Form<GrantRoleInput>
          form={form}
          layout="vertical"
          requiredMark={false}
          initialValues={{ role: "operator", scopeKind: "org_unit", orgUnitId: null }}
          onValuesChange={(changed) => {
            // 换了角色就把作用域收敛到该角色允许的范围。
            if (changed.role) {
              const scopes = roleScopes(changed.role as Role);
              const current = form.getFieldValue("scopeKind") as ScopeKind | undefined;
              if (!current || !scopes.includes(current)) form.setFieldValue("scopeKind", scopes[0]);
            }
          }}
          onFinish={(values) =>
            grant.mutate(
              { ...values, orgUnitId: values.scopeKind === "factory" ? null : values.orgUnitId },
              {
                onSuccess: () => {
                  message.success("已授予");
                  form.resetFields();
                  setOpen(false);
                },
                onError: (e) => message.error(errorMessage(e)),
              },
            )
          }
        >
          <Form.Item name="personId" label="人员" rules={[{ required: true, message: "请选择人员" }]}>
            <Select
              showSearch
              optionFilterProp="label"
              options={people.map((p) => ({ value: p.id, label: `${p.displayName}（${p.loginName}）` }))}
              placeholder="选择人员"
            />
          </Form.Item>
          <Form.Item name="role" label="角色" rules={[{ required: true }]}>
            <Select options={ROLES.map((r) => ({ value: r.value, label: r.label }))} />
          </Form.Item>
          <Form.Item name="scopeKind" label="作用域" rules={[{ required: true }]}>
            <Select options={allowedScopes.map((s) => ({ value: s, label: scopeLabel(s) }))} />
          </Form.Item>
          {scopeKind === "org_unit" ? (
            <Form.Item name="orgUnitId" label="节点" rules={[{ required: true, message: "请选择节点" }]}>
              <TreeSelect treeDefaultExpandAll treeData={toTreeOptions(tree)} placeholder="选择组织节点" />
            </Form.Item>
          ) : null}
        </Form>
      </Modal>
    </>
  );
}
