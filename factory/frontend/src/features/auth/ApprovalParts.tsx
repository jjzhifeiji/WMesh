import { LockOutlined } from "@ant-design/icons";
import { Alert, Button, Form, Input, Result, Typography } from "antd";
import dayjs from "dayjs";
import { errorMessage } from "@/shared/api/client";

// 等云端批准：现场核对码必须显眼，电话里要念给管理员。
export function WaitingApproval({ verifyCode, expiresAt, summary, error, onCancel }: { verifyCode: string; expiresAt: string; summary: string; error?: unknown; onCancel: () => void }) {
  return (
    <>
      <Typography.Paragraph type="secondary">{summary}</Typography.Paragraph>
      <Typography.Paragraph type="secondary" style={{ marginBottom: 4 }}>
        请把核对码告诉云端管理员，对方核对后点批准，这里会自动往下走：
      </Typography.Paragraph>
      <Typography.Title level={1} style={{ margin: "8px 0 16px", letterSpacing: 8, textAlign: "center" }}>
        {verifyCode}
      </Typography.Title>
      {error ? <Alert type="warning" showIcon message={`暂时查不到进度，正在重试：${errorMessage(error)}`} style={{ marginBottom: 16 }} /> : null}
      <Typography.Paragraph type="secondary">申请在 {dayjs(expiresAt).format("YYYY-MM-DD HH:mm")} 前有效。</Typography.Paragraph>
      <Button block onClick={onCancel}>
        放弃这次申请
      </Button>
    </>
  );
}

// 申请被拒绝或已失效，只能重新发起。
export function ApprovalClosed({ rejected, onRetry }: { rejected: boolean; onRetry: () => void }) {
  return (
    <Result
      status={rejected ? "error" : "warning"}
      title={rejected ? "云端拒绝了这次申请" : "这次申请已失效"}
      subTitle={rejected ? "可以和云端管理员确认后重新申请。" : "申请已过期或已经用过，请重新申请。"}
      extra={
        <Button type="primary" onClick={onRetry}>
          重新申请
        </Button>
      }
    />
  );
}

type PasswordValues = { password: string; confirm: string };

// 云端批准后现场设密码；密码只留在厂端。
export function SetPasswordForm({ submitText, loading, error, onSubmit }: { submitText: string; loading: boolean; error: unknown; onSubmit: (password: string) => void }) {
  return (
    <Form<PasswordValues> layout="vertical" requiredMark={false} onFinish={({ password }) => onSubmit(password)}>
      <Form.Item
        name="password"
        label="设置日常密码"
        rules={[
          { required: true, message: "请设置日常密码" },
          { min: 8, message: "至少 8 位" },
        ]}
      >
        <Input.Password prefix={<LockOutlined />} autoComplete="new-password" autoFocus />
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
      {error ? <Alert type="error" showIcon message={errorMessage(error)} style={{ marginBottom: 16 }} /> : null}
      <Button type="primary" htmlType="submit" block loading={loading}>
        {submitText}
      </Button>
    </Form>
  );
}
