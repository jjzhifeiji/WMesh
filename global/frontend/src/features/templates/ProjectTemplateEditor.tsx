import { App, Button, Form, Input, Modal, Space, Table } from "antd";
import { useCallback, useEffect, useState, type MutableRefObject } from "react";
import type { ContentTemplate } from "./api";
import { SchemaEditor } from "./SchemaEditor";
import type { ContentSchema } from "./schema";

export function ProjectTemplateEditor({
  rows,
  emptySchema,
  busy,
  createRef,
  onCreate,
  onSave,
  onDelete,
}: {
  rows: ContentTemplate[];
  emptySchema: ContentSchema;
  busy?: boolean;
  createRef?: MutableRefObject<(() => void) | null>;
  onCreate: (name: string, schema: ContentSchema) => Promise<void>;
  onSave: (row: ContentTemplate, name: string, schema: ContentSchema) => Promise<void>;
  onDelete: (row: ContentTemplate) => Promise<void>;
}) {
  const { modal } = App.useApp();
  const [editing, setEditing] = useState<ContentTemplate | null>(null);
  const [isNew, setIsNew] = useState(false);

  const openNew = useCallback(() => {
    setIsNew(true);
    setEditing({ id: "", kind: "project", name: "", revision: 0, schema: { ...emptySchema, fields: [] }, digest: "" });
  }, [emptySchema]);

  useEffect(() => {
    if (!createRef) return;
    createRef.current = openNew;
    return () => {
      createRef.current = null;
    };
  }, [createRef, openNew]);

  return (
    <>
      <Table
        size="small"
        rowKey="id"
        pagination={false}
        dataSource={rows}
        locale={{ emptyText: "还没有模版。" }}
        columns={[
          { title: "名称", dataIndex: "name", ellipsis: true },
          { title: "修订", dataIndex: "revision", width: 80 },
          {
            title: "",
            width: 140,
            render: (_, row) => (
              <Space size={4}>
                <Button
                  size="small"
                  disabled={busy}
                  onClick={() => {
                    setIsNew(false);
                    setEditing({ ...row, schema: { root: "object", fields: row.schema?.fields ?? [] } });
                  }}
                >
                  编辑
                </Button>
                <Button
                  size="small"
                  type="text"
                  danger
                  disabled={busy}
                  onClick={() => {
                    modal.confirm({
                      title: `删除「${row.name}」？`,
                      content: "已有工程正文不变。此后新建不能再按这份添加。",
                      onOk: () => onDelete(row),
                    });
                  }}
                >
                  删
                </Button>
              </Space>
            ),
          },
        ]}
      />
      {editing ? (
        <EditModal
          key={isNew ? "new" : editing.id}
          value={editing}
          isNew={isNew}
          busy={busy}
          usedNames={new Set(rows.filter((t) => t.id !== editing.id).map((t) => (t.name ?? "").trim()))}
          onCancel={() => {
            setEditing(null);
            setIsNew(false);
          }}
          onOk={async (name, schema) => {
            if (isNew) await onCreate(name, schema);
            else await onSave(editing, name, schema);
            setEditing(null);
            setIsNew(false);
          }}
        />
      ) : null}
    </>
  );
}

function EditModal({
  value,
  isNew,
  busy,
  usedNames,
  onCancel,
  onOk,
}: {
  value: ContentTemplate;
  isNew: boolean;
  busy?: boolean;
  usedNames: Set<string>;
  onCancel: () => void;
  onOk: (name: string, schema: ContentSchema) => Promise<void>;
}) {
  const [form] = Form.useForm<{ name: string }>();
  const [schema, setSchema] = useState<ContentSchema>({ root: "object", fields: value.schema?.fields ?? [] });

  return (
    <Modal
      title={isNew ? "新建模版" : "编辑模版"}
      open
      onCancel={onCancel}
      destroyOnHidden
      okText="确定"
      width={920}
      styles={{ body: { maxHeight: "70vh", overflow: "auto" } }}
      confirmLoading={busy}
      onOk={() => {
        return form.validateFields().then((v) => onOk(v.name.trim(), schema));
      }}
    >
      <Form form={form} layout="vertical" initialValues={{ name: value.name }}>
        <Form.Item
          name="name"
          label="名称"
          rules={[
            { required: true, message: "请填名称" },
            { max: 80, message: "名称太长" },
            {
              validator: (_, v: string) => {
                const name = (v ?? "").trim();
                if (name && usedNames.has(name)) return Promise.reject(new Error("名称已用"));
                return Promise.resolve();
              },
            },
          ]}
        >
          <Input />
        </Form.Item>
      </Form>
      <SchemaEditor value={schema} onChange={setSchema} allowProcess />
    </Modal>
  );
}
