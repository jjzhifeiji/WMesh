# 示教器老项目实现总结

> 只读对照迁入 WMesh 的 `client/`（来自 cloud；包名 `com.gbndt.shijiaoqi`，显示名「示教器2.0」，版本 **6.1.0 / versionCode 51**）。  
> 不当新 Client 目标形态。设备明文样例见 `docs/device-date/`。  
> 本文不改代码。

---

## 1. 它是什么

现场 Android 横屏平板：人在本机编焊道、选工艺、点动机械臂、把 Lua 脚本下到控制器执行焊接。

三句话：

- **只连控制器**，IP 写死 `192.168.57.2`，三路 TCP（8080 / 8082 / 8083）。
- **工程和工艺是外部存储上的文件树**，身份是文件夹名和 `processPath`。
- **没有厂、没有 WAN、没有登录、没有闭包、没有加密。** 本机 UUID 只当焊道/点位主键。

---

## 2. 技术形态

| 项 | 现状 |
| --- | --- |
| 模块 | 单模块 `:app`，无 Hilt/Koin，无 Navigation |
| UI | Jetpack Compose + Material3，三套 Activity 各管一种作业 |
| 状态 | 三个超大 `AndroidViewModel`（单层 / 多层 / T 排）实现同一 `WeldViewModelInterface` |
| 序列化 | `kotlinx.serialization`，`ignoreUnknownKeys` + `coerceInputValues` |
| 存储 | `/sdcard/ShiJiaoQi/` 明文 JSON；`allowBackup=true`；`usesCleartextTraffic=true` |
| SDK | min 24 / target 34；Kotlin 1.9；Compose 1.4.x |
| 发布 | 不混淆；debug 签名；HTTP CDN 拉 APK 与标准工艺 zip |

`data/storage/ProcessStorage.kt` 会写应用私有目录，**从未被调用**。真正落盘走 `ProjectManager` / `ProcessManager`。

---

## 3. 入口与作业模式

```
启动 MainActivity
  → 闪屏 3 秒
  → 模式选择
       单层单道 → 同 Activity 进焊道屏
       多层多道 → MultiLayerActivity
       T排对接 → TBarActivity
       指令测试 → 代码在，入口已注释
```

未注册则全屏遮罩，`RegistrationDialog` 不可取消。Android 11+ 要「所有文件访问」。

三种作业 **各一份工程目录、各一份近 3000～4000 行 ViewModel**，协议与手柄逻辑大量复制。`app_settings.json` 三模式共用；`lastOpenedProjectPath` 会被后保存的那一侧覆盖。

---

## 4. 数据模型

### 4.1 点

`WeldPointType`：起安 / 起点 / 中间 / 圆中 / 终点 / 终安；T 排另有 A下、B下、A上、B上。

`Pose`：`x,y,z,rx,ry,rz` + `ext1`（第七轴）。点上还可挂 `jointAngles`、`executionOffsets`（多层执行偏移）、`refPointX`（单层偏移坐标系）。

### 4.2 工艺（独立 JSON 文件）

字段与云端工艺模版同源，来自设备明文：

- 名称、焊枪偏移 `offsetX/Y/Z`（字符串）
- `current` / `voltage` / `speed`
- 起弧 / 收弧：时间、电流、电压
- `oscillation`：类型、等待、频率、振幅、左右停、侧长、过零、回摆比、方位角、倾角

**没有 `id`。** 身份 = 相对路径 + 无后缀文件名。加载后内部 `name` 被强制改成文件名。

### 4.3 单层焊道

运行时 `WeldPath` 带整份 `WeldProcess`；落盘 `WeldPathSurrogate` **不嵌工艺正文**，只写：

- `id, name, points, processPath, selectedPointIndex, isEnabled`
- 可选 `cornerGroupParams`（包角几何 + `processPath`）
- 可选 `extraProcesses[]`：`{id, processPath, isEnabled}` —— 同一几何再跑一遍附加工艺

