export type FieldType = "string" | "number" | "bool" | "object" | "array";

export type Kind = "string" | "enum" | "object" | "array";

export type TemplateField = {
  key: string; // JSON 键
  label: string; // 给人看的名字
  type: FieldType; // string 文本；有 options 为枚举；number/bool 为旧值
  unit?: string; // 单位
  required?: boolean; // 表单是否必填
  default?: unknown; // 缺省值
  options?: string[]; // 枚举取值
  fields?: TemplateField[]; // object 子字段
  items?: TemplateField; // array 元素
};

export type ContentSchema = {
  root: "object" | "array"; // 根类型
  fields?: TemplateField[]; // 根为 object
  item?: TemplateField; // 根为 array
};

export function kindOf(field: TemplateField): Kind {
  if (field.type === "object" || field.type === "array") return field.type;
  if (field.type === "bool" || (field.options && field.options.length > 0)) return "enum";
  return "string";
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

export function defaultField(field: TemplateField): unknown {
  switch (kindOf(field)) {
    case "enum":
      return enumDefault(field);
    case "string":
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

function objectFromFields(fields: TemplateField[]): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const f of fields) out[f.key] = defaultField(f);
  return out;
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
  return JSON.stringify(value ?? defaultValue(schema));
}

const KEY_RE = /^[A-Za-z_][A-Za-z0-9_]*$/;
const MAX_DEPTH = 8;
const MAX_FIELDS = 200;

// contentFromSchema 按工艺/工程正文写出一份带默认值的 JSON。
export function contentFromSchema(schema: ContentSchema): unknown {
  if (schema.root === "array") {
    const item = schema.item ?? { key: "item", label: "项", type: "object" as const, fields: [] };
    return [defaultField(item)];
  }
  return objectFromFields(schema.fields ?? []);
}

// schemaFromJSON 只收工艺/工程正文，按结构生成字段。
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
  if (kindOf(next) === "object") out.fields = adoptFields(next.fields ?? [], prev.fields ?? []);
  if (kindOf(next) === "array" && next.items) out.items = adoptField(next.items, prev.items);
  return out;
}

// adoptSchema 导入正文时保留已有字段的中文名、单位、枚举。
export function adoptSchema(next: ContentSchema, prev: ContentSchema | null): ContentSchema {
  if (!prev || next.root !== prev.root) return next;
  if (next.root === "object") return { root: "object", fields: adoptFields(next.fields ?? [], prev.fields ?? []) };
  if (next.item) return { root: "array", item: adoptField(next.item, prev.item) };
  return next;
}

function isSchemaShape(v: unknown): boolean {
  if (!v || typeof v !== "object" || Array.isArray(v)) return false;
  const o = v as { root?: unknown; fields?: unknown; item?: unknown };
  if (o.root !== "object" && o.root !== "array") return false;
  return Array.isArray(o.fields) || (o.item != null && typeof o.item === "object");
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
  if (typeof value === "boolean") {
    return { key, label, type: "string", options: ["否", "是"], default: value ? "是" : "否" };
  }
  if (typeof value === "number") return { key, label, type: "string", default: value };
  return { key, label, type: "string", default: typeof value === "string" ? value : "" };
}
