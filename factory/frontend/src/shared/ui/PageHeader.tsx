import { Space, Typography } from "antd";
import type { ReactNode } from "react";

type Props = {
  title: string;
  description?: ReactNode;
  extra?: ReactNode;
};

// 每个页面顶部统一的标题区：左标题与说明，右操作按钮。
export function PageHeader({ title, description, extra }: Props) {
  return (
    <div className="page-header">
      <div>
        <Typography.Title level={4} style={{ margin: 0 }}>
          {title}
        </Typography.Title>
        {description ? <Typography.Text type="secondary">{description}</Typography.Text> : null}
      </div>
      {extra ? <Space>{extra}</Space> : null}
    </div>
  );
}
