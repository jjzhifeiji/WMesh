import { Alert, Button, Form, Input, Modal, Typography } from "antd";
import { errorMessage } from "@/shared/api/client";
import { SecretOnce } from "@/shared/ui/SecretOnce";
import { useCreateFactory, type CreateFactoryInput } from "./api";

type Props = {
  open: boolean;
  onClose: () => void;
};

// 建厂两步走：填表 → 展示一次性激活口令；口令抄走前不让误关。
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
            我已抄走口令，关闭
          </Button>
        ) : (
          <>
            <Button onClick={close}>取消</Button>
            <Button type="primary" loading={create.isPending} onClick={() => form.submit()}>
              创建并下发激活口令
            </Button>
          </>
        )
      }
    >
      {created ? (
        <>
          <Typography.Paragraph>
            把下面三项交给「{created.factory.name}」的初始超管；对方在厂内管理端用激活口令自设日常口令后即可登录。
          </Typography.Paragraph>
          <SecretOnce
            title="激活口令只显示这一次，WAN 不保存"
            items={[
              { label: "工厂 ID", value: created.factory.id },
              { label: "初始超管登录名", value: form.getFieldValue("saLogin") as string },
              { label: "激活口令", value: created.activationToken },
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
