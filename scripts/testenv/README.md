# 本机测试环境初始化

把本机 WAN、厂端、示教器和假控制器重置成固定的初始状态，再灌一套接近生产的数据。只用于本机测试数据：重置直接删数据卷，不备份；账号密码明文写在 `seed.json`。

```bash
scripts/testenv/init.sh                  # 重置 + 基础数据（约 45 秒）
scripts/testenv/init.sh --with-history   # 再加过去 30 天的焊次，报表一打开就有数
scripts/testenv/init.sh --build          # 两端按当前源码重建镜像（代码有改动时用）
```

| 文件 | 作用 |
|---|---|
| `init.sh` | 总入口：`reset.sh` → `seed.py` → `verify.py` |
| `reset.sh` | 两端 `docker compose down -v` 后重建并等健康；示教器 `pm clear`；假控制器按种子设备号和真实起点位姿重启；`sim_net.sh both` |
| `seed.json` | 种子清单：工厂、组织、人员、角色、设备、资产白名单与级别、策略、历史焊次参数。改数据只改这里 |
| `seed.py` | 按清单全部走正式接口灌数据（厂库正文是加密信封，不能直接写库）。只在干净状态下跑 |
| `verify.py` | 核对数量、发布状态、不可复制、厂库无明文，写基线 `baseline.local.json` |

`state.local.json`（各身份）和 `baseline.local.json`（基线）是每次生成的，不进仓库。

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
