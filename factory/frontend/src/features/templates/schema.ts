import { inferProjectKinds, isRootItem, itemFields, selectedFields, DEFAULT_TEMPLATES, itemExtra, lookupTemplate, templatesFromKinds, type ProjectItemTemplate } from "./projectKinds";
import { newId } from "@/shared/id";

export type FieldType = "string" | "number" | "bool" | "object" | "array" | "process";

export type Kind = "string" | "enum" | "object" | "array" | "process";

export type TemplateField = {
  key: string; // JSON 键
  label: string; // 给人看的名字
  type: FieldType; // string 文本；process 工艺引用；有 options 为枚举；number/bool 为旧值
  unit?: string; // 单位
  required?: boolean; // 表单是否必填
  default?: unknown; // 缺省值
  options?: string[]; // 枚举取值
  fields?: TemplateField[]; // object 子字段
  items?: TemplateField; // array 元素
};

export type ProjectItemSchema = {
  id: string; // 这份模版的身份
  name: string; // 给人看的名称
  fields: TemplateField[]; // 这份自己的字段
};

export type ContentSchema = {
  root: "object" | "array"; // 根类型
  fields?: TemplateField[]; // 工艺：根对象字段
  item?: TemplateField; // 旧工程表：一条焊缝的字段
  kinds?: string[]; // 旧工程：种类表
  templates?: ProjectItemTemplate[]; // 旧工程：种类登记簿
  projectItems?: ProjectItemSchema[]; // 工程：各份独立字段表
};

// projectContentSchema 把已收/当前各份工程模版收成焊缝数组编辑器用的表。
export function projectContentSchema(rows: { id: string; name?: string; schema?: ContentSchema }[]): ContentSchema {
  return {
    root: "array",
    projectItems: rows.map((r) => ({ id: r.id, name: r.name ?? "", fields: r.schema?.fields ?? [] })),
  };
}

export function kindOf(field: TemplateField): Kind {
  if (field.type === "process") return "process";
  if (field.type === "object" || field.type === "array") return field.type;
  if (field.type === "bool" || (field.options && field.options.length > 0)) return "enum";
  if (field.key === "processId" && field.type === "string") return "process";
  return "string";
}

// isProcessRef 类型为工艺即引用；旧模版键 processId 的文本同样算。
export function isProcessRef(field: TemplateField): boolean {
  return kindOf(field) === "process";
}

export function enumOptions(field: TemplateField): string[] {
  if (field.options?.length) return field.options;
  if (field.type === "bool") return ["否", "是"];
  return [];
}

export function enumValue(field: TemplateField, value: unknown): string {
  const opts = enumOptions(field);
  if (typeof value === "boolean") return value ? "是" : "否";
  const s = value == null ? "" : String(value);
  if (opts.includes(s)) return s;
  return enumDefault(field);
}

export function enumDefault(field: TemplateField): string {
  const opts = enumOptions(field);
  if (typeof field.default === "boolean") return field.default ? "是" : "否";
  const s = field.default == null ? "" : String(field.default);
  if (opts.includes(s)) return s;
  return opts[0] ?? "";
}

export function textOf(value: unknown): string {
  if (value == null) return "";
  if (typeof value === "number" || typeof value === "boolean" || typeof value === "string") return String(value);
  return "";
}

export function parseText(raw: string): string | number {
  if (/^-?(?:0|[1-9]\d*)(?:\.\d+)?$/.test(raw)) return Number(raw);
  return raw;
}

export function defaultValue(schema: ContentSchema): unknown {
  if (schema.root === "array") return [];
  return objectFromFields(schema.fields ?? []);
}

// asProjectSchema 工程一律按命名模版；旧种类表转成命名份。
export function asProjectSchema(schema: ContentSchema | null): ContentSchema | null {
  if (!schema) return null;
  if (Array.isArray(schema.templates)) return { root: "array", templates: schema.templates };
  if (schema.kinds?.length) return { root: "array", templates: templatesFromKinds(schema.kinds) };
  if (schema.item) return { root: "array", templates: DEFAULT_TEMPLATES };
  return { root: "array", templates: [] };
}

