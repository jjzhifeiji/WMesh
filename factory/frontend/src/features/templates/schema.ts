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
      if (field.key === "id") return crypto.randomUUID(); // 设备侧身份不必手填
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

// collectProcessIds 从正文抽出工艺引用（processId，兼容未改模版的 processPath）。
export function collectProcessIds(value: unknown): string[] {
  const ids: string[] = [];
  const seen = new Set<string>();
  const walk = (v: unknown) => {
    if (Array.isArray(v)) {
      v.forEach(walk);
      return;
    }
    if (!v || typeof v !== "object") return;
    for (const [k, val] of Object.entries(v as Record<string, unknown>)) {
      if ((k === "processId" || k === "processPath") && typeof val === "string") {
        const id = val.trim();
        if (id && !seen.has(id)) {
          seen.add(id);
          ids.push(id);
        }
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
  return JSON.stringify(value ?? defaultValue(schema));
}
