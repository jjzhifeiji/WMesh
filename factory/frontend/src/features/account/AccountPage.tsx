import { LockOutlined } from "@ant-design/icons";
import { App, Button, Card, Col, Descriptions, Form, Input, Row, Space, Tag, Typography } from "antd";
import { unitName, useCatalog } from "@/features/catalog/api";
import { errorMessage } from "@/shared/api/client";
import { useSession } from "@/shared/auth/session";
import { roleLabel } from "@/shared/labels";
import { IdText } from "@/shared/ui/IdText";
import { PageHeader } from "@/shared/ui/PageHeader";
import { StatusTag } from "@/shared/ui/StatusTag";
import { useChangePassword, useSetMyDisplayName, type ChangePasswordInput } from "./api";

type FormValues = ChangePasswordInput & { confirm: string };

// 我的账号：看自己的身份、角色和所属组织，改自己的显示名和密码；人人可用。
export function AccountPage() {
  const catalog = useCatalog();
  const { factoryId } = useSession();
  const change = useChangePassword();
  const rename = useSetMyDisplayName();
  const { message } = App.useApp();
  const [form] = Form.useForm<FormValues>();
  const c = catalog.data;

  return (
    <>
      <PageHeader title="我的账号" description="显示名和密码自己改；登录名、角色和所属组织由超管在人员页改。" />
      <Row gutter={[16, 16]}>
        <Col xs={24} lg={14}>
          <Card title="基本信息" loading={catalog.isLoading}>
            <Descriptions
              column={1}
              items={[
                {
                  key: "display",
                  label: "显示名",
                  children: c ? (
                    <Typography.Text
                      editable={{
                        tooltip: "改显示名",
                        onChange: (v) => {
                          const name = v.trim();
                          if (!name || name === c.me.displayName) return;
                          rename.mutate(name, { onSuccess: () => message.success("显示名已更新"), onError: (e) => message.error(errorMessage(e)) });
                        },
                      }}
                    >
                      {c.me.displayName}
                    </Typography.Text>
                  ) : null,
                },
                { key: "login", label: "登录名", children: c?.me.loginName },
                { key: "status", label: "状态", children: c ? <StatusTag status={c.me.status} /> : null },
                { key: "org", label: "所属组织", children: c ? unitName(c, c.me.orgUnitId) : null },
                { key: "id", label: "稳定身份", children: c ? <IdText id={c.me.id} /> : null },
                { key: "factory", label: "所属工厂", children: <IdText id={factoryId} /> },
                {
                  key: "roles",
                  label: "我的角色",
                  children: (
                    <Space wrap>
                      {c?.myGrants.map((g) => (
                        <Tag key={g.id} color="blue">
                          {roleLabel(g.role)}
                        </Tag>
                      ))}
                      {c && c.myGrants.length === 0 ? <Tag>还没有任何角色</Tag> : null}
                    </Space>
                  ),
                },
              ]}
            />
          </Card>
        </Col>
        <Col xs={24} lg={10}>
          <Card title="修改密码">
            <Form<FormValues>
              form={form}
              layout="vertical"
              requiredMark={false}
              onFinish={({ currentPassword, password }) =>
                change.mutate(
                  { currentPassword, password },
                  {
                    onSuccess: () => {
                      message.success("密码已更新");
                      form.resetFields();
                    },
                    onError: (e) => message.error(errorMessage(e)),
                  },
                )
              }
            >
              <Form.Item name="currentPassword" label="当前密码" rules={[{ required: true, message: "请输入当前密码" }]}>
                <Input.Password prefix={<LockOutlined />} autoComplete="current-password" />
              </Form.Item>
              <Form.Item
                name="password"
                label="新密码"
                rules={[
                  { required: true, message: "请输入新密码" },
                  { min: 8, message: "至少 8 位" },
                ]}
              >
                <Input.Password prefix={<LockOutlined />} autoComplete="new-password" />
              </Form.Item>
              <Form.Item
                name="confirm"
                label="再输一次"
                dependencies={["password"]}
                rules={[
                  { required: true, message: "请再输一次" },
                  ({ getFieldValue }) => ({
                    validator: (_, v) => (v === getFieldValue("password") ? Promise.resolve() : Promise.reject(new Error("两次输入不一致"))),
                  }),
                ]}
              >
                <Input.Password prefix={<LockOutlined />} autoComplete="new-password" />
              </Form.Item>
              <Button type="primary" htmlType="submit" loading={change.isPending}>
                更新密码
              </Button>
            </Form>
          </Card>
        </Col>
      </Row>
    </>
  );
}
