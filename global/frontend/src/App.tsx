import { useEffect, useState } from "react";
import {
  api,
  hasToken,
  setToken,
  type CreatedFactory,
  type Directory,
} from "./api";

export function App() {
  const [authed, setAuthed] = useState(hasToken());
  const [error, setError] = useState("");
  const [dir, setDir] = useState<Directory | null>(null);
  const [created, setCreated] = useState<CreatedFactory | null>(null);

  async function reload() {
    const data = await api<Directory>("/v1/directory");
    setDir(data);
  }

  useEffect(() => {
    if (!authed) return;
    reload().catch((e: Error) => {
      setError(e.message);
      setToken(null);
      setAuthed(false);
    });
  }, [authed]);

  if (!authed) {
    return (
      <div className="page">
        <h1>云端管理端</h1>
        <p className="hint">只登录唯一 WAN 管理员，不能代建厂内人员。</p>
        <div className="card">
          <form
            onSubmit={async (e) => {
              e.preventDefault();
              setError("");
              const fd = new FormData(e.currentTarget);
              try {
                const out = await api<{ token: string }>("/v1/login", {
                  method: "POST",
                  body: JSON.stringify({
                    loginName: String(fd.get("loginName") ?? ""),
                    password: String(fd.get("password") ?? ""),
                  }),
                });
                setToken(out.token);
                setAuthed(true);
              } catch (err) {
                setError((err as Error).message);
              }
            }}
          >
            <label>
              登录名
              <input name="loginName" defaultValue="wan" autoComplete="username" required />
            </label>
            <label>
              口令
              <input name="password" type="password" autoComplete="current-password" required />
            </label>
            <button type="submit">登录</button>
            {error ? <div className="error">{error}</div> : null}
          </form>
        </div>
      </div>
    );
  }

  return (
    <div className="page">
      <div className="row">
        <h1>云端管理端</h1>
        <button
          className="secondary"
          onClick={async () => {
            try {
              await api("/v1/logout", { method: "POST" });
            } catch {
              /* 会话没了也清本地 */
            }
            setToken(null);
            setAuthed(false);
            setDir(null);
            setCreated(null);
          }}
        >
          退出
        </button>
      </div>
      <p className="hint">只建厂并下发一名初始超管；激活口令只出现一次，请立刻抄走。</p>
      {error ? <div className="error">{error}</div> : null}

      <div className="card">
        <h2>创建工厂</h2>
        <form
          onSubmit={async (e) => {
            e.preventDefault();
            setError("");
            const fd = new FormData(e.currentTarget);
            try {
              const out = await api<CreatedFactory>("/v1/factories", {
                method: "POST",
                body: JSON.stringify({
                  name: String(fd.get("name") ?? ""),
                  saLogin: String(fd.get("saLogin") ?? ""),
                  saDisplay: String(fd.get("saDisplay") ?? ""),
                }),
              });
              setCreated(out);
              await reload();
              e.currentTarget.reset();
            } catch (err) {
              setError((err as Error).message);
            }
          }}
        >
          <label>
            工厂名称
            <input name="name" required />
          </label>
          <label>
            初始超管登录名
            <input name="saLogin" required />
          </label>
          <label>
            初始超管显示名
            <input name="saDisplay" required />
          </label>
          <button type="submit">创建并下发激活口令</button>
        </form>
        {created ? (
          <div className="secret">
            工厂 ID：{created.factory.id}
            <br />
            超管登录名：{dir?.initials.find((i) => i.factoryId === created.factory.id)?.loginName ?? created.factory.name}
            <br />
            激活口令（只显示一次）：{created.activationToken}
          </div>
        ) : null}
      </div>

      <div className="card">
        <h2>工厂名录</h2>
        <table>
          <thead>
            <tr>
              <th>名称</th>
              <th>工厂 ID</th>
              <th>初始超管登录名</th>
            </tr>
          </thead>
          <tbody>
            {(dir?.factories ?? []).map((f) => (
              <tr key={f.id}>
                <td>{f.name}</td>
                <td>{f.id}</td>
                <td>{dir?.initials.find((i) => i.factoryId === f.id)?.loginName}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
