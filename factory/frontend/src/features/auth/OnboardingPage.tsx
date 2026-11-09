import { ShopOutlined, UserOutlined } from "@ant-design/icons";
import { Alert, App, Button, Card, Form, Input, Typography } from "antd";
import { useState } from "react";
import { Link, useNavigate } from "react-router";
import { paths } from "@/app/routes";
import { errorMessage } from "@/shared/api/client";
import { ApprovalClosed, SetPasswordForm, WaitingApproval } from "./ApprovalParts";
import { clearTicket, loadTicket, saveTicket, useApplyOnboarding, useClaim, useOnboardingStatus, type ApplyInput, type Ticket } from "./api";

const TICKET_KEY = "wmesh.factory.onboarding";

type EnrollTicket = Ticket & { factoryName: string; saLogin: string };

// 新厂接入：现场填表 → 云端核对批准 → 现场设密码。等待状态存在浏览器里，刷新不丢。
export function OnboardingPage() {
  const [ticket, setTicket] = useState(() => loadTicket<EnrollTicket>(TICKET_KEY));
  const apply = useApplyOnboarding();
  const claim = useClaim();
  const status = useOnboardingStatus(ticket?.secret);
  const navigate = useNavigate();
  const { message } = App.useApp();

  const restart = () => {
    clearTicket(TICKET_KEY);
    setTicket(null);
  };

  const body = () => {
    if (!ticket) {
      return (
        <>
          <Typography.Paragraph type="secondary">填好工厂和超管账号提交，云端管理员批准后，回到这里设置密码就能使用。</Typography.Paragraph>
          <Form<ApplyInput>
            layout="vertical"
            requiredMark={false}
            onFinish={(values) =>
              apply.mutate(values, {
                onSuccess: (out) => {
                  const next = { ...out, factoryName: values.factoryName.trim(), saLogin: values.saLogin.trim() };
                  saveTicket(TICKET_KEY, next);
                  setTicket(next);
                },
              })
            }
          >
            <Form.Item name="factoryName" label="工厂名称" rules={[{ required: true, whitespace: true, message: "请输入工厂名称" }, { max: 64, message: "最多 64 个字" }]}>
              <Input prefix={<ShopOutlined />} autoFocus />
            </Form.Item>
            <Form.Item name="saLogin" label="超管登录名" rules={[{ required: true, whitespace: true, message: "请输入超管登录名" }, { max: 64, message: "最多 64 个字" }]}>
              <Input prefix={<UserOutlined />} autoComplete="off" />
            </Form.Item>
            <Form.Item name="saDisplay" label="超管显示名" rules={[{ required: true, whitespace: true, message: "请输入超管显示名" }, { max: 64, message: "最多 64 个字" }]}>
              <Input />
            </Form.Item>
            {apply.isError ? <Alert type="error" showIcon message={errorMessage(apply.error)} style={{ marginBottom: 16 }} /> : null}
            <Button type="primary" htmlType="submit" block loading={apply.isPending}>
              提交申请
            </Button>
          </Form>
        </>
      );
    }
    const state = status.data?.status;
    if (state === "rejected" || state === "expired") return <ApprovalClosed rejected={state === "rejected"} onRetry={restart} />;
    if (state === "approved") {
      return (
        <>
          <Typography.Paragraph type="secondary">
            云端已批准「{status.data?.factoryName || ticket.factoryName}」。设置超管 {ticket.saLogin} 的日常密码，这个密码云端看不到。
          </Typography.Paragraph>
          <SetPasswordForm
            submitText="完成接入"
            loading={claim.isPending}
            error={claim.error}
            onSubmit={(password) =>
              claim.mutate(
                { enrollmentCode: ticket.secret, password },
                {
                  onSuccess: (out) => {
                    clearTicket(TICKET_KEY);
                    message.success("接入完成，请用刚设的密码登录");
                    navigate(paths.login, { replace: true, state: { loginName: out.saLogin } });
                  },
                },
              )
            }
          />
        </>
      );
    }
    return <WaitingApproval verifyCode={ticket.verifyCode} expiresAt={ticket.expiresAt} summary={`工厂「${ticket.factoryName}」，超管 ${ticket.saLogin}。`} error={status.error} onCancel={restart} />;
  };

  return (
    <Card>
      <Typography.Title level={3} style={{ marginTop: 0 }}>
        接入新工厂
      </Typography.Title>
      {body()}
      <Typography.Paragraph style={{ marginTop: 16, marginBottom: 0, textAlign: "center" }}>
        已经接入过？<Link to={paths.login}>去登录</Link>
      </Typography.Paragraph>
    </Card>
  );
}
