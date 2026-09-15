import { PlusOutlined } from "@ant-design/icons";
import { App, Button, Card, Space, Spin, Typography } from "antd";
import { useRef } from "react";
import { errorMessage } from "@/shared/api/client";
import { PageHeader } from "@/shared/ui/PageHeader";
import { useCreateProjectTemplate, useDeleteProjectTemplate, useProjectTemplates, useUpdateProjectTemplate, type ContentTemplate } from "./api";
import { ITEM_TBAR, SEED_TPL_TBAR, itemFields } from "./projectKinds";
import { ProjectTemplateEditor } from "./ProjectTemplateEditor";
import { canonicalizeSchema, type ContentSchema } from "./schema";

const emptyObject: ContentSchema = { root: "object", fields: [] };

function hasTBar(rows: ContentTemplate[]) {
  return rows.some((r) => r.id === SEED_TPL_TBAR || r.name === "T排对接");
}

export function ProjectTemplatesPage() {
  const { message } = App.useApp();
  const q = useProjectTemplates();
  const create = useCreateProjectTemplate();
  const update = useUpdateProjectTemplate();
  const remove = useDeleteProjectTemplate();
  const createRef = useRef<(() => void) | null>(null);
  const busy = create.isPending || update.isPending || remove.isPending;
  const rows = q.data ?? [];

  const persistNew = async (name: string, schema: ContentSchema) => {
    try {
      await create.mutateAsync({ name, schema: canonicalizeSchema({ root: "object", fields: schema.fields ?? [] }) });
      message.success("已新建");
    } catch (e) {
      message.error(errorMessage(e));
      throw e;
    }
  };

  const persistEdit = async (row: ContentTemplate, name: string, schema: ContentSchema) => {
    try {
      await update.mutateAsync({
        id: row.id,
        expected: row.revision,
        name,
        schema: canonicalizeSchema({ root: "object", fields: schema.fields ?? [] }),
      });
      message.success("已保存");
    } catch (e) {
      await q.refetch();
      message.error(errorMessage(e));
      throw e;
    }
  };

  const persistDelete = async (row: ContentTemplate) => {
    try {
      await remove.mutateAsync(row.id);
      message.success("已删除");
    } catch (e) {
      message.error(errorMessage(e));
      throw e;
    }
  };

  const rebuildTBar = async () => {
    try {
      await create.mutateAsync({
        id: SEED_TPL_TBAR,
        name: "T排对接",
        schema: canonicalizeSchema({ root: "object", fields: itemFields(ITEM_TBAR, false) }),
      });
      message.success("已补建 T排对接");
    } catch (e) {
      message.error(errorMessage(e));
    }
  };

  return (
    <>
      <PageHeader
        title="工程模版"
        description="每份独立：自己的名称、字段表、修订。新建、编辑、删除立刻生效，不改已有工程正文。"
        extra={
          <Space>
            {!q.isLoading && !hasTBar(rows) ? (
              <Button disabled={busy} onClick={() => void rebuildTBar()}>
                补建 T排对接
              </Button>
            ) : null}
            <Button type="primary" icon={<PlusOutlined />} disabled={busy} onClick={() => createRef.current?.()}>
              新建模版
            </Button>
          </Space>
        }
      />
      <Card size="small" styles={{ body: { padding: 12 } }}>
        {q.isLoading ? (
          <Spin />
        ) : q.isError ? (
          <Typography.Text type="danger">{errorMessage(q.error)}</Typography.Text>
        ) : (
          <ProjectTemplateEditor
            rows={rows}
            emptySchema={emptyObject}
            busy={busy}
            createRef={createRef}
            onCreate={persistNew}
            onSave={persistEdit}
            onDelete={persistDelete}
          />
        )}
      </Card>
    </>
  );
}
