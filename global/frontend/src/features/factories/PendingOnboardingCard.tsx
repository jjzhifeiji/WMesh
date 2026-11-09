import { App, Alert, Button, Card, Form, Input, Modal, Popconfirm, Space, Table, Tag, Typography, type TableColumnsType } from "antd";
import { useState } from "react";
import { errorMessage } from "@/shared/api/client";
import { formatTime } from "@/shared/format";
import { useApproveOnboarding, usePendingOnboarding, useRejectOnboarding, type OnboardingRequest } from "./api";

function kindTag(kind: OnboardingRequest["kind"]) {
  return kind === "enroll" ? <Tag color="blue">新厂接入</Tag> : <Tag color="orange">找回超管密码</Tag>;
}

// 批准前必须和现场核对码；新厂接入可以顺手改厂名。
function ApproveModal({ request, onClose }: { request: OnboardingRequest | null; onClose: () => void }) {
  const approve = useApproveOnboarding();
  const { message } = App.useApp();
  const [form] = Form.useForm<{ name: string }>();
  const isEnroll = request?.kind === "enroll";

  const submit = (name: string) => {
    if (!request) return;
    approve.mutate(
      { id: request.id, name: isEnroll ? name : "" },
      {
        onSuccess: () => {
          message.success(isEnroll ? "已批准，现场设好密码后接入完成" : "已批准，现场 1 小时内可设新密码");
          onClose();
        },
      },
    );
  };

  return (
    <Modal
      open={request !== null}
      title={isEnroll ? "批准新厂接入" : "批准找回超管密码"}
      okText="已核对，批准"
      cancelText="取消"
      confirmLoading={approve.isPending}
      onCancel={onClose}
      onOk={() => form.submit()}
      destroyOnHidden
      afterOpenChange={(open) => {
        if (!open) approve.reset();
      }}
    >
      {request ? (
        <Space direction="vertical" size="middle" style={{ width: "100%" }}>
          <Alert
            type="warning"
            showIcon
            message={
              <>
                请先和现场电话或微信确认核对码：<Typography.Text strong style={{ fontSize: 18 }}>{request.verifyCode}</Typography.Text>
              </>
            }
            description={isEnroll ? undefined : `批准后，现场可以直接重设「${request.factoryName}」初始超管 ${request.saLogin} 的密码，并使其所有登录失效。`}
          />
          <Form form={form} layout="vertical" requiredMark={false} initialValues={{ name: request.factoryName }} onFinish={(v) => submit(v.name)}>
            {isEnroll ? (
              <Form.Item name="name" label="工厂名称" rules={[{ required: true, whitespace: true, message: "请输入工厂名称" }, { max: 64, message: "最多 64 个字" }]}>
                <Input />
              </Form.Item>
            ) : null}
            <Typography.Text type="secondary">
              初始超管：{request.saDisplay}（{request.saLogin}）；来源 {request.sourceAddr || "—"}
            </Typography.Text>
          </Form>
          {approve.isError ? <Alert type="error" showIcon message={errorMessage(approve.error)} /> : null}
        </Space>
      ) : null}
    </Modal>
  );
}

// 待接入列表：现场发起的新厂接入和超管找回，没有申请时不占位。
export function PendingOnboardingCard() {
  const pending = usePendingOnboarding();
  const reject = useRejectOnboarding();
  const { message } = App.useApp();
  const [approving, setApproving] = useState<OnboardingRequest | null>(null);
  const rows = pending.data?.requests ?? [];

  if (pending.isError && rows.length === 0) return <Alert type="error" showIcon message={`读不到待接入申请：${errorMessage(pending.error)}`} style={{ marginBottom: 16 }} />;
  if (rows.length === 0) return null;

  const columns: TableColumnsType<OnboardingRequest> = [
    { title: "类型", width: 130, render: (_, row) => kindTag(row.kind) },
    { title: "工厂名称", dataIndex: "factoryName", width: 180 },
    { title: "初始超管", width: 180, render: (_, row) => `${row.saDisplay}（${row.saLogin}）` },
    { title: "核对码", dataIndex: "verifyCode", width: 110, render: (v: string) => <Typography.Text strong>{v}</Typography.Text> },
    { title: "来源", dataIndex: "sourceAddr", width: 150 },
    { title: "厂端版本", dataIndex: "serviceVersionName", width: 110, render: (v: string) => v || "—" },
    { title: "申请时间", dataIndex: "createdAt", width: 170, render: (v: string) => formatTime(v) },
    { title: "到期时间", dataIndex: "expiresAt", width: 170, render: (v: string) => formatTime(v) },
    {
      title: "操作",
      width: 150,
      fixed: "right",
      render: (_, row) => (
        <Space size="small">
          <Button size="small" type="primary" onClick={() => setApproving(row)}>
            批准
          </Button>
          <Popconfirm
            title="拒绝这条申请？"
            description="现场会看到已拒绝，可以修改后重新申请。"
            onConfirm={() =>
              reject.mutate(row.id, {
                onSuccess: () => message.success("已拒绝"),
                onError: (err) => message.error(errorMessage(err)),
              })
            }
          >
            <Button size="small" danger loading={reject.isPending && reject.variables === row.id}>
              拒绝
            </Button>
          </Popconfirm>
        </Space>
      ),
    },
  ];

  return (
    <Card title={`待接入（${rows.length}）`} style={{ marginBottom: 16 }}>
      <Table<OnboardingRequest> rowKey="id" columns={columns} dataSource={rows} pagination={false} scroll={{ x: 1250 }} />
      <ApproveModal request={approving} onClose={() => setApproving(null)} />
    </Card>
  );
}
