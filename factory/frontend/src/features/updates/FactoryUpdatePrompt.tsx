import { App, Modal, Typography } from "antd";
import { useState } from "react";
import { errorMessage } from "@/shared/api/client";
import { usePendingFactorySoftware, useConfirmFactoryUpdate } from "./api";

const DISMISS_KEY = "wmesh.factory.software.dismissed";

function pendingKey(kind: string, version: number) {
  return `${kind}:${version}`;
}

// 超管进管理台时，有待确认厂服务包就弹窗；暂不更新本会话不再烦同一版本。
export function FactoryUpdatePrompt({ enabled }: { enabled: boolean }) {
  const { message } = App.useApp();
  const pending = usePendingFactorySoftware(enabled);
  const confirm = useConfirmFactoryUpdate();
  const [skip, setSkip] = useState(() => sessionStorage.getItem(DISMISS_KEY));
  const row = pending.data ?? null;
  const key = row ? pendingKey(row.kind, row.version) : null;
  const open = Boolean(enabled && row && key !== skip);

  const dismiss = () => {
    if (!key) return;
    sessionStorage.setItem(DISMISS_KEY, key);
    setSkip(key);
  };

  return (
    <Modal
      title="厂端服务有新版本"
      open={open}
      okText="立即更新"
      cancelText="暂不更新"
      confirmLoading={confirm.isPending}
      maskClosable
      onCancel={dismiss}
      onOk={() => {
        if (!row) return;
        confirm.mutate(
          { kind: row.kind, version: row.version },
          {
            onSuccess: () => message.success("已确认，正在更换镜像并重启"),
            onError: (e) => message.error(errorMessage(e)),
          },
        );
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
        确认后会更换本机 Docker 镜像并重启厂服务，数据库和对象存储不动。管理台会短暂断开，稍后刷新即可。暂不更新则继续跑当前版本。
      </Typography.Paragraph>
    </Modal>
  );
}
