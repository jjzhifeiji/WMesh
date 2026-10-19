import { App, Button, Card, Col, DatePicker, Row, Select, Statistic, Table, Tabs, type TableColumnsType } from "antd";
import type { Dayjs } from "dayjs";
import dayjs from "dayjs";
import { useMemo, useState } from "react";
import { useIsSuperAdmin } from "@/features/catalog/api";
import { useHealth } from "@/features/dashboard/api";
import { PageHeader } from "@/shared/ui/PageHeader";
import { errorMessage } from "@/shared/api/client";
import {
  avg,
  formatDuration,
  formatLengthM,
  pathLabel,
  personLabel,
  useSeedWeldDemo,
  useWeldReports,
  useWeldRuns,
  weldKindLabel,
  type WeldGroup,
  type WeldReportRow,
  type WeldRunFilter,
  type WeldRunRow,
} from "./api";

const { RangePicker } = DatePicker;

const RUN_PAGE_SIZE = 20; // 明细每页条数，服务端分页
const GROUP_RUN_PAGE_SIZE = 10; // 展开分组行时每页条数

function defaultRange(): [Dayjs, Dayjs] {
  return [dayjs().subtract(29, "day").startOf("day"), dayjs().startOf("day")];
}

function runColumns(): TableColumnsType<WeldRunRow> {
  return [
    { title: "时间", dataIndex: "occurredAt", width: 180, render: (v: string) => dayjs(v).format("YYYY-MM-DD HH:mm") },
    { title: "工程", dataIndex: "projectName" },
    { title: "模式", dataIndex: "weldKind", width: 80, render: (v: string) => weldKindLabel(v) },
    { title: "人员", key: "person", render: (_, r) => personLabel(r) },
    { title: "组织", key: "org", render: (_, r) => pathLabel(r.orgPath) },
    { title: "焊长", key: "len", width: 100, render: (_, r) => formatLengthM(r.lengthMm) },
    { title: "时长", key: "dur", width: 100, render: (_, r) => formatDuration(r.durationSec) },
  ];
}

// 工程筛选值：有身份用身份，没身份记 none，和 /weld-runs 的 projectId 口径一致；不带工程的行没有值。
function projectKey(r: { projectId?: string | null; projectName?: string }) {
  if (r.projectId) return r.projectId;
  return r.projectName === undefined ? undefined : "none";
}

// 展开分组行时按这一行的维度单独取下钻，叠上页面的人员和工程筛选，分页由服务端做。
function GroupRuns({ tab, row, from, to, filter }: { tab: WeldGroup; row: WeldReportRow; from: string; to: string; filter: WeldRunFilter }) {
  const [page, setPage] = useState(1);
  const f: WeldRunFilter = { ...filter };
  let rangeFrom = from;
  let rangeTo = to;
  if (tab === "project") f.projectId = row.projectId ?? "none";
  if (tab === "person") f.personId = row.personId;
  if (tab === "org") f.orgUnitId = row.orgUnitId ?? "direct";
  if (tab === "day" && row.day) {
    // 按日行取浏览器当地这一天，和报表切日的时区一致。
    rangeFrom = dayjs(row.day).startOf("day").toISOString();
    rangeTo = dayjs(row.day).add(1, "day").startOf("day").toISOString();
  }
  const q = useWeldRuns(rangeFrom, rangeTo, f, GROUP_RUN_PAGE_SIZE, (page - 1) * GROUP_RUN_PAGE_SIZE);
  if (q.error) return <p>{errorMessage(q.error)}</p>;
  return (
    <Table
      rowKey="id"
      size="small"
      columns={runColumns()}
      dataSource={q.data?.items ?? []}
      loading={q.isFetching}
      pagination={{ current: page, pageSize: GROUP_RUN_PAGE_SIZE, total: q.data?.total ?? 0, hideOnSinglePage: true, showSizeChanger: false, onChange: setPage }}
    />
  );
}

