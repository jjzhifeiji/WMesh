import { FileOutlined, FolderOutlined } from "@ant-design/icons";
import { TreeSelect, type TreeSelectProps } from "antd";
import { useMemo } from "react";
import { useFS, type FSNode } from "./api";
import { fsNodeTitle } from "./ProcessExplorer";

export type ProcessOption = { value: string; label: string; disabled?: boolean };

type TreeNode = NonNullable<TreeSelectProps["treeData"]>[number];

function sortKids(a: FSNode, b: FSNode) {
  if (a.nodeKind !== b.nodeKind) return a.nodeKind === "folder" ? -1 : 1;
  return fsNodeTitle(a).localeCompare(fsNodeTitle(b), "zh");
}

function fileAssetId(n: FSNode) {
  return n.assetId || n.asset?.id || "";
}

function fileNode(opt: ProcessOption): TreeNode {
  return {
    value: opt.value,
    title: opt.label,
    icon: <FileOutlined />,
    isLeaf: true,
    disabled: opt.disabled,
  };
}

export function processTreeData(nodes: FSNode[], options: ProcessOption[], selected?: string): TreeNode[] {
  const byOpt = new Map(options.map((o) => [o.value, o]));
  if (selected && !byOpt.has(selected)) byOpt.set(selected, { value: selected, label: selected, disabled: true });
  const kids = new Map<string, FSNode[]>();
  for (const n of nodes) {
    if (!n.parentId) continue;
    const list = kids.get(n.parentId) ?? [];
    list.push(n);
    kids.set(n.parentId, list);
  }
  for (const list of kids.values()) list.sort(sortKids);

  const placed = new Set<string>();
  const walk = (n: FSNode): TreeNode | null => {
    if (n.nodeKind === "file") {
      const id = fileAssetId(n);
      const opt = id ? byOpt.get(id) : undefined;
      if (!opt) return null;
      placed.add(opt.value);
      return fileNode(opt);
    }
    const children = (kids.get(n.id) ?? []).map(walk).filter((x): x is TreeNode => Boolean(x));
    if (!children.length) return null;
    return {
      value: `folder:${n.id}`,
      title: fsNodeTitle(n),
      icon: <FolderOutlined />,
      selectable: false,
      children,
    };
  };

  const roots = nodes
    .filter((n) => !n.parentId && n.nodeKind === "folder")
    .sort(sortKids)
    .map(walk)
    .filter((x): x is TreeNode => Boolean(x));
  const orphans = [...byOpt.values()].filter((o) => !placed.has(o.value));
  if (orphans.length) {
    roots.push({
      value: "folder:orphan",
      title: nodes.length ? "未分类" : "工艺",
      icon: <FolderOutlined />,
      selectable: false,
      children: orphans.map(fileNode),
    });
  }
  return roots;
}

export function ProcessTreeSelect({
  value,
  options,
  disabled,
  onPick,
  className,
  size,
}: {
  value: string;
  options?: ProcessOption[];
  disabled?: boolean;
  onPick: (id: string) => void;
  className?: string;
  size?: "small" | "middle" | "large";
}) {
  const q = useFS("process");
  const treeData = useMemo(() => processTreeData(q.data ?? [], options ?? [], value), [q.data, options, value]);
  return (
    <TreeSelect
      allowClear
      showSearch
      treeLine
      treeIcon
      treeDefaultExpandAll
      treeNodeFilterProp="title"
      placeholder="不选工艺"
      value={value || undefined}
      treeData={treeData}
      disabled={disabled}
      size={size}
      className={className}
      style={{ width: "100%" }}
      popupMatchSelectWidth={false}
      listHeight={360}
      popupClassName="process-tree-select-drop"
      onChange={(id) => onPick((id as string) ?? "")}
    />
  );
}