`processPath` 例：`"1焊角6.5mm以下.json"`、`"Standard/1T1.2/1P-1F/1K6.5mmD.json"`、`"User/3K10-11mm-1.json"`。空字符串 = 未选工艺。

### 4.4 多层

`multilayer_data.json`：基准路径仍是 Surrogate；`passes[]` 同时带嵌入的 `process` 对象 **和** `processPath`。另有四角参考点（起点/中点/终点的 X、Z）。

### 4.5 T 排

点序：起安 → 四个坡口点 →（算出的）起点/终点 → 终安。工艺不绑在焊道上，而绑在标准库文件夹 `Standard/6T1.2-T排立对接` 下按缝隙命名的目录（如 `1H-3-4`），每目录一对「打底 / 盖面」工艺。焊接时按实测间隙切段，先打底再盖面。

UI 里 `extraProcesses` 被关掉，批量也不真下发 extras。

### 4.6 本机设置 `app_settings.json`

14 套工具位姿与备注、点动方位（前/左/后/右）、速度倍率（1/3/5）、上次工程、累计焊长/时长、注册、上次连控制器时间、安装姿态（平/侧/挂）、焊接电流电压显示、外轴开关。

---

## 5. 持久化（文件树）

根目录：`Environment.getExternalStorageDirectory()/ShiJiaoQi/`

```
ShiJiaoQi/
  projects/
    app_settings.json
    license.key
    robot_test_settings.json
    single_layer/<相对路径>/project_data.json
    multi_layer/<相对路径>/multilayer_data.json
    tbar/<相对路径>/project_data.json
  processes/
    <相对路径>/<工艺名>.json
    Standard/…          ← CDN 标准库，不可在 App 内删建文件夹
  exports/<name>.zip
```

打开工程会 `validateProcessFiles`：缺文件弹「以下工艺文件未找到」。工艺从 CDN 更新：`http://cdn.gbndt.com/sjqgy/StandardProcesses.zip`。导入 zip 文件名按 GBK。

这就是规划里说的「用文件路径当身份」：改名、换目录、重名都会让工程断引用。

---

## 6. 与控制器的硬契约

单例 `SocketManager`。连上先发 ASCII `"connect"`。断线 3 秒重连。引用计数 `start()`/`stop()`，归零后延迟 2 秒才关。

「已连接」= **8080 通且 8083 在 1 秒内有数据**。8082 不参与判定。

### 6.1 端口

| 端口 | 用途 |
| --- | --- |
| 8080 | 点动、Pause / RESUME / STOP / Start、Mode、使能、工具、单条 Lua |
| 8082 | 批量：105（Lua 文件名，固定 `/fruser/111.lua`）+ 106（脚本正文） |
| 8083 | 二进制状态。帧头 `0x5A 0x5A`，长度 ≥104 |

测试页用 `/fruser/test.lua`。

### 6.2 报文

`/f/bIII{id}III{type}III{len}III{payload}III/b/f`，UTF-8。成功回执 type 后为 `1`。错误文本 `robot errcode:N`（0 与 18「程序正在运行」忽略）。

### 6.3 8083 关键偏移（小端）

| 偏移 | 含义 |
| --- | --- |
| byte 5 | 程序：1 停止 / 2 运行 / 3 暂停 / 4 拖动 |
| byte 6 | 报警 0x00–0x0b |
| 8–103 | 12 个 double：关节 0–5，笛卡尔 6–11 |
| 176 / 177 | 程序总行 / 当前行（高亮焊点） |
| 265–293 | 外轴位置、速度、错误、就绪 |
| 383–386 | AI0/AI1 → 电流、电压 |
| 387–388 | 输入：送丝 / 录点 / 拖动 |
| 425 / 426 | 焊接中断 / 电弧中断（1=中断） |

### 6.4 实际发出的 type（摘）

