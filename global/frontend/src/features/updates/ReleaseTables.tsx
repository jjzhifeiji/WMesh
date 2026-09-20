import { App, Button, Card, Modal, Popconfirm, Progress, Space, Table, Tag, type TableColumnsType } from "antd";
import { useState } from "react";
import { formatBytes, formatTime, shortHash } from "@/shared/format";
import { errorMessage } from "@/shared/api/client";
import {
  softwareKindLabel,
  useDeleteSoftware,
  usePruneSoftware,
  useImagePruneJob,
  useStorageUsage,
  type ImageItem,
  type SoftwareKind,
  type SoftwareRelease,
} from "./api";

const kinds: SoftwareKind[] = ["wan_service", "factory_service", "client_apk"];
const localKind: SoftwareKind = "wan_service";

type KindSummary = {
  kind: SoftwareKind; // 三种包之一
  latest?: SoftwareRelease; // 该种类最高版本
  installed?: SoftwareRelease; // 本机正在跑的；仅云端服务有
  total: number; // 已发布份数
  old: number; // 可清份数
};

function keepTag(keep?: string) {
  if (keep === "installed") return <Tag color="green">已装</Tag>;
  if (keep === "latest") return <Tag color="blue">当前最高</Tag>;
  return <Tag>可清理</Tag>;
}

function imageKeepTag(keep?: string) {
  if (keep === "current") return <Tag color="green">当前</Tag>;
  if (keep === "previous") return <Tag color="blue">上一份</Tag>;
  return <Tag>可清理</Tag>;
}

function latestOf(rows: SoftwareRelease[]) {
  return rows.reduce<SoftwareRelease | undefined>((best, row) => {
    if (!best || row.version > best.version) return row;
    return best;
  }, undefined);
}

