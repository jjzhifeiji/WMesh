import { HolderOutlined } from "@ant-design/icons";
import { Button, Checkbox, Input, Select, Typography } from "antd";
import { useId, useState, type DragEvent } from "react";
import { enumDefault, enumOptions, kindOf, parseText, textOf, type ContentSchema, type Kind, type TemplateField } from "./schema";

type DragRef = { listId: string; index: number };

let dragging: DragRef | null = null;

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
  const isArray = value.root === "array";
  return (
    <div className="schema-editor">
      <div className="schema-root-bar">
        <code>root</code>
        <Typography.Text type="secondary">{isArray ? "数组" : "对象"}</Typography.Text>
      </div>
      <div className="schema-node">
        <div className="schema-node-slot">{isArray ? "item" : "fields"}</div>
        {isArray ? (
          <div className="schema-list">
            <ColHead />
            {value.item ? <FieldEditor field={value.item} allowKey={false} typeOptions={typeOptions} onChange={(item) => onChange({ ...value, item })} /> : null}
          </div>
        ) : (
          <FieldList fields={value.fields ?? []} typeOptions={typeOptions} onChange={(fields) => onChange({ ...value, fields })} />
        )}
      </div>
    </div>
  );
}

type TypeOption = { value: string; label: string };

function FieldList({
  fields,
  onChange,
  typeOptions,
}: {
  fields: TemplateField[];
  onChange: (f: TemplateField[]) => void;
  typeOptions: TypeOption[];
}) {
  const listId = useId();
  const [over, setOver] = useState<{ at: number; edge: "before" | "after" } | null>(null);
  const [from, setFrom] = useState<number | null>(null);
  const patch = (i: number, n: TemplateField) => {
    const next = fields.slice();
    next[i] = n;
    onChange(next);
  };
  const remove = (i: number) => onChange(fields.filter((_, j) => j !== i));
  const sameList = () => dragging?.listId === listId;
  const clearDrag = () => {
    dragging = null;
    setFrom(null);
    setOver(null);
  };
  const moveTo = (src: number, at: number, edge: "before" | "after") => {
    let dst = edge === "before" ? at : at + 1;
    if (src < dst) dst -= 1;
    if (src === dst) return;
    const next = fields.slice();
    const [item] = next.splice(src, 1);
    next.splice(dst, 0, item);
    onChange(next);
  };
  const markOver = (e: DragEvent<HTMLElement>, i: number, force?: "before" | "after") => {
    if (!sameList()) return;
    e.preventDefault();
    e.stopPropagation();
    e.dataTransfer.dropEffect = "move";
    const edge = force ?? dropEdge(e, e.currentTarget);
    if (over?.at !== i || over.edge !== edge) setOver({ at: i, edge });
  };
  const dropOn = (e: DragEvent<HTMLElement>, i: number, force?: "before" | "after") => {
    e.preventDefault();
    e.stopPropagation();
    const src = sameList() ? dragging?.index : undefined;
    const edge = force ?? over?.edge ?? dropEdge(e, e.currentTarget);
    clearDrag();
    if (src == null) return;
    moveTo(src, i, edge);
  };
  const last = fields.length - 1;
  return (
    <div className={from != null ? "schema-list is-sorting" : "schema-list"}>
      <ColHead />
      {fields.map((f, i) => {
        const line =
          over && from != null && over.at === i && dropDest(from, over.at, over.edge) !== from ? over.edge : null;
        return (
          <div
            key={`${f.key}-${i}`}
            className={["schema-row-wrap", from === i ? "is-dragging" : "", line ? `is-over-${line}` : ""]
              .filter(Boolean)
              .join(" ")}
            onDragOver={(e) => markOver(e, i)}
            onDrop={(e) => dropOn(e, i)}
          >
            {line ? <div className={`schema-drop-line is-${line}`} aria-hidden /> : null}
            <FieldEditor
              field={f}
              typeOptions={typeOptions}
              onChange={(n) => patch(i, n)}
              onRemove={() => remove(i)}
              sortable
              onDragStart={(e) => {
                e.stopPropagation();
                dragging = { listId, index: i };
                e.dataTransfer.effectAllowed = "move";
                e.dataTransfer.setData("text/plain", `${listId}:${i}`);
                setFrom(i);
              }}
              onDragEnd={clearDrag}
            />
          </div>
        );
      })}
      <div
        className="schema-add"
        onDragOver={(e) => {
          if (last < 0) return;
          markOver(e, last, "after");
        }}
        onDrop={(e) => {
          if (last < 0) return;
          dropOn(e, last, "after");
        }}
      >
        <Button size="small" onClick={() => onChange([...fields, { key: nextKey(fields), label: "新字段", type: "string", default: "" }])}>
          添加字段
        </Button>
      </div>
    </div>
  );
}

function dropEdge(e: DragEvent<HTMLElement>, wrap: HTMLElement): "before" | "after" {
  const rect = wrap.getBoundingClientRect();
  return e.clientY < rect.top + rect.height / 2 ? "before" : "after";
}

function dropDest(src: number, at: number, edge: "before" | "after"): number {
  let dst = edge === "before" ? at : at + 1;
  if (src < dst) dst -= 1;
  return dst;
}

function ColHead() {
  return (
    <div className="schema-col-head">
      <span className="sc-drag" />
      <span className="sc-key">JSON 键</span>
      <span className="sc-name">显示名称</span>
      <span className="sc-type">字段类型</span>
      <span className="sc-unit">计量单位</span>
      <span className="sc-def">默认值</span>
      <span className="sc-req">是否必填</span>
      <span className="sc-del" />
    </div>
  );
}

function FieldEditor({
  field,
  onChange,
  onRemove,
  sortable,
  onDragStart,
  onDragEnd,
  allowKey = true,
  typeOptions,
}: {
  field: TemplateField;
  onChange: (f: TemplateField) => void;
  onRemove?: () => void;
  sortable?: boolean;
  onDragStart?: (e: DragEvent<HTMLSpanElement>) => void;
  onDragEnd?: () => void;
  allowKey?: boolean;
  typeOptions: TypeOption[];
}) {
  const kind = kindOf(field);
  const opts = enumOptions(field);
  const branch = kind === "object" || kind === "array";
  const row = (
    <div className={branch ? "schema-item is-branch" : "schema-item"}>
      <div className="sc-drag">
        {sortable ? (
          <span className="sc-drag-handle" role="button" aria-label="拖动排序" title="拖动排序" draggable onDragStart={onDragStart} onDragEnd={onDragEnd}>
            <HolderOutlined />
          </span>
        ) : null}
      </div>
      <div className="sc-key">
        {allowKey ? (
          <Input size="small" title={field.key} value={field.key} onChange={(e) => onChange({ ...field, key: e.target.value })} />
        ) : (
          <span className="sc-key-placeholder">—</span>
        )}
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
  if (!branch) return row;
  return (
    <>
      {row}
      <div className="schema-node">
        {kind === "object" ? (
          <>
            <div className="schema-node-slot">fields</div>
            <FieldList fields={field.fields ?? []} typeOptions={typeOptions} onChange={(fields) => onChange({ ...field, fields })} />
          </>
        ) : null}
        {kind === "array" ? (
          <>
            <div className="schema-node-slot">items</div>
            <div className="schema-list">
              <ColHead />
              <FieldEditor
                field={field.items ?? { key: "item", label: "项", type: "string" }}
                allowKey={false}
                typeOptions={typeOptions}
                onChange={(items) => onChange({ ...field, items })}
              />
            </div>
          </>
        ) : null}
      </div>
    </>
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
