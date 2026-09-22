import { HolderOutlined } from "@ant-design/icons";
import { Button, Input, InputNumber, Space, Switch, Typography } from "antd";
import { useRef, useState, type DragEvent } from "react";
import { defaultItemFromFields, type ContentSchema, type TemplateField } from "@/features/templates/schema";
import {
  ITEM_MULTI,
  ITEM_SINGLE,
  ITEM_TBAR,
  SEED_TPL_MULTI,
  SEED_TPL_SINGLE,
  SEED_TPL_TBAR,
  WELD_MULTI,
  WELD_SINGLE,
  WELD_TBAR,
  itemFields,
  projectTemplatesForWeld,
  weldKindLabel,
} from "@/features/templates/projectKinds";
import { newId } from "@/shared/id";
import { ProcessTreeSelect, type ProcessOption } from "./ProcessTreeSelect";

export type { ProcessOption };
export type ProjectTpl = { id: string; name?: string; schema?: ContentSchema };

type Weld = Record<string, unknown>;

function asObj(v: unknown): Weld {
  if (v && typeof v === "object" && !Array.isArray(v)) return v as Weld;
  return {};
}

function asArr(v: unknown): unknown[] {
  return Array.isArray(v) ? v : [];
}

function str(v: unknown) {
  return typeof v === "string" ? v : v == null ? "" : String(v);
}

function num(v: unknown) {
  if (typeof v === "number" && Number.isFinite(v)) return v;
  const n = Number(v);
  return Number.isFinite(n) ? n : 0;
}

function isOn(v: unknown) {
  return v === true || v === "是" || v === "true";
}

function emptyPoint(type: string) {
  return {
    id: newId(),
    type,
    pose: null as Weld | null,
    jointAngles: null as number[] | null,
  };
}

const POINT_NAMES: Record<string, string> = {
  START_SAFE: "起安",
  START: "起点",
  MIDDLE: "中间",
  ARC_MIDDLE: "圆中",
  END: "终点",
  END_SAFE: "终安",
  GROOVE_A_LOWER: "A下",
  GROOVE_B_LOWER: "B下",
  GROOVE_A_UPPER: "A上",
  GROOVE_B_UPPER: "B上",
};

const POINT_COLORS: Record<string, string> = {
  START_SAFE: "#9e9e9e",
  END_SAFE: "#9e9e9e",
  START: "#f44336",
  END: "#f44336",
  MIDDLE: "#2196f3",
  ARC_MIDDLE: "#ff9800",
  GROOVE_A_LOWER: "#00897b",
  GROOVE_B_LOWER: "#00897b",
  GROOVE_A_UPPER: "#00897b",
  GROOVE_B_UPPER: "#00897b",
};

const POSE_FIELDS: { key: string; label: string }[] = [
  { key: "x", label: "X" },
  { key: "y", label: "Y" },
  { key: "z", label: "Z" },
  { key: "rx", label: "Rx" },
  { key: "ry", label: "Ry" },
  { key: "rz", label: "Rz" },
  { key: "ext1", label: "外轴" },
];

function emptyPose() {
  return { x: 0, y: 0, z: 0, rx: 0, ry: 0, rz: 0, ext1: 0 };
}

function pointName(type: string) {
  return POINT_NAMES[type] || type;
}

function hasPose(pt: Weld) {
  const pose = asObj(pt.pose);
  return Object.keys(pose).length > 0;
}

function pointHost(kind: string, weld: Weld) {
  return kind === WELD_MULTI ? asObj(weld.basePath) : weld;
}

function readPoints(kind: string, weld: Weld) {
  return asArr(pointHost(kind, weld).points).map(asObj);
}

function writePoints(kind: string, weld: Weld, points: Weld[], selected: number) {
  if (kind === WELD_MULTI) {
    return { ...weld, basePath: { ...asObj(weld.basePath), points, selectedPointIndex: selected } };
  }
  return { ...weld, points, selectedPointIndex: selected };
}

