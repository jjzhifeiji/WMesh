import { PageHeader } from "@/shared/ui/PageHeader";
import { useSoftwareReleases } from "./api";
import { DeployPanel } from "./DeployPanel";
import { ReleaseTables } from "./ReleaseTables";

// 上半截按旧文件导入：选目录、预览确认、带进度上传；下半截按种类分列，旧包可清。
export function UpdatesPage() {
  const releases = useSoftwareReleases();
  return (
    <>
      <PageHeader
        title="软件更新"
        description="先选 dist 目录在本机解析预览，确认后再上传。每种只看最新一条，点详情才列出全部版本、镜像和清理。"
      />
      <DeployPanel published={releases.data ?? []} />
      <ReleaseTables rows={releases.data ?? []} loading={releases.isLoading} />
    </>
  );
}
