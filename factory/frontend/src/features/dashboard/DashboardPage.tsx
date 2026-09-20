import { Card, Col, Descriptions, Row, Space, Statistic, Tag } from "antd";
import { useCatalog, useIsSuperAdmin, unitName } from "@/features/catalog/api";
import { roleLabel, scopeLabel } from "@/shared/labels";
import { UpdatesPanel } from "@/features/updates/UpdatesPage";
import { PageHeader } from "@/shared/ui/PageHeader";
import { VERSION_CODE, VERSION_NAME } from "@/shared/version";
import { useHealth } from "./api";

function healthTag(v?: string) {
  if (!v) return <Tag>未知</Tag>;
  if (v === "ok") return <Tag color="green">正常</Tag>;
  if (v === "off") return <Tag>未配置</Tag>;
  return <Tag color="red">异常</Tag>;
}

// 概览：超管看全厂规模，其他角色看自己的角色；服务健康人人可见。
export function DashboardPage() {
  const catalog = useCatalog();
  const isSA = useIsSuperAdmin();
  const health = useHealth();
  const c = catalog.data;
  const count = (status: string) => c?.people.filter((p) => p.status === status).length ?? 0;

  return (
    <>
      <PageHeader
        title="概览"
        description={isSA ? "本厂人员、组织与软件包都在这里看；WAN 不代管。" : "当前账号不是工厂超管，组织与人员由超管维护；账号在右上角。"}
      />
      {isSA ? (
        <Row gutter={[16, 16]}>
          <Col xs={24} sm={12} lg={6}>
            <Card>
              <Statistic title="有效人员" value={count("active")} suffix={`/ 待启用 ${count("pending")} · 停用 ${count("disabled")}`} loading={catalog.isLoading} />
            </Card>
          </Col>
          <Col xs={24} sm={12} lg={6}>
            <Card>
              <Statistic title="有效组织节点" value={c?.orgUnits.filter((u) => u.status === "active").length ?? 0} loading={catalog.isLoading} />
            </Card>
          </Col>
          <Col xs={24} sm={12} lg={6}>
            <Card>
              <Statistic title="有效角色" value={c?.roleGrants.length ?? 0} loading={catalog.isLoading} />
            </Card>
          </Col>
          <Col xs={24} sm={12} lg={6}>
            <Card>
              <Statistic title="当前分配" value={c?.assignments.length ?? 0} loading={catalog.isLoading} />
            </Card>
          </Col>
        </Row>
      ) : null}
      <Row gutter={[16, 16]} style={{ marginTop: 16 }}>
        <Col xs={24} lg={12}>
          <Card title="我的角色" loading={catalog.isLoading}>
            <Space wrap>
              {c?.myGrants.map((g) => (
                <Tag key={g.id} color="blue">
                  {roleLabel(g.role)} · {scopeLabel(g.scopeKind)}
                  {g.orgUnitId ? `：${unitName(c, g.orgUnitId)}` : ""}
                </Tag>
              ))}
              {c && c.myGrants.length === 0 ? <Tag>还没有任何角色</Tag> : null}
            </Space>
          </Card>
        </Col>
        <Col xs={24} lg={12}>
          <Card title="服务状态" loading={health.isLoading}>
            <Descriptions
              column={1}
              size="small"
              items={[
                { key: "webVersion", label: "前端版本号", children: VERSION_CODE },
                { key: "webVersionName", label: "前端版本名", children: VERSION_NAME },
                { key: "svcVersion", label: "服务版本号", children: health.data && health.data.version > 0 ? health.data.version : "—" },
                { key: "svcVersionName", label: "服务版本名", children: health.data?.versionName || "—" },
                { key: "build", label: "构建", children: health.data?.build ?? "—" },
                { key: "db", label: "数据库", children: healthTag(health.data?.db) },
                { key: "oss", label: "对象存储", children: healthTag(health.data?.oss) },
              ]}
            />
          </Card>
        </Col>
      </Row>
      {isSA ? (
        <div style={{ marginTop: 16 }}>
          <UpdatesPanel />
        </div>
      ) : null}
    </>
  );
}
