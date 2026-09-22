import { FolderOutlined, FileOutlined } from "@ant-design/icons";
import { App, Dropdown, Empty, Input, Tree, Typography, type MenuProps, type TreeDataNode } from "antd";
import { useEffect, useMemo, useRef, useState, type DragEvent, type KeyboardEvent, type MouseEvent, type ReactNode } from "react";
import { flushSync } from "react-dom";
import { errorMessage } from "@/shared/api/client";
import { weldKindLabel, WELD_SINGLE } from "@/features/templates/projectKinds";
import { useCopyFSNode, useCreateFSFolder, useDeleteFSNode, useFS, useMoveFSNode, useRenameFSNode, type AssetKind, type FSNode } from "./api";

const none: FSNode[] = [];

function rootLabel(n: FSNode) {
  if (n.treeLevel === "factory") return "厂级";
  if (n.treeLevel === "personal") return n.ownerName ? `个人级 · ${n.ownerName}` : "个人级";
  return "平台级";
}

export function fsNodeTitle(n: FSNode) {
  if (!n.parentId) return rootLabel(n);
  return n.name || "未命名";
}

export function fsFolderPath(n: FSNode, byId: Map<string, FSNode>) {
  const parts: string[] = [];
  const seen = new Set<string>();
  let cur: FSNode | undefined = n;
  while (cur && !seen.has(cur.id)) {
    seen.add(cur.id);
    parts.unshift(fsNodeTitle(cur));
    cur = cur.parentId ? byId.get(cur.parentId) : undefined;
  }
  return parts.join(" / ");
}

function matchesFSSearch(n: FSNode, needle: string, weld: string) {
  if (n.nodeKind !== "file") return false;
  if (weld !== "all" && (n.asset?.weldKind || WELD_SINGLE) !== weld) return false;
  if (!needle) return true;
  const hay = [fsNodeTitle(n), n.asset?.name ?? "", n.asset?.code ?? "", weldKindLabel(n.asset?.weldKind || "")].join("\n").toLowerCase();
  return hay.includes(needle);
}

export function fsFolderOptions(nodes: FSNode[], allow?: (n: FSNode) => boolean) {
  const byId = new Map(nodes.map((n) => [n.id, n]));
  return nodes
    .filter((n) => n.nodeKind === "folder" && (!allow || allow(n)))
    .map((n) => ({ value: n.id, label: fsFolderPath(n, byId) }))
    .toSorted((a, b) => a.label.localeCompare(b.label, "zh"));
}

function sameTree(a: FSNode, b: FSNode) {
  return a.treeLevel === b.treeLevel && (a.ownerId ?? "") === (b.ownerId ?? "");
}

function isAncestor(id: string, ofId: string, byId: Map<string, FSNode>) {
  const seen = new Set<string>();
  let cur = byId.get(ofId);
  while (cur && !seen.has(cur.id)) {
    if (cur.id === id) return true;
    seen.add(cur.id);
    cur = cur.parentId ? byId.get(cur.parentId) : undefined;
  }
  return false;
}

function canMoveInto(src: FSNode, dest: FSNode, byId: Map<string, FSNode>, canWrite: (n: FSNode) => boolean) {
  if (dest.nodeKind !== "folder" || !src.parentId || src.id === dest.id || src.parentId === dest.id) return false;
  if (!sameTree(src, dest) || !canWrite(src) || !canWrite(dest)) return false;
  return src.nodeKind !== "folder" || !isAncestor(src.id, dest.id, byId);
}

function canCopyNode(n: FSNode) {
  if (!n.parentId) return false;
  if (n.nodeKind === "file" && n.asset && n.asset.kind === "process" && !n.asset.copyable) return false;
  return true;
}

function canCopyInto(src: FSNode, dest: FSNode, byId: Map<string, FSNode>) {
  if (dest.nodeKind !== "folder" || !src.parentId || src.id === dest.id) return false;
  if (src.assetKind !== dest.assetKind) return false;
  if (sameTree(src, dest)) return src.nodeKind !== "folder" || !isAncestor(src.id, dest.id, byId);
  return src.treeLevel === "platform" && dest.treeLevel === "factory";
}

