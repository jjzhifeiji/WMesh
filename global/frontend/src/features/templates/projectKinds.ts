import type { TemplateField } from "./schema";

export const ITEM_SINGLE = "single";
export const ITEM_MULTI = "multi";
export const ITEM_TBAR = "tbar";
export const ITEM_EXTRA = "extra";

export type RootItemKind = typeof ITEM_SINGLE | typeof ITEM_MULTI | typeof ITEM_TBAR;
export type ProjectItemKind = RootItemKind | typeof ITEM_EXTRA;

export type ProjectItemTemplate = {
  id: string; // 这份模版的身份
  name: string; // 给人看的名称
  kind: RootItemKind; // single / multi / tbar
  extra: boolean; // 是否带附加工艺槽
};

export const ROOT_KINDS: { key: RootItemKind; label: string }[] = [
  { key: ITEM_SINGLE, label: "单层焊道" },
  { key: ITEM_MULTI, label: "多层焊缝" },
  { key: ITEM_TBAR, label: "T排对接" },
];

export const CATALOG_KINDS: { key: ProjectItemKind; label: string; root: boolean }[] = [
  { key: ITEM_SINGLE, label: "单层焊道", root: true },
  { key: ITEM_MULTI, label: "多层焊缝", root: true },
  { key: ITEM_EXTRA, label: "附加工艺", root: false },
];

export const SEED_TPL_SINGLE = "11111111-1111-4111-8111-111111111111";
export const SEED_TPL_MULTI = "22222222-2222-4222-8222-222222222222";
export const SEED_TPL_TBAR = "44444444-4444-4444-8444-444444444444";

export const DEFAULT_TEMPLATES: ProjectItemTemplate[] = [
  { id: SEED_TPL_SINGLE, name: "单层焊道", kind: ITEM_SINGLE, extra: true },
  { id: SEED_TPL_MULTI, name: "多层焊缝", kind: ITEM_MULTI, extra: false },
  { id: SEED_TPL_TBAR, name: "T排对接", kind: ITEM_TBAR, extra: false },
];

export function isProjectItemKind(v: string): v is ProjectItemKind {
  return CATALOG_KINDS.some((k) => k.key === v);
}

export function isRootItem(kind: string): kind is RootItemKind {
  return kind === ITEM_SINGLE || kind === ITEM_MULTI || kind === ITEM_TBAR;
}

export function kindLabel(kind: string): string {
  return ROOT_KINDS.find((k) => k.key === kind)?.label ?? CATALOG_KINDS.find((k) => k.key === kind)?.label ?? kind;
}

export function hasKind(kinds: string[] | undefined, kind: string): boolean {
  return (kinds ?? []).includes(kind);
}

export function rootKinds(kinds: string[] | undefined): RootItemKind[] {
  return (kinds ?? []).filter(isRootItem);
}

export function itemExtra(t: ProjectItemTemplate): boolean {
  return t.extra && t.kind !== ITEM_MULTI;
}

export function templatesFromKinds(kinds: string[]): ProjectItemTemplate[] {
  const extra = kinds.includes(ITEM_EXTRA);
  return ROOT_KINDS.filter((k) => kinds.includes(k.key)).map((k) => ({
    id: crypto.randomUUID(),
    name: k.label,
    kind: k.key,
    extra: extra && k.key !== ITEM_MULTI,
  }));
}

export function projectTemplatesValid(list: ProjectItemTemplate[] | undefined): boolean {
  const templates = list ?? [];
  if (templates.length > 50) return false;
  const ids = new Set<string>();
  const names = new Set<string>();
  for (const t of templates) {
    const name = t.name.trim();
    if (!name || name.length > 80 || !isRootItem(t.kind) || !t.id) return false;
    if (ids.has(t.id) || names.has(name)) return false;
    ids.add(t.id);
    names.add(name);
  }
  return true;
}

export function lookupTemplate(templates: ProjectItemTemplate[] | undefined, el: unknown): ProjectItemTemplate | undefined {
  const list = templates ?? [];
  if (!el || typeof el !== "object" || Array.isArray(el)) return undefined;
  const obj = el as Record<string, unknown>;
  if (typeof obj.templateId === "string") {
    const hit = list.find((t) => t.id === obj.templateId);
    if (hit) return hit;
  }
  const kind = inferItemKind(el);
  return list.find((t) => t.kind === kind);
}

export function itemFields(kind: string, extra: boolean): TemplateField[] {
  if (kind === ITEM_SINGLE) return itemSingle(extra);
  if (kind === ITEM_MULTI) return itemMulti();
  if (kind === ITEM_TBAR) return itemTBar();
  return [];
}

export function selectedFields(kinds: string[] | undefined): TemplateField[] {
  const extra = hasKind(kinds, ITEM_EXTRA);
  const out: TemplateField[] = [];
  const seen = new Set<string>();
  const add = (fields: TemplateField[]) => {
    for (const f of fields) {
      if (seen.has(f.key)) continue;
      seen.add(f.key);
      out.push(f);
    }
  };
  if (hasKind(kinds, ITEM_SINGLE)) add(itemSingle(extra));
  if (hasKind(kinds, ITEM_MULTI)) add(itemMulti());
  return out;
}

export function inferItemKind(v: unknown): ProjectItemKind {
  if (!v || typeof v !== "object" || Array.isArray(v)) return ITEM_SINGLE;
  const obj = v as Record<string, unknown>;
  if (typeof obj.kind === "string" && isRootItem(obj.kind)) return obj.kind as ProjectItemKind;
  if ("basePath" in obj || "passes" in obj) return ITEM_MULTI;
  if ("gapBands" in obj) return ITEM_TBAR;
  if (hasGroovePoint(obj)) return ITEM_TBAR;
  return ITEM_SINGLE;
}

