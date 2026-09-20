import { App, Modal, Typography } from "antd";
import { useState } from "react";
import { errorMessage } from "@/shared/api/client";
import { startApply } from "./ApplyOverlay";
import { useConfirmFactoryUpdate, usePendingFactorySoftware } from "./api";

// 首页只提示本厂已收完整厂包；本页关掉只挡这一次，再进首页再查再提示。
export function FactoryUpdatePrompt({ enabled }: { enabled: boolean }) {
  const { message } = App.useApp();
  const pending = usePendingFactorySoftware(enabled);
  const confirm = useConfirmFactoryUpdate();
  const [dismissed, setDismissed] = useState(false);
  const row = pending.data ?? null;
  const open = Boolean(enabled && row && !dismissed);

  return (
    <Modal
      title="厂端服务有新版本"
      open={open}
      okText="立即更新"
      cancelText="暂不更新"
      confirmLoading={confirm.isPending}
      maskClosable
      onCancel={() => setDismissed(true)}
      onOk={async () => {
        if (!row) return;
        try {
          await confirm.mutateAsync({ kind: row.kind, version: row.version });
          startApply({ kind: row.kind, version: row.version, versionName: row.versionName, title: "厂端服务" });
          setDismissed(true);
        } catch (e) {
          message.error(errorMessage(e));
          throw e;
        }
      }}
    >
      <Typography.Paragraph>
        种类：厂端服务
        <br />
        版本名：{row?.versionName ?? "—"}
        <br />
        版本号：{row?.version ?? "—"}
      </Typography.Paragraph>
      <Typography.Paragraph type="secondary">
        确认后只换本机 app 容器，数据库和对象存储不动。管理台会短暂断开。暂不更新则继续跑当前版本。
      </Typography.Paragraph>
    </Modal>
  );
}
