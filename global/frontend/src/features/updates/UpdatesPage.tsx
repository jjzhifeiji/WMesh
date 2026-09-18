import { PlusOutlined } from "@ant-design/icons";
import { App, Button, Card, Form, Input, InputNumber, Modal, Select, Table, Upload, type TableColumnsType, type UploadFile } from "antd";
import { useState } from "react";
import { useDirectory } from "@/features/factories/api";
import { errorMessage } from "@/shared/api/client";
import { formatTime, shortHash } from "@/shared/format";
import { PageHeader } from "@/shared/ui/PageHeader";
import {
  softwareKindLabel,
  useDistributeSoftware,
  usePublishSoftware,
  useSoftwareReleases,
  type SoftwareKind,
  type SoftwareRelease,
} from "./api";

type PublishInput = {
  kind: SoftwareKind;
  version: number;
  versionName: string;
  file: UploadFile[];
};

// WAN 上传两类更新包，按已认领工厂下发；安装要厂端或平板自己确认。
export function UpdatesPage() {
  const releases = useSoftwareReleases();
  const dir = useDirectory();
  const publish = usePublishSoftware();
  const distribute = useDistributeSoftware();
  const { message } = App.useApp();
  const [open, setOpen] = useState(false);
  const [sendFor, setSendFor] = useState<SoftwareRelease | null>(null);
  const [form] = Form.useForm<PublishInput>();
  const [sendForm] = Form.useForm<{ factoryId: string }>();

  const factories = (dir.data?.factories ?? []).filter((f) => f.status === "active" && Boolean(f.enrolledAt));

  const columns: TableColumnsType<SoftwareRelease> = [
    { title: "种类", dataIndex: "kind", width: 120, render: (k: SoftwareKind) => softwareKindLabel[k] ?? k },
    { title: "版本号", dataIndex: "version", width: 90 },
    { title: "版本名", dataIndex: "versionName" },
    { title: "摘要", dataIndex: "digest", width: 140, render: (v: string) => shortHash(v) },
    { title: "发布时间", dataIndex: "createdAt", width: 170, render: (v: string) => formatTime(v) },
    {
      title: "操作",
      key: "actions",
      width: 100,
      render: (_, row) => (
        <Button size="small" onClick={() => { setSendFor(row); sendForm.resetFields(); }}>
          下发
        </Button>
      ),
    },
  ];

  return (
    <>
      <PageHeader
        title="软件更新"
        description="厂服务上传 Docker 镜像 tar（含迁库脚本）；客户端上传 APK。下到已认领厂后，超管或平板确认才装。"
        extra={
          <Button type="primary" icon={<PlusOutlined />} onClick={() => setOpen(true)}>
            发布
          </Button>
        }
      />
      <Card>
        <Table<SoftwareRelease>
          rowKey={(r) => `${r.kind}:${r.version}`}
          columns={columns}
          dataSource={releases.data ?? []}
          loading={releases.isLoading}
          pagination={{ pageSize: 20, hideOnSinglePage: true }}
        />
      </Card>
      <Modal
        title="发布软件包"
        open={open}
        onCancel={() => setOpen(false)}
        okText="发布"
        confirmLoading={publish.isPending}
        destroyOnHidden
        onOk={() => form.submit()}
      >
        <Form<PublishInput>
          form={form}
          layout="vertical"
          requiredMark={false}
          onFinish={(values) => {
            const file = values.file?.[0]?.originFileObj;
            if (!file) {
              message.error("请选择文件");
              return;
            }
            const formData = new FormData();
            formData.append("kind", values.kind);
            formData.append("version", String(values.version));
            formData.append("versionName", values.versionName);
            formData.append("file", file);
            publish.mutate(formData, {
              onSuccess: () => {
                message.success("已发布");
                setOpen(false);
                form.resetFields();
              },
              onError: (e) => message.error(errorMessage(e)),
            });
          }}
        >
          <Form.Item name="kind" label="种类" rules={[{ required: true, message: "请选择种类" }]}>
            <Select
              options={[
                { value: "factory_service", label: softwareKindLabel.factory_service },
                { value: "client_apk", label: softwareKindLabel.client_apk },
              ]}
            />
          </Form.Item>
          <Form.Item name="version" label="版本号" extra="只比较这个整数，必须比已发过的更高才能下到同一厂。" rules={[{ required: true, message: "请输入版本号" }]}>
            <InputNumber min={1} precision={0} style={{ width: "100%" }} />
          </Form.Item>
          <Form.Item name="versionName" label="版本名" rules={[{ required: true, message: "请输入版本名" }, { max: 80, message: "最多 80 个字" }]}>
            <Input maxLength={80} placeholder="例如 1.5.0" />
          </Form.Item>
          <Form.Item noStyle shouldUpdate={(prev, next) => prev.kind !== next.kind}>
            {({ getFieldValue }) => {
              const kind = getFieldValue("kind") as SoftwareKind | undefined;
              const isAPK = kind === "client_apk";
              return (
                <Form.Item
                  name="file"
                  label={isAPK ? "APK" : "厂服务镜像"}
                  extra={isAPK ? "Android 安装包。" : "docker save 的 tar 或 gzip 压缩包（.tar / .tar.gz / .tgz）。"}
                  valuePropName="fileList"
                  getValueFromEvent={(e: { fileList?: UploadFile[] } | UploadFile[]) => (Array.isArray(e) ? e : (e?.fileList ?? []))}
                  rules={[{ required: true, message: "请选择文件" }]}
                >
                  <Upload maxCount={1} beforeUpload={() => false} accept={isAPK ? ".apk,application/vnd.android.package-archive" : ".tar,.gz,.tgz,application/x-tar,application/gzip,application/x-gzip"}>
                    <Button>选择文件</Button>
                  </Upload>
                </Form.Item>
              );
            }}
          </Form.Item>
        </Form>
      </Modal>
      <Modal
        title={sendFor ? `下发 ${softwareKindLabel[sendFor.kind]} ${sendFor.versionName}` : "下发"}
        open={sendFor !== null}
        onCancel={() => setSendFor(null)}
        okText="下发"
        confirmLoading={distribute.isPending}
        destroyOnHidden
        onOk={() => sendForm.submit()}
      >
        <Form<{ factoryId: string }>
          form={sendForm}
          layout="vertical"
          requiredMark={false}
          onFinish={(values) => {
            if (!sendFor) return;
            distribute.mutate(
              { kind: sendFor.kind, version: sendFor.version, factoryId: values.factoryId },
              {
                onSuccess: () => {
                  message.success("已下发，厂端确认后才安装");
                  setSendFor(null);
                },
                onError: (e) => message.error(errorMessage(e)),
              },
            );
          }}
        >
          <Form.Item name="factoryId" label="工厂" extra="只列已认领且未停用的厂。" rules={[{ required: true, message: "请选择工厂" }]}>
            <Select
              options={factories.map((f) => ({ value: f.id, label: f.name }))}
              notFoundContent={factories.length === 0 ? "没有已认领的工厂" : undefined}
            />
          </Form.Item>
        </Form>
      </Modal>
    </>
  );
}
