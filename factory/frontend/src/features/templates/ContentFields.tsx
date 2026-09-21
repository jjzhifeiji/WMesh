import { Button, Collapse, Input, Segmented, Select, Space, Typography } from "antd";
import { useState } from "react";
import type { ContentSchema, ProjectItemSchema, TemplateField } from "./schema";
import { defaultField, defaultItemFromFields, defaultValue, encodeContent, enumOptions, enumValue, isProcessRef, kindOf, parseContent, parseText, textOf } from "./schema";
import { itemExtra, itemFields, kindLabel, weldKindOfTemplate } from "./projectKinds";
import type { ProjectItemTemplate } from "./projectKinds";

export type ProcessOption = { value: string; label: string; disabled?: boolean }; // 焊道上选工艺

type FieldsProps = {
  schema: ContentSchema;
  value?: unknown;
  onChange?: (v: unknown) => void;
  disabled?: boolean;
  processOptions?: ProcessOption[];
  weldKind?: string;
};

export function ContentFields({ schema, value, onChange, disabled, processOptions, weldKind }: FieldsProps) {
  const current = value === undefined ? defaultValue(schema) : value;
  if (schema.root === "array") {
    if (schema.projectItems || schema.templates || schema.kinds?.length) {
      return (
        <div className="content-fields-scroll">
          <ProjectArrayFields schema={schema} value={asArr(current)} onChange={(v) => onChange?.(v)} disabled={disabled} processOptions={processOptions} weldKind={weldKind} />
        </div>
      );
    }
    return (
      <div className="content-fields-scroll">
        <ArrayFields
          item={schema.item ?? { key: "item", label: "项", type: "object", fields: [] }}
          value={asArr(current)}
          onChange={(v) => onChange?.(v)}
          disabled={disabled}
          processOptions={processOptions}
        />
      </div>
    );
  }
  return (
    <div className="content-fields-scroll">
      <ObjectFields fields={schema.fields ?? []} value={asObj(current)} onChange={(v) => onChange?.(v)} disabled={disabled} processOptions={processOptions} />
    </div>
  );
}

type EditorProps = {
  schema: ContentSchema | null;
  value?: string;
  onChange?: (raw: string) => void;
  disabled?: boolean;
  processOptions?: ProcessOption[];
  weldKind?: string; // 只展示同作业类型的工程模版
};

function prettyJSON(raw: string | undefined): string {
  if (!raw) return "";
  try {
    return JSON.stringify(JSON.parse(raw), null, 4);
  } catch {
    return raw;
  }
}

export function ContentEditor({ schema, value, onChange, disabled, processOptions, weldKind }: EditorProps) {
  const [mode, setMode] = useState<"form" | "json">("form");
  const [jsonText, setJsonText] = useState("");
  const [jsonErr, setJsonErr] = useState("");

  if (!schema) {
    return <Input.TextArea rows={8} value={value} onChange={(e) => onChange?.(e.target.value)} disabled={disabled} />;
  }

  const toJSON = () => {
    setJsonText(prettyJSON(value));
    setJsonErr("");
    setMode("json");
  };

  const toForm = () => {
    try {
      JSON.parse(jsonText || "null");
      onChange?.(encodeContent(parseContent(jsonText, schema), schema, jsonText));
      setJsonErr("");
      setMode("form");
    } catch (e) {
      setJsonErr(e instanceof Error ? e.message : "不是合法 JSON");
    }
  };

  return (
    <div>
      <Segmented
        size="small"
        value={mode}
        style={{ marginBottom: 8 }}
        onChange={(v) => (v === "json" ? toJSON() : toForm())}
        options={[
          { label: "表单", value: "form" },
          { label: "JSON", value: "json" },
        ]}
      />
      {mode === "form" ? (
        <ContentFields
          schema={schema}
          value={parseContent(value, schema)}
          onChange={(v) => onChange?.(encodeContent(v, schema, value ?? ""))}
          disabled={disabled}
          processOptions={processOptions}
          weldKind={weldKind}
        />
      ) : (
        <>
          {jsonErr ? (
            <Typography.Text type="danger" style={{ display: "block", marginBottom: 8 }}>
              {jsonErr}
            </Typography.Text>
          ) : null}
          <Input.TextArea
            className="schema-json"
            rows={16}
            value={jsonText}
            disabled={disabled}
            onChange={(e) => {
              const text = e.target.value;
              setJsonText(text);
              try {
                JSON.parse(text || "null");
                setJsonErr("");
                onChange?.(encodeContent(parseContent(text, schema), schema, text));
              } catch (err) {
                setJsonErr(err instanceof Error ? err.message : "不是合法 JSON");
                onChange?.(text);
              }
            }}
          />
        </>
      )}
    </div>
  );
}

