import { LockOutlined, ShopOutlined, UserOutlined } from "@ant-design/icons";
import { Alert, Button, Card, Form, Input, Select, Space, Typography } from "antd";
import { Link, Navigate, useLocation, useNavigate } from "react-router";
import { paths } from "@/app/routes";
import { errorMessage } from "@/shared/api/client";
import { useSession } from "@/shared/auth/session";
import { useLogin, useSite, type LoginInput } from "./api";
import { FactoryClosedNotice, FactoryStatusTag, factoriesBlocked, factoryOptionLabel, pickFactoryId, selectableFactories } from "./status";

const uuidRule = { pattern: /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i, message: "工厂 ID 应为 UUID" };

// 登录本厂账号；本机已认领时工厂 ID 由服务端带出，不必手抄。
export function LoginPage() {
  const { token, factoryId } = useSession();
  const navigate = useNavigate();
  const location = useLocation();
  const login = useLogin();
  const site = useSite();
  const [form] = Form.useForm<LoginInput>();
  const state = location.state as { from?: string; loginName?: string } | null;
  const from = state?.from ?? paths.dashboard;
  const factories = site.data?.factories ?? [];
  const listed = selectableFactories(factories);
  const only = listed.length === 1 ? listed[0] : undefined;
  const presetId = pickFactoryId(listed, factoryId);
  const hideFactoryId = Boolean(presetId) || site.isLoading;
  const watchedId = Form.useWatch("factoryId", form);
  const selected = listed.find((f) => f.id === watchedId) ?? only;
  const blocked = factoriesBlocked(factories, selected);

  if (token && factoryId) return <Navigate to={from} replace />;

  return (
    <Card>
      <Space align="center" style={{ marginBottom: 8 }}>
        <Typography.Title level={3} style={{ margin: 0 }}>
          WMesh 厂内管理
        </Typography.Title>
        <FactoryStatusTag status={selected?.status ?? (factories.length > 0 && factories.every((f) => f.status === "retired") ? "retired" : undefined)} />
      </Space>
      <Typography.Paragraph type="secondary">用本厂账号登录。第一次开厂请用云端建厂码认领。</Typography.Paragraph>
      <FactoryClosedNotice factories={factories} factoryId={watchedId || only?.id} />
      <Form<LoginInput>
        form={form}
        layout="vertical"
        requiredMark={false}
        initialValues={{
          factoryId: presetId,
          loginName: state?.loginName || only?.saLogin,
        }}
        onFinish={(values) =>
          login.mutate(
            { ...values, factoryId: values.factoryId || presetId },
            { onSuccess: () => navigate(from, { replace: true }) },
          )
        }
      >
        {listed.length === 0 ? (
          <Form.Item name="factoryId" hidden={hideFactoryId} rules={hideFactoryId ? [] : [{ required: true, message: "请输入工厂 ID" }, uuidRule]}>
            <Input prefix={<ShopOutlined />} placeholder="xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx" autoComplete="off" />
          </Form.Item>
        ) : (
          <Form.Item name="factoryId" label="工厂" rules={[{ required: true, message: "请选择工厂" }]}>
            <Select options={listed.map((f) => ({ value: f.id, label: factoryOptionLabel(f) }))} />
          </Form.Item>
        )}
        <Form.Item name="loginName" label="登录名" rules={[{ required: true, message: "请输入登录名" }]}>
          <Input prefix={<UserOutlined />} autoComplete="username" disabled={blocked} />
        </Form.Item>
        <Form.Item name="password" label="密码" rules={[{ required: true, message: "请输入密码" }]}>
          <Input.Password prefix={<LockOutlined />} autoComplete="current-password" disabled={blocked} />
        </Form.Item>
        {login.isError ? <Alert type="error" showIcon message={errorMessage(login.error)} style={{ marginBottom: 16 }} /> : null}
        <Button type="primary" htmlType="submit" block loading={login.isPending} disabled={blocked}>
          登录
        </Button>
      </Form>
      <Typography.Paragraph style={{ marginTop: 16, marginBottom: 0, textAlign: "center" }}>
        {site.data?.wanConfigured ? (
          <>
            拿到建厂码？<Link to={paths.claim}>认领工厂</Link>
            <br />
          </>
        ) : null}
        初始超管激活码？<Link to={paths.activate}>去激活</Link>
        <br />
        忘了日常密码？找本厂超管在人员页重置，密码改回登录名+123456。
      </Typography.Paragraph>
    </Card>
  );
}
