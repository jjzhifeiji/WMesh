# Client 架构与目录结构

> 已拍板的是**方向**：分层、依赖方向、目录形态。具体迁移步骤与改动清单另议，本文不写实现细节。  
> 现状对照：`docs/client-示教器老项目实现总结.md`；业务规格见 `docs/specs/控制器Client.md`。

---

## 1. 结论

按 Android 官方推荐架构（参照 `nowinandroid`）的 **UI / domain / data 三层**重做 Client，**单 Gradle 模块**，分层靠包名与约定，不拆多模块。

项目规模小，多模块带来的编译期边界收益抵不过维护成本。包名按下面这套划，将来真要拆模块可原样平移。

---

## 2. 为什么要动

现状不是"分层不好"，是没有分层、没有依赖方向、没有数据流方向：

- **没有单向数据流。** `WeldViewModelInterface` 里约 60 个可写 `var` 与 `SnapshotStateList` 直接交给 Composable，UI 自己改状态。三个 ViewModel 共 235 处 `mutableStateOf`、0 个 `StateFlow`。状态没有单一变更入口。
- **线程模型未定义。** `BagSession` 是阻塞 API 却用 Compose 快照状态存 `busy/error`，由 Composable 自持的 scope 在 IO 线程调用；`Pouch` 的可变集合无任何并发保护，主线程与 IO 线程同时访问。
- **没有 DI，边界不可替换。** 全局 `object` 单例 + Application 手工装配；三个 ViewModel 各有 60~70 处直接触碰 `File` / `Environment` / `DownloadManager` / `TextToSpeech`，ViewModel 与数据层无法单测。
- **加一个焊接模式等于复制一遍应用。** 单层 3498 行、T 排 3661 行、多层 3978 行，**77 个同名函数三份重复**，三个 Activity 平行存在。协议改一次要改三处。
- **依赖是一张网。** `ui` 直接 import `platform`、`data.manager`、`robot`；没有 domain 层，没有仓库层。

可直接复用的资产只有两块：`robot/`（协议已隔离）与 `weld/`（基本纯 Kotlin）。

---

## 3. 目录结构

```
app/src/main/kotlin/com/gbndt/shijiaoqi/
  ShiJiaoQiApp.kt  MainActivity.kt      单 Activity
  model/                 纯数据类，跨层共用
  domain/
    weld/                点列、层偏移、包角、T 排切段 —— 算“形状”
    script/              指令与 Lua 生成 —— 算“下发”，纯函数
  data/
    session/             登录会话、节点凭证
    pouch/               闭包缓存与激活
    crypt/               信封加解密
    db/                  Room + SQLCipher
    prefs/               本机身份与设置
    remote/              HTTP 厂端网关、局域网发现、MQTT 下行
    update/              APK 版本检查与下载
    legacy/              过渡壳：只剩本机设置读写是活的，其余是空实现，随 ViewModel 合并一并删
    robot/
      protocol/          帧编解码、8083 解析 —— 纯 Kotlin
      link/              三路 Socket、连接状态 —— IO
  ui/
    theme/  component/   无状态 Composable
    login/  project/  robottest/
    welding/             三模式共用的对话框与接口
      single/  multilayer/  tbar/     每个含 Screen + ViewModel
```

`ui/welding` 下按模式分的三个子包是过渡态：三份实现里有大量同名 Composable，不分开连编译都过不了。合并成一个 ViewModel + 路径策略后，这三个子包取消。

---

## 4. 四条分层规则

1. **UI 只读 `StateFlow<UiState>`，只调方法。** UiState 是不可变 data class / sealed interface，对外不暴露 `var`。
2. **`model/` 与 `domain/` 不得出现 `android.*` 与 `androidx.compose.*`。** 这是唯一需要机器守的一条，其余靠 review。
3. **线程收敛在 repository。** 对外只有 `suspend` 与 `Flow`，内部自己 `withContext`；ViewModel 与调用方不感知线程。
4. **依赖只向下。** `ui → domain → data → model`；`ui` 不直接 import `data`。

---

## 5. 关键取舍

| 取舍 | 定的是 | 为什么 |
| --- | --- | --- |
| 单模块 | 不拆 Gradle 模块，用包名分层 | 项目中小规模，编译期强制边界的收益抵不过模块维护成本 |
| `weld` 与 `script` 分开 | 几何一包、指令生成一包 | 变更原因不同（改工艺 vs 改控制器时序）；`script` 最该被快照测试锁住 |
| `robot` 拆 `protocol` / `link` | 编解码纯 Kotlin，Socket 归 IO | 现在混在一起，协议无法脱离设备验证 |
| 取消 `platform/` 层 | `crypt` / `pouch` / `session` 落进 `data/` | 它们是数据与安全的实现，不是与 data 并列的第四层 |
| 焊接模式收成一个 | 一个 `ui/welding`，模式是其内部策略 | 三份平行实现是复制粘贴的结果，不是领域差异 |
| UiState 不进 `model/` | 留在各自 `ui/xxx` 包 | UiState 随界面变，与领域模型生命周期不同 |

顺带：源码目录 `java/` 改 `kotlin/`；三个 Activity 收成一个，屏间切换走 Navigation，不再用手写的 `AppScreen` sealed class。

---

## 6. 工具链

取各家当前稳定版，且互相兼容。KSP 目前只发到 2.3 线，Room 与 Hilt 都依赖 KSP，所以 Kotlin 停在 2.3 最新，不上 2.4。

| 件 | 版本 | 备注 |
| --- | --- | --- |
| Gradle | 9.7.1 | |
| AGP | 9.4.0 | 自带 Kotlin 支持，不再单独应用 `kotlin-android` 插件 |
| Kotlin | 2.3.21 | Compose 编译器插件随 Kotlin 版本走 |
| KSP | 2.3.12 | 上限，决定了 Kotlin 版本 |
| compileSdk | 37 | 最新 androidx 要求 |
| targetSdk | 34 | **不动**：改它会改运行期行为，与本次结构调整无关，另议 |
| minSdk | 24 | 不动 |

版本全部收进 `gradle/libs.versions.toml`，构建文件里不再出现硬编码版本号。Hilt、Navigation、DataStore、lifecycle-runtime-compose 已随之引入，等后续接线。

---

## 7. 当前落地状态

已完成并验证（单测 66 个全过、debug APK 可打包）：工具链升级、版本目录、源码目录从 `java/` 迁到 `kotlin/` 并按上表分包、删掉死代码 `ProcessStorage`。

唯一还破着的红线：`model/WeldModels.kt` 与 `domain/weld/CornerWeldGenerator.kt` 仍在用 Compose 的 `SnapshotStateList`/`mutableStateListOf`，即领域模型被 Compose 绑住。这处只能和 ViewModel 改 UiState 一起拆，单独改会牵动三个 ViewModel 的原地增删点位逻辑。

---

## 8. 本文不管

不定迁移顺序、不定工具链版本、不定模块拆分时机、不改业务规格的允许/拒绝。焊接时序、协议帧、几何算法一律照现网，本次只搬位置、不改行为。
