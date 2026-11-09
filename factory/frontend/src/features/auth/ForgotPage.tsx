import { Alert, App, Button, Card, Form, Select, Typography } from "antd";
import { useState } from "react";
import { Link, useNavigate } from "react-router";
import { paths } from "@/app/routes";
import { errorMessage } from "@/shared/api/client";
import { useSession } from "@/shared/auth/session";
import { ApprovalClosed, SetPasswordForm, WaitingApproval } from "./ApprovalParts";
import { clearTicket, loadTicket, saveTicket, useCompleteSAReset, useOnboardingStatus, useSite, useStartSAReset, type Ticket } from "./api";
import { factoryOptionLabel, loginFactories, pickFactoryId } from "./status";

const TICKET_KEY = "wmesh.factory.sa-reset";

type ResetTicket = Ticket & { factoryId: string; factoryName: string; saLogin: string };

// 找回初始超管密码：现场发起 → 云端核对批准 → 现场设新密码。普通账号仍由超管重置。
export function ForgotPage() {
  const [ticket, setTicket] = useState(() => loadTicket<ResetTicket>(TICKET_KEY));
  const site = useSite();
  const start = useStartSAReset();
  const complete = useCompleteSAReset();
  const status = useOnboardingStatus(ticket?.secret);
  const { factoryId } = useSession();
  const navigate = useNavigate();
  const { message } = App.useApp();
  const listed = loginFactories(site.data?.factories ?? [], factoryId).filter((f) => f.status !== "retired");

  const restart = () => {
    clearTicket(TICKET_KEY);
    setTicket(null);
  };

  const body = () => {
    if (!ticket) {
      return (
        <>
          <Typography.Paragraph type="secondary">只用于找回工厂初始超管的密码。提交后请联系云端管理员核对批准；普通账号忘记密码找本厂超管重置。</Typography.Paragraph>
          <Form<{ factoryId: string }>
            key={listed.length}
            layout="vertical"
            requiredMark={false}
            initialValues={{ factoryId: pickFactoryId(listed, factoryId) || undefined }}
            onFinish={(values) => {
              const chosen = listed.find((f) => f.id === values.factoryId);
              start.mutate(values.factoryId, {
                onSuccess: (out) => {
                  const next = { ...out, factoryId: values.factoryId, factoryName: chosen?.name ?? "", saLogin: chosen?.saLogin ?? "" };
                  saveTicket(TICKET_KEY, next);
                  setTicket(next);
                },
              });
            }}
          >
            <Form.Item name="factoryId" label="工厂" rules={[{ required: true, message: "请选择工厂" }]}>
              <Select loading={site.isLoading} options={listed.map((f) => ({ value: f.id, label: `${factoryOptionLabel(f)}（超管 ${f.saLogin}）` }))} />
            </Form.Item>
            {site.isError ? <Alert type="error" showIcon message={errorMessage(site.error)} style={{ marginBottom: 16 }} /> : null}
            {start.isError ? <Alert type="error" showIcon message={errorMessage(start.error)} style={{ marginBottom: 16 }} /> : null}
            <Button type="primary" htmlType="submit" block loading={start.isPending}>
              申请找回
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
          <Typography.Paragraph type="secondary">云端已批准。设置超管 {ticket.saLogin} 的新密码，旧密码和所有登录会立刻失效。</Typography.Paragraph>
          <SetPasswordForm
            submitText="设置新密码"
            loading={complete.isPending}
            error={complete.error}
            onSubmit={(password) =>
              complete.mutate(
                { factoryId: ticket.factoryId, secret: ticket.secret, password },
                {
                  onSuccess: (out) => {
                    clearTicket(TICKET_KEY);
                    message.success("新密码已生效，请登录");
                    navigate(paths.login, { replace: true, state: { loginName: out.loginName } });
                  },
                },
              )
            }
          />
        </>
      );
    }
    return <WaitingApproval verifyCode={ticket.verifyCode} expiresAt={ticket.expiresAt} summary={`找回工厂「${ticket.factoryName}」超管 ${ticket.saLogin} 的密码。`} error={status.error} onCancel={restart} />;
  };

  return (
    <Card>
      <Typography.Title level={3} style={{ marginTop: 0 }}>
        找回超管密码
      </Typography.Title>
      {body()}
      <Typography.Paragraph style={{ marginTop: 16, marginBottom: 0, textAlign: "center" }}>
        <Link to={paths.login}>返回登录</Link>
      </Typography.Paragraph>
    </Card>
  );
}
