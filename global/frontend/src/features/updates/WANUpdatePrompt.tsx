import { App, Modal, Typography } from "antd";
import { useState } from "react";
import { errorMessage } from "@/shared/api/client";
import { startApply } from "./ApplyOverlay";
import { useConfirmWANUpdate, usePendingWANSoftware } from "./api";

// 首页检查待确认云端包；本页关掉只挡这一次，再进首页再查再提示。
export function WANUpdatePrompt() {
  const { message } = App.useApp();
  const pending = usePendingWANSoftware(true);
  const confirm = useConfirmWANUpdate();
  const [dismissed, setDismissed] = useState(false);
  const row = pending.data ?? null;
  const open = Boolean(row && !dismissed);

  return (
    <Modal
      title="云端服务有新版本"
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
          startApply({ kind: row.kind, version: row.version, versionName: row.versionName, title: "云端服务" });
          setDismissed(true);
        } catch (e) {
          message.error(errorMessage(e));
          throw e;
        }
      }}
    >
      <Typography.Paragraph>
        种类：云端服务
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