function hasGroovePoint(obj: Record<string, unknown>): boolean {
  if (!Array.isArray(obj.points)) return false;
  return obj.points.some((el) => {
    if (!el || typeof el !== "object" || Array.isArray(el)) return false;
    const typ = (el as Record<string, unknown>).type;
    return typ === "GROOVE_A_LOWER" || typ === "GROOVE_B_LOWER" || typ === "GROOVE_A_UPPER" || typ === "GROOVE_B_UPPER";
  });
}

export function inferProjectKinds(value: unknown): ProjectItemKind[] {
  if (!Array.isArray(value)) return [ITEM_SINGLE];
  const set = new Set<ProjectItemKind>();
  for (const el of value) {
    set.add(inferItemKind(el));
    if (el && typeof el === "object" && !Array.isArray(el) && "extraProcesses" in el) set.add(ITEM_EXTRA);
  }
  if (![...set].some((k) => k !== ITEM_EXTRA)) set.add(ITEM_SINGLE);
  return CATALOG_KINDS.map((k) => k.key).filter((k) => set.has(k));
}

function num(key: string, label: string, unit: string, def: number): TemplateField {
  return { key, label, type: "string", unit, default: def };
}

function str(key: string, label: string, def: string): TemplateField {
  return { key, label, type: "string", default: def };
}

function procRef(key: string, label: string): TemplateField {
  return { key, label, type: "process", default: "" };
}

function flag(key: string, label: string, on: boolean): TemplateField {
  return { key, label, type: "string", options: ["否", "是"], default: on ? "是" : "否" };
}

function poseFields(): TemplateField[] {
  return [
    num("x", "X", "mm", 0),
    num("y", "Y", "mm", 0),
    num("z", "Z", "mm", 0),
    num("rx", "Rx", "°", 0),
    num("ry", "Ry", "°", 0),
    num("rz", "Rz", "°", 0),
    num("ext1", "外部轴", "mm", 0),
  ];
}

function pointField(): TemplateField {
  return {
    type: "object",
    key: "point",
    label: "点",
    fields: [
      str("id", "点身份", ""),
      str("type", "点类型", "START"),
      { key: "pose", label: "位姿", type: "object", fields: poseFields() },
      { key: "jointAngles", label: "关节角", type: "array", items: { key: "a", label: "角", type: "string", default: 0 } },
    ],
  };
}

function pathFields(): TemplateField[] {
  return [
    str("id", "路径身份", ""),
    str("name", "路径名", ""),
    { key: "points", label: "点", type: "array", items: pointField() },
    procRef("processId", "工艺"),
    num("selectedPointIndex", "选中点", "", 0),
  ];
}

function extraProcessesField(): TemplateField {
  return {
    key: "extraProcesses",
    label: "附加工艺",
    type: "array",
    items: {
      type: "object",
      key: "extra",
      label: "附加",
      fields: [str("id", "身份", ""), procRef("processId", "工艺")],
    },
  };
}

function itemSingle(extra: boolean): TemplateField[] {
  const fields: TemplateField[] = [
    str("id", "身份", ""),
    str("name", "名称", ""),
    { key: "points", label: "点", type: "array", items: pointField() },
    procRef("processId", "工艺"),
    num("selectedPointIndex", "选中点", "", 0),
    flag("isEnabled", "启用", true),
  ];
  if (extra) fields.push(extraProcessesField());
  return fields;
}

function itemMulti(): TemplateField[] {
  const pass: TemplateField = {
    type: "object",
    key: "pass",
    label: "焊道",
    fields: [
      str("id", "焊道身份", ""),
      str("name", "焊道名", ""),
      num("valX", "X", "mm", 0),
      num("valYLeft", "左 Y", "mm", 0),
      num("valYRight", "右 Y", "mm", 0),
      num("valZ", "Z", "mm", 0),
      num("valR", "R", "mm", 0),
      procRef("processId", "工艺"),
      flag("isCompleted", "已完成", false),
      flag("isEnabled", "启用", true),
    ],
  };
  const ref: TemplateField[] = [
    { key: "pose", label: "位姿", type: "object", fields: poseFields() },
    { key: "jointAngles", label: "关节角", type: "array", items: { key: "a", label: "角", type: "string", default: 0 } },
  ];
  return [
    str("id", "身份", ""),
    str("name", "名称", ""),
    { key: "basePath", label: "基准路径", type: "object", fields: pathFields() },
    { key: "passes", label: "多层焊道", type: "array", items: pass },
    { key: "refPointX1", label: "起点 X", type: "object", fields: ref },
    { key: "refPointZ1", label: "起点 Z", type: "object", fields: ref },
    { key: "refPointXEnd", label: "终点 X", type: "object", fields: ref },
    { key: "refPointZEnd", label: "终点 Z", type: "object", fields: ref },
    flag("isBaseCompleted", "基准完成", false),
    flag("isEnabled", "启用", true),
  ];
}

function itemTBar(): TemplateField[] {
  const band: TemplateField = {
    type: "object",
    key: "band",
    label: "间隙带",
    fields: [
      num("minGap", "最小间隙", "mm", 0),
      num("maxGap", "最大间隙", "mm", 0),
      num("layer", "层", "", 1),
      procRef("rootProcessId", "打底工艺"),
      procRef("capProcessId", "盖面工艺"),
    ],
  };
  return [
    str("id", "身份", ""),
    str("name", "名称", ""),
    { key: "points", label: "点", type: "array", items: pointField() },
    num("selectedPointIndex", "选中点", "", 0),
    flag("isEnabled", "启用", true),
    { key: "gapBands", label: "间隙带", type: "array", items: band },
  ];
}
