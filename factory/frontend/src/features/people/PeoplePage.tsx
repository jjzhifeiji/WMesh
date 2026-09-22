import { PlusOutlined } from "@ant-design/icons";
import { App, Button, Card, Form, Input, Modal, Popconfirm, Radio, Select, Space, Switch, Table, Tag, TreeSelect, type TableColumnsType } from "antd";
import { useMemo, useState } from "react";
import { unitName, useCatalog, type Account } from "@/features/catalog/api";
import { buildUnitTree, type UnitTreeNode } from "@/features/org/api";
import { errorMessage } from "@/shared/api/client";
import { formatAppRelease, formatDateTime, formatTime } from "@/shared/format";
import { ROLES, roleLabel, roleScopes, scopeLabel, type Role, type ScopeKind } from "@/shared/labels";
import { IdText } from "@/shared/ui/IdText";
import { PageHeader } from "@/shared/ui/PageHeader";
import { StatusTag } from "@/shared/ui/StatusTag";
import {
  useCreatePerson,
  useDisablePerson,
  useEnablePerson,
  useGrantRole,
  usePersonLogins,
  useResetPersonPassword,
  useRevokeRole,
  useSetKeepPouch,
  type CreatePersonInput,
  type GrantRoleInput,
  type PersonLoginLog,
} from "./api";

function loginKindLabel(kind: string) {
  switch (kind) {
    case "pad":
      return "厂网登录";
    case "client":
      return "本机登录";
    case "mqtt":
      return "通道回连";
    default:
      return kind || "—";
  }
}

function dash(v?: string | null) {
  return v ? v : "—";
}

function deviceLabel(name?: string | null, serial?: string | null) {
  const n = name?.trim() ?? "";
  const s = serial?.trim() ?? "";
  if (n && s) return `${n}（${s}）`;
  return n || s || "—";
}

type TreeOption = { value: string; title: string; children?: TreeOption[] };

function toTreeOptions(nodes: UnitTreeNode[]): TreeOption[] {
  return nodes
    .filter((n) => n.status === "active")
    .map((n) => ({ value: n.id, title: n.name, children: n.children ? toTreeOptions(n.children) : undefined }));
}