101 Start · 102 STOP · 103 Pause · 104 RESUME · 105/106 脚本 · 107 复位 · 201 单条 MoveL/ServoCart/焊接参数/外轴 · 206 SetSpeed · 247/248 手动起收弧 · 302 使能 · 303 Mode(0 自动 / 1 手动) · 316/205 工具坐标 · 337 安装姿态 · 375 逆解 · 806 断弧再焊 · 807 中止断弧焊 · 825 在线摆动 · 826 读 MAC · 1368 在线摆幅偏移。

手柄 SELECT：`Mode(1)`（303）→ 50ms → `RobotEnable(1)`（302）。

---

## 7. 焊接怎么跑

### 7.1 批量流水（必须按这个时序）

`BatchCommandBuilder` 拼所有启用焊道 → **同步 105** → delay 50ms → **106 等 ACK（最长约 8s）** → delay 500ms → `Mode(0)` → delay 100ms → `Start`。105 与 106 不得并发乱序。

进度：8083 当前行 → 行号表 → 高亮点。

### 7.2 单层脚本顺序

1. 全局一次 `SetSpeed(10)`
2. `WeldingSetProcessParam(...)`（起弧/焊接/收弧）
3. 有摆动则 `WeaveSetPara(3, ...)`
4. 点：起安/起点/终安空走 `MoveL` 速度 100；`ARC_MIDDLE` 与下一点合成 `MoveC`；其余 `MoveL`
5. **`ARCStart` / `WeaveStart(3)` 必须在到达 START 的运动指令之后**（不能在脚本头）
6. END：`ARCEnd` + `WeaveEnd`
7. 外轴开：每点前 `ExtAxisMoveJ`；有偏移时走 `GetInverseKinExaxis`

附加工艺：主焊道跑完，对每个启用 extra 用同一几何再拼一遍。断点续焊时 extras 从 0 重跑。

模拟与起弧走同一套批量；模拟不加起收弧，速度可乘 3/5 倍。

### 7.3 暂停 / 继续 / 停止（不重算路径）

- 暂停：8080 `Pause` → 1s → `Mode(1)`
- 继续（长按防误触）：先 `Mode(0)`，若焊接中断再 `WeldingStartReWeldAfterBreakOff()`，再 `RESUME`
- 停止：记下断点位姿/关节与路径索引 → `STOP` → `Mode(1)`；中断时再 `WeldingAbortWeldAfterBreakOff()`
- `programState==1` 且焊接中断，保持暂停 UI，不当完成

从停止重下发：先空走到断点；已过 START 则补起弧/摆焊；圆弧段重算中点后 `MoveC`。

### 7.4 多层

所有路径第 0 层 → 第 1 层… 交错。`generatePassPath` 把 `valX / 左右Y / Z / R` 变成点上的 `executionOffsets`。WeaveStart 同样必须在 START 运动之后。

### 7.5 T 排

不示教 START/END。采齐坡口后算焊枪姿态，按间隙切段：打底 → 空走回起点 → 盖面 → 终安。段切换 `WeaveOnlineSetPara`。每段逆解 + MoveL。

### 7.6 点动与手柄

| 键 | 行为 |
| --- | --- |
| A | 停止（单层 `force=true`） |
| B 长按 1s | 模拟焊 |
| B+R1 长按 1s | 起弧焊 |
| X | 采集当前点 |
| Y 长按 1s | 点动到当前点 |
| START | 手柄伺服开关 |
| SELECT | 手动模式 + 使能 |
| 摇杆 / 方向键 | `ServoCart`（仅伺服开） |
| L2/R2 | 外轴 jog |

点动方位随 `positionMode`。Y 点动：当前点 `ExtAxisMoveJ` + `MoveL` type 201。

---

## 8. 现场功能面（焊工实际在用的）