// 本厂焊长时长：按工程 / 人 / 组织 / 日查看，可下钻每次起停。
export function ReportsPage() {
  const { message } = App.useApp();
  const isSA = useIsSuperAdmin();
  const health = useHealth();
  // 只开发版放开演示焊次；生产探活报假，按钮不露出。
  const showDemoSeed = isSA && health.data?.demoData === true;
  const [range, setRange] = useState<[Dayjs, Dayjs]>(defaultRange);
  const [tab, setTab] = useState<WeldGroup>("project");
  const [personFilter, setPersonFilter] = useState<string>();
  const [projectFilter, setProjectFilter] = useState<string>();
  const [page, setPage] = useState(1);
  const from = range[0].startOf("day").toISOString();
  const to = range[1].add(1, "day").startOf("day").toISOString();
  const grouped = useWeldReports(tab === "person-day" ? "person" : tab, from, to, tab !== "person-day");
  const personReport = useWeldReports("person", from, to);
  const projectReport = useWeldReports("project", from, to);
  const runFilter: WeldRunFilter = { personId: personFilter, projectId: projectFilter };
  // 明细和统计卡共用这一页：筛选走服务端，合计按全部命中的行算。
  const runsQ = useWeldRuns(from, to, runFilter, RUN_PAGE_SIZE, (page - 1) * RUN_PAGE_SIZE);
  const seed = useSeedWeldDemo();
  const reportRows = useMemo(() => {
    let rows = grouped.data ?? [];
    if (personFilter) rows = rows.filter((r) => r.personId === personFilter);
    if (projectFilter) rows = rows.filter((r) => projectKey(r) === projectFilter);
    return rows;
  }, [grouped.data, personFilter, projectFilter]);
  const totals = {
    lengthMm: runsQ.data?.lengthMm ?? 0,
    durationSec: runsQ.data?.durationSec ?? 0,
    runCount: runsQ.data?.total ?? 0,
  };
  const people = useMemo(() => {
    const map = new Map<string, string>();
    for (const r of personReport.data ?? []) if (r.personId) map.set(r.personId, personLabel(r));
    return [...map.entries()].map(([value, label]) => ({ value, label }));
  }, [personReport.data]);
  const projects = useMemo(() => {
    const map = new Map<string, string>();
    for (const r of projectReport.data ?? []) {
      const key = projectKey(r);
      if (key && !map.has(key)) map.set(key, r.projectName ?? "");
    }
    return [...map.entries()].map(([value, label]) => ({ value, label }));
  }, [projectReport.data]);

  const groupColumns: TableColumnsType<WeldReportRow> = [
    ...(tab === "project" ? [{ title: "工程", dataIndex: "projectName" } satisfies TableColumnsType<WeldReportRow>[number]] : []),
    ...(tab === "person" ? [{ title: "人员", key: "person", render: (_: unknown, r: WeldReportRow) => personLabel(r) }] : []),
    ...(tab === "org" ? [{ title: "组织", key: "org", render: (_: unknown, r: WeldReportRow) => pathLabel(r.orgPath) }] : []),
    ...(tab === "day" ? [{ title: "日期", dataIndex: "day", width: 120 }] : []),
    { title: "次数", dataIndex: "runCount", width: 80 },
    { title: "总焊长", key: "len", width: 110, render: (_: unknown, r: WeldReportRow) => formatLengthM(r.lengthMm) },
    { title: "总时长", key: "dur", width: 110, render: (_: unknown, r: WeldReportRow) => formatDuration(r.durationSec) },
    { title: "次均焊长", key: "alen", width: 110, render: (_: unknown, r: WeldReportRow) => formatLengthM(avg(r.lengthMm, r.runCount)) },
    { title: "次均时长", key: "adur", width: 110, render: (_: unknown, r: WeldReportRow) => formatDuration(avg(r.durationSec, r.runCount)) },
  ];

  return (
    <>
      <PageHeader
        title="报表"
        description="按当时工程、登录人和组织路径归集；可下钻每次起停。换人、调动、改工程名不改历史。"
        extra={
          <>
            <Select
              allowClear
              placeholder="人员"
              style={{ width: 180 }}
              options={people}
              value={personFilter}
              onChange={(v) => {
                setPersonFilter(v);
                setPage(1);
              }}
            />
            <Select
              allowClear
              placeholder="工程"
              style={{ width: 200 }}
              options={projects}
              value={projectFilter}
              onChange={(v) => {
                setProjectFilter(v);
                setPage(1);
              }}
            />
            <RangePicker
              allowClear={false}
              value={range}
              onChange={(v) => {
                if (v?.[0] && v[1]) {
                  setRange([v[0].startOf("day"), v[1].startOf("day")]);
                  setPage(1);
                }
              }}
            />
            {showDemoSeed ? (
              <Button
                onClick={() =>
                  seed.mutate(undefined, {
                    onSuccess: (r) => message.success(r.created > 0 ? `已写入 ${r.created} 次演示焊次` : "演示焊次已在库中"),
                    onError: (e) => message.error(errorMessage(e)),
                  })
                }
                loading={seed.isPending}
              >
                写入演示焊次
              </Button>
            ) : null}
          </>
        }
      />
      {grouped.error ? <p>{errorMessage(grouped.error)}</p> : null}
      {runsQ.error ? <p>{errorMessage(runsQ.error)}</p> : null}
      <Row gutter={[16, 16]} style={{ marginBottom: 16 }}>
        <Col xs={24} sm={6}>
          <Card>
            <Statistic title="起停次数" value={totals.runCount} loading={runsQ.isLoading} />
          </Card>
        </Col>
        <Col xs={24} sm={6}>
          <Card>
            <Statistic title="总焊长" value={formatLengthM(totals.lengthMm)} loading={runsQ.isLoading} />
          </Card>
        </Col>
        <Col xs={24} sm={6}>
          <Card>
            <Statistic title="总时长" value={formatDuration(totals.durationSec)} loading={runsQ.isLoading} />
          </Card>
        </Col>
        <Col xs={24} sm={6}>
          <Card>
            <Statistic title="次均时长" value={formatDuration(avg(totals.durationSec, totals.runCount))} loading={runsQ.isLoading} />
          </Card>
        </Col>
      </Row>
      <Tabs
        activeKey={tab}
        onChange={(k) => setTab(k as WeldGroup)}
        items={[
          { key: "project", label: "按工程" },
          { key: "person", label: "按人" },
          { key: "org", label: "按组织" },
          { key: "day", label: "按日" },
          { key: "person-day", label: "明细" },
        ]}
      />
      {tab === "person-day" ? (
        <Table
          rowKey="id"
          columns={runColumns()}
          dataSource={runsQ.data?.items ?? []}
          loading={runsQ.isFetching}
          pagination={{ current: page, pageSize: RUN_PAGE_SIZE, total: runsQ.data?.total ?? 0, showSizeChanger: false, onChange: setPage }}
        />
      ) : (
        <Table
          rowKey={(r, i) => `${r.personId ?? ""}-${r.projectId ?? r.projectName ?? ""}-${r.orgUnitId ?? ""}-${r.day ?? ""}-${i}`}
          columns={groupColumns}
          dataSource={reportRows}
          loading={grouped.isLoading}
          pagination={{ pageSize: 20 }}
          expandable={{
            expandedRowRender: (row) => (
              <GroupRuns key={`${from}|${to}|${personFilter ?? ""}|${projectFilter ?? ""}`} tab={tab} row={row} from={from} to={to} filter={runFilter} />
            ),
          }}
        />
      )}
    </>
  );
}
