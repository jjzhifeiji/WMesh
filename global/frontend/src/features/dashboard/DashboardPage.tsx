import { Card, Col, Row, Statistic, Table, Tag, type TableColumnsType } from "antd";
import { Link } from "react-router";
import { paths } from "@/app/routes";
import { useDirectory, type Factory } from "@/features/factories/api";
import { formatTime } from "@/shared/format";
import { IdText } from "@/shared/ui/IdText";
import { PageHeader } from "@/shared/ui/PageHeader";
import { useHealth } from "./api";

const recentColumns: TableColumnsType<Factory> = [
  { title: "工厂名称", dataIndex: "name" },
  { title: "工厂 ID", dataIndex: "id", render: (id: string) => <IdText id={id} /> },
  { title: "创建时间", dataIndex: "createdAt", width: 170, render: (v: string) => formatTime(v) },
];

function statusTag(v?: string) {
  if (!v) return <Tag>未知</Tag>;
  if (v === "ok") return <Tag color="green">正常</Tag>;
  if (v === "off") return <Tag>未配置</Tag>;
  return <Tag color="red">异常</Tag>;
}

// 概览：名录规模与服务健康一眼可见。
export function DashboardPage() {
  const dir = useDirectory();
  const health = useHealth();
  const recent = (dir.data?.factories ?? []).toSorted((a, b) => b.createdAt.localeCompare(a.createdAt)).slice(0, 5);

  return (
    <>
      <PageHeader title="概览" description="WAN 只做全局治理与建厂；厂内日常管理在各厂自己的管理端。" />
      <Row gutter={[16, 16]}>
        <Col xs={24} sm={12} lg={6}>
          <Card>
            <Statistic title="工厂数量" value={dir.data?.factories.length ?? 0} loading={dir.isLoading} />
          </Card>
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <Card>
            <Statistic title="服务版本" value={health.data?.version ?? "—"} loading={health.isLoading} />
          </Card>
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <Card>
            <Statistic title="数据库" valueRender={() => statusTag(health.data?.db)} />
          </Card>
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <Card>
            <Statistic title="对象存储" valueRender={() => statusTag(health.data?.oss)} />
          </Card>
        </Col>
      </Row>
      <Card title="最近创建的工厂" extra={<Link to={paths.factories}>全部名录</Link>} style={{ marginTop: 16 }}>
        <Table<Factory> rowKey="id" columns={recentColumns} dataSource={recent} loading={dir.isLoading} pagination={false} size="small" />
      </Card>
    </>
  );
}