export function defaultField(field: TemplateField): unknown {
  if (field.type === "bool") {
    return field.default === true || field.default === "是" || field.default === "true";
  }
  switch (kindOf(field)) {
    case "enum":
      return enumDefault(field);
    case "process":
      return typeof field.default === "string" ? field.default : "";
    case "string":
      if (field.key === "id") return newId(); // 设备侧身份不必手填
      if (typeof field.default === "number") return field.default;
      return typeof field.default === "string" ? field.default : "";
    case "object":
      return objectFromFields(field.fields ?? []);
    case "array":
      return [];
    default:
      return null;
  }
}

export function defaultItem(kind: string, extra: boolean, templateId?: string): Record<string, unknown> {
  const out = objectFromFields(itemFields(kind, extra));
  out.kind = kind;
  if (templateId) out.templateId = templateId;
  return out;
}

export function defaultItemFromFields(fields: TemplateField[], templateId: string): Record<string, unknown> {
  const out = objectFromFields(fields);
  out.templateId = templateId;
  return out;
}

function objectFromFields(fields: TemplateField[]): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const f of fields) out[f.key] = defaultField(f);
  return out;
}

// collectProcessIds 按模版抽出工艺引用；无模版时仍认 processId。
export function collectProcessIds(value: unknown, schema?: ContentSchema | null): string[] {
  const ids: string[] = [];
  const seen = new Set<string>();
  const add = (val: unknown) => {
    if (typeof val !== "string") return;
    const id = val.trim();
    if (id && !seen.has(id)) {
      seen.add(id);
      ids.push(id);
    }
  };
  const walkField = (field: TemplateField, v: unknown) => {
    if (isProcessRef(field)) {
      add(v);
      return;
    }
    const k = kindOf(field);
    if (k === "object") {
      walkObject(field.fields ?? [], v);
      return;
    }
    if (k === "array") {
      const item = field.items ?? { key: "item", label: "项", type: "string" as const };
      if (Array.isArray(v)) v.forEach((el) => walkField(item, el));
    }
  };
  const walkObject = (fields: TemplateField[], v: unknown) => {
    if (!v || typeof v !== "object" || Array.isArray(v)) return;
    const obj = v as Record<string, unknown>;
    for (const f of fields) walkField(f, obj[f.key]);
  };
  if (schema) {
    if (schema.root === "array") {
      if (schema.projectItems) {
        const byId = new Map(schema.projectItems.map((t) => [t.id, t.fields]));
        const union: TemplateField[] = [];
        const seenKeys = new Set<string>();
        for (const t of schema.projectItems) {
          for (const f of t.fields) {
            if (seenKeys.has(f.key)) continue;
            seenKeys.add(f.key);
            union.push(f);
          }
        }
        if (Array.isArray(value)) {
          value.forEach((el) => {
            const id = el && typeof el === "object" && !Array.isArray(el) ? String((el as Record<string, unknown>).templateId ?? "") : "";
            walkObject(byId.get(id) ?? union, el);
          });
        }
      } else if (Array.isArray(schema.templates)) {
        if (Array.isArray(value)) {
          value.forEach((el) => {
            const t = lookupTemplate(schema.templates, el);
            if (t) walkObject(itemFields(t.kind, itemExtra(t)), el);
          });
        }
      } else if (schema.kinds?.length) {
        const fields = selectedFields(schema.kinds);
        if (Array.isArray(value)) value.forEach((el) => walkObject(fields, el));
      } else {
        const item = schema.item ?? { key: "item", label: "项", type: "object" as const, fields: [] };
        if (Array.isArray(value)) value.forEach((el) => walkField(item, el));
      }
    } else {
      walkObject(schema.fields ?? [], value);
    }
    return ids;
  }
  const walk = (v: unknown) => {
    if (Array.isArray(v)) {
      v.forEach(walk);
      return;
    }
    if (!v || typeof v !== "object") return;
    for (const [k, val] of Object.entries(v as Record<string, unknown>)) {
      if ((k === "processId" || k === "rootProcessId" || k === "capProcessId") && typeof val === "string") {
        add(val);
        continue;
      }
      walk(val);
    }
  };
  walk(value);
  return ids;
}

