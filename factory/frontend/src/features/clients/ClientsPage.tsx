import { App, Card, Table, Tag, Typography, type TableColumnsType } from "antd";
import { formatLengthM } from "@/features/reports/api";
import { errorMessage } from "@/shared/api/client";
import { formatTime } from "@/shared/format";
import { statusColor, statusLabel } from "@/shared/labels";
import { CodeText } from "@/shared/ui/CodeText";
import { PageHeader } from "@/shared/ui/PageHeader";
import { useClients, useRenameClient, type Client } from "./api";

function userLabel(row: Client) {
  if (row.lastUserDisplay && row.lastUserLogin) return `${row.lastUserDisplay}（${row.lastUserLogin}）`;
  return row.lastUserDisplay || row.lastUserLogin || "—";
}

// 云端分来的现场设备：设备名由云端定，厂端只起昵称；谁焊了多少从焊次里来。
export function ClientsPage() {
  const clients = useClients();
  const rename = useRenameClient();
  const { message } = App.useApp();

  const columns: TableColumnsType<Client> = [
    { title: "短码", key: "code", width: 110, render: (_, row) => <CodeText code={row.shortCode} id={row.id} /> },
    { title: "设备名", dataIndex: "name", width: 150, ellipsis: true, render: (name: string) => <Typography.Text strong>{name}</Typography.Text> },
    {
      title: "昵称",
      dataIndex: "nickname",
      width: 150,
      render: (nickname: string, row) => (
        <Typography.Text
          type={nickname ? undefined : "secondary"}
          editable={{
            tooltip: "改昵称，留空去掉",
            text: nickname,
            onChange: (v) => {
              const next = v.trim();
              if (next === nickname) return;
              rename.mutate(
                { id: row.id, nickname: next },
                { onSuccess: () => message.success(next ? "昵称已更新" : "已去掉昵称"), onError: (e) => message.error(errorMessage(e)) },
              );
            },
          }}
        >
          {nickname || "—"}
        </Typography.Text>
      ),
    },
    { title: "识别号", dataIndex: "deviceSerial", width: 150, render: (v: string | undefined) => v || "—" },
    {
      title: "状态",
      dataIndex: "status",
      width: 90,
      render: (s: string) => (s === "void" ? <Tag>{statusLabel("void")}</Tag> : <Tag color={statusColor("bound")}>{statusLabel("bound")}</Tag>),
    },
    { title: "最近使用人", key: "user", width: 170, render: (_, row) => userLabel(row) },
    { title: "最近使用", dataIndex: "lastUsedAt", width: 160, render: (v?: string) => formatTime(v) },
    { title: "累计焊长", dataIndex: "weldLengthMm", width: 110, render: (v: number) => (v ? formatLengthM(v) : "—") },
    { title: "焊次", dataIndex: "weldRunCount", width: 80, render: (v: number) => v || "—" },
    { title: "分配时间", dataIndex: "boundAt", width: 160, render: (v: string) => formatTime(v) },
  ];

  return (
    <>
      <PageHeader title="设备" description="云端把设备分到本厂后会自动出现，设备名和识别号由云端定，这里只能起昵称。最近使用人、累计焊长按焊次统计。" />
      <Card>
        <Table<Client> rowKey="id" columns={columns} dataSource={clients.data ?? []} loading={clients.isLoading} pagination={{ pageSize: 20, hideOnSinglePage: true }} scroll={{ x: "max-content" }} />
      </Card>
    </>
  );
}
