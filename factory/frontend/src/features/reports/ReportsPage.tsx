import { App, Button, Card, Col, DatePicker, Row, Select, Statistic, Table, Tabs, type TableColumnsType } from "antd";
import type { Dayjs } from "dayjs";
import dayjs from "dayjs";
import { useMemo, useState } from "react";
import { useIsSuperAdmin } from "@/features/catalog/api";
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
  type WeldRunRow,
} from "./api";

const { RangePicker } = DatePicker;

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

// 本厂焊长时长：按工程 / 人 / 组织 / 日查看，可下钻每次起停。
export function ReportsPage() {
  const { message } = App.useApp();
  const isSA = useIsSuperAdmin();
  const [range, setRange] = useState<[Dayjs, Dayjs]>(defaultRange);
  const [tab, setTab] = useState<WeldGroup>("project");
  const [personFilter, setPersonFilter] = useState<string>();
  const [projectFilter, setProjectFilter] = useState<string>();
  const from = range[0].startOf("day").toISOString();
  const to = range[1].add(1, "day").startOf("day").toISOString();
  const grouped = useWeldReports(tab === "person-day" ? "person" : tab, from, to, tab !== "person-day");
  const runsQ = useWeldRuns(from, to);
  const seed = useSeedWeldDemo();
  const runs = useMemo(() => {
    let rows = runsQ.data ?? [];
    if (personFilter) rows = rows.filter((r) => r.personId === personFilter);
    if (projectFilter) rows = rows.filter((r) => (r.projectId ?? "") === projectFilter || r.projectName === projectFilter);
    return rows;
  }, [runsQ.data, personFilter, projectFilter]);
  const reportRows = useMemo(() => {
    let rows = grouped.data ?? [];
    if (personFilter) rows = rows.filter((r) => r.personId === personFilter);
    if (projectFilter) rows = rows.filter((r) => (r.projectId ?? "") === projectFilter || r.projectName === projectFilter);
    return rows;
  }, [grouped.data, personFilter, projectFilter]);
  const totals = useMemo(
    () =>
      runs.reduce(
        (acc, r) => {
          acc.lengthMm += r.lengthMm;
          acc.durationSec += r.durationSec;
          acc.runCount += 1;
          return acc;
        },
        { lengthMm: 0, durationSec: 0, runCount: 0 },
      ),
    [runs],
  );
  const people = useMemo(() => {
    const map = new Map<string, string>();
    for (const r of runsQ.data ?? []) map.set(r.personId, personLabel(r));
    return [...map.entries()].map(([value, label]) => ({ value, label }));
  }, [runsQ.data]);
  const projects = useMemo(() => {
    const map = new Map<string, string>();
    for (const r of runsQ.data ?? []) map.set(r.projectId ?? r.projectName, r.projectName);
    return [...map.entries()].map(([value, label]) => ({ value, label }));
  }, [runsQ.data]);

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

  const runsFor = (row: WeldReportRow) =>
    runs.filter((r) => {
      if (tab === "project") return (row.projectId && r.projectId === row.projectId) || r.projectName === row.projectName;
      if (tab === "person") return r.personId === row.personId;
      if (tab === "org") return (r.orgUnitId ?? "direct") === (row.orgUnitId ?? "direct");
      if (tab === "day") return r.occurredAt.slice(0, 10) === row.day;
      return true;
    });

  return (
    <>
      <PageHeader
        title="报表"
        description="按当时工程、登录人和组织路径归集；可下钻每次起停。换人、调动、改工程名不改历史。"
        extra={
          <>
            <Select allowClear placeholder="人员" style={{ width: 180 }} options={people} value={personFilter} onChange={setPersonFilter} />
            <Select allowClear placeholder="工程" style={{ width: 200 }} options={projects} value={projectFilter} onChange={setProjectFilter} />
            <RangePicker
              allowClear={false}
              value={range}
              onChange={(v) => {
                if (v?.[0] && v[1]) setRange([v[0].startOf("day"), v[1].startOf("day")]);
              }}
            />
            {isSA ? (
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
        <Table rowKey="id" columns={runColumns()} dataSource={runs} loading={runsQ.isLoading} pagination={{ pageSize: 20 }} />
      ) : (
        <Table
          rowKey={(r, i) => `${r.personId ?? ""}-${r.projectId ?? r.projectName ?? ""}-${r.orgUnitId ?? ""}-${r.day ?? ""}-${i}`}
          columns={groupColumns}
          dataSource={reportRows}
          loading={grouped.isLoading}
          pagination={{ pageSize: 20 }}
          expandable={{
            expandedRowRender: (row) => (
              <Table rowKey="id" size="small" columns={runColumns()} dataSource={runsFor(row)} pagination={false} />
            ),
          }}
        />
      )}
    </>
  );
}
