import { KeyOutlined, LockOutlined } from "@ant-design/icons";
import { Alert, App, Button, Card, Form, Input, Typography } from "antd";
import { Link, useNavigate } from "react-router";
import { paths } from "@/app/routes";
import { errorMessage } from "@/shared/api/client";
import { useClaim, type ClaimInput } from "./api";

type FormValues = ClaimInput & { confirm: string };

// 认领工厂：一张建厂码 + 自设密码，不再抄工厂 UUID 和激活码。
export function ClaimPage() {
  const claim = useClaim();
  const navigate = useNavigate();
  const { message } = App.useApp();

  return (
    <Card>
      <Typography.Title level={3} style={{ marginTop: 0 }}>
        认领工厂
      </Typography.Title>
      <Typography.Paragraph type="secondary">
        把云端给的建厂码贴在这里并自设日常密码。WAN 看不到这密码。本厂其他账号仍走「激活」。
      </Typography.Paragraph>
      <Form<FormValues>
        layout="vertical"
        requiredMark={false}
        onFinish={({ confirm: _confirm, ...values }) =>
          claim.mutate(values, {
            onSuccess: (out) => {
              message.success("已认领并激活，请用刚设的密码登录");
              navigate(paths.login, { replace: true, state: { loginName: out.saLogin } });
            },
          })
        }
      >
        <Form.Item name="enrollmentCode" label="建厂码" rules={[{ required: true, message: "请输入建厂码" }]}>
          <Input prefix={<KeyOutlined />} autoComplete="one-time-code" autoFocus />
        </Form.Item>
        <Form.Item
          name="password"
          label="自设日常密码"
          rules={[
            { required: true, message: "请设置日常密码" },
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
        {claim.isError ? <Alert type="error" showIcon message={errorMessage(claim.error)} style={{ marginBottom: 16 }} /> : null}
        <Button type="primary" htmlType="submit" block loading={claim.isPending}>
          认领并激活
        </Button>
      </Form>
      <Typography.Paragraph style={{ marginTop: 16, marginBottom: 0, textAlign: "center" }}>
        已经认领过？<Link to={paths.login}>去登录</Link>
      </Typography.Paragraph>
    </Card>
  );
}
