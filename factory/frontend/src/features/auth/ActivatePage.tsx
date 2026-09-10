import { KeyOutlined, LockOutlined, ShopOutlined, UserOutlined } from "@ant-design/icons";
import { Alert, App, Button, Card, Form, Input, Typography } from "antd";
import { Link, useNavigate } from "react-router";
import { paths } from "@/app/routes";
import { errorMessage } from "@/shared/api/client";
import { useSession } from "@/shared/auth/session";
import { useActivate, type ActivateInput } from "./api";

type FormValues = ActivateInput & { confirm: string };

// 激活：一次性激活口令换成自设日常口令；成功后回登录页并带上登录名。
export function ActivatePage() {
  const { factoryId } = useSession();
  const navigate = useNavigate();
  const activate = useActivate();
  const { message } = App.useApp();

  return (
    <Card>
      <Typography.Title level={3} style={{ marginTop: 0 }}>
        激活账号
      </Typography.Title>
      <Typography.Paragraph type="secondary">激活口令由云端或本厂超管交给你，只能用一次；日常口令由你自己设，任何人都无法代设。</Typography.Paragraph>
      <Form<FormValues>
        layout="vertical"
        requiredMark={false}
        initialValues={{ factoryId }}
        onFinish={({ confirm: _confirm, ...values }) =>
          activate.mutate(values, {
            onSuccess: () => {
              message.success("已激活，请用刚设的口令登录");
              navigate(paths.login, { replace: true, state: { loginName: values.loginName } });
            },
          })
        }
      >
        <Form.Item name="factoryId" label="工厂 ID" rules={[{ required: true, message: "请输入工厂 ID" }]}>
          <Input prefix={<ShopOutlined />} autoComplete="off" />
        </Form.Item>
        <Form.Item name="loginName" label="登录名" rules={[{ required: true, message: "请输入登录名" }]}>
          <Input prefix={<UserOutlined />} autoComplete="username" />
        </Form.Item>
        <Form.Item name="activationToken" label="激活口令" rules={[{ required: true, message: "请输入激活口令" }]}>
          <Input prefix={<KeyOutlined />} autoComplete="one-time-code" />
        </Form.Item>
        <Form.Item
          name="password"
          label="自设日常口令"
          rules={[
            { required: true, message: "请设置日常口令" },
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
        {activate.isError ? <Alert type="error" showIcon message={errorMessage(activate.error)} style={{ marginBottom: 16 }} /> : null}
        <Button type="primary" htmlType="submit" block loading={activate.isPending}>
          激活
        </Button>
      </Form>
      <Typography.Paragraph style={{ marginTop: 16, marginBottom: 0, textAlign: "center" }}>
        已经激活过？<Link to={paths.login}>去登录</Link>
      </Typography.Paragraph>
    </Card>
  );
}