function seedId(weldKind: string) {
  if (weldKind === WELD_MULTI) return SEED_TPL_MULTI;
  if (weldKind === WELD_TBAR) return SEED_TPL_TBAR;
  return SEED_TPL_SINGLE;
}

function seedKind(weldKind: string) {
  if (weldKind === WELD_MULTI) return ITEM_MULTI;
  if (weldKind === WELD_TBAR) return ITEM_TBAR;
  return ITEM_SINGLE;
}

function tplFields(weldKind: string, templates: ProjectTpl[]): TemplateField[] {
  const tpl = projectTemplatesForWeld(templates, weldKind)[0];
  return tpl?.schema?.fields ?? itemFields(seedKind(weldKind), weldKind === WELD_SINGLE);
}

// emptyWeld 按 App 新建焊道：名称 + 占位点，工艺空着。
export function emptyWeld(weldKind: string, templates: ProjectTpl[], index: number, arc = false): Weld {
  const tpl = projectTemplatesForWeld(templates, weldKind)[0];
  const item = defaultItemFromFields(tplFields(weldKind, templates), tpl?.id ?? seedId(weldKind));
  item.id = newId();
  if (weldKind === WELD_MULTI) {
    item.name = `${arc ? "多层圆弧" : "多层直线"} ${index + 1}`;
    const base = asObj(item.basePath);
    const pts = [emptyPoint("START_SAFE"), emptyPoint("START")];
    if (arc) pts.push(emptyPoint("ARC_MIDDLE"));
    pts.push(emptyPoint("END"), emptyPoint("END_SAFE"));
    base.id = newId();
    base.name = "Base Path";
    base.points = pts;
    base.processId = "";
    base.isEnabled = true;
    item.basePath = base;
    item.passes = [];
    item.kind = ITEM_MULTI;
    item.isEnabled = true;
    item.isBaseCompleted = false;
    item.refPointX1 = null;
    item.refPointZ1 = null;
    item.refPointXMiddle = null;
    item.refPointZMiddle = null;
    item.refPointXEnd = null;
    item.refPointZEnd = null;
    delete item.points;
    delete item.processId;
    delete item.extraProcesses;
    delete item.selectedPointIndex;
  } else if (weldKind === WELD_TBAR) {
    item.name = `焊道 ${index + 1}`;
    item.kind = ITEM_TBAR;
    item.isEnabled = true;
    item.points = [
      emptyPoint("START_SAFE"),
      emptyPoint("GROOVE_A_LOWER"),
      emptyPoint("GROOVE_B_LOWER"),
      emptyPoint("GROOVE_A_UPPER"),
      emptyPoint("GROOVE_B_UPPER"),
      emptyPoint("START"),
      emptyPoint("END"),
      emptyPoint("END_SAFE"),
    ];
    item.processId = "";
  } else {
    item.name = `焊道 ${index + 1}`;
    item.points = [emptyPoint("START_SAFE"), emptyPoint("START"), emptyPoint("END"), emptyPoint("END_SAFE")];
    item.processId = "";
    item.kind = ITEM_SINGLE;
    item.isEnabled = true;
    item.extraProcesses = asArr(item.extraProcesses);
  }
  return item;
}

export function parseWelds(raw: string | undefined): Weld[] {
  try {
    const v = JSON.parse(raw || "[]");
    return Array.isArray(v) ? v.map(asObj) : [];
  } catch {
    return [];
  }
}

function processSelect(value: string, options: ProcessOption[] | undefined, disabled: boolean | undefined, onPick: (id: string) => void) {
  return <ProcessTreeSelect value={value} options={options} disabled={disabled} onPick={onPick} />;
}