function TreeTitle({
  n,
  dragging,
  over,
  canDrag,
  onDragStart,
  onDragOver,
  onDrop,
}: {
  n: FSNode;
  dragging: boolean;
  over: boolean;
  canDrag: boolean;
  onDragStart: (e: DragEvent) => void;
  onDragOver: (e: DragEvent) => void;
  onDrop: (e: DragEvent) => void;
}) {
  return (
    <span
      className={`fs-tree-title${over ? " is-drop" : ""}${dragging ? " is-drag" : ""}`}
      data-fs-id={n.id}
      draggable={canDrag}
      onDragStart={onDragStart}
      onDragOver={onDragOver}
      onDrop={onDrop}
    >
      {fsNodeTitle(n)}
    </span>
  );
}

export function ProcessExplorer({
  kind = "process",
  noun = "工艺",
  writable,
  canCreateFile,
  onNewProcess,
  onSelectFile,
  selectedAssetId,
  listRow,
  detail,
  query = "",
  weldFilter = "all",
}: {
  kind?: AssetKind;
  noun?: string;
  writable: (n: FSNode) => boolean;
  canCreateFile?: (n: FSNode) => boolean;
  onNewProcess: (parent: FSNode) => void;
  onSelectFile: (n: FSNode | null) => void;
  selectedAssetId: string | null;
  listRow: (n: FSNode) => ReactNode;
  detail: ReactNode;
  query?: string;
  weldFilter?: string;
}) {
  const { message, modal } = App.useApp();
  const q = useFS(kind);
  const nodes = q.data ?? none;
  const mkdir = useCreateFSFolder();
  const rename = useRenameFSNode();
  const move = useMoveFSNode();
  const copy = useCopyFSNode();
  const remove = useDeleteFSNode();
  const [pickedId, setPickedId] = useState<string | null>(null);
  const [listFocusId, setListFocusId] = useState<string | null>(null);
  const [clipId, setClipId] = useState<string | null>(null);
  const [expandedKeys, setExpandedKeys] = useState<string[]>([]);
  const expandInited = useRef(false);
  const [listMenu, setListMenu] = useState<MenuProps["items"]>([]);
  const [treeMenu, setTreeMenu] = useState<MenuProps["items"]>([]);
  const [dragId, setDragId] = useState<string | null>(null);
  const [overId, setOverId] = useState<string | null>(null);
  const didDrag = useRef(false);

  const byId = useMemo(() => new Map(nodes.map((n) => [n.id, n])), [nodes]);
  const roots = useMemo(() => nodes.filter((n) => !n.parentId && n.nodeKind === "folder"), [nodes]);
  const current = (pickedId && byId.get(pickedId)) || roots[0] || null;
  const dragSrc = dragId ? (byId.get(dragId) ?? null) : null;

  const expandPath = (id: string) => {
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

  const clearFile = () => {
    setListFocusId(null);
    onSelectFile(null);
  };

  const pickFolder = (id: string) => {
    setPickedId(id);
    clearFile();
    expandPath(id);
  };

  const pickFile = (n: FSNode) => {
    if (n.parentId) {
      setPickedId(n.parentId);
      expandPath(n.parentId);
    }
    onSelectFile(n);
  };

  useEffect(() => {
    if (expandInited.current || !roots.length) return;
    expandInited.current = true;
    setExpandedKeys(roots.map((r) => r.id));
  }, [roots]);

  useEffect(() => {
    if (!overId || !dragSrc) return;
    const dest = byId.get(overId);
    if (!dest || dest.nodeKind !== "folder") return;
    const t = window.setTimeout(() => {
      const ids: string[] = [];
      const seen = new Set<string>();
      let cur = byId.get(overId);
      while (cur && !seen.has(cur.id)) {
        seen.add(cur.id);
        ids.push(cur.id);
        cur = cur.parentId ? byId.get(cur.parentId) : undefined;
      }
      setExpandedKeys((keys) => [...new Set([...keys, ...ids])]);
    }, 450);
    return () => window.clearTimeout(t);
  }, [overId, dragSrc, byId]);

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

  const children = useMemo(() => {
    if (!current) return none;
    return nodes
      .filter((n) => n.parentId === current.id)
      .toSorted((a, b) => {
        if (a.nodeKind !== b.nodeKind) return a.nodeKind === "folder" ? -1 : 1;
        return fsNodeTitle(a).localeCompare(fsNodeTitle(b), "zh");
      });
  }, [nodes, current]);

  const needle = query.trim().toLowerCase();
  const searching = Boolean(needle || weldFilter !== "all");
  const hits = useMemo(() => {
    if (!searching) return none;
    return nodes
      .filter((n) => matchesFSSearch(n, needle, weldFilter))
      .toSorted((a, b) => {
        if (a.nodeKind !== b.nodeKind) return a.nodeKind === "folder" ? -1 : 1;
        return fsNodeTitle(a).localeCompare(fsNodeTitle(b), "zh");
      });
  }, [nodes, searching, needle, weldFilter]);
  const shown = searching ? hits : children;

  const openHit = (n: FSNode) => {
    pickFile(n);
    setListFocusId(n.id);
  };

  useEffect(() => {
    if (!listFocusId) return;
    const el = document.querySelector(`.fs-list-body [data-fs-id="${listFocusId}"]`);
    el?.scrollIntoView({ block: "nearest" });
  }, [listFocusId, current, searching]);

  useEffect(() => {
    if (!listFocusId) return;
    if (shown.some((n) => n.id === listFocusId)) return;
    clearFile();
  }, [shown, listFocusId]);

  const onErr = (err: unknown) => message.error(errorMessage(err));
  const allowFile = (n: FSNode) => Boolean((canCreateFile ?? writable)(n));

  const askName = (title: string, initial: string, onOk: (name: string) => void) => {
    let value = initial;
    modal.confirm({
      title,
      content: <Input defaultValue={initial} autoFocus onChange={(e) => (value = e.target.value)} />,
      onOk: () => {
        const name = value.trim();
        if (!name) {
          message.error("请输入名称");
          return Promise.reject();
        }
        onOk(name);
      },
    });
  };

  const newFolder = (parent: FSNode) => {
    askName("新建文件夹", "新建文件夹", (name) => {
      mkdir.mutate({ parentId: parent.id, name }, { onError: onErr, onSuccess: () => message.success("已创建") });
    });
  };

  const renameFolder = (n: FSNode) => {
    askName("重命名", n.name, (name) => {
      rename.mutate({ id: n.id, name }, { onError: onErr, onSuccess: () => message.success("已改名") });
    });
  };

  const deleteNode = (n: FSNode) => {
    modal.confirm({
      title: `删除「${fsNodeTitle(n)}」？`,
      content: n.nodeKind === "folder" ? `文件夹里的${noun}也会删掉。` : noun === "工艺" ? "删除后不能恢复。若已被工程依赖会拒绝。" : "删除后不能恢复。",
      okButtonProps: { danger: true },
      onOk: () =>
        remove.mutate(n.id, {
          onError: onErr,
          onSuccess: () => {
            message.success("已删除");
            if (n.id === pickedId && n.parentId) setPickedId(n.parentId);
            if (n.id === listFocusId || n.nodeKind === "file") clearFile();
          },
        }),
    });
  };

  const copyNode = (n: FSNode) => {
    if (!canCopyNode(n)) return;
    setClipId(n.id);
    message.success("已复制");
  };

  const pasteInto = (dest: FSNode) => {
    const clip = clipId ? byId.get(clipId) : undefined;
    if (!clip || !writable(dest) || !canCopyInto(clip, dest, byId)) return;
    copy.mutate(
      { id: clip.id, parentId: dest.id },
      {
        onError: onErr,
        onSuccess: () => {
          message.success("已粘贴");
          expandPath(dest.id);
        },
      },
    );
  };

  const pasteItem = (dest: FSNode | null) => {
    const clip = clipId ? byId.get(clipId) : undefined;
    const ok = Boolean(dest && writable(dest) && clip && canCopyInto(clip, dest, byId));
    return {
      key: "paste",
      label: "粘贴",
      disabled: !ok,
      onClick: () => {
        if (dest) pasteInto(dest);
      },
    };
  };

  const moveTo = (n: FSNode) => {
    const folders = nodes.filter((x) => x.nodeKind === "folder" && canMoveInto(n, x, byId, writable));
    let target = folders.find((f) => !f.parentId)?.id ?? folders[0]?.id ?? "";
    modal.confirm({
      title: `移动「${fsNodeTitle(n)}」`,
      content: (
        <select defaultValue={target} style={{ width: "100%" }} onChange={(e) => (target = e.target.value)}>
          {folders.map((f) => (
            <option key={f.id} value={f.id}>
              {fsFolderPath(f, byId)}
            </option>
          ))}
        </select>
      ),
      onOk: () => {
        if (!target) return Promise.reject();
        move.mutate({ id: n.id, parentId: target }, { onError: onErr, onSuccess: () => message.success("已移动") });
      },
    });
  };

  const endDrag = () => {
    setDragId(null);
    setOverId(null);
    window.setTimeout(() => {
      didDrag.current = false;
    }, 50);
  };

  const beginDrag = (n: FSNode, e: DragEvent) => {
    if (!n.parentId || !writable(n)) {
      e.preventDefault();
      return;
    }
    e.stopPropagation();
    e.dataTransfer.setData("text/plain", n.id);
    e.dataTransfer.effectAllowed = "move";
    didDrag.current = true;
    setDragId(n.id);
  };

  const hoverDest = (dest: FSNode, e: DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
    if (dragSrc && canMoveInto(dragSrc, dest, byId, writable)) {
      e.dataTransfer.dropEffect = "move";
      setOverId(dest.id);
      return;
    }
    e.dataTransfer.dropEffect = "none";
    setOverId(null);
  };

  const dropInto = (dest: FSNode, e: DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
    const src = dragSrc ?? byId.get(e.dataTransfer.getData("text/plain"));
    endDrag();
    if (!src || !canMoveInto(src, dest, byId, writable)) return;
    move.mutate(
      { id: src.id, parentId: dest.id },
      {
        onError: onErr,
        onSuccess: () => {
          message.success("已移动");
          expandPath(dest.id);
          if (src.id === listFocusId) clearFile();
        },
      },
    );
  };

  const folderMenu = (n: FSNode): MenuProps["items"] => {
    const write = writable(n);
    const makeFile = allowFile(n);
    return [
      write ? { key: "new-folder", label: "新建文件夹", onClick: () => newFolder(n) } : null,
      makeFile ? { key: "new-file", label: `新建${noun}`, onClick: () => onNewProcess(n) } : null,
      { type: "divider" },
      n.parentId ? { key: "copy", label: "复制", disabled: !canCopyNode(n), onClick: () => copyNode(n) } : null,
      pasteItem(n),
      write && n.parentId ? { type: "divider" } : null,
      write && n.parentId ? { key: "rename", label: "重命名", onClick: () => renameFolder(n) } : null,
      write && n.parentId ? { key: "move", label: "移动到…", onClick: () => moveTo(n) } : null,
      write && n.parentId ? { key: "delete", label: "删除", danger: true, onClick: () => deleteNode(n) } : null,
    ].filter(Boolean) as MenuProps["items"];
  };

  const fileMenu = (n: FSNode): MenuProps["items"] => {
    const write = writable(n);
    return [
      { key: "open", label: "打开", onClick: () => pickFile(n) },
      { type: "divider" },
      { key: "copy", label: "复制", disabled: !canCopyNode(n), onClick: () => copyNode(n) },
      pasteItem(current),
      write ? { type: "divider" } : null,
      write ? { key: "move", label: "移动到…", onClick: () => moveTo(n) } : null,
      write ? { key: "delete", label: "删除", danger: true, onClick: () => deleteNode(n) } : null,
    ].filter(Boolean) as MenuProps["items"];
  };

  const menuOf = (n: FSNode | null) => {
    if (!n) return [];
    return n.nodeKind === "file" ? fileMenu(n) : folderMenu(n);
  };

  const openListMenu = (e: MouseEvent) => {
    const hit = (e.target as HTMLElement).closest("[data-fs-id]");
    const id = hit?.getAttribute("data-fs-id") ?? current?.id ?? null;
    flushSync(() => setListMenu(menuOf(id ? (byId.get(id) ?? null) : current)));
  };

  const openTreeMenu = (e: MouseEvent) => {
    const row = (e.target as HTMLElement).closest(".ant-tree-treenode");
    const hit = (row as HTMLElement | null)?.querySelector("[data-fs-id]") ?? (e.target as HTMLElement).closest("[data-fs-id]");
    const id = hit?.getAttribute("data-fs-id");
    const n = id ? byId.get(id) : null;
    flushSync(() => {
      if (n?.nodeKind === "folder") setPickedId(n.id);
      setTreeMenu(n?.nodeKind === "folder" ? folderMenu(n) : []);
    });
  };

  const onExplorerKey = (e: KeyboardEvent<HTMLDivElement>) => {
    if ((e.target as HTMLElement).closest("input, textarea, .fs-detail")) return;
    const focus = (listFocusId && byId.get(listFocusId)) || null;
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "c") {
      if (focus && canCopyNode(focus)) {
        e.preventDefault();
        copyNode(focus);
      }
      return;
    }
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "v") {
      if (current) {
        e.preventDefault();
        pasteInto(current);
      }
      return;
    }
    if (e.key === "Backspace" && current?.parentId) {
      e.preventDefault();
      pickFolder(current.parentId);
      return;
    }
    if (e.key === "Enter" && focus) {
      e.preventDefault();
      if (searching) openHit(focus);
      else if (focus.nodeKind === "folder") pickFolder(focus.id);
      else pickFile(focus);
      return;
    }
    if (e.key === "F2" && focus?.nodeKind === "folder" && focus.parentId && writable(focus)) {
      e.preventDefault();
      renameFolder(focus);
      return;
    }
    if (e.key === "Delete" && focus?.parentId && writable(focus)) {
      e.preventDefault();
      deleteNode(focus);
    }
  };

  const treeData = (() => {
    const walk = (id: string): TreeDataNode => {
      const n = byId.get(id);
      const folders = nodes.filter((x) => x.parentId === id && x.nodeKind === "folder");
      return {
        key: id,
        title: n ? (
          <TreeTitle
            n={n}
            dragging={dragId === n.id}
            over={overId === n.id}
            canDrag={Boolean(n.parentId && writable(n))}
            onDragStart={(e) => beginDrag(n, e)}
            onDragOver={(e) => hoverDest(n, e)}
            onDrop={(e) => dropInto(n, e)}
          />
        ) : (
          id
        ),
        icon: <FolderOutlined />,
        children: folders.map((f) => walk(f.id)),
      };
    };
    return roots.map((r) => walk(r.id));
  })();

  const listDropClass = current && overId === current.id && !children.some((n) => n.id === overId && n.nodeKind === "folder") ? " is-drop" : "";

  return (
    <div className="fs-explorer" tabIndex={0} onKeyDown={onExplorerKey} onDragEnd={endDrag}>
      <Dropdown menu={{ items: treeMenu }} trigger={["contextMenu"]}>
        <div className="fs-col fs-tree" onContextMenu={openTreeMenu}>
          <div className="fs-col-head">
            <Typography.Text strong>文件树</Typography.Text>
          </div>
          <div className="fs-tree-body">
            {q.isLoading ? (
              <Typography.Text type="secondary">载入目录…</Typography.Text>
            ) : (
              <Tree
                showIcon
                blockNode
                selectedKeys={current ? [current.id] : []}
                expandedKeys={expandedKeys}
                treeData={treeData}
                onExpand={(keys) => setExpandedKeys(keys.map(String))}
                onSelect={(_, info) => {
                  const id = String(info.node.key);
                  const n = byId.get(id);
                  if (n?.nodeKind !== "folder") return;
                  setPickedId(id);
                  clearFile();
                  setExpandedKeys((keys) => (keys.includes(id) ? keys.filter((k) => k !== id) : [...keys, id]));
                }}
              />
            )}
          </div>
        </div>
      </Dropdown>
      <div className="fs-col fs-list">
        <div className="fs-col-head">
          <Typography.Text strong>文件列表</Typography.Text>
          {searching ? (
            <div className="fs-crumbs">
              <span className="fs-crumb-sep">搜索结果 {hits.length} 项</span>
            </div>
          ) : crumbs.length > 0 ? (
            <div className="fs-crumbs">
              {crumbs.map((c, i) => (
                <span key={c.id} className="fs-crumb-wrap">
                  {i > 0 ? <span className="fs-crumb-sep">/</span> : null}
                  <button
                    type="button"
                    className={`fs-crumb${overId === c.id ? " is-drop" : ""}`}
                    onClick={() => pickFolder(c.id)}
                    onDragOver={(e) => hoverDest(c, e)}
                    onDrop={(e) => dropInto(c, e)}
                  >
                    {fsNodeTitle(c)}
                  </button>
                </span>
              ))}
            </div>
          ) : null}
        </div>
        <Dropdown menu={{ items: listMenu }} trigger={["contextMenu"]}>
          <div
            className={`fs-list-body${searching ? "" : listDropClass}`}
            onContextMenu={openListMenu}
            onClick={(e) => {
              if ((e.target as HTMLElement).closest(".fs-row")) return;
              clearFile();
            }}
            onDragOver={(e) => {
              if (!searching && current) hoverDest(current, e);
            }}
            onDrop={(e) => {
              if (!searching && current) dropInto(current, e);
            }}
          >
            {shown.length === 0 ? (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={searching ? `没有符合的${noun}` : "这个文件夹是空的"} />
            ) : (
              shown.map((n) => (
                <button
                  key={n.id}
                  type="button"
                  data-fs-id={n.id}
                  draggable={Boolean(!searching && n.parentId && writable(n))}
                  className={`fs-row${n.id === listFocusId || (n.assetId && n.assetId === selectedAssetId) ? " is-on" : ""}${overId === n.id && n.nodeKind === "folder" ? " is-drop" : ""}${dragId === n.id ? " is-drag" : ""}${!searching && n.parentId && writable(n) ? " is-grab" : ""}`}
                  onDragStart={(e) => beginDrag(n, e)}
                  onDragOver={(e) => {
                    if (!searching && n.nodeKind === "folder") hoverDest(n, e);
                  }}
                  onDrop={(e) => {
                    if (!searching && n.nodeKind === "folder") dropInto(n, e);
                  }}
                  onClick={() => {
                    if (didDrag.current) return;
                    if (searching) {
                      openHit(n);
                      return;
                    }
                    setListFocusId(n.id);
                    if (n.nodeKind === "folder") {
                      expandPath(n.id);
                      onSelectFile(null);
                    } else pickFile(n);
                  }}
                  onDoubleClick={() => {
                    if (didDrag.current) return;
                    if (searching) {
                      openHit(n);
                      return;
                    }
                    if (n.nodeKind === "folder") pickFolder(n.id);
                    else pickFile(n);
                  }}
                >
                  {n.nodeKind === "folder" ? <FolderOutlined /> : <FileOutlined />}
                  <span className="fs-row-main">
                    {listRow(n)}
                    {searching ? (
                      <Typography.Text type="secondary" className="fs-hit-path">
                        {n.parentId ? fsFolderPath(byId.get(n.parentId) ?? n, byId) : fsFolderPath(n, byId)}
                      </Typography.Text>
                    ) : null}
                  </span>
                </button>
              ))
            )}
          </div>
        </Dropdown>
      </div>
      <div className="fs-col fs-detail">
        <div className="fs-col-head">
          <Typography.Text strong>文件详情</Typography.Text>
        </div>
        <div className="fs-detail-body">{detail}</div>
      </div>
    </div>
  );
}
