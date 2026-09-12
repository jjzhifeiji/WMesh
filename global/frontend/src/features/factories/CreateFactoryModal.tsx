import { Alert, Button, Form, Input, Modal, Typography } from "antd";
import { errorMessage } from "@/shared/api/client";
import { SecretOnce } from "@/shared/ui/SecretOnce";
import { useCreateFactory, type CreateFactoryInput } from "./api";

type Props = {
  open: boolean;
  onClose: () => void;
};

// 建厂两步走：填表 → 展示一次性建厂码；抄走前不让误关。
export function CreateFactoryModal({ open, onClose }: Props) {
  const [form] = Form.useForm<CreateFactoryInput>();
  const create = useCreateFactory();
  const created = create.data;

  const close = () => {
    form.resetFields();
    create.reset();
    onClose();
  };

  return (
    <Modal
      title={created ? "工厂已创建" : "创建工厂"}
      open={open}
      onCancel={created ? undefined : close}
      closable={!created}
      maskClosable={false}
      destroyOnHidden
      footer={
        created ? (
          <Button type="primary" onClick={close}>
            我已抄走建厂码，关闭
          </Button>
        ) : (
          <>
            <Button onClick={close}>取消</Button>
            <Button type="primary" loading={create.isPending} onClick={() => form.submit()}>
              创建并下发建厂码
            </Button>
          </>
        )
      }
    >
      {created ? (
        <>
          <Typography.Paragraph>
            把建厂码交给「{created.factory.name}」的初始超管；对方在厂内管理端贴上码并自设日常密码即完成激活，不必再填工厂 ID。
          </Typography.Paragraph>
          <SecretOnce
            title="建厂码只显示这一次，WAN 不保存原文"
            items={[
              { label: "建厂码", value: created.enrollmentToken },
              { label: "初始超管登录名", value: form.getFieldValue("saLogin") as string },
            ]}
          />
        </>
      ) : (
        <Form<CreateFactoryInput>
          form={form}
          layout="vertical"
          requiredMark={false}
          onFinish={(values) => create.mutate(values)}
        >
          <Form.Item name="name" label="工厂名称" rules={[{ required: true, message: "请输入工厂名称" }]}>
            <Input maxLength={64} autoFocus />
          </Form.Item>
          <Form.Item
            name="saLogin"
            label="初始超管登录名"
            extra="厂内唯一；之后由超管自己在厂内改名、建人、建组织。"
            rules={[{ required: true, message: "请输入登录名" }]}
          >
            <Input maxLength={64} autoComplete="off" />
          </Form.Item>
          <Form.Item name="saDisplay" label="初始超管显示名" rules={[{ required: true, message: "请输入显示名" }]}>
            <Input maxLength={64} />
          </Form.Item>
          {create.isError ? <Alert type="error" showIcon message={errorMessage(create.error)} /> : null}
        </Form>
      )}
    </Modal>
  );
}