// ProjectWeldEditor 工程改正文：焊道、工艺、点坐标；坐标 App 采集、后台手填。
export function ProjectWeldEditor({
  value,
  onChange,
  disabled,
  weldKind,
  templates,
  processOptions,
}: {
  value?: string;
  onChange?: (raw: string) => void;
  disabled?: boolean;
  weldKind?: string;
  templates: ProjectTpl[];
  processOptions?: ProcessOption[];
}) {
  const kind = weldKind || WELD_SINGLE;
  const welds = parseWelds(value);
  const [sel, setSel] = useState(0);
  const idx = welds.length === 0 ? 0 : Math.min(sel, welds.length - 1);
  const current = welds[idx];
  const emit = (next: Weld[]) => onChange?.(JSON.stringify(next));
  const patch = (i: number, next: Weld) => {
    const rows = welds.slice();
    rows[i] = next;
    emit(rows);
  };
  const add = (arc = false) => {
    const item = emptyWeld(kind, templates, welds.length, arc);
    if (kind === WELD_TBAR && welds[0]) item.gapBands = asArr(welds[0].gapBands);
    const rows = [...welds, item];
    emit(rows);
    setSel(rows.length - 1);
  };
  const move = (from: number, to: number) => {
    if (from === to || to < 0 || to >= welds.length) return;
    const rows = welds.slice();
    const [hit] = rows.splice(from, 1);
    rows.splice(to, 0, hit);
    emit(rows);
    setSel(to);
  };
  const remove = (i: number) => {
    emit(welds.filter((_, j) => j !== i));
  };
  const [dragFrom, setDragFrom] = useState<number | null>(null);
  const [dropAt, setDropAt] = useState<{ i: number; after: boolean } | null>(null);
  const skipClick = useRef(false);
  const dragFromRef = useRef<number | null>(null);
  const canDrag = !disabled && welds.length > 1;
  const dropIndex = (hover: number, after: boolean, from: number) => {
    let to = after ? hover + 1 : hover;
    if (from < to) to -= 1;
    return to;
  };
  const beginDrag = (i: number, e: DragEvent) => {
    dragFromRef.current = i;
    setDragFrom(i);
    skipClick.current = false;
    e.dataTransfer.effectAllowed = "move";
    e.dataTransfer.setData("text/plain", String(i));
  };
  const hoverRow = (i: number, e: DragEvent) => {
    if (dragFromRef.current == null) return;
    e.preventDefault();
    e.dataTransfer.dropEffect = "move";
    const box = (e.currentTarget as HTMLElement).getBoundingClientRect();
    setDropAt({ i, after: e.clientY > box.top + box.height / 2 });
  };
  const dropRow = (i: number, e: DragEvent) => {
    e.preventDefault();
    const from = dragFromRef.current ?? Number(e.dataTransfer.getData("text/plain"));
    const box = (e.currentTarget as HTMLElement).getBoundingClientRect();
    const after = e.clientY > box.top + box.height / 2;
    dragFromRef.current = null;
    setDragFrom(null);
    setDropAt(null);
    if (!Number.isFinite(from)) return;
    const to = dropIndex(i, after, from);
    if (from !== to) skipClick.current = true;
    move(from, to);
  };
  const endDrag = () => {
    dragFromRef.current = null;
    setDragFrom(null);
    setDropAt(null);
  };
  const pickRow = (i: number) => {
    if (skipClick.current) {
      skipClick.current = false;
      return;
    }
    setSel(i);
  };

  const hasWeld = Boolean(current);
  return (
    <div className="weld-editor">
      <div className="weld-editor-list" onDragEnd={endDrag}>
        {disabled ? null : (
          <div className="weld-editor-list-bar">
            {kind === WELD_MULTI ? (
              <>
                <Button size="small" onClick={() => add(false)}>
                  添加直线焊道
                </Button>
                <Button size="small" onClick={() => add(true)}>
                  添加圆弧焊道
                </Button>
              </>
            ) : (
              <Button size="small" onClick={() => add()}>
                添加焊道
              </Button>
            )}
            <Button size="small" danger disabled={!hasWeld} onClick={() => remove(idx)}>
              {kind === WELD_MULTI ? "删除多层焊道" : "删除焊道"}
            </Button>
          </div>
        )}
        {welds.length === 0 ? (
          <Typography.Text type="secondary" style={{ display: "block", padding: 12 }}>
            还没有焊道
          </Typography.Text>
        ) : (
          welds.map((w, i) => (
            <div
              key={str(w.id) || i}
              role="button"
              tabIndex={0}
              className={`weld-editor-item${i === idx ? " is-on" : ""}${dragFrom === i ? " is-drag" : ""}${dropAt?.i === i ? (dropAt.after ? " is-drop-after" : " is-drop-before") : ""}`}
              onClick={() => pickRow(i)}
              onKeyDown={(e) => {
                if (e.key === "Enter" || e.key === " ") pickRow(i);
              }}
              onDragOver={canDrag ? (e) => hoverRow(i, e) : undefined}
              onDrop={canDrag ? (e) => dropRow(i, e) : undefined}
            >
              {canDrag ? (
                <span className="weld-editor-grip" draggable onDragStart={(e) => beginDrag(i, e)} onClick={(e) => e.stopPropagation()}>
                  <HolderOutlined />
                </span>
              ) : null}
              <span style={{ flex: 1, minWidth: 0 }}>
                <Typography.Text ellipsis>{str(w.name) || `焊道 ${i + 1}`}</Typography.Text>
              </span>
              <Typography.Text type="secondary">{isOn(w.isEnabled) || isOn(asObj(w.basePath).isEnabled) ? "启用" : "停用"}</Typography.Text>
            </div>
          ))
        )}
      </div>
      <div className="weld-editor-main">
        {!current ? (
          <Typography.Text type="secondary">从左边添加焊道。点坐标可在这里手填，App 里仍用机械臂采集。</Typography.Text>
        ) : (
          <Space direction="vertical" size={8} style={{ width: "100%" }}>
            <Typography.Text type="secondary">{weldKindLabel(kind)} · 点选后手填坐标；平板仍可用机械臂采集。</Typography.Text>
            <div>
              <Typography.Text type="secondary">名称</Typography.Text>
              <Input
                value={str(current.name)}
                disabled={disabled}
                onChange={(e) => patch(idx, { ...current, name: e.target.value })}
              />
            </div>
            {kind === WELD_MULTI ? (
              <MultiLayerPanel weld={current} disabled={disabled} processOptions={processOptions} onChange={(next) => patch(idx, next)} />
            ) : (
              <>
                <Space>
                  <span>启用</span>
                  <Switch
                    checked={isOn(current.isEnabled)}
                    disabled={disabled}
                    onChange={(on) => patch(idx, { ...current, isEnabled: on })}
                  />
                </Space>
                {kind === WELD_TBAR ? null : (
                  <div>
                    <Typography.Text type="secondary">工艺</Typography.Text>
                    {processSelect(str(current.processId), processOptions, disabled, (id) => patch(idx, { ...current, processId: id }))}
                  </div>
                )}
                <PointSlots key={str(current.id)} kind={kind} weld={current} disabled={disabled} onChange={(next) => patch(idx, next)} />
                {kind === WELD_SINGLE ? (
                  <ExtraSlots
                    rows={asArr(current.extraProcesses).map(asObj)}
                    disabled={disabled}
                    processOptions={processOptions}
                    onChange={(rows) => patch(idx, { ...current, extraProcesses: rows })}
                  />
                ) : null}
                {kind === WELD_TBAR ? (
                  <BandSlots
                    rows={asArr(current.gapBands).map(asObj)}
                    disabled={disabled}
                    processOptions={processOptions}
                    onChange={(rows) => patch(idx, { ...current, gapBands: rows })}
                  />
                ) : null}
              </>
            )}
          </Space>
        )}
      </div>
    </div>
  );
}

