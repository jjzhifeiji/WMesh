import { Button, Collapse, Input, Select, Space } from "antd";
import type { ContentSchema, TemplateField } from "./schema";
import { defaultField, defaultValue, encodeContent, enumOptions, enumValue, kindOf, parseContent, parseText, textOf } from "./schema";

type FieldsProps = {
  schema: ContentSchema;
  value?: unknown;
  onChange?: (v: unknown) => void;
  disabled?: boolean;
};

export function ContentFields({ schema, value, onChange, disabled }: FieldsProps) {
  const current = value === undefined ? defaultValue(schema) : value;
  if (schema.root === "array") {
    return (
      <div className="content-fields-scroll">
        <ArrayFields
          item={schema.item ?? { key: "item", label: "项", type: "object", fields: [] }}
          value={asArr(current)}
          onChange={(v) => onChange?.(v)}
          disabled={disabled}
        />
      </div>
    );
  }
  return (
    <div className="content-fields-scroll">
      <ObjectFields fields={schema.fields ?? []} value={asObj(current)} onChange={(v) => onChange?.(v)} disabled={disabled} />
    </div>
  );
}

type EditorProps = {
  schema: ContentSchema | null;
  value?: string;
  onChange?: (raw: string) => void;
  disabled?: boolean;
};

export function ContentEditor({ schema, value, onChange, disabled }: EditorProps) {
  if (!schema) {
    return <Input.TextArea rows={8} value={value} onChange={(e) => onChange?.(e.target.value)} disabled={disabled} />;
  }
  return (
    <ContentFields
      schema={schema}
      value={parseContent(value, schema)}
      onChange={(v) => onChange?.(encodeContent(v, schema, value ?? ""))}
      disabled={disabled}
    />
  );
}

function ObjectFields({
  fields,
  value,
  onChange,
  disabled,
}: {
  fields: TemplateField[];
  value: Record<string, unknown>;
  onChange: (v: Record<string, unknown>) => void;
  disabled?: boolean;
}) {
  return (
    <div className="content-fields">
      {fields.map((f) => (
        <FieldInput
          key={f.key || f.label}
          field={f}
          value={value[f.key]}
          onChange={(v) => onChange({ ...value, [f.key]: v })}
          disabled={disabled}
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
}: {
  item: TemplateField;
  value: unknown[];
  onChange: (v: unknown[]) => void;
  disabled?: boolean;
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
}: {
  field: TemplateField;
  value: unknown;
  onChange: (v: unknown) => void;
  disabled?: boolean;
  bare?: boolean;
}) {
  const kind = kindOf(field);
  const label = field.unit ? `${field.label}（${field.unit}）` : field.label;
  const control = (() => {
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
            <ObjectFields fields={field.fields ?? []} value={asObj(value)} onChange={onChange} disabled={disabled} />
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

function asObj(v: unknown): Record<string, unknown> {
  return v && typeof v === "object" && !Array.isArray(v) ? (v as Record<string, unknown>) : {};
}

function asArr(v: unknown): unknown[] {
  return Array.isArray(v) ? v : [];
}