function ProjectArrayFields({
  schema,
  value,
  onChange,
  disabled,
  processOptions,
  weldKind,
}: {
  schema: ContentSchema;
  value: unknown[];
  onChange: (v: unknown[]) => void;
  disabled?: boolean;
  processOptions?: ProcessOption[];
  weldKind?: string;
}) {
  const items: ProjectItemSchema[] =
    schema.projectItems ??
    (schema.templates ?? []).map((t: ProjectItemTemplate) => ({
      id: t.id,
      name: t.name,
      fields: itemFields(t.kind, itemExtra(t)),
    }));
  const byId = new Map(items.map((t) => [t.id, t]));
  const union: ProjectItemSchema["fields"] = [];
  const seen = new Set<string>();
  for (const t of items) {
    for (const f of t.fields) {
      if (seen.has(f.key)) continue;
      seen.add(f.key);
      union.push(f);
    }
  }
  const addable = weldKind
    ? items.filter((t) => {
        const k = weldKindOfTemplate(t.id);
        return !k || k === weldKind;
      })
    : items;
  const add = (t: ProjectItemSchema) => onChange([...value, defaultItemFromFields(t.fields, t.id)]);
  return (
    <div>
      {value.length === 0 && !disabled ? (
        <Typography.Text type="secondary" style={{ display: "block", marginBottom: 8 }}>
          还没有焊缝。从下面按模版添加。
        </Typography.Text>
      ) : null}
      <Collapse
        size="small"
        items={value.map((el, i) => {
          const obj = asObj(el);
          const tpl = typeof obj.templateId === "string" ? byId.get(obj.templateId) : undefined;
          const kind = typeof obj.kind === "string" ? obj.kind : "";
          const name = typeof obj.name === "string" && obj.name ? obj.name : "";
          const rowKey = typeof obj.id === "string" && obj.id ? obj.id : `row-${i}`;
          const title = tpl?.name ?? (kind ? kindLabel(kind) : "焊缝");
          const fields = tpl?.fields ?? union;
          return {
            key: rowKey,
            label: `${title}${name ? ` · ${name}` : ""}`,
            extra: disabled ? null : (
              <Button
                size="small"
                type="text"
                danger
                onClick={(e) => {
                  e.stopPropagation();
                  onChange(value.filter((_, j) => j !== i));
                }}
              >
                删
              </Button>
            ),
            children: (
              <ObjectFields
                fields={fields}
                value={obj}
                onChange={(v) => {
                  const next = value.slice();
                  next[i] = { ...v, templateId: tpl?.id ?? obj.templateId };
                  onChange(next);
                }}
                disabled={disabled}
                processOptions={processOptions}
              />
            ),
          };
        })}
      />
      {disabled ? null : (
        <Space size={4} wrap style={{ marginTop: 4 }}>
          {addable.map((t) => (
            <Button key={t.id} size="small" onClick={() => add(t)}>
              添加{t.name}
            </Button>
          ))}
        </Space>
      )}
    </div>
  );
}

function ObjectFields({
  fields,
  value,
  onChange,
  disabled,
  processOptions,
}: {
  fields: TemplateField[];
  value: Record<string, unknown>;
  onChange: (v: Record<string, unknown>) => void;
  disabled?: boolean;
  processOptions?: ProcessOption[];
}) {
  return (
    <div className="content-fields">
      {fields.filter((f) => !isIdentityField(f)).map((f) => (
        <FieldInput
          key={f.key || f.label}
          field={f}
          value={value[f.key]}
          onChange={(v) => onChange({ ...value, [f.key]: v })}
          disabled={disabled}
          processOptions={processOptions}
        />
      ))}
    </div>
  );
}