const REF_KEYS: { key: string; label: string; needArc?: boolean }[] = [
  { key: "refPointX1", label: "X1" },
  { key: "refPointZ1", label: "Z1" },
  { key: "refPointXMiddle", label: "X中", needArc: true },
  { key: "refPointZMiddle", label: "Z中", needArc: true },
  { key: "refPointXEnd", label: "X2" },
  { key: "refPointZEnd", label: "Z2" },
];

function refFilled(weld: Weld, key: string) {
  return Object.keys(asObj(asObj(weld[key]).pose)).length > 0;
}

function MultiLayerPanel({
  weld,
  disabled,
  processOptions,
  onChange,
}: {
  weld: Weld;
  disabled?: boolean;
  processOptions?: ProcessOption[];
  onChange: (next: Weld) => void;
}) {
  const [passSel, setPassSel] = useState(-1);
  const [refKey, setRefKey] = useState<string | null>(null);
  const base = asObj(weld.basePath);
  const points = readPoints(WELD_MULTI, weld);
  const hasArc = points.some((p) => str(p.type) === "ARC_MIDDLE");
  const passes = asArr(weld.passes).map(asObj);
  const onBase = passSel < 0;
  const pickBase = () => {
    setPassSel(-1);
  };

  return (
    <>
      <div className={`weld-editor-layer${onBase ? " is-on" : ""}`} onClick={pickBase}>
        <Typography.Text strong>基准层 (第1道)</Typography.Text>
        <div style={{ marginTop: 6 }}>
          <Typography.Text type="secondary">工艺</Typography.Text>
          {processSelect(str(base.processId), processOptions, disabled, (id) =>
            onChange({ ...weld, basePath: { ...base, processId: id } }),
          )}
        </div>
        <Space style={{ marginTop: 6 }}>
          <span>焊接</span>
          <Switch
            size="small"
            checked={isOn(base.isEnabled)}
            disabled={disabled}
            onChange={(on) => {
              onChange({ ...weld, isEnabled: on, basePath: { ...base, isEnabled: on } });
            }}
          />
          <Typography.Text type="secondary">{isOn(base.isEnabled) ? "焊接" : "跳过"}</Typography.Text>
        </Space>
        {isOn(base.isEnabled) ? (
          <>
            <PointSlots
              key={str(weld.id)}
              kind={WELD_MULTI}
              weld={weld}
              disabled={disabled}
              locked={!onBase}
              onChange={(next) => {
                setPassSel(-1);
                setRefKey(null);
                onChange(next);
              }}
            />
            <RefSlots
              weld={weld}
              hasArc={hasArc}
              selected={onBase ? refKey : null}
              disabled={disabled || !onBase}
              onSelect={(key) => {
                setPassSel(-1);
                setRefKey(key);
              }}
              onChange={onChange}
            />
          </>
        ) : null}
      </div>
      <PassSlots
        rows={passes}
        selected={passSel}
        disabled={disabled}
        processOptions={processOptions}
        onSelect={(i) => {
          setPassSel(i);
          setRefKey(null);
        }}
        onChange={(rows) => {
          onChange({ ...weld, passes: rows });
          if (passSel >= rows.length) setPassSel(-1);
        }}
      />
    </>
  );
}

