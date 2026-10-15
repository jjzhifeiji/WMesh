import { FileOutlined, FolderOutlined } from "@ant-design/icons";
import { Button, Empty, Input, Select, Tag, Tree, Typography, type TreeDataNode } from "antd";
import { useEffect, useMemo, useState } from "react";
import { errorMessage } from "@/shared/api/client";
import { weldKindLabel, WELD_SINGLE } from "@/features/templates/projectKinds";
import { useFactoryFS, type Asset, type AssetKind, type FSNode } from "./api";
import { fsFolderPath, fsNodeTitle, innerCounts } from "./ProcessExplorer";

const none: FSNode[] = [];

type FactoryOpt = { id: string; name: string; channelOnline?: boolean };

function statusLabel(status: string) {
  if (status === "draft") return "未发布";
  if (status === "available") return "可用";
  if (status === "disabled") return "已停用";
  return status;
}

function levelLabel(level: string) {
  if (level === "factory") return "厂级";
  if (level === "personal") return "个人级";
  if (level === "platform") return "平台级";
  return level;
}

function levelColor(level: string) {
  if (level === "factory") return "blue";
  if (level === "platform") return "cyan";
  return "purple";
}

export function PromotePicker({
  kind,
  noun,
  factoryId,
  factories,
  platformAssets,
  promoting,
  onPickFactory,
  onPromote,
}: {
  kind: AssetKind;
  noun: string;
  factoryId: string | null;
  factories: FactoryOpt[];
  platformAssets: Asset[];
  promoting: boolean;
  onPickFactory: (id: string) => void;
  onPromote: (assetId: string, outcome: "same" | "cover" | "new") => void;
}) {
  const fs = useFactoryFS(factoryId, kind);
  const nodes = fs.data ?? none;
  const [query, setQuery] = useState("");
  const [pickedId, setPickedId] = useState<string | null>(null);
  const [expandedKeys, setExpandedKeys] = useState<string[]>([]);
  const byId = useMemo(() => new Map(nodes.map((n) => [n.id, n])), [nodes]);
  const counts = useMemo(() => innerCounts(nodes), [nodes]);
  const roots = useMemo(() => nodes.filter((n) => !n.parentId && n.nodeKind === "folder"), [nodes]);
  const current = (pickedId && byId.get(pickedId)?.nodeKind === "folder" ? byId.get(pickedId) : null) ?? roots[0] ?? null;

  useEffect(() => {
    if (!roots.length) return;
    setExpandedKeys((keys) => (keys.length ? keys : roots.map((r) => r.id)));
  }, [roots]);

  const needle = query.trim().toLowerCase();
  const exact = query.trim();
  const searching = needle.length > 0;
  const children = useMemo(() => {
    if (!current || searching) return [];
    return nodes
      .filter((n) => n.parentId === current.id)
      .toSorted((a, b) => {
        if (a.nodeKind !== b.nodeKind) return a.nodeKind === "folder" ? -1 : 1;
        return fsNodeTitle(a).localeCompare(fsNodeTitle(b), "zh");
      });
  }, [nodes, current, searching]);
  const hits = useMemo(() => {
    if (!searching) return [];
    return nodes
      .filter((n) => {
        if (n.nodeKind !== "file") return false;
        const name = n.asset?.name || n.name;
        const code = n.asset?.code ?? "";
        if (code && (code === exact || code === exact.toUpperCase())) return true;
        return name.toLowerCase().includes(needle) || code.toLowerCase().includes(needle);
      })
      .toSorted((a, b) => (a.asset?.name || a.name).localeCompare(b.asset?.name || b.name, "zh"));
  }, [nodes, searching, needle, exact]);
  const shown = searching ? hits : children;

  const crumbs = useMemo(() => {
    const parts: FSNode[] = [];
    const seen = new Set<string>();
    let cur: FSNode | undefined = current ?? undefined;
    while (cur && !seen.has(cur.id)) {
      seen.add(cur.id);
      parts.unshift(cur);
      cur = cur.parentId ? byId.get(cur.parentId) : undefined;
    }
    return parts;
  }, [current, byId]);

  const treeData = useMemo(() => {
    const walk = (id: string): TreeDataNode => {
      const n = byId.get(id);
      const folders = nodes.filter((x) => x.parentId === id && x.nodeKind === "folder");
      return {
        key: id,
        title: (
          <span className="fs-tree-title">
            <FolderOutlined />
            <span className="fs-tree-name">{n ? fsNodeTitle(n) : id}</span>
            <span className="fs-tree-count">{counts.get(id) ?? 0}</span>
          </span>
        ),
        children: folders.map((f) => walk(f.id)),
      };
    };
    return roots.map((r) => walk(r.id));
  }, [roots, nodes, byId, counts]);

  const openFolder = (id: string) => {
    setPickedId(id);
    const ids: string[] = [];
    const seen = new Set<string>();
    let cur = byId.get(id);
    while (cur && !seen.has(cur.id)) {
      seen.add(cur.id);
      ids.push(cur.id);
      cur = cur.parentId ? byId.get(cur.parentId) : undefined;
    }
    setExpandedKeys((keys) => [...new Set([...keys, ...ids])]);
  };

  return (
    <>
      <Select
        style={{ width: "100%", marginBottom: 12 }}
        placeholder="选择工厂"
        value={factoryId ?? undefined}
        onChange={onPickFactory}
        options={factories.map((f) => ({
          value: f.id,
          label: `${f.name}${f.channelOnline ? " · 在线" : " · 离线"}`,
          disabled: !f.channelOnline,
        }))}
      />
      <Input.Search
        allowClear
        placeholder={`搜索${noun}名称或编号`}
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        style={{ width: "100%", marginBottom: 12 }}
      />
      <div className="promote-browser">
        <div className="fs-col fs-tree">
          <div className="fs-col-head">
            <Typography.Text strong>文件树</Typography.Text>
          </div>
          <div className="fs-tree-body">
            {!factoryId ? (
              <Typography.Text type="secondary">先选一个在线工厂。</Typography.Text>
            ) : fs.isLoading ? (
              <Typography.Text type="secondary">载入目录…</Typography.Text>
            ) : fs.isError ? (
              <Typography.Text type="danger">{errorMessage(fs.error)}</Typography.Text>
            ) : roots.length === 0 ? (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={`该厂没有${noun}目录。`} />
            ) : (
              <Tree
                blockNode
                selectedKeys={current ? [current.id] : []}
                expandedKeys={expandedKeys}
                treeData={treeData}
                onExpand={(keys) => setExpandedKeys(keys.map(String))}
                onSelect={(_, info) => openFolder(String(info.node.key))}
              />
            )}
          </div>
        </div>
        <div className="fs-col fs-list">
          <div className="fs-col-head">
            <Typography.Text strong>{searching ? `搜索结果 ${hits.length} 项` : "文件列表"}</Typography.Text>
            {!searching && crumbs.length > 0 ? (
              <div className="fs-crumbs">
                {crumbs.map((c, i) => (
                  <span key={c.id} className="fs-crumb-wrap">
                    {i > 0 ? <span className="fs-crumb-sep">/</span> : null}
                    <button type="button" className="fs-crumb" onClick={() => openFolder(c.id)}>
                      {fsNodeTitle(c)}
                    </button>
                  </span>
                ))}
              </div>
            ) : null}
          </div>
          <div className="fs-list-body">
            {shown.length === 0 ? (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={searching ? `没有匹配的${noun}。` : "这个文件夹是空的"} />
            ) : (
              shown.map((n) => (
                <PromoteRow
                  key={n.id}
                  n={n}
                  path={searching && n.parentId ? fsFolderPath(byId.get(n.parentId) ?? n, byId) : ""}
                  platformAssets={platformAssets}
                  promoting={promoting}
                  onOpen={() => {
                    if (n.nodeKind === "folder") openFolder(n.id);
                  }}
                  onPromote={onPromote}
                />
              ))
            )}
          </div>
        </div>
      </div>
    </>
  );
}

