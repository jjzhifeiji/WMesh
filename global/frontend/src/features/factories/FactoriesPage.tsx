import { PlusOutlined, ReloadOutlined } from "@ant-design/icons";
import { Alert, App, Button, Card, Popconfirm, Space, Table, Tag, type TableColumnsType } from "antd";
import { useMemo, useState } from "react";
import { errorMessage } from "@/shared/api/client";
import { formatDateTime, formatTime } from "@/shared/format";
import { IdText } from "@/shared/ui/IdText";
import { PageHeader } from "@/shared/ui/PageHeader";
import { CreateFactoryModal } from "./CreateFactoryModal";
import {
  useDeleteFactory,
  useDirectory,
  useDisableFactory,
  useEnableFactory,
  type Factory,
  type InitialSuperAdmin,
} from "./api";

type Row = Factory & { initial?: InitialSuperAdmin };

function channelStatus(f: Row) {
  if (f.status === "retired") return <Tag>已注销</Tag>;
  if (f.status === "disabled") return <Tag color="orange">已停用</Tag>;
  if (!f.enrolledAt) return <Tag>未认领</Tag>;
  if (f.channelOnline) return <Tag color="green">在线</Tag>;
  return <Tag color="red">离线</Tag>;
}

// 工厂名录：停用/启用/删除由 WAN 说了算，在线时立刻推给厂端。
export function FactoriesPage() {
  const dir = useDirectory();
  const disable = useDisableFactory();
  const enable = useEnableFactory();
  const remove = useDeleteFactory();
  const { message } = App.useApp();
  const [creating, setCreating] = useState(false);

  const rows = useMemo<Row[]>(() => {
    const byFactory = new Map(dir.data?.initials.map((i) => [i.factoryId, i]) ?? []);
    return (dir.data?.factories ?? []).map((f) => ({ ...f, initial: byFactory.get(f.id) }));
  }, [dir.data]);

  const onErr = (err: unknown) => message.error(errorMessage(err));

  const columns: TableColumnsType<Row> = [
    { title: "工厂名称", dataIndex: "name", width: 180 },
    { title: "状态", width: 100, render: (_, row) => channelStatus(row) },
    { title: "上线时间", dataIndex: "channelConnectedAt", width: 180, render: (v?: string) => formatDateTime(v) },
    { title: "离线时间", dataIndex: "channelDisconnectedAt", width: 180, render: (v?: string) => formatDateTime(v) },
    { title: "最近心跳", dataIndex: "channelLastSeenAt", width: 180, render: (v?: string) => formatDateTime(v) },
    { title: "工厂 ID", dataIndex: "id", width: 280, render: (id: string) => <IdText id={id} /> },
    { title: "初始超管登录名", dataIndex: ["initial", "loginName"], width: 160, render: (v?: string) => v ?? "—" },
    { title: "创建时间", dataIndex: "createdAt", width: 170, render: (v: string) => formatTime(v) },
    {
      title: "操作",
      width: 200,
      fixed: "right",
      render: (_, row) => {
        const busy =
          (disable.isPending && disable.variables === row.id) ||
          (enable.isPending && enable.variables === row.id) ||
          (remove.isPending && remove.variables === row.id);
        if (row.status === "retired") return "—";
        return (
          <Space size="small">
            {row.status === "disabled" ? (
              <Button
                size="small"
                loading={busy}
                onClick={() => enable.mutate(row.id, { onSuccess: () => message.success("已启用"), onError: onErr })}
              >
                启用
              </Button>
            ) : (
              <Popconfirm
                title={`停用「${row.name}」？`}
                description="厂内将无法登录和新操作；通道仍保持，才能再启用。"
                onConfirm={() => disable.mutate(row.id, { onSuccess: () => message.success("已停用"), onError: onErr })}
              >
                <Button size="small" loading={busy}>
                  停用
                </Button>
              </Popconfirm>
            )}
            <Popconfirm
              title={`删除「${row.name}」？`}
              description={row.enrolledAt ? "已认领的工厂只注销，不能再启用。" : "未认领的工厂会从名录拿掉。"}
              onConfirm={() =>
                remove.mutate(row.id, {
                  onSuccess: () => message.success(row.enrolledAt ? "已注销" : "已从名录删除"),
                  onError: onErr,
                })
              }
            >
              <Button size="small" danger loading={busy}>
                删除
              </Button>
            </Popconfirm>
          </Space>
        );
      },
    },
  ];

  return (
    <>
      <PageHeader
        title="工厂名录"
        description="停用、启用、删除由这里控制厂端；在线时立刻生效，离线则回连后收敛。"
        extra={
          <>
            <Button icon={<ReloadOutlined />} onClick={() => void dir.refetch()} loading={dir.isFetching}>
              刷新
            </Button>
            <Button type="primary" icon={<PlusOutlined />} onClick={() => setCreating(true)}>
              创建工厂
            </Button>
          </>
        }
      />
      {dir.isError ? <Alert type="error" showIcon message={errorMessage(dir.error)} style={{ marginBottom: 16 }} /> : null}
      <Card>
        <Table<Row>
          rowKey="id"
          columns={columns}
          dataSource={rows}
          loading={dir.isLoading}
          pagination={{ pageSize: 20, hideOnSinglePage: true }}
          scroll={{ x: 1600 }}
        />
      </Card>
      <CreateFactoryModal open={creating} onClose={() => setCreating(false)} />
    </>
  );
}
