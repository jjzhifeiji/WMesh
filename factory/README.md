# WMesh Factory（厂内服务）

厂内账号与组织的业务权威。一台厂端服务可承载多家工厂：按工厂稳定身份在同一 PostgreSQL 实例上各建一个库（`wmesh_fac_<id>`），人员、口令哈希、组织树、分配、角色、会话、审计全部只在本厂库里。点云/图片本体落本地对象存储（S3 兼容），不进 WAN。

## 目录

```
factory/
├── Dockerfile            前端构建 → Go 静态编译 → alpine 运行时（单进程：API + 管理端静态页）
├── docker-compose.yml    部署：app + db（PostgreSQL 18）+ oss（RustFS，S3 兼容）
├── Makefile              up / down / upgrade / bundle / test …
├── .env.example          部署配置模板，复制为 .env
├── frontend/             管理后台（见下）
└── server/               Go 1.27 服务
    ├── cmd/server        进程入口：读配置、连维护库、保证 OSS 桶、起 HTTP
    ├── internal/httpapi  JSON HTTP 适配，按 URL 里的工厂身份选库
    ├── internal/hub      按工厂身份建/开厂库并组装应用服务
    ├── internal/service  应用服务：认证、权限、归属、审计
    ├── internal/store    本厂库读写（GORM，仅结构体对齐，禁 AutoMigrate）
    ├── internal/web      托管已构建的管理端静态页
    ├── internal/platform 底座：config / provision / oss / id / secret / audit / domain / migrate / testpg
    ├── migrations/       只向前的版本化 SQL，新厂库首次打开时自动套用
    └── docker-compose.yml  仅 go test 用的一次性测试库（:55433）
```

### 管理后台（frontend/）

React 19 + TypeScript 7 + Vite 8，UI 用 Ant Design 6，路由 React Router，服务端状态 TanStack Query，lint 用 oxlint。按功能分层，和服务端的 `httpapi → service → store` 一样各管一段：

```
src/
├── main.tsx          挂载：Providers + Router
├── app/              应用装配：providers、router（路由表；管理页套 RequireSuperAdmin）、routes（路径常量）、navigation（菜单，按角色裁剪）
├── layouts/          页面骨架：AdminLayout（侧栏 + 顶栏 + 内容区）、AuthLayout（登录/激活居中卡片）
├── shared/           跨功能复用，不含业务
│   ├── api/          client（唯一请求出口：带令牌、翻译错误码、401 清会话）、errors（错误码 → 中文）
│   ├── auth/         session（工厂 ID + 令牌，可订阅；fpath 拼本厂路径）、RequireAuth、RequireSuperAdmin
│   ├── ui/           PageHeader / SecretOnce / IdText / StatusTag / NotFoundPage
│   ├── labels.ts     角色、状态、作用域的中文与允许组合
│   └── format.ts     时间、ID 展示
└── features/         每个业务功能一个目录：api.ts（类型 + 查询/变更 hook）+ 页面 + 弹窗
    ├── auth/         登录、激活
    ├── catalog/      名册读模型（唯一的 GET，所有写操作成功后让它失效重拉）+ 是否超管
    ├── dashboard/    概览（人员/组织/角色规模、我的角色、服务健康）
    ├── org/          组织类型、组织节点（树表 + 上级节点树选）
    ├── people/       人员（新建待启用账号 → 一次性激活口令）
    ├── grants/       角色授予（角色决定可选作用域）
    ├── assignments/  组织分配
    └── account/      我的账号、改口令
```

加一个功能：在 `features/<name>/` 写 `api.ts` 与页面 → `app/router.tsx` 加路由 → `app/navigation.tsx` 加菜单（需要超管就标 `superAdminOnly`）。页面不直接 `fetch`，一律经 `shared/api/client`；改名册的写操作用 `useCatalogMutation`。

## 运行

```bash
make up        # 首次自动生成 .env，请改口令；构建镜像并等到 app/db/oss 全部健康
make logs      # 跟随日志（JSON）
make upgrade   # 拉新基础镜像、重建应用、替换容器；各厂库迁移在首次访问时自动向前
make bundle    # 导出离线安装包 dist/*.tar（app + postgres + rustfs），厂内无外网时 docker load -i 后 make up
make down      # 停容器，保留数据卷
```

关键配置（`.env`）：

| 变量 | 作用 |
| --- | --- |
| `POSTGRES_PASSWORD` | 维护库口令；账号需能建库，各厂库由服务自动创建 |
| `WMESH_BOOTSTRAP_TOKEN` | 建厂引导共享口令，必须与 WAN 端一致 |
| `OSS_ACCESS_KEY` / `OSS_SECRET_KEY` / `OSS_BUCKET` | 对象存储密钥与默认桶；桶由 app 启动时自动创建 |
| `OSS_PORT` / `OSS_CONSOLE_PORT` | S3 端点（默认 9002，Client 上传点云/图片用）与 RustFS 管理台（默认 9003，用同一对密钥登录） |
| `WMESH_REGISTRY` / `WMESH_VERSION` | 镜像前缀与标签，供 `make push` |

服务进程直接读 `WMESH_*` 环境变量（见 `server/internal/platform/config`）；`/healthz` 报告版本、库与 OSS 状态，OSS 掉线不影响账号管理。

## 开发

```bash
make test        # 起测试库 → go vet + go test（矩阵 1.1～18.3 中的厂内部分）→ 前端 oxlint + tsc
make dev-server  # 本地跑 Go（连测试库 :55433）
make dev-web     # Vite 开发服务器 :5174，/v1 与 /healthz 代理到 :8081
```

## HTTP

- `GET /healthz` 版本、库与 OSS 状态
- `POST /internal/bootstrap` 仅供 WAN 用共享口令调用：建厂库并写入待启用初始超管
- `/v1/factories/{id}/…`：`login` `activate` `logout` `me` `me/password` `catalog`、`org-types` `org-units` `people` `grants` `assignments` 及各自的 `disable` / `revoke` / `end`

激活口令与会话令牌只在响应里出现一次，库里只存哈希，审计里不出现原文。
