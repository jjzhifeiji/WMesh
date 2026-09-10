import { Empty } from "antd";
import { useLocation } from "react-router";
import { placeholderMeta } from "@/app/navigation";
import { PageHeader } from "./PageHeader";

// 后续阶段的空页：先占菜单，真正功能落地时换成对应 features 页。
export function PlaceholderPage() {
  const { pathname } = useLocation();
  const meta = placeholderMeta(pathname);
  return (
    <>
      <PageHeader title={meta?.title ?? "页面"} description={meta?.description} />
      <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="尚未开放，先占菜单位置。" />
    </>
  );
}
