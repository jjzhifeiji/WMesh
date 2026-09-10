import { LockOutlined, UserOutlined } from "@ant-design/icons";
import { Alert, Button, Card, Form, Input, Typography } from "antd";
import { Navigate, useLocation, useNavigate } from "react-router";
import { paths } from "@/app/routes";
import { errorMessage } from "@/shared/api/client";
import { useSessionToken } from "@/shared/auth/session";
import { useLogin, type LoginInput } from "./api";

// 只登录唯一 WAN 管理员；已登录的直接回后台。
export function LoginPage() {
  const token = useSessionToken();
  const navigate = useNavigate();
  const location = useLocation();
  const login = useLogin();
  const from = (location.state as { from?: string } | null)?.from ?? paths.dashboard;

  if (token) return <Navigate to={from} replace />;

  return (
    <Card>
      <Typography.Title level={3} style={{ marginTop: 0 }}>
        WMesh 云端总控
      </Typography.Title>
      <Typography.Paragraph type="secondary">只登录唯一 WAN 管理员，不代管任何厂内人员。</Typography.Paragraph>
      <Form<LoginInput>
        layout="vertical"
        requiredMark={false}
        onFinish={(values) => login.mutate(values, { onSuccess: () => navigate(from, { replace: true }) })}
      >
        <Form.Item name="loginName" label="登录名" rules={[{ required: true, message: "请输入登录名" }]}>
          <Input prefix={<UserOutlined />} autoComplete="username" autoFocus />
        </Form.Item>
        <Form.Item name="password" label="口令" rules={[{ required: true, message: "请输入口令" }]}>
          <Input.Password prefix={<LockOutlined />} autoComplete="current-password" />
        </Form.Item>
        {login.isError ? <Alert type="error" showIcon message={errorMessage(login.error)} style={{ marginBottom: 16 }} /> : null}
        <Button type="primary" htmlType="submit" block loading={login.isPending}>
          登录
        </Button>
      </Form>
    </Card>
  );
}