function RefSlots({
  weld,
  hasArc,
  selected,
  disabled,
  onSelect,
  onChange,
}: {
  weld: Weld;
  hasArc: boolean;
  selected: string | null;
  disabled?: boolean;
  onSelect: (key: string) => void;
  onChange: (next: Weld) => void;
}) {
  const keys = REF_KEYS.filter((r) => !r.needArc || hasArc);
  const cur = selected ? asObj(asObj(weld[selected]).pose) : {};
  const filled = selected ? refFilled(weld, selected) : false;
  return (
    <div>
      <Typography.Text type="secondary">参考点 · 选中后手填坐标</Typography.Text>
      <div className="weld-editor-points">
        {keys.map((r) => {
          const on = selected === r.key;
          const set = refFilled(weld, r.key);
          const color = "#9e9e9e";
          return (
            <button
              key={r.key}
              type="button"
              className={`weld-editor-pt${on ? " is-on" : ""}${set ? " has-pose" : ""}`}
              style={on ? { color, borderColor: color, background: "#fff" } : { background: color, borderColor: color }}
              onClick={() => onSelect(r.key)}
            >
              {r.label}
              {set ? " ✓" : ""}
            </button>
          );
        })}
      </div>
      {selected ? (
        <div className="weld-editor-pose">
          {POSE_FIELDS.map((f) => (
            <InputNumber
              key={f.key}
              size="small"
              addonBefore={f.label}
              disabled={disabled}
              value={filled ? num(cur[f.key]) : null}
              onChange={(v) =>
                onChange({
                  ...weld,
                  [selected]: { pose: { ...emptyPose(), ...cur, [f.key]: v ?? 0 }, jointAngles: Array.isArray(asObj(weld[selected]).jointAngles) ? asObj(weld[selected]).jointAngles : [] },
                })
              }
            />
          ))}
          {disabled ? null : (
            <Button size="small" disabled={!filled} onClick={() => onChange({ ...weld, [selected]: null })}>
              清除坐标
            </Button>
          )}
        </div>
      ) : null}
    </div>
  );
}

