# Client（示教器）

现场 Android 示教器：登录本厂、同步闭包密文、连机械臂作业。单 Gradle 模块 `:app`，包名 `com.gbndt.shijiaoqi`。业务对错以仓库 `docs/平台架构核心需求.md` 和 `docs/specs/控制器Client.md` 为准。

源码根：`app/src/main/kotlin/com/gbndt/shijiaoqi/`。分层靠包名，不拆多模块。`scripts/layer_guard.py` 拦住 `model/`、`domain/`、`geom/` 引用 `android.*` 或 Compose，并拦住 `geom/` 再依赖本应用其它包。

焊接三模式各有独立包：单层（含包角）`single`、多层 `multilayer`、T 排 `tbar`。Lua、几何、本模式工程编解码跟本包走。私有计算允许重复；与焊接无关的三维运算才进 `geom`。

```
com.gbndt.shijiaoqi/
├── ShiJiaoQiApp.kt / MainActivity.kt     唯一进程入口与唯一 Activity
├── di/                                   只装配：谁实现谁、谁是单例、IO 调度器；不写焊接规则
├── geom/                                 纯几何叶子：Vec3、旋转（度）；不管焊道/工艺/Lua/Android
├── model/                                持久化形状：无 Android、无 Compose、无 IO；不算路径、不组 Lua
│   ├── （根）                            三模式共用原子：Pose、焊点、点类型、工艺参数、参考点、摆焊
│   │                                     另放本机偏好、会话、更新、厂端报价、控制器错误码
│   │                                     坡口四点枚举在根包，避免单层/多层引用 T 排
│   ├── tbar/                             T 排字段：间隙带（只写工艺身份）；坡口点判定
│   │                                     不管坡口几何、切段、Lua；不依赖 model.single
│   ├── single/                           单层（含包角）工程：WeldPath、包角组、附加工艺槽、JSON 代理
│   │                                     不管采点/分层/组 Lua
│   │                                     可依赖 model.tbar（间隙带类型）
│   │                                     现状：WeldPath 也是 T 排落库正文和多层底道
│   └── multilayer/                       多层工程：底道 + 层偏移、参考点、完成标记、JSON 代理
│                                         不管层交错、参考点几何、Lua
│                                         可依赖 model.single（底道复用 WeldPath）
│                                         无环：tbar ← single ← multilayer
├── domain/                               焊接规则：算形状、编本模式工程、组 Lua；不持 Socket、不渲染
│   ├── shared/                           三模式共用且不属于某一模式：工艺身份、点序/Lua 行、跟行号、
│   │                                     工艺偏移、采点快照、Pose→Vec3
│   │                                     不管某一模式的几何或工程 JSON、不管袋与界面
│   │                                     现状：组帧仍引用 data.robot.protocol（领域指向数据层）
│   ├── single/                           单层：包角分层与生成焊道、单层 Lua、单层工程编解码（只认 processId）
│   │                                     不管 T 排、多层、组帧、本机袋
│   ├── tbar/                             T 排：间隙切段、坡口几何、T 排 Lua、T 排工程编解码（只认模版身份）
│   │                                     不管包角、多层；不把单层带 gapBands 的焊道当 T 排
│   │                                     不依赖 domain.single；焊道 JSON 仍读 model.single
│   └── multilayer/                       多层：层交错、有/无参考点的层偏移、多层工程编解码、交错后组 Lua
│                                         不管包角、T 排、采点界面
│                                         交错后的点序列复用 domain.single 的 Lua，不重写一版
│                                         本包坐标系辅助只服务多层参考点，不抽到 geom
├── data/                                 IO 与本机事实：线程、Socket、磁盘、网络、密文袋
│   ├── repository/                       界面入口：会话、袋、控制器、更新；不管组 Lua、不算焊道几何
│   ├── session/                          登录会话、节点凭证、设备号、信封仓库；不管焊接几何
│   ├── pouch/                            闭包缓存、激活、按身份给工艺、工程存回袋
│   │                                     不管把密文给人看，不把「不可复制」改成可复制
│   ├── crypt/                            信封加解密；不管业务对错
│   ├── db/                               Room + SQLCipher 持久化信封；不管业务规则
│   ├── prefs/                            本机身份与示教偏好；不管焊接路径
│   ├── remote/                           厂端 HTTP、局域网发现、MQTT 下行；不管控制器 8080/8082/8083
│   ├── update/                           APK 检查与下载；不管作业流程
│   └── robot/
│       ├── protocol/                     ASCII 帧编解码、8083 解析、指令字面量；不管 Socket
│       └── link/                         三路 Socket、连接状态；不管帧语义、Lua 正文
│                                         双网：登录不要求连臂；匹配机械臂只在连上臂时做
└── ui/                                   屏幕与协调：只展示和发操作；不算几何、不拼 Lua、不直接 fetch 厂端
    ├── theme/                            颜色字体
    ├── component/                        弱业务共用控件；不管模式专有作业流程
    ├── navigation/                       导航图、模式选择；不管焊接计算
    ├── login/                            登录、启动；不管连臂作业
    ├── project/                          袋内工程/工艺列表与选用；不管展示不可复制的平台级正文
    ├── update/                           应用更新提示
    ├── robottest/                        连臂后点动/测试；不管生产焊道
    ├── teach/                            示教会话壳；不管各模式几何
    └── welding/                          三模式共用外壳、工具/微调/错误对话框
        ├── single/                       单层屏与协调器（包角入口在此）
        ├── tbar/                         T 排屏与协调器、间隙工艺编辑
        └── multilayer/                   多层屏与协调器
```

依赖方向：

```
ui ──► domain ──► model
 │        │
 │        └──► geom
 │
 └──► data ──► model
        ▲
        di
```

按作业模式：

```
              model              domain             ui
单层（含包角）  model.single       domain.single      ui.welding.single
T 排          model.tbar          domain.tbar        ui.welding.tbar
              + 共用 WeldPath
多层          model.multilayer    domain.multilayer  ui.welding.multilayer
三家共用      model 根包          domain.shared      ui.welding 外壳
```

单测跟领域包走：`app/src/test/.../domain/{single,tbar,multilayer}/`，几何在 `.../geom/`。

```bash
cd client
python3 scripts/layer_guard.py
./gradlew :app:testDebugUnitTest
```
