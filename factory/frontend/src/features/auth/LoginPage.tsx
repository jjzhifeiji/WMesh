import { LockOutlined, ShopOutlined, UserOutlined } from "@ant-design/icons";
import { Alert, Button, Card, Form, Input, Typography } from "antd";
import { Link, Navigate, useLocation, useNavigate } from "react-router";
import { paths } from "@/app/routes";
import { errorMessage } from "@/shared/api/client";
import { useSession } from "@/shared/auth/session";
import { useLogin, type LoginInput } from "./api";

const uuidRule = { pattern: /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i, message: "工厂 ID 应为 UUID" };

// 登录本厂账号；工厂 ID 是云端建厂时发下来的，记住后下次免填。
export function LoginPage() {
  const { token, factoryId } = useSession();
  const navigate = useNavigate();
  const location = useLocation();
  const login = useLogin();
  const state = location.state as { from?: string; loginName?: string } | null;
  const from = state?.from ?? paths.dashboard;

  if (token && factoryId) return <Navigate to={from} replace />;

  return (
    <Card>
      <Typography.Title level={3} style={{ marginTop: 0 }}>
        WMesh 厂内管理
      </Typography.Title>
      <Typography.Paragraph type="secondary">用本厂账号登录；初次使用请先用激活口令激活。</Typography.Paragraph>
      <Form<LoginInput>
        layout="vertical"
        requiredMark={false}
        initialValues={{ factoryId, loginName: state?.loginName }}
        onFinish={(values) => login.mutate(values, { onSuccess: () => navigate(from, { replace: true }) })}
      >
        <Form.Item name="factoryId" label="工厂 ID" rules={[{ required: true, message: "请输入工厂 ID" }, uuidRule]}>
          <Input prefix={<ShopOutlined />} placeholder="xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx" autoComplete="off" />
        </Form.Item>
        <Form.Item name="loginName" label="登录名" rules={[{ required: true, message: "请输入登录名" }]}>
          <Input prefix={<UserOutlined />} autoComplete="username" />
        </Form.Item>
        <Form.Item name="password" label="口令" rules={[{ required: true, message: "请输入口令" }]}>
          <Input.Password prefix={<LockOutlined />} autoComplete="current-password" />
        </Form.Item>
        {login.isError ? <Alert type="error" showIcon message={errorMessage(login.error)} style={{ marginBottom: 16 }} /> : null}
        <Button type="primary" htmlType="submit" block loading={login.isPending}>
          登录
        </Button>
      </Form>
      <Typography.Paragraph style={{ marginTop: 16, marginBottom: 0, textAlign: "center" }}>
        拿到激活口令的新账号？<Link to={paths.activate}>去激活</Link>
      </Typography.Paragraph>
    </Card>
  );
}