function PointSlots({
  kind,
  weld,
  disabled,
  locked,
  onChange,
}: {
  kind: string;
  weld: Weld;
  disabled?: boolean;
  locked?: boolean;
  onChange: (next: Weld) => void;
}) {
  const points = readPoints(kind, weld);
  const stored = points.length === 0 ? 0 : Math.min(Math.max(0, num(pointHost(kind, weld).selectedPointIndex)), points.length - 1);
  const [pick, setPick] = useState(stored);
  const sel = points.length === 0 ? 0 : Math.min(pick, points.length - 1);
  const current = points[sel];
  const pose = asObj(current?.pose);
  const filled = current ? hasPose(current) : false;
  const typ = current ? str(current.type) : "";
  const canDrop = typ === "MIDDLE" || typ === "ARC_MIDDLE";
  const canInsert = kind !== WELD_TBAR;
  const frozen = disabled || locked;
  const setSel = (i: number) => {
    setPick(i);
    if (!disabled) onChange(writePoints(kind, weld, points, i));
  };
  const setPts = (next: Weld[], i = sel) => {
    setPick(i);
    onChange(writePoints(kind, weld, next, i));
  };
  const patchPt = (next: Weld) => setPts(points.map((p, i) => (i === sel ? next : p)));
  const insertBeforeEnd = (type: string) => {
    const end = points.findIndex((p) => str(p.type) === "END");
    if (end <= 0) return;
    const next = points.slice();
    next.splice(end, 0, emptyPoint(type));
    setPts(next, end);
  };
  const seed = () => {
    if (kind === WELD_MULTI) {
      const arc = points.some((p) => str(p.type) === "ARC_MIDDLE");
      const pts = [emptyPoint("START_SAFE"), emptyPoint("START")];
      if (arc) pts.push(emptyPoint("ARC_MIDDLE"));
      pts.push(emptyPoint("END"), emptyPoint("END_SAFE"));
      setPts(pts, 0);
      return;
    }
    if (kind === WELD_TBAR) {
      setPts(
        [
          emptyPoint("START_SAFE"),
          emptyPoint("GROOVE_A_LOWER"),
          emptyPoint("GROOVE_B_LOWER"),
          emptyPoint("GROOVE_A_UPPER"),
          emptyPoint("GROOVE_B_UPPER"),
          emptyPoint("START"),
          emptyPoint("END"),
          emptyPoint("END_SAFE"),
        ],
        0,
      );
      return;
    }
    setPts([emptyPoint("START_SAFE"), emptyPoint("START"), emptyPoint("END"), emptyPoint("END_SAFE")], 0);
  };

  return (
    <div>
      <Typography.Text type="secondary">点列 · 选中后手填坐标</Typography.Text>
      {points.length === 0 ? (
        <div style={{ marginTop: 4 }}>
          <Typography.Text type="secondary">还没有点。</Typography.Text>
          {disabled ? null : (
            <Button size="small" style={{ marginLeft: 8 }} onClick={seed}>
              补占位点
            </Button>
          )}
        </div>
      ) : (
        <>
          <div className="weld-editor-points">
            {points.map((pt, i) => {
              const color = POINT_COLORS[str(pt.type)] || "#607d8b";
              const on = i === sel;
              return (
                <button
                  key={str(pt.id) || i}
                  type="button"
                  className={`weld-editor-pt${on ? " is-on" : ""}${hasPose(pt) ? " has-pose" : ""}`}
                  style={on ? { color, borderColor: color, background: "#fff" } : { background: color, borderColor: color }}
                  onClick={() => setSel(i)}
                >
                  {pointName(str(pt.type))}
                  {hasPose(pt) ? " ✓" : ""}
                </button>
              );
            })}
          </div>
          {canInsert && !frozen ? (
            <Space wrap size={4} style={{ marginTop: 6 }}>
              <Button size="small" onClick={() => insertBeforeEnd("MIDDLE")}>
                添加中间点
              </Button>
              <Button size="small" onClick={() => insertBeforeEnd("ARC_MIDDLE")}>
                添加圆弧点
              </Button>
              <Button size="small" danger disabled={!canDrop} onClick={() => setPts(points.filter((_, i) => i !== sel), Math.max(0, sel - 1))}>
                删除中间点
              </Button>
            </Space>
          ) : null}
          {current ? (
            <div className="weld-editor-pose">
              {POSE_FIELDS.map((f) => (
                <InputNumber
                  key={f.key}
                  size="small"
                  addonBefore={f.label}
                  disabled={frozen}
                  value={filled ? num(pose[f.key]) : null}
                  onChange={(v) => patchPt({ ...current, pose: { ...emptyPose(), ...pose, [f.key]: v ?? 0 } })}
                />
              ))}
              {frozen ? null : (
                <Button size="small" disabled={!filled} onClick={() => patchPt({ ...current, pose: null, jointAngles: null })}>
                  清除坐标
                </Button>
              )}
            </div>
          ) : null}
        </>
      )}
    </div>
  );
}

