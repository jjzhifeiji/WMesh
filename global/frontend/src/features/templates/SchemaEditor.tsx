import { Button, Checkbox, Input, Select, Typography } from "antd";
import { enumDefault, enumOptions, kindOf, parseText, textOf, type ContentSchema, type Kind, type TemplateField } from "./schema";

const typeOptionsBase = [
  { value: "string", label: "文本" },
  { value: "enum", label: "枚举" },
  { value: "object", label: "对象" },
  { value: "array", label: "数组" },
];

const typeOptionsWithProcess = [
  { value: "string", label: "文本" },
  { value: "process", label: "工艺" },
  { value: "enum", label: "枚举" },
  { value: "object", label: "对象" },
  { value: "array", label: "数组" },
];

export function SchemaEditor({ value, onChange, allowProcess }: { value: ContentSchema; onChange: (s: ContentSchema) => void; allowProcess?: boolean }) {
  const typeOptions = allowProcess ? typeOptionsWithProcess : typeOptionsBase;
  if (value.root === "array") {
    return (
      <div className="schema-editor">
        <Typography.Text type="secondary" style={{ fontSize: 12, display: "block", marginBottom: 6 }}>
          根是数组，下面是每个元素。
        </Typography.Text>
        {value.item ? <FieldEditor field={value.item} allowKey={false} typeOptions={typeOptions} onChange={(item) => onChange({ ...value, item })} /> : null}
      </div>
    );
  }
  return (
    <div className="schema-editor">
      <FieldList fields={value.fields ?? []} typeOptions={typeOptions} onChange={(fields) => onChange({ ...value, fields })} headed />
    </div>
  );
}

type TypeOption = { value: string; label: string };

function FieldList({
  fields,
  onChange,
  headed,
  typeOptions,
}: {
  fields: TemplateField[];
  onChange: (f: TemplateField[]) => void;
  headed?: boolean;
  typeOptions: TypeOption[];
}) {
  const compact: { f: TemplateField; i: number }[] = [];
  const wide: { f: TemplateField; i: number }[] = [];
  fields.forEach((f, i) => {
    const k = kindOf(f);
    if (k === "object" || k === "array") wide.push({ f, i });
    else compact.push({ f, i });
  });
  const mid = Math.ceil(compact.length / 2);
  const cols = [compact.slice(0, mid), compact.slice(mid)];
  const patch = (i: number, n: TemplateField) => {
    const next = fields.slice();
    next[i] = n;
    onChange(next);
  };
  const remove = (i: number) => onChange(fields.filter((_, j) => j !== i));
  return (
    <>
      <div className="schema-list">
        {cols.map((col, ci) =>
          col.length === 0 ? null : (
            <div className="schema-col" key={ci}>
              {headed ? <ColHead /> : null}
              {col.map(({ f, i }) => (
                <FieldEditor key={`${f.key}-${i}`} field={f} typeOptions={typeOptions} onChange={(n) => patch(i, n)} onRemove={() => remove(i)} />
              ))}
            </div>
          ),
        )}
      </div>
      {wide.map(({ f, i }) => (
        <FieldEditor key={`${f.key}-${i}`} field={f} typeOptions={typeOptions} onChange={(n) => patch(i, n)} onRemove={() => remove(i)} />
      ))}
      <Button size="small" style={{ marginTop: 8 }} onClick={() => onChange([...fields, { key: nextKey(fields), label: "新字段", type: "string", default: "" }])}>
        添加字段
      </Button>
    </>
  );
}

function ColHead() {
  return (
    <div className="schema-col-head">
      <span className="sc-key">键</span>
      <span className="sc-name">名称</span>
      <span className="sc-type">类型</span>
      <span className="sc-unit">单位</span>
      <span className="sc-def">默认</span>
      <span className="sc-req">必</span>
      <span className="sc-del" />
    </div>
  );
}

