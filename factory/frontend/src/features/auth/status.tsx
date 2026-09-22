import { Alert, Tag } from "antd";
import type { SiteFactory } from "./api";

export function factoryClosed(status?: string) {
  return status === "retired" || status === "disabled";
}

export function factoriesBlocked(factories: SiteFactory[], selected?: SiteFactory) {
  if (factoryClosed(selected?.status)) return true;
  return factories.length > 0 && factories.every((f) => factoryClosed(f.status));
}

export function selectableFactories(factories: SiteFactory[]) {
  return factories.filter((f) => f.status !== "retired");
}

// 下拉不展示已注销厂；刚认领时清单可能还没刷出来，沿用会话里的工厂 ID。
export function pickFactoryId(listed: SiteFactory[], sessionId: string) {
  if (listed.some((f) => f.id === sessionId)) return sessionId;
  if (listed.length === 1) return listed[0].id;
  return listed.length === 0 ? sessionId : "";
}

export function factoryOptionLabel(f: SiteFactory) {
  const name = f.name?.trim();
  const code = f.shortCode?.trim();
  const title = name && code ? `${name}（${code}）` : name || (code ? `工厂 ${code}` : "未命名工厂");
  if (f.status === "disabled") return `${title}（已停用）`;
  return title;
}

function closedMessage(status: string | undefined, action: string) {
  if (status === "disabled") return `本厂已被云端停用，暂时不能${action}。`;
  if (status === "retired") return `本厂已注销，不能${action}。`;
  return "";
}

// 停用/注销要写在登录、激活页最上面，不能只藏在下拉里。
export function FactoryClosedNotice({
  factories,
  factoryId,
  action = "登录",
}: {
  factories: SiteFactory[];
  factoryId?: string;
  action?: string;
}) {
  if (factories.length === 0) return null;
  const selected = factories.find((f) => f.id === factoryId) ?? (factories.length === 1 ? factories[0] : undefined);
  const allRetired = factories.every((f) => f.status === "retired");
  const msg =
    allRetired && factories.length > 1
      ? `本机工厂均已由云端注销，不能${action}。需要重新开厂请用建厂码认领。`
      : closedMessage(selected?.status, action);
  if (!msg) return null;
  return <Alert type={selected?.status === "disabled" && !allRetired ? "warning" : "error"} showIcon message={msg} style={{ marginBottom: 16 }} />;
}

export function FactoryStatusTag({ status }: { status?: string }) {
  if (status === "retired") return <Tag color="red">已注销</Tag>;
  if (status === "disabled") return <Tag color="orange">已停用</Tag>;
  return null;
}
