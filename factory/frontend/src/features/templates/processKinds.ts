import type { ContentTemplate } from "./api";
import { defaultValue, type ContentSchema } from "./schema";

// 工艺种类 = 这条工艺按哪份工艺模版建的。预置两份的身份和服务端一致。
export const SEED_TPL_WELD = "55555555-5555-4555-8555-555555555555"; // 焊接参数
export const SEED_TPL_CORNER = "66666666-6666-4666-8666-666666666666"; // 包角
export const CORNER_KIND = "cornerGeometry"; // 包角正文顶层的标记，示教器靠它认包角
const TPL_KEY = "templateId"; // 自建种类在正文顶层记所属模版

const SEED_NAMES: Record<string, string> = { [SEED_TPL_WELD]: "焊接参数", [SEED_TPL_CORNER]: "包角" };

export const PROCESS_SEEDS = [
  { id: SEED_TPL_WELD, name: SEED_NAMES[SEED_TPL_WELD] },
  { id: SEED_TPL_CORNER, name: SEED_NAMES[SEED_TPL_CORNER] },
];

// 只有焊接参数才分适用作业（单层 / 多层 / T 排）。
export function isWeldKind(templateId: string | undefined): boolean {
  return !templateId || templateId === SEED_TPL_WELD;
}

// processKindLabel 工艺种类给人看的名字：模版在就用它的名称；模版删了的预置种类仍叫原名，别的显示未归类。
export function processKindLabel(templateId: string | undefined, templates: ContentTemplate[] | undefined): string {
  if (!templateId) return "未归类";
  const hit = (templates ?? []).find((t) => t.id === templateId);
  if (hit?.name) return hit.name;
  return SEED_NAMES[templateId] ?? "未归类";
}

// processKindOptions 筛选和新建用的种类清单：当前各份模版，再补上列表里出现过但模版已删的种类。
export function processKindOptions(templates: ContentTemplate[] | undefined, usedIds: (string | undefined)[] = []): { value: string; label: string }[] {
  // 预置的焊接参数、包角排最前，其余按名称；两端列表原始顺序不同，这里统一。
  const rank = (id: string) => (id === SEED_TPL_WELD ? 0 : id === SEED_TPL_CORNER ? 1 : 2);
  const out = (templates ?? [])
    .map((t) => ({ value: t.id, label: t.name || processKindLabel(t.id, templates) }))
    .sort((a, b) => rank(a.value) - rank(b.value) || a.label.localeCompare(b.label, "zh"));
  const seen = new Set(out.map((o) => o.value));
  for (const id of usedIds) {
    if (!id || seen.has(id)) continue;
    seen.add(id);
    out.push({ value: id, label: `${processKindLabel(id, templates)}（模版已删）` });
  }
  return out;
}

// processSchemaOf 取这份模版的对象字段表；模版不在就没有字段表，正文按 JSON 原样编辑。
export function processSchemaOf(templateId: string | undefined, templates: ContentTemplate[] | undefined): ContentSchema | null {
  const hit = (templates ?? []).find((t) => t.id === (templateId || SEED_TPL_WELD));
  return hit ? { root: "object", fields: hit.schema?.fields ?? [] } : null;
}

// stampProcess 把服务端认种类用的键写进正文：包角写 kind，自建种类写 templateId，焊接参数什么都不写。
export function stampProcess(value: unknown, templateId: string): Record<string, unknown> {
  const obj = value && typeof value === "object" && !Array.isArray(value) ? { ...(value as Record<string, unknown>) } : {};
  delete obj[TPL_KEY];
  delete obj.kind;
  if (templateId === SEED_TPL_CORNER) obj.kind = CORNER_KIND;
  else if (templateId !== SEED_TPL_WELD) obj[TPL_KEY] = templateId;
  return obj;
}

// newProcessContent 按选中的种类给出新建工艺的默认正文。
export function newProcessContent(templateId: string, templates: ContentTemplate[] | undefined): string {
  const schema = processSchemaOf(templateId, templates);
  return JSON.stringify(stampProcess(schema ? defaultValue(schema) : {}, templateId));
}

// stampProcessText 提交前把种类标记补回正文文本；表单模式不显示这两个键，JSON 模式也可能被删掉。
export function stampProcessText(content: string, templateId: string): string {
  try {
    return JSON.stringify(stampProcess(JSON.parse(content || "{}"), templateId));
  } catch {
    return content;
  }
}
