import { App, Button, Card, Form, InputNumber, Space, Switch, Typography } from "antd";
import { useEffect } from "react";
import { errorMessage } from "@/shared/api/client";
import { PageHeader } from "@/shared/ui/PageHeader";
import { useClientPolicy, useSaveClientPolicy, type ClientPolicyInput } from "./api";

type FormValues = {
  persistUnwrapKey: boolean;
  keyTtlSeconds: number;
  encryptPouch: boolean;
};

// 本厂全部 Client 共用这一份策略；超管可改，现场设备只收更高修订。
export function ClientPolicyPage() {
  const policy = useClientPolicy();
  const save = useSaveClientPolicy();
  const { message } = App.useApp();
  const [form] = Form.useForm<FormValues>();
  const data = policy.data;

  useEffect(() => {
    if (!data) return;
    form.setFieldsValue({
      persistUnwrapKey: data.persistUnwrapKey,
      keyTtlSeconds: data.keyTtlSeconds,
      encryptPouch: data.encryptPouch,
    });
  }, [data, form]);

  function persist(patch: Partial<FormValues>) {
    if (!data) return;
    const values = form.getFieldsValue();
    save.mutate(
      {
        maxCachedProjects: data.maxCachedProjects,
        cacheScope: "all",
        persistUnwrapKey: patch.persistUnwrapKey ?? values.persistUnwrapKey ?? false,
        keyTtlSeconds: patch.keyTtlSeconds ?? values.keyTtlSeconds ?? 0,
        encryptPouch: patch.encryptPouch ?? values.encryptPouch ?? true,
        extra: data.extra ?? {},
      } satisfies ClientPolicyInput,
      {
        onSuccess: () => message.success("策略已保存"),
        onError: (e) => {
          form.setFieldsValue({
            persistUnwrapKey: data.persistUnwrapKey,
            keyTtlSeconds: data.keyTtlSeconds,
            encryptPouch: data.encryptPouch,
          });
          message.error(errorMessage(e));
        },
      },
    );
  }

  return (
    <>
      <PageHeader title="Client 策略" description="对本厂全部现场设备生效。开关当场保存；数字改完点保存。" />
      <Card loading={policy.isLoading} style={{ maxWidth: 560 }}>
        <Space direction="vertical" size={4} style={{ marginBottom: 16 }}>
          <Typography.Text type="secondary">当前修订 {data?.revision ?? "—"}</Typography.Text>
        </Space>
        <Form<FormValues>
          form={form}
          layout="vertical"
          requiredMark={false}
          disabled={!data || save.isPending}
          onFinish={(values) => persist(values)}
        >
          <Form.Item
            name="encryptPouch"
            label="本机数据库加密"
            extra="开启用 SQLCipher；关闭后平板可用 Database Inspector。切换后设备重新登录会重建本机库。"
            valuePropName="checked"
          >
            <Switch
              checkedChildren="加密"
              unCheckedChildren="明文"
              onChange={(checked) => persist({ encryptPouch: checked })}
            />
          </Form.Item>
          <Form.Item
            name="persistUnwrapKey"
            label="解封钥落盘"
            extra="开启则解封钥随登录落盘，杀进程后可继续离线。关闭则只留内存，进程退出须重新登录。工艺明文永不进库。退出登录或登录到期都会清钥。"
            valuePropName="checked"
          >
            <Switch
              checkedChildren="落盘"
              unCheckedChildren="仅内存"
              onChange={(checked) => persist({ persistUnwrapKey: checked })}
            />
          </Form.Item>
          <Form.Item
            name="keyTtlSeconds"
            label="登录时效（秒）"
            extra="0 表示直到主动退出。到期后下次打开须重新登录并清钥。已登录进程中途不踢出，可离线焊接。"
            rules={[{ required: true, message: "请填写时效" }]}
          >
            <InputNumber min={0} precision={0} style={{ width: "100%" }} />
          </Form.Item>
          <Button type="primary" htmlType="submit" loading={save.isPending}>
            保存
          </Button>
        </Form>
      </Card>
    </>
  );
}