// 主表每种只留最新一条；详情里看全部版本、本机镜像并清理。
export function ReleaseTables({ rows, loading }: { rows: SoftwareRelease[]; loading: boolean }) {
  const { message } = App.useApp();
  const del = useDeleteSoftware();
  const prune = usePruneSoftware();
  const pruneImages = useImagePruneJob();
  const storage = useStorageUsage();
  const [detail, setDetail] = useState<SoftwareKind | null>(null);
  const disk = storage.data?.disk;
  const diskPct = disk && disk.total > 0 ? Math.min(100, Math.round((disk.used / disk.total) * 100)) : 0;
  const images = storage.data?.images;
  const imageItems = images?.items ?? [];
  const imageOld = imageItems.filter((row) => !row.keep).length;
  const imageText = images?.kind ? `${images.count} 个　${formatBytes(images.used)}` : "还没有回报";

  const summaries: KindSummary[] = kinds.map((kind) => {
    const data = rows.filter((r) => r.kind === kind);
    return {
      kind,
      latest: latestOf(data),
      installed: data.find((r) => r.keep === "installed"),
      total: data.length,
      old: data.filter((r) => !r.keep).length,
    };
  });

  const detailRows = detail ? rows.filter((r) => r.kind === detail) : [];
  const detailOld = detailRows.filter((r) => !r.keep).length;
  const detailImages = detail === localKind;

  const onPruneImages = async (body: { ref: string } | { all: true }) => {
    try {
      await pruneImages.run(body);
    } catch (e) {
      message.error(errorMessage(e));
    }
  };

  const summaryCols: TableColumnsType<KindSummary> = [
    { title: "种类", dataIndex: "kind", width: 120, render: (k: SoftwareKind) => softwareKindLabel[k] },
    {
      title: "最新版本",
      key: "latest",
      render: (_, row) =>
        row.latest ? (
          <Space>
            <span>
              {row.latest.versionName}（{row.latest.version}）
            </span>
            {keepTag(row.latest.keep)}
          </Space>
        ) : (
          "还没有这个种类的包"
        ),
    },
    {
      title: "本机已装",
      key: "installed",
      width: 180,
      render: (_, row) =>
        row.kind !== "wan_service"
          ? "—"
          : row.installed
            ? `${row.installed.versionName}（${row.installed.version}）`
            : "未装",
    },
    { title: "已发布", dataIndex: "total", width: 90, render: (n: number) => `${n} 份` },
    {
      title: "镜像",
      key: "images",
      width: 160,
      render: (_, row) => (row.kind === localKind ? imageText : "—"),
    },
    {
      title: "",
      key: "act",
      width: 90,
      render: (_, row) => (
        <Button type="link" size="small" disabled={row.kind !== localKind && row.total === 0} onClick={() => setDetail(row.kind)}>
          详情
        </Button>
      ),
    },
  ];

  const detailCols: TableColumnsType<SoftwareRelease> = [
    { title: "版本号", dataIndex: "version", width: 90 },
    { title: "版本名", dataIndex: "versionName" },
    { title: "摘要", dataIndex: "digest", width: 140, render: (v: string) => shortHash(v) },
    { title: "发布时间", dataIndex: "createdAt", width: 170, render: (v: string) => formatTime(v) },
    { title: "状态", dataIndex: "keep", width: 110, render: (v: string) => keepTag(v) },
    {
      title: "",
      key: "act",
      width: 90,
      render: (_, row) =>
        row.keep ? null : (
          <Popconfirm
            title="删掉这个旧包？"
            okText="删除"
            cancelText="取消"
            onConfirm={async () => {
              try {
                await del.mutateAsync({ kind: row.kind, version: row.version });
              } catch (e) {
                message.error(errorMessage(e));
              }
            }}
          >
            <Button type="link" size="small" danger>
              删除
            </Button>
          </Popconfirm>
        ),
    },
  ];

  const imageCols: TableColumnsType<ImageItem> = [
    { title: "标签", dataIndex: "ref" },
    { title: "占用", dataIndex: "size", width: 120, render: (n: number) => formatBytes(n) },
    { title: "状态", dataIndex: "keep", width: 110, render: (v: string) => imageKeepTag(v) },
    {
      title: "",
      key: "act",
      width: 90,
      render: (_, row) =>
        row.keep ? null : (
          <Popconfirm
            title="删掉这个无用镜像？"
            okText="删除"
            cancelText="取消"
            disabled={pruneImages.waiting && !pruneImages.rowBusy(row.ref)}
            onConfirm={() => void onPruneImages({ ref: row.ref })}
          >
            <Button
              type="link"
              size="small"
              danger
              loading={pruneImages.rowBusy(row.ref)}
              disabled={pruneImages.waiting && !pruneImages.rowBusy(row.ref)}
            >
              {pruneImages.rowText(row.ref) ?? "删除"}
            </Button>
          </Popconfirm>
        ),
    },
  ];

  return (
    <Space direction="vertical" size={16} style={{ width: "100%", marginTop: 16 }}>
      <Card title="已发布">
        <Table<KindSummary>
          rowKey="kind"
          columns={summaryCols}
          dataSource={summaries}
          loading={loading}
          pagination={false}
        />
      </Card>
      <Card title="本机存储" loading={storage.isLoading}>
        <Space direction="vertical" size={12} style={{ width: "100%" }}>
          <div>
            <div>
              本机磁盘　已用 {formatBytes(disk?.used ?? 0)} / 共 {formatBytes(disk?.total ?? 0)}，可用 {formatBytes(disk?.avail ?? 0)}
            </div>
            <Progress percent={diskPct} size="small" status={diskPct >= 90 ? "exception" : undefined} />
          </div>
          <div>
            对象存储　已用 {formatBytes(storage.data?.oss.used ?? 0)}（{storage.data?.oss.objects ?? 0} 个对象）
            {storage.data?.oss.bucket ? `　${storage.data.oss.bucket}` : ""}
          </div>
          <div>数据库　已用 {formatBytes(storage.data?.database ?? 0)}</div>
          <div>本机镜像　{images?.kind ? `已用 ${formatBytes(images.used)}（${images.count} 个）` : "还没有回报"}</div>
        </Space>
      </Card>
      <Modal
        title={detail ? `${softwareKindLabel[detail]}全部版本` : "全部版本"}
        open={detail !== null}
        onCancel={() => setDetail(null)}
        footer={null}
        width={880}
        destroyOnHidden
        styles={{ body: { maxHeight: "60vh", overflow: "auto" } }}
      >
        <Space style={{ marginBottom: 12 }}>
          <Popconfirm
            title="清掉该种类所有可删旧包？"
            okText="清理"
            cancelText="取消"
            disabled={detailOld === 0}
            onConfirm={async () => {
              if (!detail) return;
              try {
                const got = await prune.mutateAsync(detail);
                message.success(got.deleted ? `已清理 ${got.deleted} 份` : "没有可清理的旧包");
              } catch (e) {
                message.error(errorMessage(e));
              }
            }}
          >
            <Button size="small" disabled={detailOld === 0} loading={prune.isPending}>
              清理旧版本
            </Button>
          </Popconfirm>
          <span>{detailOld ? `${detailOld} 份可清` : "没有可清理的旧包"}</span>
        </Space>
        <Table<SoftwareRelease>
          rowKey={(r) => `${r.kind}:${r.version}`}
          columns={detailCols}
          dataSource={detailRows}
          pagination={{ pageSize: 10, hideOnSinglePage: true }}
        />
        {detailImages ? (
          <Space direction="vertical" size={12} style={{ width: "100%", marginTop: 16 }}>
            <Space>
              <Popconfirm
                title="清掉不再用的 app 镜像？"
                okText="清理"
                cancelText="取消"
                disabled={imageOld === 0 || (pruneImages.waiting && !pruneImages.allBusy)}
                onConfirm={() => void onPruneImages({ all: true })}
              >
                <Button
                  size="small"
                  disabled={imageOld === 0 || (pruneImages.waiting && !pruneImages.allBusy)}
                  loading={pruneImages.allBusy}
                >
                  清理无用镜像
                </Button>
              </Popconfirm>
              <span>{imageOld ? `${imageOld} 个可清` : "没有可清理的镜像"}</span>
              {pruneImages.allText ? <span>{pruneImages.allText}</span> : null}
            </Space>
            <Table<ImageItem>
              rowKey={(r) => `${r.ref}:${r.id}`}
              columns={imageCols}
              dataSource={imageItems}
              pagination={false}
              locale={{ emptyText: images?.kind ? "本机没有这类镜像" : "本机还没有回报镜像占用" }}
            />
          </Space>
        ) : null}
      </Modal>
    </Space>
  );
}