function FieldEditor({
  field,
  onChange,
  onRemove,
  allowKey = true,
  typeOptions,
}: {
  field: TemplateField;
  onChange: (f: TemplateField) => void;
  onRemove?: () => void;
  allowKey?: boolean;
  typeOptions: TypeOption[];
}) {
  const kind = kindOf(field);
  const opts = enumOptions(field);
  const row = (
    <div className="schema-item">
      <div className="sc-key">
        {allowKey ? (
          <Input size="small" title={field.key} value={field.key} onChange={(e) => onChange({ ...field, key: e.target.value })} />
        ) : null}
      </div>
      <div className="sc-name">
        <Input size="small" value={field.label} onChange={(e) => onChange({ ...field, label: e.target.value })} />
      </div>
      <div className="sc-type">
        <Select size="small" value={kind} options={typeOptions} popupMatchSelectWidth={false} onChange={(type: Kind) => onChange(withKind(field, type))} />
      </div>
      <div className="sc-unit">
        {kind === "string" ? <Input size="small" value={field.unit ?? ""} onChange={(e) => onChange({ ...field, unit: e.target.value })} /> : null}
      </div>
      <div className="sc-def">
        <DefaultCell field={field} onChange={onChange} />
      </div>
      <div className="sc-req">
        <Checkbox checked={Boolean(field.required)} onChange={(e) => onChange({ ...field, required: e.target.checked })} />
      </div>
      <div className="sc-del">
        {onRemove ? (
          <Button size="small" type="text" danger onClick={onRemove}>
            删
          </Button>
        ) : null}
      </div>
      {kind === "enum" ? (
        <div className="schema-enum-options">
          <span>选项</span>
          <Select
            size="small"
            mode="tags"
            value={opts}
            tokenSeparators={[","]}
            placeholder="输入后回车"
            onChange={(options: string[]) => onChange(withEnumOptions(field, options))}
          />
        </div>
      ) : null}
    </div>
  );
  if (kind !== "object" && kind !== "array") return row;
  return (
    <div className="schema-item-wide">
      {row}
      {kind === "object" ? (
        <div className="schema-item-nested">
          <FieldList fields={field.fields ?? []} typeOptions={typeOptions} onChange={(fields) => onChange({ ...field, fields })} headed />
        </div>
      ) : null}
      {kind === "array" && field.items ? (
        <div className="schema-item-nested">
          <FieldEditor field={field.items} allowKey={false} typeOptions={typeOptions} onChange={(items) => onChange({ ...field, items })} />
        </div>
      ) : null}
    </div>
  );
}

function DefaultCell({ field, onChange }: { field: TemplateField; onChange: (f: TemplateField) => void }) {
  if (kindOf(field) === "enum") {
    const opts = enumOptions(field);
    return (
      <Select
        size="small"
        value={enumDefault(field)}
        options={opts.map((o) => ({ value: o, label: o }))}
        popupMatchSelectWidth={false}
        onChange={(v: string) => onChange(withEnumOptions(field, opts, v))}
      />
    );
  }
  if (kindOf(field) === "string") {
    return (
      <Input
        size="small"
        value={textOf(field.default)}
        onChange={(e) => onChange({ ...field, default: parseText(e.target.value) })}
      />
    );
  }
  return null;
}

function withEnumOptions(field: TemplateField, options: string[], picked?: string): TemplateField {
  const next: TemplateField = { ...field, type: "string", options };
  delete next.fields;
  delete next.items;
  delete next.unit;
  const def = picked ?? enumDefault(next);
  next.default = options.includes(def) ? def : (options[0] ?? "");
  return next;
}

function withKind(field: TemplateField, kind: Kind): TemplateField {
  if (kind === "object") {
    const next: TemplateField = { ...field, type: "object", fields: field.fields ?? [] };
    delete next.items;
    delete next.default;
    delete next.options;
    delete next.unit;
    return next;
  }
  if (kind === "array") {
    const next: TemplateField = { ...field, type: "array", items: field.items ?? { key: "item", label: "项", type: "string" } };
    delete next.fields;
    delete next.default;
    delete next.options;
    delete next.unit;
    return next;
  }
  if (kind === "enum") {
    const options = enumOptions(field).length ? enumOptions(field) : ["选项1"];
    return withEnumOptions({ ...field, type: "string" }, options);
  }
  if (kind === "process") {
    const next: TemplateField = { ...field, type: "process", default: "" };
    delete next.fields;
    delete next.items;
    delete next.options;
    delete next.unit;
    return next;
  }
  const next: TemplateField = { ...field, type: "string" };
  delete next.fields;
  delete next.items;
  delete next.options;
  if (typeof field.default === "boolean") next.default = "";
  return next;
}

function nextKey(fields: TemplateField[]) {
  const used = new Set(fields.map((f) => f.key));
  let i = fields.length + 1;
  while (used.has(`field_${i}`)) i += 1;
  return `field_${i}`;
}
