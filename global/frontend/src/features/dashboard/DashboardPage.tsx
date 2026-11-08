import { Card, Col, Row, Statistic, Table, Tag, type TableColumnsType } from "antd";
import { Link } from "react-router";
import { paths } from "@/app/routes";
import { useDirectory, type Factory } from "@/features/factories/api";
import { formatRelease } from "@/shared/format";
import { PageHeader } from "@/shared/ui/PageHeader";
import { QueryErrorAlert, readFailed } from "@/shared/ui/QueryErrorAlert";
import { VERSION_CODE, VERSION_NAME } from "@/shared/version";
import { useHealth } from "./api";

type FactoryState = "online" | "offline" | "unclaimed" | "disabled" | "retired";

// factoryState 名录一行归到一种状态：注销、停用优先，其余看是否认领、通道是否在线；首页计数和状态标签同一口径。
function factoryState(f: Factory): FactoryState {
  if (f.status === "retired") return "retired";
  if (f.status === "disabled") return "disabled";
  if (f.channelOnline) return "online";
  return f.enrolledAt ? "offline" : "unclaimed";
}

const STATE_TAGS: Record<FactoryState, { label: string; color?: string }> = {
  online: { label: "在线", color: "green" },
  offline: { label: "离线", color: "red" },
  unclaimed: { label: "未认领" },
  disabled: { label: "已停用", color: "orange" },
  retired: { label: "已注销" },
};

const STATE_COUNTS: { state: FactoryState; title: string }[] = [
  { state: "online", title: "在线工厂" },
  { state: "offline", title: "离线工厂" },
  { state: "unclaimed", title: "未认领" },
  { state: "disabled", title: "已停用" },
  { state: "retired", title: "已注销" },
];

const factoryColumns: TableColumnsType<Factory> = [
  { title: "工厂名称", dataIndex: "name" },
  {
    title: "状态",
    width: 90,
    render: (_, f) => {
      const tag = STATE_TAGS[factoryState(f)];
      return <Tag color={tag.color}>{tag.label}</Tag>;
    },
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
  // 按状态计数，和下方名录的状态标签一致。
  const counts: Record<FactoryState, number> = { online: 0, offline: 0, unclaimed: 0, disabled: 0, retired: 0 };
  for (const f of factories) counts[factoryState(f)] += 1;
  // 名录读失败时计数写「—」，不能写 0，免得当成真没工厂。
  const dirFailed = readFailed(dir);

  return (
    <>
      <PageHeader title="概览" description="WAN 只做全局治理与建厂；厂内日常管理在各厂自己的管理端。" />
      <QueryErrorAlert query={dir} what="工厂名录" />
      <QueryErrorAlert query={health} what="服务状态" />
      <Row gutter={[16, 16]}>
        <Col xs={12} sm={8} lg={4}>
          <Card>
            <Statistic title="工厂数量" value={dirFailed ? "—" : factories.length} loading={dir.isLoading && !dirFailed} />
          </Card>
        </Col>
        {STATE_COUNTS.map((c) => (
          <Col key={c.state} xs={12} sm={8} lg={4}>
            <Card>
              <Statistic title={c.title} value={dirFailed ? "—" : counts[c.state]} loading={dir.isLoading && !dirFailed} />
            </Card>
          </Col>
        ))}
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
            <Statistic title="服务版本号" value={health.data && health.data.version > 0 ? health.data.version : "—"} loading={health.isLoading && !readFailed(health)} />
          </Card>
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <Card>
            <Statistic title="服务版本名" value={health.data?.versionName || "—"} loading={health.isLoading && !readFailed(health)} />
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
      {dirFailed ? null : (
        <Card title="工厂" extra={<Link to={paths.factories}>全部名录</Link>} style={{ marginTop: 16 }}>
          <Table<Factory> rowKey="id" columns={factoryColumns} dataSource={factories} loading={dir.isLoading} pagination={false} size="small" />
        </Card>
      )}
    </>
  );
}
