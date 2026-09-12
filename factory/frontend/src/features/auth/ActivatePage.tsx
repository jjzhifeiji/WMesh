import { KeyOutlined, LockOutlined, ShopOutlined, UserOutlined } from "@ant-design/icons";
import { Alert, App, Button, Card, Form, Input, Select, Space, Typography } from "antd";
import { Link, useNavigate } from "react-router";
import { paths } from "@/app/routes";
import { errorMessage } from "@/shared/api/client";
import { useSession } from "@/shared/auth/session";
import { useActivate, useSite, type ActivateInput } from "./api";
import { FactoryClosedNotice, FactoryStatusTag, factoriesBlocked, factoryOptionLabel, pickFactoryId, selectableFactories } from "./status";

type FormValues = ActivateInput & { confirm: string };

const uuidRule = { pattern: /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i, message: "工厂 ID 应为 UUID" };

// 激活：仅初始超管把 8 位激活码换成自设日常密码；本机已认领则不必手抄工厂 ID。
export function ActivatePage() {
  const { factoryId } = useSession();
  const navigate = useNavigate();
  const activate = useActivate();
  const site = useSite();
  const { message } = App.useApp();
  const [form] = Form.useForm<FormValues>();
  const factories = site.data?.factories ?? [];
  const listed = selectableFactories(factories);
  const only = listed.length === 1 ? listed[0] : undefined;
  const presetId = pickFactoryId(listed, factoryId);
  const hideFactoryId = Boolean(presetId) || site.isLoading;
  const watchedId = Form.useWatch("factoryId", form);
  const selected = listed.find((f) => f.id === watchedId) ?? only;
  const blocked = factoriesBlocked(factories, selected);

  return (
    <Card>
      <Space align="center" style={{ marginBottom: 8 }}>
        <Typography.Title level={3} style={{ margin: 0 }}>
          激活账号
        </Typography.Title>
        <FactoryStatusTag status={selected?.status ?? (factories.length > 0 && factories.every((f) => f.status === "retired") ? "retired" : undefined)} />
      </Space>
      <Typography.Paragraph type="secondary">
        仅初始超管用 8 位激活码自设日常密码。普通账号默认密码为登录名+123456，直接登录。第一次开厂请用建厂码认领。
      </Typography.Paragraph>
      <FactoryClosedNotice factories={factories} factoryId={watchedId || only?.id} action="激活" />
      <Form<FormValues>
        form={form}
        layout="vertical"
        requiredMark={false}
        initialValues={{ factoryId: presetId }}
        onFinish={({ confirm: _confirm, ...values }) =>
          activate.mutate(
            { ...values, factoryId: values.factoryId || presetId },
            {
              onSuccess: () => {
                message.success("已激活，请用刚设的密码登录");
                navigate(paths.login, { replace: true, state: { loginName: values.loginName } });
              },
            },
          )
        }
      >
        {listed.length <= 1 ? (
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
        <Form.Item
          name="activationToken"
          label="激活码"
          rules={[
            { required: true, message: "请输入激活码" },
            { pattern: /^\d{8}$/, message: "激活码为 8 位数字" },
          ]}
        >
          <Input prefix={<KeyOutlined />} maxLength={8} inputMode="numeric" autoComplete="one-time-code" disabled={blocked} />
        </Form.Item>
        <Form.Item
          name="password"
          label="自设日常密码"
          rules={[
            { required: true, message: "请设置日常密码" },
            { min: 8, message: "至少 8 位" },
          ]}
        >
          <Input.Password prefix={<LockOutlined />} autoComplete="new-password" disabled={blocked} />
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
          <Input.Password prefix={<LockOutlined />} autoComplete="new-password" disabled={blocked} />
        </Form.Item>
        {activate.isError ? <Alert type="error" showIcon message={errorMessage(activate.error)} style={{ marginBottom: 16 }} /> : null}
        <Button type="primary" htmlType="submit" block loading={activate.isPending} disabled={blocked}>
          激活
        </Button>
      </Form>
      <Typography.Paragraph style={{ marginTop: 16, marginBottom: 0, textAlign: "center" }}>
        已经激活过？<Link to={paths.login}>去登录</Link>
        {" · "}
        拿到建厂码？<Link to={paths.claim}>认领工厂</Link>
      </Typography.Paragraph>
    </Card>
  );
}
