import { Button, Result, Space, Typography } from "antd";
import { Link, isRouteErrorResponse, useRouteError } from "react-router";
import { paths } from "@/app/routes";
import { errorMessage } from "@/shared/api/client";

// 路由兜底：页面渲染抛错时显示中文提示，不落到框架默认的英文错误页。
export function RouteErrorPage() {
  const err = useRouteError();
  const detail = isRouteErrorResponse(err) ? `${err.status} ${err.statusText}` : errorMessage(err);
  return (
    <Result
      status="error"
      title="这个页面出错了，没能显示"
      subTitle={
        <Space direction="vertical" size={4}>
          <span>可以先重新加载。如果一直这样，可能是浏览器版本太低，请换用新版 Chrome 或 Edge；还不行就把下面的错误信息告诉管理员。</span>
          <Typography.Text type="secondary">错误信息：{detail}</Typography.Text>
        </Space>
      }
      extra={
        <Space>
          <Button type="primary" onClick={() => window.location.reload()}>
            重新加载
          </Button>
          <Link to={paths.dashboard}>
            <Button>回到概览</Button>
          </Link>
        </Space>
      }
    />
  );
}