function PromoteRow({
  n,
  path,
  platformAssets,
  promoting,
  onOpen,
  onPromote,
}: {
  n: FSNode;
  path: string;
  platformAssets: Asset[];
  promoting: boolean;
  onOpen: () => void;
  onPromote: (assetId: string, outcome: "same" | "cover" | "new") => void;
}) {
  if (n.nodeKind === "folder") {
    return (
      <div className="fs-row" onClick={onOpen}>
        <FolderOutlined />
        <span className="fs-row-main">
          <Typography.Text>{fsNodeTitle(n)}</Typography.Text>
        </span>
      </div>
    );
  }
  const asset = n.asset;
  const level = String(asset?.level ?? "");
  const plat = asset ? platformAssets.find((a) => a.id === asset.id || a.sourceId === asset.id) : undefined;
  const same = Boolean(plat && asset && plat.digest === asset.digest);
  const locked = level === "platform";
  const label = locked ? (plat && same ? "已是最新" : "已在云端") : same ? "已是最新" : plat ? "覆盖" : "升档";
  return (
    <div className="fs-row" style={{ cursor: "default" }}>
      <FileOutlined />
      <span className="fs-row-main">
        <Typography.Text strong>{asset?.name || n.name}</Typography.Text>
        {asset ? (
          <Typography.Text type="secondary" style={{ marginLeft: 8 }}>
            {[asset.code, statusLabel(asset.status), weldKindLabel(asset.weldKind || WELD_SINGLE), `修订 ${asset.revision}`].filter(Boolean).join(" · ")}
          </Typography.Text>
        ) : null}
        {level ? (
          <Tag color={levelColor(level)} style={{ marginLeft: 8 }}>
            {levelLabel(level)}
          </Tag>
        ) : null}
        {path ? (
          <Typography.Text type="secondary" className="fs-hit-path">
            {path}
          </Typography.Text>
        ) : null}
      </span>
      <Button
        size="small"
        type={locked ? "default" : "primary"}
        disabled={locked || !asset}
        loading={promoting}
        style={{ flex: "none" }}
        onClick={() => {
          if (!asset || locked) return;
          onPromote(asset.id, same ? "same" : plat ? "cover" : "new");
        }}
      >
        {label}
      </Button>
    </div>
  );
}