export function parseContent(raw: string | undefined, schema: ContentSchema | null): unknown {
  if (!schema) return raw ?? "";
  if (!raw?.trim()) return defaultValue(schema);
  try {
    return JSON.parse(raw) as unknown;
  } catch {
    return defaultValue(schema);
  }
}

export function encodeContent(value: unknown, schema: ContentSchema | null, fallback: string): string {
  if (!schema) return typeof value === "string" ? value : fallback;
  return JSON.stringify(dropProcessPath(value ?? defaultValue(schema)));
}

/** 写出前丢掉路径键。 */
function dropProcessPath(v: unknown): unknown {
  if (Array.isArray(v)) return v.map(dropProcessPath);
  if (!v || typeof v !== "object") return v;
  const out: Record<string, unknown> = {};
  for (const [k, val] of Object.entries(v as Record<string, unknown>)) {
    if (k === "processPath") continue;
    out[k] = dropProcessPath(val);
  }
  return out;
}

const KEY_RE = /^[A-Za-z_][A-Za-z0-9_]*$/;
const MAX_DEPTH = 8;
const MAX_FIELDS = 200;

// contentFromSchema 按工艺/工程正文写出一份带默认值的 JSON。
export function contentFromSchema(schema: ContentSchema): unknown {
  if (schema.root === "array") {
    if (schema.templates?.length) {
      return schema.templates.map((t) => defaultItem(t.kind, itemExtra(t), t.id));
    }
    if (schema.kinds?.length) {
      const extra = (schema.kinds ?? []).includes("extra");
      return rootKindsOf(schema).map((kind) => defaultItem(kind, extra));
    }
    const item = schema.item ?? { key: "item", label: "项", type: "object" as const, fields: [] };
    return [defaultField(item)];
  }
  return objectFromFields(schema.fields ?? []);
}

function rootKindsOf(schema: ContentSchema): string[] {
  return (schema.kinds ?? []).filter(isRootItem);
}

// schemaFromJSON 只收工艺/工程正文；工程推断种类，不生成字段表。
export function schemaFromJSON(raw: string): ContentSchema {
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    throw new Error("不是合法 JSON");
  }
  if (isSchemaShape(parsed)) {
    throw new Error("请用工艺或工程的正文 JSON");
  }
  if (Array.isArray(parsed)) {
    if (parsed.length === 0) throw new Error("正文里没有焊缝");
    return { root: "array", templates: templatesFromKinds(inferProjectKinds(parsed)) };
  }
  const schema = inferSchema(parsed, 1);
  if (schema.root === "object" && (schema.fields?.length ?? 0) === 0) {
    throw new Error("正文里没有可用字段");
  }
  return schema;
}

function adoptFields(next: TemplateField[], prev: TemplateField[]): TemplateField[] {
  const map = new Map(prev.map((f) => [f.key, f]));
  return next.map((f) => adoptField(f, map.get(f.key)));
}

function adoptField(next: TemplateField, prev?: TemplateField): TemplateField {
  if (!prev) return next;
  const out: TemplateField = { ...next, label: prev.label || next.label };
  if (prev.unit) out.unit = prev.unit;
  if (prev.required) out.required = true;
  if (kindOf(prev) === "enum" && kindOf(next) !== "object" && kindOf(next) !== "array") {
    out.type = "string";
    out.options = enumOptions(prev);
    const val = next.default;
    out.default = typeof val === "string" && out.options.includes(val) ? val : enumDefault(out);
  }
  if (kindOf(prev) === "process" && kindOf(next) !== "object" && kindOf(next) !== "array" && kindOf(next) !== "enum") {
    out.type = "process";
    delete out.options;
    out.default = typeof next.default === "string" ? next.default : "";
  }
  if (kindOf(next) === "object") out.fields = adoptFields(next.fields ?? [], prev.fields ?? []);
  if (kindOf(next) === "array" && next.items) out.items = adoptField(next.items, prev.items);
  return out;
}

