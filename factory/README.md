# WMesh Factory（厂内服务）

厂内账号与组织的业务权威。一台厂端服务可承载多家工厂：按工厂稳定身份在同一 PostgreSQL 实例上各建一个库（`wmesh_fac_<id>`），人员、密码哈希、组织树、分配、角色、会话、审计全部只在本厂库里。点云/图片本体落本地对象存储（S3 兼容），不进 WAN。

## 目录

```
factory/
├── Dockerfile            前端构建 → Go 静态编译 → alpine 运行时（单进程：API + 管理端静态页）
├── docker-compose.yml    部署：app + db（PostgreSQL 18）+ oss（RustFS，S3 兼容）+ alloy（日志）
├── alloy/                Grafana Alloy：采本项目容器日志推 Loki
├── Makefile              up / down / upgrade / bundle / test …
├── .env.example          部署配置模板，复制为 .env
├── frontend/             管理后台（见下）
└── server/               Go 1.27 服务
    ├── cmd/server        进程入口：读配置、连维护库、保证 OSS 桶、起 HTTP
    ├── internal/httpapi  JSON HTTP 适配，按 URL 里的工厂身份选库
    ├── internal/hub      按工厂身份建/开厂库并组装应用服务
    ├── internal/service  应用服务：认证、权限、归属、审计
    ├── internal/store    本厂库读写（GORM，仅结构体对齐，禁 AutoMigrate）
    ├── internal/wanchannel 厂出站连 WAN：HTTPS 认领，日常 MQTT + HTTPS 拉正文
    ├── internal/web      托管已构建的管理端静态页
    ├── internal/platform 底座：config / applog / provision / oss / id / secret / audit / domain / migrate / testpg
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
    ├── auth/         登录、认领、激活
    ├── catalog/      名册读模型（唯一的 GET，所有写操作成功后让它失效重拉）+ 是否超管
    ├── dashboard/    概览（人员/组织/角色规模、我的角色、服务健康）
    ├── org/          组织节点（工厂下一棵树）
    ├── people/       人员（新建、授/收角色、默认密码登录名+123456）
    ├── assignments/  组织分配（每人最多一个节点）
    ├── clients/      Client 节点绑定与运行许可
    ├── assets/       工艺与工程（厂级/个人级）
    └── account/      我的账号、改密码
```

加一个功能：在 `features/<name>/` 写 `api.ts` 与页面 → `app/router.tsx` 加路由 → `app/navigation.tsx` 加菜单（需要超管就标 `superAdminOnly`）。页面不直接 `fetch`，一律经 `shared/api/client`；改名册的写操作用 `useCatalogMutation`。

## 运行

```bash
make up        # 首次自动生成 .env，请改密码；构建镜像并等到 app/db/oss/alloy 全部健康
make logs      # 跟随日志（JSON）
make upgrade   # 拉新基础镜像、重建应用、替换容器；各厂库迁移在首次访问时自动向前
make bundle    # 导出离线安装包 dist/*.tar（app + postgres + rustfs + alloy），厂内无外网时 docker load -i 后 make up
make down      # 停容器，保留数据卷
```

关键配置（`.env`）：

| 变量 | 作用 |
| --- | --- |
| `POSTGRES_PASSWORD` | 维护库密码；账号需能建库，各厂库由服务自动创建 |
| `WMESH_WAN_URL` | WAN 根地址，厂出站认领（如 `http://host.docker.internal:52080`） |
| `WMESH_BOOTSTRAP_TOKEN` | 可选；仅 `/internal/bootstrap` 安装期入口 |
| `OSS_ACCESS_KEY` / `OSS_SECRET_KEY` / `OSS_BUCKET` | 对象存储密钥与默认桶；桶由 app 启动时自动创建 |
| `OSS_PORT` / `OSS_CONSOLE_PORT` | S3 端点（默认 52902，Client 上传点云/图片用）与 RustFS 管理台（默认 52903，用同一对密钥登录） |
| `WMESH_REGISTRY` / `WMESH_VERSION` | 镜像前缀与标签，供 `make push` |
| `WMESH_LOG_LEVEL` | app JSON 日志级别，默认 info |
| `LOKI_URL` | Alloy 推送地址，默认 `http://154.82.81.16:3100/loki/api/v1/push` |
| `ALLOY_PAD_PORT` | Pad 直推 Loki push 的宿主机端口，默认 `3500`；与厂 HTTP 同机，不登录 |

服务进程直接读 `WMESH_*` 环境变量（见 `server/internal/platform/config`）；`/healthz` 报告版本、库与 OSS 状态，OSS 掉线不影响账号管理。

## 开发

```bash
make test        # 起测试库 → go vet + go test（账号矩阵厂内部分 + 节点 1.1～16.3）→ 前端 oxlint + tsc
make dev-server  # 本地跑 Go（连测试库 :55433）
make dev-web     # Vite 开发服务器 :5174，/v1 与 /healthz 代理到 :52081
```

## HTTP

- `GET /healthz` 版本、库与 OSS 状态
- `GET /v1/site` 本机已认领工厂；`POST /v1/site/claim` 贴建厂码并当场设密码
- `POST /internal/bootstrap` 可选安装期入口（共享密码）：测试夹具仍可用
- `/v1/factories/{id}/…`：`login` `activate` `logout` `me` `me/password` `catalog`、`org-units` `people` `grants` `assignments` 及各自的 `disable` / `revoke` / `end`
- 节点：`clients`（接受/作废绑定）、`clients/{id}/runtime`（签发/撤销运行许可）、`signing-key`（仅公钥）
- 资产：`assets`（厂级/个人级工艺与工程）、`assets/{id}` 改名/改正文/发布/停用/升厂级、`assets/{id}/snapshot`（升平台快照）、`asset-author-context`
- 模版：`templates`（本厂已收工艺/工程字段模版，只读）

初始超管激活码与会话令牌只在响应里出现一次，库里只存哈希，审计里不出现原文。厂内普通账号默认密码为登录名+123456，只存哈希，不回传。
