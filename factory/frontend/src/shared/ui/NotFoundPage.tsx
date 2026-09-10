import { Button, Result } from "antd";
import { Link } from "react-router";
import { paths } from "@/app/routes";

export function NotFoundPage() {
  return (
    <Result
      status="404"
      title="没有这个页面"
      extra={
        <Link to={paths.dashboard}>
          <Button type="primary">回到概览</Button>
        </Link>
      }
    />
  );
}
