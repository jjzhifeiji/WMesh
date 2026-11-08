import { Card, Col, Descriptions, Row, Statistic, Tag } from "antd";
import { useCatalog, useIsSuperAdmin } from "@/features/catalog/api";
import { UpdatesPanel } from "@/features/updates/UpdatesPage";
import { errorMessage } from "@/shared/api/client";
import { PageHeader } from "@/shared/ui/PageHeader";
import { QueryErrorAlert, readFailed } from "@/shared/ui/QueryErrorAlert";
import { VERSION_CODE, VERSION_NAME } from "@/shared/version";
import { useHealth } from "./api";

function healthTag(v?: string) {
  if (!v) return <Tag>未知</Tag>;
  if (v === "ok") return <Tag color="green">正常</Tag>;
  if (v === "off") return <Tag>未配置</Tag>;
  return <Tag color="red">异常</Tag>;
}

// 概览：超管看全厂规模；服务健康人人可见。自己的角色在「我的账号」。
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
        description={
          isSA
            ? "本厂人员、组织与软件包都在这里看；WAN 不代管。"
            : !c && catalog.isError
              ? // 名册读不到时不能当成「不是超管」，照实说读不到。
                `读不到本厂名册，暂时无法确认账号权限：${errorMessage(catalog.error)}`
              : c
                ? "当前账号不是工厂超管，组织与人员由超管维护；账号在右上角。"
                : "正在读取本厂名册…"
        }
      />
      {/* 名册一条都没读到时页头已经说了，这里只提示手上是旧数据。 */}
      {c ? <QueryErrorAlert query={catalog} what="本厂名册" /> : null}
      <QueryErrorAlert query={health} what="服务状态" />
      {isSA ? (
        <Row gutter={[16, 16]}>
          <Col xs={24} sm={8}>
            <Card>
              <Statistic title="有效人员" value={count("active")} suffix={`/ 停用 ${count("disabled")}`} loading={catalog.isLoading} />
            </Card>
          </Col>
          <Col xs={24} sm={8}>
            <Card>
              <Statistic title="有效组织节点" value={c?.orgUnits.filter((u) => u.status === "active").length ?? 0} loading={catalog.isLoading} />
            </Card>
          </Col>
          <Col xs={24} sm={8}>
            <Card>
              <Statistic title="有效角色" value={c?.roleGrants.length ?? 0} loading={catalog.isLoading} />
            </Card>
          </Col>
        </Row>
      ) : null}
      <Row gutter={[16, 16]} style={{ marginTop: 16 }}>
        <Col xs={24}>
          <Card title="服务状态" loading={health.isLoading && !readFailed(health)}>
            <Descriptions
              column={1}
              size="small"
              items={[
                { key: "webVersion", label: "前端版本号", children: VERSION_CODE },
                { key: "webVersionName", label: "前端版本名", children: VERSION_NAME },
                // 探活读失败时不列服务端各项，免得「未知」「—」被当成查过的结果。
                ...(readFailed(health)
                  ? []
                  : [
                      { key: "svcVersion", label: "服务版本号", children: health.data && health.data.version > 0 ? health.data.version : "—" },
                      { key: "svcVersionName", label: "服务版本名", children: health.data?.versionName || "—" },
                      { key: "build", label: "构建", children: health.data?.build ?? "—" },
                      { key: "db", label: "数据库", children: healthTag(health.data?.db) },
                      { key: "oss", label: "对象存储", children: healthTag(health.data?.oss) },
                    ]),
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
