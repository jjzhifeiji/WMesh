import { PlusOutlined } from "@ant-design/icons";
import { App, Button, Card, Form, Input, Modal, Popconfirm, Select, Space, Switch, Table, Tag, TreeSelect, Typography, type TableColumnsType } from "antd";
import { useMemo, useState } from "react";
import { useCatalog, type Account } from "@/features/catalog/api";
import { buildUnitTree, type UnitTreeNode } from "@/features/org/api";
import { errorMessage } from "@/shared/api/client";
import { formatAppRelease, formatDateTime, formatTime } from "@/shared/format";
import { ROLES, roleLabel } from "@/shared/labels";
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
  useSetPersonDisplayName,
  useSetPersonOrg,
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

// 人员账号固定归本厂；停用不删。角色、所属组织、显示名都在本页改。
export function PeoplePage() {
  const catalog = useCatalog();
  const create = useCreatePerson();
  const disable = useDisablePerson();
  const enable = useEnablePerson();
  const reset = useResetPersonPassword();
  const setKeep = useSetKeepPouch();
  const grant = useGrantRole();
  const revoke = useRevokeRole();
  const setOrg = useSetPersonOrg();
  const rename = useSetPersonDisplayName();
  const { message } = App.useApp();
  const [open, setOpen] = useState(false);
  const [grantOpen, setGrantOpen] = useState(false);
  const [grantTarget, setGrantTarget] = useState<Account | null>(null);
  const [form] = Form.useForm<CreatePersonInput>();
  const [grantForm] = Form.useForm<GrantRoleInput>();
  const [logPerson, setLogPerson] = useState<Account | null>(null);
  const logins = usePersonLogins(logPerson?.id ?? null);

  const c = catalog.data;
  const tree = useMemo(() => buildUnitTree(c?.orgUnits ?? []), [c?.orgUnits]);
  const people = (c?.people ?? []).filter((p) => p.status !== "disabled");
  const grantPeople = useMemo(() => {
    if (!grantTarget || people.some((p) => p.id === grantTarget.id)) return people;
    return [grantTarget, ...people];
  }, [people, grantTarget]);
  const rolesOf = (personId: string) => (c?.roleGrants ?? []).filter((g) => g.personId === personId);

  // 表单实例是页面级的，旧值会盖过 initialValues；每次打开都显式填回当前人员和默认角色，免得授给上一个人。
  const openGrant = (person?: Account) => {
    grantForm.setFieldsValue({ personId: person?.id, role: "operator" });
    setGrantTarget(person ?? null);
    setGrantOpen(true);
  };

  const columns: TableColumnsType<Account> = [
    { title: "登录名", dataIndex: "loginName", width: 140 },
    {
      title: "显示名",
      dataIndex: "displayName",
      width: 160,
      render: (name: string, p) => (
        <Typography.Text
          editable={{
            tooltip: "改显示名",
            onChange: (v) => {
              const next = v.trim();
              if (!next || next === name) return;
              rename.mutate(
                { personId: p.id, displayName: next },
                { onSuccess: () => message.success("显示名已更新"), onError: (e) => message.error(errorMessage(e)) },
              );
            },
          }}
        >
          {name}
        </Typography.Text>
      ),
    },
    {
      title: "所属组织",
      key: "org",
      width: 180,
      render: (_, p) => (
        <TreeSelect
          size="small"
          style={{ width: "100%" }}
          allowClear
          treeDefaultExpandAll
          value={p.orgUnitId ?? undefined}
          placeholder="厂直属"
          treeData={toTreeOptions(tree)}
          onChange={(v?: string) =>
            setOrg.mutate(
              { personId: p.id, orgUnitId: v ?? null },
              { onSuccess: () => message.success("所属组织已更新"), onError: (e) => message.error(errorMessage(e)) },
            )
          }
        />
      ),
    },
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
        description="停用不删账号。角色都覆盖整厂；所属组织只用于按组织统计焊次，不给权限。退出留库默认留着这个人在示教器上的库文件。点角色标签可收回。"
        extra={
          <Space>
            <Button type="primary" icon={<PlusOutlined />} onClick={() => setOpen(true)}>
              新建账号
            </Button>
          </Space>
        }
      />
      <Card>
        <Table<Account> rowKey="id" columns={columns} dataSource={c?.people ?? []} loading={catalog.isLoading} pagination={{ pageSize: 20, hideOnSinglePage: true }} scroll={{ x: "max-content" }} />
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
          }}
          onFinish={(values) =>
            grant.mutate(
              { personId: values.personId, role: values.role },
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
            <Select
              options={ROLES.map((r) => ({ value: r.value, label: `${r.label}（${r.hint}）` }))}
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
