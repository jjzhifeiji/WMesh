import { Card, Col, Row, Statistic, Table, Tag, type TableColumnsType } from "antd";
import { Link } from "react-router";
import { paths } from "@/app/routes";
import { useDirectory, type Factory } from "@/features/factories/api";
import { formatRelease } from "@/shared/format";
import { PageHeader } from "@/shared/ui/PageHeader";
import { VERSION_CODE, VERSION_NAME } from "@/shared/version";
import { useHealth } from "./api";

const factoryColumns: TableColumnsType<Factory> = [
  { title: "工厂名称", dataIndex: "name" },
  {
    title: "状态",
    width: 90,
    render: (_, f) =>
      f.status === "retired" ? (
        <Tag>已注销</Tag>
      ) : f.status === "disabled" ? (
        <Tag color="orange">已停用</Tag>
      ) : f.channelOnline ? (
        <Tag color="green">在线</Tag>
      ) : f.enrolledAt ? (
        <Tag color="red">离线</Tag>
      ) : (
        <Tag>未认领</Tag>
      ),
  },
  { title: "前端版本", width: 140, render: (_, f) => formatRelease(f.channelOnline, f.webVersion, f.webVersionName) },
  { title: "服务版本", width: 140, render: (_, f) => formatRelease(f.channelOnline, f.serviceVersion, f.serviceVersionName) },
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
  const factories = dir.data?.factories ?? [];
  const online = factories.filter((f) => f.channelOnline && (f.status ?? "active") === "active").length;

  return (
    <>
      <PageHeader title="概览" description="WAN 只做全局治理与建厂；厂内日常管理在各厂自己的管理端。" />
      <Row gutter={[16, 16]}>
        <Col xs={24} sm={12} lg={6}>
          <Card>
            <Statistic title="工厂数量" value={factories.length} loading={dir.isLoading} />
          </Card>
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <Card>
            <Statistic title="在线工厂" value={online} loading={dir.isLoading} />
          </Card>
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <Card>
            <Statistic title="前端版本号" value={VERSION_CODE} />
          </Card>
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <Card>
            <Statistic title="前端版本名" value={VERSION_NAME} />
          </Card>
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <Card>
            <Statistic title="服务版本号" value={health.data && health.data.version > 0 ? health.data.version : "—"} loading={health.isLoading} />
          </Card>
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <Card>
            <Statistic title="服务版本名" value={health.data?.versionName || "—"} loading={health.isLoading} />
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
      <Card title="工厂" extra={<Link to={paths.factories}>全部名录</Link>} style={{ marginTop: 16 }}>
        <Table<Factory> rowKey="id" columns={factoryColumns} dataSource={factories} loading={dir.isLoading} pagination={false} size="small" />
      </Card>
    </>
  );
}
