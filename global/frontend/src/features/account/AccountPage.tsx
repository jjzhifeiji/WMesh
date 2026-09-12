import { LockOutlined } from "@ant-design/icons";
import { App, Button, Card, Col, Descriptions, Form, Input, Row } from "antd";
import { useMe } from "@/features/auth/api";
import { errorMessage } from "@/shared/api/client";
import { IdText } from "@/shared/ui/IdText";
import { PageHeader } from "@/shared/ui/PageHeader";
import { useChangePassword, type ChangePasswordInput } from "./api";

type FormValues = ChangePasswordInput & { confirm: string };

// 我的账号：看自己的身份，改自己的密码。WAN 只有这一名管理员。
export function AccountPage() {
  const me = useMe();
  const change = useChangePassword();
  const { message } = App.useApp();
  const [form] = Form.useForm<FormValues>();

  return (
    <>
      <PageHeader title="我的账号" description="登录名和密码都可以变，稳定身份永远不变。WAN 不代管厂内密码。" />
      <Row gutter={[16, 16]}>
        <Col xs={24} lg={14}>
          <Card title="基本信息" loading={me.isLoading}>
            <Descriptions
              column={1}
              items={[
                { key: "login", label: "登录名", children: me.data?.loginName },
                { key: "id", label: "稳定身份", children: me.data ? <IdText id={me.data.id} /> : null },
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
              onFinish={({ password }) =>
                change.mutate(
                  { password },
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
