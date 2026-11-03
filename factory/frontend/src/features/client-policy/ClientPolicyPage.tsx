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

// ttlText 把秒折成天、小时、分钟给人看，免得自己算 86400。
function ttlText(sec: number | null | undefined) {
  if (sec == null) return "";
  if (sec <= 0) return "直到主动退出";
  const d = Math.floor(sec / 86400);
  const h = Math.floor((sec % 86400) / 3600);
  const m = Math.floor((sec % 3600) / 60);
  const parts = [d ? `${d} 天` : "", h ? `${h} 小时` : "", m ? `${m} 分钟` : ""].filter(Boolean);
  return parts.length ? `约 ${parts.join(" ")}` : `${sec} 秒`;
}

// 本厂全部 Client 共用这一份策略；超管可改，现场设备只收更高修订。
export function ClientPolicyPage() {
  const policy = useClientPolicy();
  const save = useSaveClientPolicy();
  const { message } = App.useApp();
  const [form] = Form.useForm<FormValues>();
  const data = policy.data;
  const ttl = Form.useWatch("keyTtlSeconds", form);

  // 时效框改过还没保存就不回填，免得点开关后的重取把手填的值冲掉。
  useEffect(() => {
    if (!data) return;
    form.setFieldsValue({ persistUnwrapKey: data.persistUnwrapKey, encryptPouch: data.encryptPouch });
    if (!form.isFieldTouched("keyTtlSeconds")) form.setFieldsValue({ keyTtlSeconds: data.keyTtlSeconds });
  }, [data, form]);

  // 只提交 patch 里的项，其余取服务端当前值：开关不能顺带把时效框里未校验的输入（如清空）存成 0。
  function persist(patch: Partial<FormValues>) {
    if (!data) return;
    save.mutate(
      {
        maxCachedProjects: data.maxCachedProjects,
        cacheScope: "all",
        persistUnwrapKey: patch.persistUnwrapKey ?? data.persistUnwrapKey,
        keyTtlSeconds: patch.keyTtlSeconds ?? data.keyTtlSeconds,
        encryptPouch: patch.encryptPouch ?? data.encryptPouch,
        extra: data.extra ?? {},
      } satisfies ClientPolicyInput,
      {
        onSuccess: () => {
          if (patch.keyTtlSeconds !== undefined) form.setFields([{ name: "keyTtlSeconds", touched: false }]);
          message.success("策略已保存");
        },
        onError: (e) => {
          // 只退回这次提交的项，别的框里没存的输入留着。
          const back: Partial<FormValues> = {};
          if (patch.persistUnwrapKey !== undefined) back.persistUnwrapKey = data.persistUnwrapKey;
          if (patch.encryptPouch !== undefined) back.encryptPouch = data.encryptPouch;
          if (patch.keyTtlSeconds !== undefined) back.keyTtlSeconds = data.keyTtlSeconds;
          form.setFieldsValue(back);
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
            extra={
              <>
                {ttl != null ? <Typography.Text strong>{ttlText(ttl)}。</Typography.Text> : null}
                0 表示直到主动退出；1 天 = 86400 秒。到期后下次打开须重新登录并清钥。已登录进程中途不踢出，可离线焊接。
              </>
            }
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