function ArrayFields({
  item,
  value,
  onChange,
  disabled,
  processOptions,
}: {
  item: TemplateField;
  value: unknown[];
  onChange: (v: unknown[]) => void;
  disabled?: boolean;
  processOptions?: ProcessOption[];
}) {
  const primitive = item.type !== "object" && item.type !== "array";
  if (primitive) {
    return (
      <Space size={4} wrap>
        {value.map((el, i) => (
          <Space key={i} size={4} align="center">
            <FieldInput
              field={item}
              value={el}
              onChange={(v) => {
                const next = value.slice();
                next[i] = v;
                onChange(next);
              }}
              disabled={disabled}
              processOptions={processOptions}
              bare
            />
            {disabled ? null : (
              <Button size="small" type="text" danger onClick={() => onChange(value.filter((_, j) => j !== i))}>
                删
              </Button>
            )}
          </Space>
        ))}
        {disabled ? null : (
          <Button size="small" onClick={() => onChange([...value, defaultField(item)])}>
            添加{item.label}
          </Button>
        )}
      </Space>
    );
  }
  return (
    <div>
      <Collapse
        size="small"
        items={value.map((el, i) => ({
          key: String(i),
          label: `${item.label} ${i + 1}`,
          extra: disabled ? null : (
            <Button
              size="small"
              type="text"
              danger
              onClick={(e) => {
                e.stopPropagation();
                onChange(value.filter((_, j) => j !== i));
              }}
            >
              删
            </Button>
          ),
          children: (
            <FieldInput
              field={item}
              value={el}
              onChange={(v) => {
                const next = value.slice();
                next[i] = v;
                onChange(next);
              }}
              disabled={disabled}
              processOptions={processOptions}
              bare
            />
          ),
        }))}
      />
      {disabled ? null : (
        <Button size="small" style={{ marginTop: 4 }} onClick={() => onChange([...value, defaultField(item)])}>
          添加{item.label}
        </Button>
      )}
    </div>
  );
}

function FieldInput({
  field,
  value,
  onChange,
  disabled,
  bare,
  processOptions,
}: {
  field: TemplateField;
  value: unknown;
  onChange: (v: unknown) => void;
  disabled?: boolean;
  bare?: boolean;
  processOptions?: ProcessOption[];
}) {
  const kind = kindOf(field);
  const label = field.unit ? `${field.label}（${field.unit}）` : field.label;
  const control = (() => {
    if (isProcessRef(field)) {
      const cur = typeof value === "string" && value ? value : undefined;
      return (
        <Select
          size="small"
          allowClear
          showSearch
          optionFilterProp="label"
          value={cur}
          options={processOptions ?? []}
          onChange={(v: string | undefined) => onChange(v ?? "")}
          disabled={disabled}
          placeholder="未选工艺"
          className="content-ctrl-process"
          popupMatchSelectWidth={false}
          listHeight={360}
          virtual={false}
          getPopupContainer={() => document.body}
        />
      );
    }
    switch (kind) {
      case "enum": {
        const opts = enumOptions(field);
        return (
          <Select
            size="small"
            value={enumValue(field, value)}
            options={opts.map((o) => ({ value: o, label: o }))}
            onChange={(v: string) => onChange(field.type === "bool" ? v === "是" : v)}
            disabled={disabled}
            allowClear={!field.required}
            className="content-ctrl-text"
          />
        );
      }
      case "string":
        return (
          <Input
            size="small"
            className="content-ctrl-text"
            value={textOf(value)}
            onChange={(e) => onChange(parseText(e.target.value))}
            disabled={disabled}
          />
        );
      case "object":
        return (
          <div className="content-field-group">
            {bare ? null : <div className="content-field-group-title">{label}</div>}
            <ObjectFields fields={field.fields ?? []} value={asObj(value)} onChange={onChange} disabled={disabled} processOptions={processOptions} />
          </div>
        );
      case "array":
        return (
          <div className="content-field-group">
            {bare ? null : <div className="content-field-group-title">{label}</div>}
            <ArrayFields
              item={field.items ?? { key: "item", label: "项", type: "string" }}
              value={asArr(value)}
              onChange={onChange}
              disabled={disabled}
              processOptions={processOptions}
            />
          </div>
        );
      default:
        return null;
    }
  })();
  if (bare || kind === "object" || kind === "array") return control;
  return (
    <div className="content-field">
      <span className="content-field-label">
        {field.required ? <span style={{ color: "#ff4d4f" }}>* </span> : null}
        {label}
      </span>
      {control}
    </div>
  );
}

// 焊道/点/路径自己的身份，添加时已生成，不用填。
function isIdentityField(field: TemplateField): boolean {
  return (field.key === "id" || field.key === "kind" || field.key === "templateId") && kindOf(field) === "string";
}

function asObj(v: unknown): Record<string, unknown> {
  return v && typeof v === "object" && !Array.isArray(v) ? (v as Record<string, unknown>) : {};
}

function asArr(v: unknown): unknown[] {
  return Array.isArray(v) ? v : [];
}