function ExtraSlots({
  rows,
  disabled,
  processOptions,
  onChange,
}: {
  rows: Weld[];
  disabled?: boolean;
  processOptions?: ProcessOption[];
  onChange: (rows: Weld[]) => void;
}) {
  return (
    <div>
      <Typography.Text type="secondary">附加工艺</Typography.Text>
      {rows.map((row, i) => (
        <div key={str(row.id) || i} className="weld-editor-extra">
          <div className="weld-editor-extra-pick">
            {processSelect(str(row.processId), processOptions, disabled, (id) => onChange(rows.map((r, j) => (j === i ? { ...r, processId: id } : r))))}
          </div>
          <Switch
            size="small"
            checked={isOn(row.isEnabled)}
            disabled={disabled}
            onChange={(on) => onChange(rows.map((r, j) => (j === i ? { ...r, isEnabled: on } : r)))}
          />
          {disabled ? null : (
            <Button size="small" type="text" danger onClick={() => onChange(rows.filter((_, j) => j !== i))}>
              删
            </Button>
          )}
        </div>
      ))}
      {disabled ? null : (
        <Button size="small" style={{ marginTop: 4 }} onClick={() => onChange([...rows, { id: newId(), processId: "", isEnabled: true }])}>
          添加附加工艺
        </Button>
      )}
    </div>
  );
}

