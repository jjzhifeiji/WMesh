import { Outlet } from "react-router";

// 登录区：整屏居中一张卡片。
export function AuthLayout() {
  return (
    <div className="auth-layout">
      <div className="auth-card">
        <Outlet />
      </div>
    </div>
  );
}
