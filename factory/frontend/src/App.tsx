import { useEffect, useState } from "react";
import {
  api,
  basePath,
  factoryId,
  hasToken,
  setFactoryId,
  setToken,
  type Catalog,
} from "./api";

const roles = [
  ["factory_super_admin", "工厂超管"],
  ["org_admin", "组织管理员"],
  ["org_lead", "组织负责人"],
  ["process_engineer", "工艺工程师"],
  ["operator", "操作员"],
  ["auditor", "审计员"],
] as const;

export function App() {
  const [fid, setFid] = useState(factoryId());
  const [authed, setAuthed] = useState(hasToken() && Boolean(factoryId()));
  const [tab, setTab] = useState<"login" | "activate">("login");
  const [error, setError] = useState("");
  const [catalog, setCatalog] = useState<Catalog | null>(null);
  const [once, setOnce] = useState("");

  async function reload() {
    const data = await api<Catalog>(`${basePath()}/catalog`);
    setCatalog(data);
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
        <h1>厂内管理端</h1>
        <p className="hint">先填工厂 ID（云端建厂时发给你的），再激活或登录。WAN 不能代你设日常口令。</p>
        <div className="card">
          <label>
            工厂 ID
            <input
              value={fid}
              onChange={(e) => {
                setFid(e.target.value.trim());
                setFactoryId(e.target.value.trim());
              }}
              required
            />
          </label>
          <div className="row tabs">
            <button className={tab === "login" ? "on" : ""} type="button" onClick={() => setTab("login")}>
              登录
            </button>
            <button className={tab === "activate" ? "on" : ""} type="button" onClick={() => setTab("activate")}>
              激活
            </button>
          </div>
          {tab === "login" ? (
            <form
              onSubmit={async (e) => {
                e.preventDefault();
                setError("");
                const fd = new FormData(e.currentTarget);
                try {
                  const out = await api<{ token: string }>(`${basePath()}/login`, {
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
                <input name="loginName" autoComplete="username" required />
              </label>
              <label>
                口令
                <input name="password" type="password" autoComplete="current-password" required />
              </label>
              <button type="submit">登录</button>
            </form>
          ) : (
            <form
              onSubmit={async (e) => {
                e.preventDefault();
                setError("");
                const fd = new FormData(e.currentTarget);
                try {
                  await api(`${basePath()}/activate`, {
                    method: "POST",
                    body: JSON.stringify({
                      loginName: String(fd.get("loginName") ?? ""),
                      activationToken: String(fd.get("activationToken") ?? ""),
                      password: String(fd.get("password") ?? ""),
                    }),
                  });
                  setTab("login");
                  setError("");
                  setOnce("已激活，请用刚设的口令登录。");
                } catch (err) {
                  setError((err as Error).message);
                }
              }}
            >
              <label>
                登录名
                <input name="loginName" required />
              </label>
              <label>
                激活口令
                <input name="activationToken" required />
              </label>
              <label>
                自设日常口令
                <input name="password" type="password" required />
              </label>
              <button type="submit">激活</button>
            </form>
          )}
          {once ? <div className="ok">{once}</div> : null}
          {error ? <div className="error">{error}</div> : null}
        </div>
      </div>
    );
  }

  const isSA = (catalog?.people.length ?? 0) > 0;

  return (
    <div className="page">
      <div className="row">
        <h1>厂内管理端</h1>
        <span>{catalog?.me.displayName}（{catalog?.me.loginName}）</span>
        <button
          className="secondary"
          onClick={async () => {
            try {
              await api(`${basePath()}/logout`, { method: "POST" });
            } catch {
              /* 会话没了也清本地 */
            }
            setToken(null);
            setAuthed(false);
            setCatalog(null);
          }}
        >
          退出
        </button>
      </div>
      {error ? <div className="error">{error}</div> : null}
      {once ? <div className="secret">{once}</div> : null}

      {!isSA ? (
        <div className="card">
          <p className="hint">当前账号不是工厂超管，只能看到自己。</p>
        </div>
      ) : (
        <>
          <div className="card">
            <h2>组织类型</h2>
            <form
              onSubmit={async (e) => {
                e.preventDefault();
                const fd = new FormData(e.currentTarget);
                try {
                  await api(`${basePath()}/org-types`, {
                    method: "POST",
                    body: JSON.stringify({ name: String(fd.get("name") ?? "") }),
                  });
                  e.currentTarget.reset();
                  await reload();
                } catch (err) {
                  setError((err as Error).message);
                }
              }}
            >
              <label>
                名称
                <input name="name" required />
              </label>
              <button type="submit">新建类型</button>
            </form>
            <table>
              <thead>
                <tr>
                  <th>名称</th>
                  <th>状态</th>
                </tr>
              </thead>
              <tbody>
                {catalog?.orgTypes.map((t) => (
                  <tr key={t.id}>
                    <td>{t.name}</td>
                    <td>{t.status}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          <div className="card">
            <h2>组织节点</h2>
            <form
              onSubmit={async (e) => {
                e.preventDefault();
                const fd = new FormData(e.currentTarget);
                const parentId = String(fd.get("parentId") ?? "");
                try {
                  await api(`${basePath()}/org-units`, {
                    method: "POST",
                    body: JSON.stringify({
                      typeId: String(fd.get("typeId") ?? ""),
                      name: String(fd.get("name") ?? ""),
                      parentId: parentId || null,
                    }),
                  });
                  e.currentTarget.reset();
                  await reload();
                } catch (err) {
                  setError((err as Error).message);
                }
              }}
            >
              <label>
                类型
                <select name="typeId" required>
                  {(catalog?.orgTypes ?? []).map((t) => (
                    <option key={t.id} value={t.id}>
                      {t.name}
                    </option>
                  ))}
                </select>
              </label>
              <label>
                父节点（空=直挂工厂）
                <select name="parentId" defaultValue="">
                  <option value="">直挂工厂</option>
                  {(catalog?.orgUnits ?? []).map((u) => (
                    <option key={u.id} value={u.id}>
                      {u.name}
                    </option>
                  ))}
                </select>
              </label>
              <label>
                名称
                <input name="name" required />
              </label>
              <button type="submit">新建节点</button>
            </form>
            <table>
              <thead>
                <tr>
                  <th>名称</th>
                  <th>状态</th>
                </tr>
              </thead>
              <tbody>
                {catalog?.orgUnits.map((u) => (
                  <tr key={u.id}>
                    <td>{u.name}</td>
                    <td>{u.status}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          <div className="card">
            <h2>人员</h2>
            <form
              onSubmit={async (e) => {
                e.preventDefault();
                const fd = new FormData(e.currentTarget);
                try {
                  const out = await api<{ account: { id: string }; activationToken: string }>(
                    `${basePath()}/people`,
                    {
                      method: "POST",
                      body: JSON.stringify({
                        loginName: String(fd.get("loginName") ?? ""),
                        displayName: String(fd.get("displayName") ?? ""),
                      }),
                    },
                  );
                  setOnce(`新人口令（只显示一次）：${out.activationToken}`);
                  e.currentTarget.reset();
                  await reload();
                } catch (err) {
                  setError((err as Error).message);
                }
              }}
            >
              <label>
                登录名
                <input name="loginName" required />
              </label>
              <label>
                显示名
                <input name="displayName" required />
              </label>
              <button type="submit">创建待启用账号</button>
            </form>
            <table>
              <thead>
                <tr>
                  <th>登录名</th>
                  <th>显示名</th>
                  <th>状态</th>
                  <th></th>
                </tr>
              </thead>
              <tbody>
                {catalog?.people.map((p) => (
                  <tr key={p.id}>
                    <td>{p.loginName}</td>
                    <td>{p.displayName}</td>
                    <td>{p.status}</td>
                    <td>
                      {p.id !== catalog?.me.id ? (
                        <button
                          className="danger"
                          type="button"
                          onClick={async () => {
                            try {
                              await api(`${basePath()}/people/${p.id}/disable`, { method: "POST" });
                              await reload();
                            } catch (err) {
                              setError((err as Error).message);
                            }
                          }}
                        >
                          停用
                        </button>
                      ) : null}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          <div className="card">
            <h2>授角色</h2>
            <form
              onSubmit={async (e) => {
                e.preventDefault();
                const fd = new FormData(e.currentTarget);
                const orgUnitId = String(fd.get("orgUnitId") ?? "");
                const scopeKind = String(fd.get("scopeKind") ?? "");
                try {
                  await api(`${basePath()}/grants`, {
                    method: "POST",
                    body: JSON.stringify({
                      personId: String(fd.get("personId") ?? ""),
                      role: String(fd.get("role") ?? ""),
                      scopeKind,
                      orgUnitId: scopeKind === "factory" ? null : orgUnitId || null,
                    }),
                  });
                  e.currentTarget.reset();
                  await reload();
                } catch (err) {
                  setError((err as Error).message);
                }
              }}
            >
              <label>
                人员
                <select name="personId" required>
                  {(catalog?.people ?? []).map((p) => (
                    <option key={p.id} value={p.id}>
                      {p.displayName}（{p.loginName}）
                    </option>
                  ))}
                </select>
              </label>
              <label>
                角色
                <select name="role" required>
                  {roles.map(([id, label]) => (
                    <option key={id} value={id}>
                      {label}
                    </option>
                  ))}
                </select>
              </label>
              <label>
                作用域
                <select name="scopeKind" required defaultValue="factory">
                  <option value="factory">整厂</option>
                  <option value="org_unit">组织节点</option>
                </select>
              </label>
              <label>
                节点（节点作用域必填）
                <select name="orgUnitId" defaultValue="">
                  <option value="">无</option>
                  {(catalog?.orgUnits ?? []).map((u) => (
                    <option key={u.id} value={u.id}>
                      {u.name}
                    </option>
                  ))}
                </select>
              </label>
              <button type="submit">授予</button>
            </form>
            <table>
              <thead>
                <tr>
                  <th>人员</th>
                  <th>角色</th>
                  <th>作用域</th>
                </tr>
              </thead>
              <tbody>
                {catalog?.roleGrants.map((g) => (
                  <tr key={g.id}>
                    <td>{catalog?.people.find((p) => p.id === g.personId)?.loginName}</td>
                    <td>{g.role}</td>
                    <td>{g.scopeKind}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          <div className="card">
            <h2>分配到组织</h2>
            <form
              onSubmit={async (e) => {
                e.preventDefault();
                const fd = new FormData(e.currentTarget);
                try {
                  await api(`${basePath()}/assignments`, {
                    method: "POST",
                    body: JSON.stringify({
                      personId: String(fd.get("personId") ?? ""),
                      orgUnitId: String(fd.get("orgUnitId") ?? ""),
                    }),
                  });
                  e.currentTarget.reset();
                  await reload();
                } catch (err) {
                  setError((err as Error).message);
                }
              }}
            >
              <label>
                人员
                <select name="personId" required>
                  {(catalog?.people ?? []).map((p) => (
                    <option key={p.id} value={p.id}>
                      {p.loginName}
                    </option>
                  ))}
                </select>
              </label>
              <label>
                节点
                <select name="orgUnitId" required>
                  {(catalog?.orgUnits ?? []).map((u) => (
                    <option key={u.id} value={u.id}>
                      {u.name}
                    </option>
                  ))}
                </select>
              </label>
              <button type="submit">分配</button>
            </form>
          </div>
        </>
      )}
    </div>
  );
}