// 人员账号固定归本厂；停用不删。角色在本页授予或收回，不另开一页。
export function PeoplePage() {
  const catalog = useCatalog();
  const create = useCreatePerson();
  const disable = useDisablePerson();
  const enable = useEnablePerson();
  const reset = useResetPersonPassword();
  const setKeep = useSetKeepPouch();
  const grant = useGrantRole();
  const revoke = useRevokeRole();
  const { message } = App.useApp();
  const [open, setOpen] = useState(false);
  const [grantOpen, setGrantOpen] = useState(false);
  const [grantTarget, setGrantTarget] = useState<Account | null>(null);
  const [form] = Form.useForm<CreatePersonInput>();
  const [grantForm] = Form.useForm<GrantRoleInput>();
  const [logPerson, setLogPerson] = useState<Account | null>(null);
  const logins = usePersonLogins(logPerson?.id ?? null);
  const role = Form.useWatch("role", grantForm) as Role | undefined;
  const scopeKind = Form.useWatch("scopeKind", grantForm) as ScopeKind | undefined;

  const c = catalog.data;
  const tree = useMemo(() => buildUnitTree(c?.orgUnits ?? []), [c?.orgUnits]);
  const allowedScopes = role ? roleScopes(role) : [];
  const people = (c?.people ?? []).filter((p) => p.status !== "disabled");
  const grantPeople = useMemo(() => {
    if (!grantTarget || people.some((p) => p.id === grantTarget.id)) return people;
    return [grantTarget, ...people];
  }, [people, grantTarget]);
  const rolesOf = (personId: string) => (c?.roleGrants ?? []).filter((g) => g.personId === personId);

  const openGrant = (person?: Account) => {
    setGrantTarget(person ?? null);
    setGrantOpen(true);
  };

  const columns: TableColumnsType<Account> = [
    { title: "登录名", dataIndex: "loginName", width: 140 },
    { title: "显示名", dataIndex: "displayName", width: 140 },
    { title: "状态", dataIndex: "status", width: 90, render: (s: string) => <StatusTag status={s} /> },
    {
      title: "APP",
      key: "appOnline",
      width: 80,
      render: (_, p) => (p.appOnline ? <Tag color="green">在线</Tag> : <Tag>离线</Tag>),
    },
    { title: "最后在线", dataIndex: "appLastSeenAt", width: 160, render: (v?: string) => formatTime(v) },
    {
      title: "APP 版本",
      key: "appVersion",
      width: 140,
      render: (_, p) => formatAppRelease(p.appVersion, p.appVersionName),
    },
    {
      title: "最近设备",
      key: "lastDevice",
      width: 180,
      render: (_, p) => deviceLabel(p.appClientName, p.appDeviceSerial),
    },
    {
      title: "角色",
      key: "roles",
      render: (_, p) => (
        <Space wrap size={[4, 4]}>
          {rolesOf(p.id).map((g) => (
            <Popconfirm
              key={g.id}
              title={`收回「${roleLabel(g.role)}」？`}
              description="收回后该账号的新操作立刻按剩余角色判定。"
              onConfirm={() =>
                revoke.mutate(g.id, {
                  onSuccess: () => message.success("已收回"),
                  onError: (e) => message.error(errorMessage(e)),
                })
              }
            >
              <Tag color="blue" style={{ cursor: "pointer" }}>
                {roleLabel(g.role)}
                {g.scopeKind === "org_unit" ? ` · ${unitName(c, g.orgUnitId)}` : " · 整厂"}
              </Tag>
            </Popconfirm>
          ))}
        </Space>
      ),
    },
    {
      title: "退出留库",
      dataIndex: "keepPouch",
      width: 100,
      render: (keep: boolean, p) => (
        <Switch
          checked={keep}
          checkedChildren="留"
          unCheckedChildren="删"
          onChange={(checked) =>
            setKeep.mutate(
              { personId: p.id, keepPouch: checked },
              {
                onSuccess: () => message.success(checked ? "退出后留下库文件" : "退出后删除库文件"),
                onError: (e) => message.error(errorMessage(e)),
              },
            )
          }
        />
      ),
    },
    { title: "稳定身份", dataIndex: "id", width: 280, render: (id: string) => <IdText id={id} /> },
    {
      title: "操作",
      key: "actions",
      width: 280,
      render: (_, p) => (
        <Space size={4} wrap>
          <Button size="small" onClick={() => setLogPerson(p)}>
            登录日志
          </Button>
          {p.id === c?.me.id ? null : (
            <>
              {p.status !== "disabled" ? (
                <Button size="small" onClick={() => openGrant(p)}>
                  授角色
                </Button>
              ) : null}
            {p.status === "disabled" ? (
              <Button
                size="small"
                onClick={() =>
                  enable.mutate(p.id, {
                    onSuccess: () => message.success("已启用"),
                    onError: (e) => message.error(errorMessage(e)),
                  })
                }
              >
                启用
              </Button>
            ) : (
              <Popconfirm
                title={`停用 ${p.displayName}？`}
                description="停用后不能登录；账号还在，可再启用。历史归属与审计不变。"
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
            )}
            <Popconfirm
              title={`重置 ${p.displayName} 的密码？`}
              description="旧密码立刻失效，改回登录名+123456。"
              onConfirm={() =>
                reset.mutate(p.id, {
                  onSuccess: (data) => message.success(`已重置为 ${data.account.loginName}123456`),
                  onError: (e) => message.error(errorMessage(e)),
                })
              }
            >
              <Button size="small">重置密码</Button>
            </Popconfirm>
            </>
          )}
        </Space>
      ),
    },
  ];

  const closeCreate = () => {
    form.resetFields();
    setOpen(false);
  };

  return (
    <>
      <PageHeader
        title="人员"
        description="停用不删账号。退出留库默认留着这个人在示教器上的库文件。点角色标签可收回。"
        extra={
          <Space>
            <Button type="primary" icon={<PlusOutlined />} onClick={() => setOpen(true)}>
              新建账号
            </Button>
          </Space>
        }
      />
      <Card>
        <Table<Account> rowKey="id" columns={columns} dataSource={c?.people ?? []} loading={catalog.isLoading} pagination={{ pageSize: 20, hideOnSinglePage: true }} />
      </Card>
      <Modal
        title="新建账号"
        open={open}
        onCancel={closeCreate}
        maskClosable={false}
        destroyOnHidden
        footer={
          <>
            <Button onClick={closeCreate}>取消</Button>
            <Button type="primary" loading={create.isPending} onClick={() => form.submit()}>
              创建
            </Button>
          </>
        }
      >
        <Form<CreatePersonInput>
          form={form}
          layout="vertical"
          requiredMark={false}
          onFinish={(values) =>
            create.mutate(values, {
              onSuccess: (data) => {
                message.success(`已创建，默认密码为 ${data.account.loginName}123456`);
                closeCreate();
                openGrant(data.account);
              },
              onError: (e) => message.error(errorMessage(e)),
            })
          }
        >
          <Form.Item name="loginName" label="登录名" extra="本厂内唯一。默认密码为登录名+123456。" rules={[{ required: true, message: "请输入登录名" }]}>
            <Input maxLength={64} autoComplete="off" autoFocus />
          </Form.Item>
          <Form.Item name="displayName" label="显示名" rules={[{ required: true, message: "请输入显示名" }]}>
            <Input maxLength={64} />
          </Form.Item>
        </Form>
      </Modal>
      <Modal
        title="授角色"
        open={grantOpen}
        onCancel={() => {
          setGrantOpen(false);
          setGrantTarget(null);
        }}
        okText="授予"
        confirmLoading={grant.isPending}
        destroyOnHidden
        onOk={() => grantForm.submit()}
      >
        <Form<GrantRoleInput>
          form={grantForm}
          layout="vertical"
          requiredMark={false}
          initialValues={{
            personId: grantTarget?.id,
            role: "operator",
            scopeKind: "factory",
            orgUnitId: null,
          }}
          onValuesChange={(changed) => {
            if (changed.role) {
              const scopes = roleScopes(changed.role as Role);
              const current = grantForm.getFieldValue("scopeKind") as ScopeKind | undefined;
              if (!current || !scopes.includes(current)) grantForm.setFieldValue("scopeKind", scopes[0]);
            }
            // 整厂不带节点；切过去必须清掉，否则会按「角色+作用域+节点」校验失败。
            const kind = (changed.scopeKind ?? grantForm.getFieldValue("scopeKind")) as ScopeKind | undefined;
            if (kind === "factory") grantForm.setFieldValue("orgUnitId", null);
          }}
          onFinish={(values) =>
            grant.mutate(
              {
                personId: values.personId,
                role: values.role,
                scopeKind: values.scopeKind,
                orgUnitId: values.scopeKind === "factory" ? null : values.orgUnitId,
              },
              {
                onSuccess: () => {
                  message.success("已授予");
                  grantForm.resetFields();
                  setGrantOpen(false);
                  setGrantTarget(null);
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
              options={grantPeople.map((p) => ({ value: p.id, label: `${p.displayName}（${p.loginName}）` }))}
              placeholder="选择人员"
              getPopupContainer={(n) => n.parentElement ?? document.body}
            />
          </Form.Item>
          <Form.Item name="role" label="角色" rules={[{ required: true }]}>
            <Select options={ROLES.map((r) => ({ value: r.value, label: r.label }))} getPopupContainer={(n) => n.parentElement ?? document.body} />
          </Form.Item>
          <Form.Item name="scopeKind" label="作用域" rules={[{ required: true }]}>
            <Radio.Group
              optionType="button"
              options={allowedScopes.map((s) => ({ value: s, label: scopeLabel(s) }))}
            />
          </Form.Item>
          <Form.Item
            name="orgUnitId"
            label="节点"
            hidden={scopeKind !== "org_unit"}
            rules={[
              {
                validator: async (_, v) => {
                  if (grantForm.getFieldValue("scopeKind") !== "org_unit") return;
                  if (!v) throw new Error("请选择节点");
                },
              },
            ]}
          >
            <TreeSelect
              treeDefaultExpandAll
              treeData={toTreeOptions(tree)}
              placeholder="选择组织节点"
              getPopupContainer={(n) => n.parentElement ?? document.body}
            />
          </Form.Item>
        </Form>
      </Modal>
      <Modal
        title={logPerson ? `${logPerson.displayName} 的登录日志` : "登录日志"}
        open={Boolean(logPerson)}
        onCancel={() => setLogPerson(null)}
        footer={null}
        width={960}
        destroyOnHidden
      >
        <Table<PersonLoginLog>
          rowKey="id"
          size="small"
          loading={logins.isLoading}
          dataSource={logins.data ?? []}
          pagination={{ pageSize: 10, hideOnSinglePage: true }}
          columns={[
            { title: "时间", dataIndex: "occurredAt", width: 170, render: (v: string) => formatDateTime(v) },
            { title: "方式", dataIndex: "kind", width: 100, render: (k: string) => loginKindLabel(k) },
            { title: "APP 版本", key: "app", width: 140, render: (_, r) => formatAppRelease(r.appVersion, r.appVersionName) },
            { title: "设备", key: "device", width: 180, render: (_, r) => deviceLabel(r.clientName, r.deviceSerial) },
            { title: "型号", dataIndex: "deviceModel", width: 140, render: dash },
            { title: "系统", dataIndex: "androidRelease", width: 80, render: dash },
            { title: "网络", dataIndex: "networkName", width: 140, render: dash },
          ]}
        />
      </Modal>
    </>
  );
}