// adoptSchema 导入正文时保留已有字段的中文名、单位、枚举。
export function adoptSchema(next: ContentSchema, prev: ContentSchema | null): ContentSchema {
  if (next.templates) return { root: "array", templates: next.templates };
  if (next.kinds?.length) return { root: "array", templates: templatesFromKinds(next.kinds) };
  if (!prev || next.root !== prev.root) return next;
  if (next.root === "object") return { root: "object", fields: adoptFields(next.fields ?? [], prev.fields ?? []) };
  if (next.item) return { root: "array", item: adoptField(next.item, prev.item) };
  return next;
}

// canonicalizeSchema 工艺写成 type=process；工程只留下种类表。
export function canonicalizeSchema(schema: ContentSchema): ContentSchema {
  if (Array.isArray(schema.templates)) return { root: "array", templates: schema.templates };
  if (schema.kinds?.length) return { root: "array", templates: templatesFromKinds(schema.kinds) };
  if (schema.root === "array") {
    return schema.item ? { root: "array", item: canonicalizeField(schema.item) } : schema;
  }
  return { root: "object", fields: (schema.fields ?? []).map(canonicalizeField) };
}

function canonicalizeField(field: TemplateField): TemplateField {
  if (kindOf(field) === "process") {
    const next: TemplateField = { ...field, type: "process", default: typeof field.default === "string" ? field.default : "" };
    delete next.options;
    delete next.fields;
    delete next.items;
    delete next.unit;
    return next;
  }
  const next: TemplateField = { ...field };
  if (kindOf(field) === "object") next.fields = (field.fields ?? []).map(canonicalizeField);
  if (kindOf(field) === "array" && field.items) next.items = canonicalizeField(field.items);
  return next;
}

function isSchemaShape(v: unknown): boolean {
  if (!v || typeof v !== "object" || Array.isArray(v)) return false;
  const o = v as { root?: unknown; fields?: unknown; item?: unknown; kinds?: unknown; templates?: unknown };
  if (o.root !== "object" && o.root !== "array") return false;
  return Array.isArray(o.fields) || Array.isArray(o.kinds) || Array.isArray(o.templates) || (o.item != null && typeof o.item === "object");
}

function inferSchema(v: unknown, depth: number): ContentSchema {
  if (Array.isArray(v)) return { root: "array", item: inferItem(v, depth) };
  if (v && typeof v === "object") return { root: "object", fields: inferFields(v as Record<string, unknown>, depth) };
  throw new Error("根必须是对象或数组");
}

function inferFields(obj: Record<string, unknown>, depth: number): TemplateField[] {
  const out: TemplateField[] = [];
  for (const [key, value] of Object.entries(obj)) {
    if (!KEY_RE.test(key) || out.length >= MAX_FIELDS) continue;
    out.push(inferField(key, key, value, depth));
  }
  return out;
}

function inferItem(arr: unknown[], depth: number): TemplateField {
  const sample = arr.find((el) => el != null);
  return inferField("item", "项", sample ?? "", depth);
}

function inferField(key: string, label: string, value: unknown, depth: number): TemplateField {
  if (depth > MAX_DEPTH) return { key, label, type: "string", default: "" };
  if (Array.isArray(value)) {
    return { key, label, type: "array", items: inferItem(value, depth + 1) };
  }
  if (value && typeof value === "object") {
    return { key, label, type: "object", fields: inferFields(value as Record<string, unknown>, depth + 1) };
  }
  if (key === "processId") {
    return { key, label: "工艺", type: "process", default: typeof value === "string" ? value : "" };
  }
  if (typeof value === "boolean") {
    return { key, label, type: "bool", default: value };
  }
  if (typeof value === "number") return { key, label, type: "string", default: value };
  return { key, label, type: "string", default: typeof value === "string" ? value : "" };
}