- 焊道列表：启用、改名、拖排序、滑删；加直线点 / 圆弧点；采集 / 清点；X 向参考
- 工艺管理：文件树；选工艺绑到当前焊道或附加槽；标准库只读结构；CDN 更新
- 工程管理：打开含 `project_data.json` / `multilayer_data.json` 的目录
- 包角：参考焊道 A/B、层数、初长、上偏、递减、焊枪姿态；改参密码 `bd888888`；生成后逆解填关节
- 微调（不可点外部关闭）：在线电流电压、摆幅频率、`SetSpeed`、`SetWeaveOffsetRT`
- 3D：Canvas 线框，红焊接 / 黄空走
- 状态栏：工程、14 套工具、方位、速度、安装、外轴、报警复位、重连、焊接/电弧中断
- 送丝 / 退丝 / 送气 / 手动起弧；累计焊长时长
- 注册：读控制盒 MAC（826）；码 = SHA-256(`P{mac}L`) 十六进制后 12 位；明文写 `license.key`；超过 3 天未连控制器要重注册
- 更新：`http://cdn.gbndt.com/sjqapk/update.json` → APK `app-release6.1.apk`

---

## 9. 几何（必须原样移植的计算）

| 模块 | 做什么 |
| --- | --- |
| `CornerWeldGenerator` | 两线段公垂线中点；辨立焊/平焊；等腰直角三角锥分层；每层起安-起点-终点-终安 |
| `TBarGeometry` | 文件夹名 `数字H-min-max`；21 点采样间隙；焊枪绕焊缝轴转到垂直坡口平面 |
| `CoordinateUtils` | 原点 + X 参考 + Z 参考建系；欧拉↔矩阵；局部偏移 |
| `DcddUtils` | 多层 `valR` 转姿态偏移 |

单层偏移：焊缝切向 Y、水平法向 X；外轴开启时 `offsetX` 取反。

---

## 10. 明确没有的

- 人员登录、会话、角色、组织
- WAN / 厂绑定 / Client 稳定身份 / 节点运行凭证
- MQTT；HTTPS 拉闭包
- 工艺/工程 UUID、修订、摘要、`deps`、`processId`、`templateId`
- 信封加密、Room、SQLCipher（规划要求关备份；老项目开着备份）
- 缓存上限、激活一份工程、个人级汇聚、点云上传
- 控制器发现（IP 写死）、链路鉴权

---

## 11. 和规划目标差在哪

| 能力 | 老项目 | 已拍板的规划 |
| --- | --- | --- |
| 工艺身份 | 文件路径 | 稳定 UUID；焊道只写 `processId` |
| 工程持工艺 | 本机全量 `processes/` 目录 | 只持闭包里的工艺成员 |
| 落盘 | 明文 JSON 树 | Room 信封；钥与明文只在内存 |
| 连谁 | 只连控制器局域网 | 出站 MQTT 连本厂；永不连 WAN；控制器仍直连 |
| 谁能用 | MAC 注册码 | 本厂有效账号登录 + 节点凭证 + 已绑定机械臂 |
| 下发 | U 盘/拷文件/CDN zip | 厂组包 → 过站重封 → 本机袋 |
| 编号 | 无 | `GY/GC` + Client 短码 + 本地序号（个人级） |
| 作业种类 | 单层 / 多层 / T 排；包角挂在单层上 | 空库三份模版：单层 / 多层 / T排对接；包角不进工程模版 |

---

## 12. 重写时必须原样保留的

1. 三端口、帧格式、105/106 时序、Pause/RESUME/断弧焊指令。
2. WeaveStart 在到达 START 之后；Mode(0) 与 Start 拆开发、中间留延时。
3. 点类型、位姿/关节、外轴、工具坐标、安装姿态、手柄键位。
4. 包角 / T 排 / 多层偏移的几何公式。
5. 工艺参数字段名（与云端工艺模版对齐的那份明文）。

可丢：文件树、`processPath`、CDN 当工艺真相、注册码、三套复制粘贴 ViewModel、明文备份、写死 IP（应可配置，但默认仍指向现网段）。
