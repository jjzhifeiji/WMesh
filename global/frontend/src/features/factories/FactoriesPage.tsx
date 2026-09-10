import { PlusOutlined, ReloadOutlined } from "@ant-design/icons";
import { Alert, Button, Card, Table, type TableColumnsType } from "antd";
import { useMemo, useState } from "react";
import { errorMessage } from "@/shared/api/client";
import { formatTime } from "@/shared/format";
import { IdText } from "@/shared/ui/IdText";
import { PageHeader } from "@/shared/ui/PageHeader";
import { CreateFactoryModal } from "./CreateFactoryModal";
import { useDirectory, type Factory, type InitialSuperAdmin } from "./api";

type Row = Factory & { initial?: InitialSuperAdmin };

const columns: TableColumnsType<Row> = [
  { title: "工厂名称", dataIndex: "name", width: 220 },
  { title: "工厂 ID", dataIndex: "id", render: (id: string) => <IdText id={id} /> },
  { title: "初始超管登录名", dataIndex: ["initial", "loginName"], width: 180, render: (v?: string) => v ?? "—" },
  {
    title: "初始超管身份",
    dataIndex: ["initial", "personId"],
    render: (v?: string) => (v ? <IdText id={v} /> : "—"),
  },
  { title: "创建时间", dataIndex: "createdAt", width: 170, render: (v: string) => formatTime(v) },
];

// 工厂名录：WAN 只看得到工厂和初始超管身份，看不到厂内人员。
export function FactoriesPage() {
  const dir = useDirectory();
  const [creating, setCreating] = useState(false);

  const rows = useMemo<Row[]>(() => {
    const byFactory = new Map(dir.data?.initials.map((i) => [i.factoryId, i]) ?? []);
    return (dir.data?.factories ?? []).map((f) => ({ ...f, initial: byFactory.get(f.id) }));
  }, [dir.data]);

  return (
    <>
      <PageHeader
        title="工厂名录"
        description="创建工厂时只下发一名初始超管；厂内人员、组织、角色由该超管在厂内自行建立。"
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
        <Table<Row> rowKey="id" columns={columns} dataSource={rows} loading={dir.isLoading} pagination={{ pageSize: 20, hideOnSinglePage: true }} />
      </Card>
      <CreateFactoryModal open={creating} onClose={() => setCreating(false)} />
    </>
  );
}
