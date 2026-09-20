import { Card, Col, DatePicker, Row, Select, Statistic, Table, Tabs, type TableColumnsType } from "antd";
import type { Dayjs } from "dayjs";
import dayjs from "dayjs";
import { useMemo, useState } from "react";
import { PageHeader } from "@/shared/ui/PageHeader";
import { errorMessage } from "@/shared/api/client";
import { useDirectory } from "@/features/factories/api";
import {
  avg,
  formatDuration,
  formatLengthM,
  groupWeldRows,
  useWeldReports,
  weldKindLabel,
  type WeldGroup,
  type WeldSummaryRow,
} from "./api";

const { RangePicker } = DatePicker;

function defaultRange(): [Dayjs, Dayjs] {
  return [dayjs().subtract(29, "day").startOf("day"), dayjs().startOf("day")];
}

function grainColumns(): TableColumnsType<WeldSummaryRow> {
  return [
    { title: "工厂", dataIndex: "factoryName" },
    { title: "日期", dataIndex: "day", width: 120 },
    { title: "工程", dataIndex: "projectName" },
    { title: "模式", dataIndex: "weldKind", width: 80, render: (v: string) => weldKindLabel(v) },
    { title: "次数", dataIndex: "runCount", width: 80 },
    { title: "焊长", key: "len", width: 100, render: (_, r) => formatLengthM(r.lengthMm) },
    { title: "时长", key: "dur", width: 100, render: (_, r) => formatDuration(r.durationSec) },
  ];
}

// 跨厂焊长时长：只按工厂、工程名和日，不含人员与组织。
export function ReportsPage() {
  const dir = useDirectory();
  const [range, setRange] = useState<[Dayjs, Dayjs]>(defaultRange);
  const [tab, setTab] = useState<WeldGroup>("factory");
  const [factoryId, setFactoryId] = useState<string>();
  const from = range[0].startOf("day").toISOString();
  const to = range[1].add(1, "day").startOf("day").toISOString();
  const listed = useWeldReports(from, to, factoryId ?? "");
  const grains = listed.data;
  const reportRows = useMemo(() => groupWeldRows(grains ?? [], tab), [grains, tab]);
  const totals = useMemo(
    () =>
      (grains ?? []).reduce(
        (acc, r) => {
          acc.lengthMm += r.lengthMm;
          acc.durationSec += r.durationSec;
          acc.runCount += r.runCount;
          return acc;
        },
        { lengthMm: 0, durationSec: 0, runCount: 0 },
      ),
    [grains],
  );
  const factories = (dir.data?.factories ?? []).map((f) => ({ value: f.id, label: f.name }));

  const groupColumns: TableColumnsType<WeldSummaryRow> = [
    ...(tab === "factory" ? [{ title: "工厂", dataIndex: "factoryName" } satisfies TableColumnsType<WeldSummaryRow>[number]] : []),
    ...(tab === "project"
      ? [
          { title: "工厂", dataIndex: "factoryName", width: 160 } satisfies TableColumnsType<WeldSummaryRow>[number],
          { title: "工程", dataIndex: "projectName" } satisfies TableColumnsType<WeldSummaryRow>[number],
        ]
      : []),
    ...(tab === "day"
      ? [
          { title: "工厂", dataIndex: "factoryName", width: 160 } satisfies TableColumnsType<WeldSummaryRow>[number],
          { title: "日期", dataIndex: "day", width: 120 } satisfies TableColumnsType<WeldSummaryRow>[number],
        ]
      : []),
    { title: "次数", dataIndex: "runCount", width: 80 },
    { title: "总焊长", key: "len", width: 110, render: (_: unknown, r: WeldSummaryRow) => formatLengthM(r.lengthMm) },
    { title: "总时长", key: "dur", width: 110, render: (_: unknown, r: WeldSummaryRow) => formatDuration(r.durationSec) },
    { title: "次均焊长", key: "alen", width: 110, render: (_: unknown, r: WeldSummaryRow) => formatLengthM(avg(r.lengthMm, r.runCount)) },
    { title: "次均时长", key: "adur", width: 110, render: (_: unknown, r: WeldSummaryRow) => formatDuration(avg(r.durationSec, r.runCount)) },
  ];

  return (
    <>
      <PageHeader
        title="报表"
        description="跨厂按工程和日汇总焊长时长；不含厂内人员与组织。厂端连上后自动上送。"
        extra={
          <>
            <Select allowClear placeholder="工厂" style={{ width: 200 }} options={factories} value={factoryId} onChange={setFactoryId} />
            <RangePicker
              allowClear={false}
              value={range}
              onChange={(v) => {
                if (v?.[0] && v[1]) setRange([v[0].startOf("day"), v[1].startOf("day")]);
              }}
            />
          </>
        }
      />
      {listed.error ? <p>{errorMessage(listed.error)}</p> : null}
      <Row gutter={[16, 16]} style={{ marginBottom: 16 }}>
        <Col xs={24} sm={6}>
          <Card>
            <Statistic title="起停次数" value={totals.runCount} loading={listed.isLoading} />
          </Card>
        </Col>
        <Col xs={24} sm={6}>
          <Card>
            <Statistic title="总焊长" value={formatLengthM(totals.lengthMm)} loading={listed.isLoading} />
          </Card>
        </Col>
        <Col xs={24} sm={6}>
          <Card>
            <Statistic title="总时长" value={formatDuration(totals.durationSec)} loading={listed.isLoading} />
          </Card>
        </Col>
        <Col xs={24} sm={6}>
          <Card>
            <Statistic title="次均时长" value={formatDuration(avg(totals.durationSec, totals.runCount))} loading={listed.isLoading} />
          </Card>
        </Col>
      </Row>
      <Tabs
        activeKey={tab}
        onChange={(k) => setTab(k as WeldGroup)}
        items={[
          { key: "factory", label: "按工厂" },
          { key: "project", label: "按工程" },
          { key: "day", label: "按日" },
          { key: "grain", label: "明细" },
        ]}
      />
      {tab === "grain" ? (
        <Table
          rowKey={(r, i) => `${r.factoryId}-${r.day}-${r.projectName}-${r.weldKind}-${i}`}
          columns={grainColumns()}
          dataSource={grains ?? []}
          loading={listed.isLoading}
          pagination={{ pageSize: 20 }}
        />
      ) : (
        <Table
          rowKey={(r, i) => `${r.factoryId}-${r.projectName}-${r.day}-${i}`}
          columns={groupColumns}
          dataSource={reportRows}
          loading={listed.isLoading}
          pagination={{ pageSize: 20 }}
        />
      )}
    </>
  );
}