function PassSlots({
  rows,
  selected = -1,
  disabled,
  processOptions,
  onSelect,
  onChange,
}: {
  rows: Weld[];
  selected?: number;
  disabled?: boolean;
  processOptions?: ProcessOption[];
  onSelect?: (i: number) => void;
  onChange: (rows: Weld[]) => void;
}) {
  const patch = (i: number, next: Weld) => onChange(rows.map((r, j) => (j === i ? next : r)));
  const add = () => {
    onChange([...rows, { id: newId(), name: `第 ${rows.length + 1} 道`, processId: "", isEnabled: true, isCompleted: false, valX: 0, valYLeft: 0, valYRight: 0, valZ: 0, valR: 0 }]);
  };
  return (
    <div>
      <Typography.Text strong>填充层</Typography.Text>
      {rows.map((row, i) => (
        <div key={str(row.id) || i} className={`weld-editor-pass${i === selected ? " is-on" : ""}`} onClick={() => onSelect?.(i)}>
          <Space wrap>
            <Input
              size="small"
              style={{ width: 140 }}
              value={str(row.name)}
              disabled={disabled}
              onChange={(e) => patch(i, { ...row, name: e.target.value })}
            />
            <Switch
              size="small"
              checked={isOn(row.isEnabled)}
              disabled={disabled}
              onChange={(on) => patch(i, { ...row, isEnabled: on })}
            />
            <Typography.Text type="secondary">{isOn(row.isEnabled) ? "焊接" : "跳过"}</Typography.Text>
            {disabled ? null : (
              <Button
                size="small"
                type="text"
                danger
                onClick={(e) => {
                  e.stopPropagation();
                  onChange(rows.filter((_, j) => j !== i));
                  onSelect?.(-1);
                }}
              >
                删除焊道层
              </Button>
            )}
          </Space>
          <div style={{ marginTop: 4 }}>{processSelect(str(row.processId), processOptions, disabled, (id) => patch(i, { ...row, processId: id }))}</div>
          {isOn(row.isEnabled) ? (
            <Space wrap size={4} style={{ marginTop: 4 }}>
              {(["valX", "valYLeft", "valYRight", "valZ", "valR"] as const).map((key) => (
                <InputNumber
                  key={key}
                  size="small"
                  addonBefore={key === "valX" ? "X" : key === "valYLeft" ? "Y左" : key === "valYRight" ? "Y右" : key === "valZ" ? "Z" : "R"}
                  value={num(row[key])}
                  disabled={disabled}
                  onChange={(v) => patch(i, { ...row, [key]: v ?? 0 })}
                />
              ))}
            </Space>
          ) : null}
        </div>
      ))}
      {disabled ? null : (
        <Button size="small" style={{ marginTop: 4 }} onClick={add}>
          添加焊道层
        </Button>
      )}
    </div>
  );
}

function BandSlots({
  rows,
  disabled,
  processOptions,
  onChange,
}: {
  rows: Weld[];
  disabled?: boolean;
  processOptions?: ProcessOption[];
  onChange: (rows: Weld[]) => void;
}) {
  const patch = (i: number, next: Weld) => onChange(rows.map((r, j) => (j === i ? next : r)));
  return (
    <div>
      <Typography.Text type="secondary">间隙带</Typography.Text>
      {rows.map((row, i) => (
        <div key={i} className="weld-editor-pass">
          <Space wrap size={4}>
            <Input size="small" style={{ width: 110 }} addonBefore="最小" value={String(num(row.minGap))} disabled={disabled} onChange={(e) => patch(i, { ...row, minGap: Number(e.target.value) || 0 })} />
            <Input size="small" style={{ width: 110 }} addonBefore="最大" value={String(num(row.maxGap))} disabled={disabled} onChange={(e) => patch(i, { ...row, maxGap: Number(e.target.value) || 0 })} />
            <Input size="small" style={{ width: 90 }} addonBefore="层" value={String(num(row.layer) || 1)} disabled={disabled} onChange={(e) => patch(i, { ...row, layer: Number(e.target.value) || 1 })} />
            {disabled ? null : (
              <Button size="small" type="text" danger onClick={() => onChange(rows.filter((_, j) => j !== i))}>
                删
              </Button>
            )}
          </Space>
          <div style={{ marginTop: 4 }}>
            <Typography.Text type="secondary">打底</Typography.Text>
            {processSelect(str(row.rootProcessId), processOptions, disabled, (id) => patch(i, { ...row, rootProcessId: id }))}
          </div>
          <div style={{ marginTop: 4 }}>
            <Typography.Text type="secondary">盖面</Typography.Text>
            {processSelect(str(row.capProcessId), processOptions, disabled, (id) => patch(i, { ...row, capProcessId: id }))}
          </div>
        </div>
      ))}
      {disabled ? null : (
        <Button size="small" style={{ marginTop: 4 }} onClick={() => onChange([...rows, { minGap: 0, maxGap: 0, layer: 1, rootProcessId: "", capProcessId: "" }])}>
          添加间隙带
        </Button>
      )}
    </div>
  );
}
