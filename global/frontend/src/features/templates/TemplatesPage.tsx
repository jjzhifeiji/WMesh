import { PlusOutlined } from "@ant-design/icons";
import { App, Button, Card, Space, Spin, Typography } from "antd";
import { useRef } from "react";
import { errorMessage } from "@/shared/api/client";
import { PageHeader } from "@/shared/ui/PageHeader";
import { useCreateProcessTemplate, useDeleteProcessTemplate, useProcessTemplates, useUpdateProcessTemplate, type ContentTemplate } from "./api";
import { PROCESS_SEEDS } from "./processKinds";
import { ProjectTemplateEditor } from "./ProjectTemplateEditor";
import { canonicalizeSchema, type ContentSchema } from "./schema";

const emptyObject: ContentSchema = { root: "object", fields: [] };

// 工艺模版页：每份模版就是一个工艺种类，可新建、编辑、删除；预置的焊接参数和包角删掉后能按原身份补建。
export function TemplatesPage() {
  const { message } = App.useApp();
  const q = useProcessTemplates();
  const create = useCreateProcessTemplate();
  const update = useUpdateProcessTemplate();
  const remove = useDeleteProcessTemplate();
  const createRef = useRef<(() => void) | null>(null);
  const busy = create.isPending || update.isPending || remove.isPending;
  const rows = q.data ?? [];
  const missingSeeds = q.isLoading || q.isError ? [] : PROCESS_SEEDS.filter((s) => !rows.some((r) => r.id === s.id));

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

  // 名称和字段表留空，服务端按预置的补。
  const rebuildSeed = async (seed: { id: string; name: string }) => {
    try {
      await create.mutateAsync({ id: seed.id, name: "" });
      message.success(`已补建 ${seed.name}`);
    } catch (e) {
      message.error(errorMessage(e));
    }
  };

  return (
    <>
      <PageHeader
        title="工艺模版"
        description="每份模版是一个工艺种类：自己的名称、字段表、修订。新建工艺时先选种类再按它的字段填。新建、编辑、删除立刻生效，不改已有工艺正文。"
        extra={
          <Space>
            {missingSeeds.map((seed) => (
              <Button key={seed.id} disabled={busy} onClick={() => void rebuildSeed(seed)}>
                补建 {seed.name}
              </Button>
            ))}
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
            kind="process"
            allowProcess={false}
            deleteHint="已有工艺正文不变，照常能用；此后不能再按这个种类新建，示教器上送的这类工艺也不再套模版。"
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
