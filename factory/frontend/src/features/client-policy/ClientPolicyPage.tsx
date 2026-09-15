import { App, Button, Card, Form, InputNumber, Radio, Space, Switch, Typography } from "antd";
import { useEffect } from "react";
import { errorMessage } from "@/shared/api/client";
import { PageHeader } from "@/shared/ui/PageHeader";
import { useClientPolicy, useSaveClientPolicy, type CacheScope, type ClientPolicyInput } from "./api";

type FormValues = {
  maxCachedProjects: number;
  cacheScope: CacheScope;
  persistUnwrapKey: boolean;
  keyTtlSeconds: number;
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
      maxCachedProjects: data.maxCachedProjects,
      cacheScope: data.cacheScope,
      persistUnwrapKey: data.persistUnwrapKey,
      keyTtlSeconds: data.keyTtlSeconds,
    });
  }, [data, form]);

  return (
    <>
      <PageHeader title="Client 策略" description="对本厂全部现场设备生效。改完会升高修订；设备端稍后只接受更高修订。" />
      <Card loading={policy.isLoading} style={{ maxWidth: 560 }}>
        <Space direction="vertical" size={4} style={{ marginBottom: 16 }}>
          <Typography.Text type="secondary">当前修订 {data?.revision ?? "—"}</Typography.Text>
        </Space>
        <Form<FormValues>
          form={form}
          layout="vertical"
          requiredMark={false}
          disabled={!data || save.isPending}
          onFinish={(values) =>
            save.mutate(
              {
                ...values,
                extra: data?.extra ?? {},
              } satisfies ClientPolicyInput,
              {
                onSuccess: () => message.success("策略已保存"),
                onError: (e) => message.error(errorMessage(e)),
              },
            )
          }
        >
          <Form.Item
            name="maxCachedProjects"
            label="每台设备可缓存工程份数"
            extra="满了再收新工程会被拒绝，不会踢正在焊接或当前激活的。"
            rules={[{ required: true, message: "请填写上限" }]}
          >
            <InputNumber min={1} precision={0} style={{ width: "100%" }} />
          </Form.Item>
          <Form.Item name="cacheScope" label="缓存范围" rules={[{ required: true, message: "请选择范围" }]}>
            <Radio.Group
              options={[
                { value: "all", label: "该人获准的全部工程（仍受上限）" },
                { value: "current", label: "只缓存当前激活的那一份工程" },
              ]}
            />
          </Form.Item>
          <Form.Item
            name="persistUnwrapKey"
            label="解封钥落盘"
            extra="只允许落包装材料，工艺明文和钥原文都不能进磁盘。"
            valuePropName="checked"
          >
            <Switch checkedChildren="落盘" unCheckedChildren="仅内存" />
          </Form.Item>
          <Form.Item
            name="keyTtlSeconds"
            label="解封钥时效（秒）"
            extra="0 表示只在进程内存，退出即清。焊接中途到期不中断这一焊。"
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
