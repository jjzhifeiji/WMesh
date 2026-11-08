# 本机测试环境初始化

把本机 WAN、厂端、示教器和假控制器重置成固定的初始状态，再灌一套接近生产的数据。只用于本机测试数据：重置直接删数据卷，不备份；账号密码明文写在 `seed.json`。

```bash
scripts/testenv/init.sh                  # 重置 + 基础数据（约 45 秒）
scripts/testenv/init.sh --with-history   # 再加过去 30 天的焊次，报表一打开就有数
scripts/testenv/init.sh --build          # 两端按当前源码重建镜像（代码有改动时用）
```

| 文件 | 作用 |
|---|---|
| `init.sh` | 总入口：`reset.sh` → `seed.py` → `verify.py` → `logs.py check` |
| `reset.sh` | 先起本机 Loki，并把两端 alloy 的 `LOKI_URL` 指向它；两端 `docker compose down -v` 后重建并等健康；示教器 `pm clear`；假控制器按种子设备号和真实起点位姿重启；`sim_net.sh both` |
| `seed.json` | 种子清单：工厂、组织、人员、角色、设备、资产白名单与级别、策略、历史焊次参数。改数据只改这里 |
| `seed.py` | 按清单全部走正式接口灌数据（厂库正文是加密信封，不能直接写库）。只在干净状态下跑 |
| `verify.py` | 核对数量、发布状态、不可复制、厂库无明文，写基线 `baseline.local.json` |
| `loki/` | 本机 Loki（`grafana/loki:3.1.1`，compose 项目 `wmesh-testlog`），数据落 `loki/data/` |
| `logs.py` | 查本机 Loki 里的三端日志、导出到文件；`logs.py check` 确认两端只推本机 |

`state.local.json`（各身份）、`baseline.local.json`（基线）和 `loki/data/`（日志）是本机生成的，不进仓库。

## 日志

本机测试的日志全部落本机，不推共享环境 154.82.81.16：

- WAN、厂端容器日志由各自的 alloy 采集；示教器日志推到厂端 alloy 的 `:3500`，一并转发。
- `reset.sh` 先起 `loki/` 里的本机 Loki（宿主 `127.0.0.1:3100`），再以 `LOKI_URL=http://host.docker.internal:3100/loki/api/v1/push` 重建两端，两端 alloy 照常开着，不用再手动停。外面已 export 的 `LOKI_URL` 优先，但不是本机地址或指向 154.82.81.16 时 `reset.sh` 直接退出。
- 两端 `docker compose down -v` 不动本机 Loki，日志跨多次重置保留 30 天。要清空：`docker compose -f scripts/testenv/loki/docker-compose.yml down && rm -rf scripts/testenv/loki/data`。
- 不经 `reset.sh`、直接在 `global/` 或 `factory/` 下 `make up` / `make dev-up` / `docker compose up -d`（不带服务名或不带 `--no-deps`）时，要先 `export LOKI_URL=http://host.docker.internal:3100/loki/api/v1/push`，否则 compose 会按生产默认把 alloy 重建成推共享环境。只重建 app 用 `docker compose up -d --no-deps app`，不碰 alloy。外面 export 的 `LOKI_URL` 不是本机地址（host.docker.internal / 127.0.0.1 / localhost）时 `reset.sh` 也直接退出。
- Linux 宿主上 Loki 默认只开回环，容器连不到，起之前设 `TESTLOG_LOKI_BIND=0.0.0.0`。

**level 标签统一大写**：`INFO` / `WARN` / `ERROR`。服务端 JSON 本来就是大写，示教器推来的小写 `info` / `warn` / `error` 由两端 alloy 转成大写；其它级别（如 DEBUG）不打 level 标签。厂、设备不进索引，进结构化元数据 `factory`、`client`（服务端取 JSON 正文里的同名字段，示教器取推送时带的标签）。

```bash
scripts/testenv/logs.py                                    # 最近 15 分钟全部
scripts/testenv/logs.py -s pad -l warn,error --since 2h    # 示教器 2 小时内的告警、错误
scripts/testenv/logs.py -s factory -k /pad/assets -k 400   # 厂端同时含两个关键字的行；-v 排除、-x 正则
scripts/testenv/logs.py --client <设备id> --from "11-04 19:48" --to "11-04 20:20" -n 0 -o d94.log
scripts/testenv/logs.py -s wan --format jsonl -o wan.jsonl # 导出 JSON 行
scripts/testenv/logs.py labels                             # 现有服务、级别取值
scripts/testenv/logs.py check                              # 两端 alloy 只推本机、日志已落本机
```

`-s` 取 `wan`、`factory`、`pad`（逗号分隔多个）；时间是本地时间；`-n` 默认 2000 行，`0` 不限。也可以在 Grafana 里把本机 Loki `http://127.0.0.1:3100` 加成数据源直接查。

`logs.py check` 核对三件事：两端 alloy 容器的 `LOKI_URL` 是本机地址；alloy 自己的 `/metrics` 里 `loki.write` 实际发往的主机只有本机、运行日志里没有 154.82.81.16；本机 Loki 里近 30 分钟查得到 WAN、厂端日志，且这段时间的 level 没有小写取值。`init.sh` 最后自动跑一次。

示教器旧包在登录页扫描时会把日志直接推给扫到的厂服地址，含预置的 154.82.81.16（D-83）。这条路径不经本机 alloy，`logs.py check` 查不到。修复包改为扫描时不推、登录成功后只推给登录的那台厂服；平板装上修复包之前仍有这个风险。

## 数据来源

全部取自现场示教器导出的 `docs/device-date/`，随手测的条目（「新工艺」「焊缝141号…」「天天」「团团」等）不进：

- **平台**：`processes/Standard/1T1.2/` 下 1.2 丝标准工艺库 33 条（1P-1F 平角焊、2L-3F 立焊、3BJ 对接），3BJ 对接设为不可复制。
- **测试厂A 厂级**：`User/`、`1焊角6.5mm以下` 等现场工艺；第七轴、第八轴两台臂上真实示教的单层和多层工程。
- **个人**：`hangong1` 名下一份平焊练习工程。
- 原始数据里引用了随手工艺的，用 `remap` 换成标准库；多层工程有的层没配工艺，用 `fill` 按打底 + 3K10-11mm 分层补齐。

暂缺 T 排工程和包角工艺：现场导出里没有，且示教器进 T 排要的口令厂端还没下发（测试方案 B-15）。修好后在示教器上对着假控制器示教一份，导出存进种子。

## 示教器

`reset.sh` 只清应用数据，不装包。示教器要装指向本机的调试包：

```bash
cd client/ShiJiaoQi-New && ./gradlew :app:assembleDebug -ProbotHost=<本机网卡地址>
adb install -r app/build/outputs/apk/debug/app-debug.apk
```
