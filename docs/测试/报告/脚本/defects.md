# 缺陷草稿（报告汇总用）

## D-01 S2 WAN 能浏览并升档厂内个人件、停用件（W-38）
- 复现：WAN `GET /v1/factories/{A}/promotable-assets` 列出 hangong1 的个人工艺「1K6.5mmD」、个人工程「张焊工-平焊练习」和已停用的厂级件「W38停用件」；`POST /v1/assets/promote-from {factoryId:A, assetId:<个人工艺>}` → 201，平台出现草稿 GY-W-000041，`GET /v1/assets/{id}/content` 读到个人正文；停用件同样 201（GY-W-000040）。
- 预期：只列、只收厂级可用件（promoteFromReq 注释「该厂可升档厂级身份」；方案 W-38「停用拒绝」；03 个人件只有属主可见）。厂端网页导出 `ExportAssetSnapshot` 有级别和状态检查。
- 位置：`factory/server/internal/service/asset.go:1799` `SnapshotForChannel` 只查可复制，不查 Level==factory、Status==available；可升档列表的生成同样没过滤（WAN `GET /v1/factories/{id}/promotable-assets`）。

## D-02 S4 密码长度只在前端校验，接口不校验（W-04、F-01、F-05）
- 复现：`POST /v1/me/password {currentPassword:<对>, password:"short"}` → 204，之后用 short 能登录。
- 预期：至少 8 位（前端 `AccountPage.tsx:60` 有，后端没有，绕过前端可设弱口令）。
- 同样：厂端认领 `POST /v1/site/claim` 用 5 位 short → 201 且能登录（前端 ClaimPage.tsx:44 限 8 位）；厂端 `POST me/password` 3 位 abc → 204（前端 AccountPage.tsx:101 限 8 位）。
- 位置：`global/server/internal/service/auth.go:79` ChangePassword；厂端认领与改密的 service 层同样缺长度校验。

## D-03 S3 设备识别号大小写不同可重复登记（W-21）
- 复现：已有 `FA:KE:00:00:00:01`，再登记 `fa:ke:00:00:00:01` → 201。
- 预期：识别号是网卡号，应按大小写不敏感判重（待核对示教器 826 匹配是否大小写敏感）。
- 附：129 字识别号报错文案是 `device serial is required`，应为超长。

## D-04 S3 WAN 删除工程模版后厂端一直保留（W-51）
- 复现：WAN 新建工程模版「W51模版」→ 改名 → `DELETE /v1/project-templates/{id}` 200；A 厂 `GET project-templates` 20 秒后仍有「W51模版改」，手动 `POST templates/sync` 再等 10 秒仍在。
- 预期：模版-T10～T16 增删改立即生效并下发，删除应同步到厂端（示教器新建工程时不再出现）。
- 位置：`global/server/internal/service/template.go:659` 删除后只调 `notifyRemainingTemplates`（`notify.go:273`）通知剩下的模版；厂端 `factory/server/internal/service/template.go:245` `AcceptTemplateDelivery` 只增改不删，没有删除通道。

## D-05 S3 包角工艺槽校验只在网页入口有，示教器上传入口没有；网页新建会把包角正文洗成焊接参数（F-33）
- 复现①：示教器接口 `POST /pad/assets` 建个人包角工艺（正文 `kind=cornerGeometry`）→ 201、corner=t；再 `POST /pad/assets` 建工程，把它挂进焊道 `processId` → 201，厂库 deps 里有这条包角。
- 复现②：网页接口 `POST /assets` 用同样的包角正文建厂级工艺 → 201，但 corner=f、正文被模版整成焊接参数（`kind`、`layers` 被丢掉，补上 current/voltage 默认值）。
- 预期：任何入口的工程工艺槽都不能挂包角工艺（`ErrCornerSlotMismatch`）；包角正文不被焊接模版改写。
- 位置：`factory/server/internal/service/asset.go:244`（insertAuthored）和 `:921`（UpdateAssetContent）调用 `assertCornerSlots`，`CreatePadIn`（`:311`）、示教器改正文入口未调用；`service/template.go:83` `normalizeContent` 对包角正文也按焊接工艺模版 `applyObject` 只留模版字段。

## D-06 S3 厂端登录没有任何限速（S-09，牵连 C-04）
- 复现：对 A 厂 `POST /login` 用同一账号连错 30 次、`POST /pad/login` 连错 20 次，全部 401，从不 429；紧接着正确密码登录 200。
- 预期：厂端暴力登录应 429（方案 S-09）；示教器「错太多次」提示「5 分钟后再试」（C-04）依赖服务端限速，现在永远出不来。WAN 有按来源限速（`global/server/internal/httpapi/limit.go`）。
- 位置：厂端 `factory/server/internal/httpapi/auth.go`、`org.go` 的登录入口没有限速闸；`domain.ErrTooManyAttempts` 定义了（`platform/domain/errors.go:61`）但厂端登录路径没有任何地方返回它。

## D-07 S2 小屏状态栏右半截看不到、点不到：报警、连接、外轴伺服、焊接、电弧（B-10、R-17、R-18）
- 复现：panogo mini（1200×1080 横屏）进单层焊接页，状态栏只显示到「外轴位置」；注入 `alarm 3`、`arcbreak` 后屏上和 uiautomator 树里都没有报警、电弧项；横向滑动状态栏无反应。报警时无法点状态栏发 ResetAllError 清报警，连臂校验失败后无法点「连接」重连。
- 预期：状态全部可见、可点（方案 B-10、R-17、R-18）。
- 位置：`client/ShiJiaoQi-New/app/src/main/kotlin/com/gbndt/shijiaoqi/ui/component/StatusBar.kt:32` 起的 `Row(fillMaxWidth, SpaceBetween)` 没有 `horizontalScroll`，也不换行，12 项超出 1200px 的部分被裁掉。规避：只能杀进程重开。

## D-08 S3（现场风险）示教器没有 TTS 引擎，语音播报全部失效
- 复现：`alarm 3` 后 logcat 每 5 秒一条 `W/TextToSpeech: speak failed: not bound to TTS engine`；`settings get secure tts_default_synth` = null，`query-services TTS_SERVICE` 无服务。
- 影响：报警播报（R-18）、T 排道间敲药皮语音（C-64）在 panogo mini 上都听不到。需要在设备上预装 TTS 引擎，或 App 检测到无引擎时改为醒目弹窗/蜂鸣。
- 位置：示教器 TTS 调用处没有引擎缺失兜底（`TextToSpeech` 初始化失败只打日志）。

## D-09 S2 设备号读不到（826 不回）时仍放行作业（R-04）
- 复现：假控制器 `silent 826`，重启示教器 App；连臂后 826 每 3 秒问一次、5 次后停；进单层焊接页长按「模拟焊接」→ 照常下发 105/106/Mode(0)/Start。
- 预期：设备号没匹配上（`armMatched=false`）不放行作业（方案 R-04；名录校验是设备绑定的唯一闸）。现在只有「读到且不在名录」会主动断三路，「读不到」既不断开也不拦开焊，等于绕过设备绑定。
- 位置：`client/ShiJiaoQi-New/app/src/main/kotlin/com/gbndt/shijiaoqi/data/session/BagSession.kt:250` matchLocal 只在拿到设备号时调用；各焊接 ViewModel 开焊前（如 `ui/welding/single/SingleWeldViewModel.kt` 起焊入口）没有检查 `session.armMatched`。

## D-10 S1 106 程序正文没确认仍然启动（R-09、B-13）
- 复现：假控制器 `noack 106`，长按模拟焊接：105 → 106（无确认）→ 8 秒后照发 Mode(0)、Start，程序开跑，界面没有任何提示。
- 预期：106 没确认就停下并提示「程序下发失败」，不发 Mode(0)/Start。真控制器上 111.lua 可能还是上一次的程序，会按旧轨迹焊（焊错件）。
- 位置：`client/ShiJiaoQi-New/app/src/main/kotlin/com/gbndt/shijiaoqi/ui/welding/single/SingleWeldViewModel.kt:968-980`，`withTimeoutOrNull(8000)` 返回 null 后 `ack.await()` 的结果被丢弃，继续发 modeAuto/start；确认判断 `it.contains("106")` 也过宽（任意含 106 的回包都算）。多层、T 排 ViewModel 有同样写法（见下方 grep）。
  - ui/welding/tbar/TBarWeldViewModel.kt:1228:                withTimeoutOrNull(8000) {
  - ui/welding/tbar/TBarWeldViewModel.kt:1828:                withTimeoutOrNull(8000) {
  - ui/welding/single/SingleWeldViewModel.kt:969:                        withTimeoutOrNull(8000) {
  - ui/welding/multilayer/MultiLayerWeldViewModel.kt:783:                        withTimeoutOrNull(8000) {

## D-11 S2 示教器 MQTT 客户端泄漏，同一 clientId 多条连接互踢，在线状态抖动、厂端日志暴涨
- 复现：本轮 hangong1 第一次登录（20:46）起，厂端日志 `client mqtt online/offline` 每分钟 887～2648 条（`docker compose logs app | grep 'client mqtt o' | uniq -c`），示教器 logcat 同频 `MQTT lost 已断开连接 / reconnected`；示教器 `/proc/net/tcp6` 里同时有约 9 条到 192.168.123.150:52184 的 ESTABLISHED 连接（只有一个 App 进程）。首页「在线」偶尔显示成「离线」。
- 影响：厂端日志一台示教器一天约 300 万行，磁盘与 Loki 压力；策略、账号变化等 MQTT 下行可能投到被踢掉的那条连接上而丢失；厂端人员页在线状态不可信。
- 位置（推测，需开发确认）：`client/ShiJiaoQi-New/app/src/main/kotlin/com/gbndt/shijiaoqi/data/remote/PahoFactoryMqtt.kt:39-56` `start()` 换连接时只 `disconnectForcibly` 已登记的 `client`；`open()`（`:58`）里新建的 `MqttAsyncClient` 在 `connect` 完成前才登记、且开了 `isAutomaticReconnect`，`start` 重入（同值 spec 视为未变）时旧客户端没关掉，继续自动重连，与新客户端用同一 clientId 互踢。

## D-12 S4 示教器登录「账号已停用」显示英文 account disabled（C-04）
- 复现：示教器登录停用账号 hangong5（密码正确）→ 提示框显示 `account disabled`。
- 预期：中文提示（如「账号已停用，请联系管理员」）。厂端已停业务（factory disabled 等）错误码同样没有翻译。
- 位置：`client/ShiJiaoQi-New/app/src/main/kotlin/com/gbndt/shijiaoqi/data/session/BagSession.kt:1231` `translate()` 没有 `account disabled` 等分支，原样回显英文。

## D-13 S3 记住的密码明文存在 DataStore（B-07、C-05、S-07）
- 复现：登录框勾「记住账号密码」登录成功后，`adb shell run-as com.gbndt.shijiaoqi cat files/datastore/wmesh.device.preferences_pb | strings` 读到 `loginPassword hangong1123456`。
- 预期：加密存或改存令牌；设备被拿走即可读出员工口令（员工多半在多处复用）。
- 位置：`client/ShiJiaoQi-New/app/src/main/kotlin/com/gbndt/shijiaoqi/data/prefs/DeviceSettingsStore.kt:72` `stringPreferencesKey("loginPassword")` 直接写明文。

## D-14 S3 示教器管理员在厂级树根新建文件夹无效（C-28、N-12 相关）
- 复现：gongyi1（管理员）示教器登录 → 单层 → 工程管理 → 厂级 → 右上「新建文件夹」→ 填 admindir → 创建：弹框关闭，厂端日志没有 `POST …/fs/folders`，厂级目录也没有新文件夹（3 次）。提示被 MQTT 抖动刷掉的日志盖住，没取到。
- 预期：管理员能在厂级树建文件夹（方案 C-28）。
- 位置（推测）：`client/ShiJiaoQi-New/app/src/main/kotlin/com/gbndt/shijiaoqi/data/repository/PouchRepository.kt:166` `folderId(parentId)` 对树根路径取不到文件夹身份就抛 `ERR_NOT_FOUND`，而 `PouchFolders.canMkdir`（`ui/project/PouchExplorer.kt:143`）在根上又显示了按钮。

## 观察 O-1（偶现，未复现）切换账号后激活工程无反应
- hangong1 退出→gongyi1 登录后，单层/多层打开任一工程只记 `activate project=…`，之后无结果、无提示，需杀进程重开才恢复；再换回 hangong1 未复现。疑与 D-11 的 MQTT 抖动造成的锁竞争有关（logcat 有 `Long monitor contention … PahoFactoryMqtt.start`）。

## D-15 S3 操作员焊一次厂级多层工程，就自动另存出个人工程并复制 8 份重复工艺（C-52、C-50）
- 复现：hangong1 打开厂级「第八轴-多层」，模拟焊接 2 次、起弧焊接 1 次后停止，回首页同步。厂端随即出现个人工程 GC-C0001-000002「第八轴-多层」（12 个依赖）和 8 条个人工艺 GY-C0001-000004～011：默认工艺 ×4、3K7.5-8.5mm打底-1 ×2、3K10-11mm-2 ×2，创建时间都是 13:36:00。操作员没有改任何参数。
- 预期：焊接只更新层完成标记，不应把厂级工程变成个人副本；即使要另存，也不该把同一工艺复制多份。否则每个操作员每焊一次厂级多层工程，个人树就多一套重复件，下次打开的也不再是厂级原件。
- 位置：`client/ShiJiaoQi-New/app/src/main/kotlin/com/gbndt/shijiaoqi/ui/welding/multilayer/MultiLayerWeldViewModel.kt:724` `saveCurrentProject()` 在焊接过程中被调用（层完成标记写回工程），`pouch.saveMultiLayerPaths` 对每一层的工艺各另存一份、不去重，`pouch.saveProject` 对厂级工程走「另存为个人」。

## D-16 S4 改了焊道所用工艺后，焊道卡片仍显示旧参数
- 复现：ctestxin 的焊道用个人工艺 1K6.5mmD（250A）；工艺管理里改成 245A 保存，回到焊道，卡片仍是「1K6.5mmD | 250.0A | …」，但下发的 Lua 已是 245（G-04）。
- 预期：卡片随工艺刷新，避免操作工看错参数。

## D-17 S4 工具编辑框标题差 1（B-03）
- 复现：状态栏「工具」→ 工具0 →「未设置（点击设置）」→ 标题「修改工具1参数」。
- 位置：`client/ShiJiaoQi-New/app/src/main/kotlin/com/gbndt/shijiaoqi/ui/welding/ToolDialogs.kt:369` `修改工具${index + 1}参数`，列表用的是 `工具$index`。

## D-18 S2 运行中误触手动「起弧」：先起弧、松手发 STOP 停掉程序（B-06）
- 复现：第七轴模拟焊接运行中（状态 2、行 12/94），按一下右栏手动区「起弧」0.8 秒 → fake.log `ARCStart(0,0,10000)` → `STOP` → 3 次 Mode(1)，程序停在第 12 行。
- 预期：程序运行中手动起弧按钮应禁用；松手不应发 STOP 停整个程序。模拟焊接本不该起弧，误触会真起弧；起弧焊接中误触会把正在焊的程序停掉。
- 位置：`client/ShiJiaoQi-New/app/src/main/kotlin/com/gbndt/shijiaoqi/ui/welding/single/WeldPathScreen.kt:416`、`ui/welding/tbar/TBarScreen.kt:470` `HoldButton("起弧", onPress = ARCStart, onRelease = STOP)`，没有按运行状态禁用。

## D-19 S3 断流窗口内采点不拒绝；位姿为空时写假位姿 (100,200,300)（B-05）
- 复现：`mute` 后 0.3 秒点「采集数据」→ 采点成功无提示，写入断流前最后一帧位姿。
- 预期：8083 停推（位姿不新鲜）时拒绝采点并提示；代码兜底写假位姿更危险（下次走到点会去 100,200,300）。
- 位置：`client/ShiJiaoQi-New/app/src/main/kotlin/com/gbndt/shijiaoqi/ui/welding/single/SingleWeldViewModel.kt:560-573`（多层 `MultiLayerWeldViewModel.kt:586`、T 排 `TBarWeldViewModel.kt:702` 同）：只看 `connectionStatus == DOWN`（1 秒判定），`Capture.snapshot` 为空时写 `Pose(100.0, 200.0, 300.0…)`。

## D-20 S2 厂端不下发 T 排/包角口令，T 排进不去、包角几何解不了锁（B-15）
- 复现：示教器首页 T 排卡片 → 输任意口令 →「密码错误」；`POST /pad/login` 回包 policy = {revision, maxCachedProjects, cacheScope, persistUnwrapKey, keyTtlSeconds, encryptPouch, extra:{}}，没有 cornerPassword。
- 预期：厂端策略有口令设置并随策略下发（方案 C-13、I-18）。
- 位置：厂端 `factory/server/internal/store/policy.go:19` `ClientPolicy` 及策略页缺字段；示教器 `client/ShiJiaoQi-New/app/src/main/kotlin/com/gbndt/shijiaoqi/data/session/BagSession.kt:185/228` 从顶层读，`extra` 里放也不认。
- 影响：17 条用例阻塞（C-13、C-36、C-37、C-60～C-69、R-27、G-03、I-18、B-01、B-02）。

## D-21 S4 多层「删除中间点」可用条件运算优先级错（B-04）
- 位置：`client/ShiJiaoQi-New/app/src/main/kotlin/com/gbndt/shijiaoqi/ui/welding/multilayer/MultiLayerScreen.kt:388` `enabled = isPointMode && selectedBasePoint?.type == MIDDLE || selectedBasePoint?.type == ARC_MIDDLE`，选中圆弧点时不在点位模式也可用；应加括号。（代码确认，UI 未构造）

## D-22 S3 程序超过 255 行，焊长统计少记（R-11、B-09、E-31）
- 复现：厂级 28 道「R11大工程」（432 行 Lua）起弧焊接跑完，厂端焊次 3090mm；同一 7 道单份是 801mm，4 份应约 3204mm，少约一道。
- 原因：8083 帧当前行只有 1 字节（假控制器 `fake_robot.py:113` `b[177] = cur_line & 0xFF`，示教器按此解析），行号过 255 回绕，跟行命中错位。需按 §12 抓真控制器帧确认行号字段宽度；若真机也只有 1 字节，示教器需按回绕次数累加行号。
- 位置：示教器 8083 解析 `data/robot/protocol/Status8083.kt` 与各焊接 ViewModel 的跟行（`follow` 命中 startStats/stopStats）。

## D-23 S3 示教器设备名录只在登录时拉，同步不刷新（B-12）
- 复现：hangong1 登录中，WAN 给 A 厂新登记 C0005（FA:KE:00:00:00:31）；示教器回首页同步、甚至重启 App（恢复会话）后连这台臂，仍「设备号未在本厂登记」断开；退出重新登录再重启 App 才匹配上。
- 预期：首页同步时刷新名录；否则新臂上线必须让每个操作工重新登录。
- 另：设备号不符断开后示教器不再自动重连，只能重启 App（见 R-04、D-07）。

## D-24 S2 T 排间隙带工艺写错带（B-02，代码确认，T 排因 B-15 无法实测）
- 位置：`client/ShiJiaoQi-New/app/src/main/kotlin/com/gbndt/shijiaoqi/ui/welding/tbar/TBarWeldViewModel.kt:454-455` 界面用排序后的 `currentGapBands()`，`:457-476 bindGapBandProcess`、`:482-495 bindCapProcess` 用同一下标写 `path.gapBands`（未排序）。gapBands 存储顺序与 (layer, minGap) 不一致时，给第 1 条配的打底/盖面落到别的带上，焊错工艺。

## D-25（需求待定）T 排断点续焊未实现（B-01）
- `TBarWeldViewModel.kt:1052` 写 StopResume、`:1149` 清空，没有读取方。

## D-26 S3 焊次上报接受未来时间（E-09）
- 复现：`POST /pad/weld-facts` 带 `occurredAt: 2099-01-01T00:00:00Z` → accepted，厂库出现 2099 年的焊次，报表日期被污染。
- 预期：拒收明显超前的时间（如超过服务器时间若干分钟），或至少按接收时刻纠正。

## D-11 补充
- 厂端 `person_login_logs` 里 hangong1 一小时内 200 条登录记录（单分钟最多 44 条），示教器把每次重连都当登录上报（`POST /pad/login-log` 一秒多次），登录记录页基本不可读。

## D-27 S3 厂端「按日」按本地时区、WAN 按 UTC 日，跨零点焊次两边日期不一致（E-20、E-21）
- 复现：焊次 occurredAt 2026-11-02T00:01+08:00 与 03:30+08:00 两条；厂端报表 group=day&tz=Asia/Shanghai 计入 11-02，WAN /v1/weld-reports 计入 11-01（同一厂同一工程）。厂端不带 tz 时又按 UTC。北京时间 0～8 点焊的都会错一天。
- 预期：方案 E-20「按日报表归属规则明确，厂端和 WAN 一致」。
- 位置：厂端上送汇总 `factory/server/internal/wanchannel/weld.go:15`、`service/stats.go:62` 固定按 UTC 日；厂端网页报表按浏览器/参数时区。

## D-28 S2 示教器不应用登录后下发的策略，改策略要等重新登录才生效（F-21、I-11）
- 复现：厂端改策略（TTL 0→120、解封钥不落盘），厂端经 MQTT 推 `{"typ":"policy","revision":3,…,"sig":…}`（抓包已收到）；在线示教器 3 分钟内没有任何变化、TTL 不生效；退出重新登录后才按新策略。
- 预期：在线示教器收到策略推送立即生效（到期登出、重启恢复规则）。收紧安全策略（缩短时效、禁止落盘）现在对已登录设备无效，直到操作工自己退出。
- 位置：`client/ShiJiaoQi-New/app/src/main/kotlin/com/gbndt/shijiaoqi/data/remote/PahoFactoryMqtt.kt` `messageArrived` 只触发同步、不解析 payload；同步拉 `/pad/inbox` 时 `HttpFactoryGateway.kt:394` 解出了 `policy`，但 `BagSession` 没有任何地方使用 `inbox.policy`，策略只在登录时 `BagSession.kt:184-191` 保存。

## D-29 S2 资产多时示教器首次同步卡死，遮罩一直不退、登录时效也不生效（E-32、C-07）
- 复现：A 厂新增 300 条厂级工艺（共 341 条资产）后 hangong1 登录：同步遮罩「正在拉取「E32批量190」108 / 300」停住 5 分钟不动，厂端日志最后一次 `/pad/closures` 在 22:11:34，之后示教器不再请求；App 日志只剩每 0.5 秒一条 `BagSession sync skip running`（同步标记一直占着）。重启 App 重新登录后续拉到「70 / 193」又停（22:17:42 后无请求）。两次都卡住，遮罩挡住整个界面无法作业。
- 同时：策略 TTL=120 秒，登录后 300 秒未被登出——登录时效的到期任务要拿同一把锁（`SessionRepository.kt:168` `gate.withLock { expireLoginIfDue() }`），被卡住的同步占着。
- 预期：300 条以上资产同步时长可接受、进度正常、不挂死（方案 E-32）。
- 位置（推测）：示教器 `data/session/BagSession.kt:516` 同步互斥 + 闭包拉取循环；可能与 D-11 的 MQTT 抖动造成的锁竞争有关，需开发抓线程栈确认。

## 观察 O-2 认领后立即登录偶发失败
- I-01：`POST /v1/site/claim` 200 后立刻 `POST /v1/factories/{新厂}/login` 回包无 token（脚本 KeyError），几秒后同一账号密码登录 200。疑似新厂库建表/迁移未完成时就放行了登录请求，需看日志确认（未复现第二次）。

## D-30（需求待定）回厂网后停在焊接页焊次不上报（N-09）
- 臂网焊 1 道 → 切厂网停在焊接页 90 秒，厂端 0 条；回首页后才补传。若操作工整班不回首页，焊次长时间不报，厂端报表滞后。建议焊接页检测到厂网恢复也触发补传，或定时补传。
- 补充（N-30）：80 条新工艺同步到 18/80 时切臂网，遮罩 160 秒不退；回厂网后走到 80/80 又卡住 196 秒，只能杀进程。同步中途网络变化后会挂死，方案 N-30/N-36「不长时间卡遮罩、没有僵死遮罩」不满足。
- 补充（第 3 次复现）：pm clear 后首次登录同步，厂里共 138 条（基础 58 + 80 条 N30 工艺），停在「第七轴-单道 91 / 138」超过 60 秒不动。数量不到 300 也会挂。

## D-31 S2（待开发复现）臂网上离线新建的个人工程丢失或建不出来（N-31）
- 第 1 次：臂网上依次新建 pusha、pushb、pushc，回首页推送时断网再恢复，厂端只有 pushc（GC-C0001-000003），本机列表里 pusha、pushb 也消失。
- 第 2 次：臂网上新建 repa、repb，创建后状态栏「工程: 未打开」，列表与厂端都没有。
- 对照：N-06 在臂网上新建 armnew 成功并回传。提示被 D-11 的日志洪水冲掉，没拿到原因；怀疑本机发号（GC-C0001-xxxxxx）在离线时重复，或创建依赖设备短码就绪（`pouchUserMessage` 有「本机短号未就绪」）。

## D-32 S2 Android 10 示教器上 App 自动更新一律「签名不一致」拒装（U-03、C-16、I-15）
- 复现：WAN 发布 client_apk 83（与本机已装包字节完全相同、同一签名证书 CN=WMesh ShiJiaoQi Test，sha256 一致）→ 厂端静默拉取 → 示教器个人中心「检查更新」弹「发现新版本 6.1.32 / 83」→「立即更新」下载成功 → 提示「安装包签名和本机不一致，已拒装」；「再试一次」同样。App 日志 `Update apk signer mismatch`。
- 原因：panogo mini 为 Android 10（SDK 29）。`UpdateManager.sameSigner()` 对下载的安装包用 `getPackageArchiveInfo(path, GET_SIGNING_CERTIFICATES)`，Android 13 以下对未安装的归档文件不填 `signingInfo`，`signersOf` 返回空列表，`signersMatch` 要求非空 → 恒为 false。
- 影响：现场示教器永远无法通过厂服升级 App。
- 位置：`client/ShiJiaoQi-New/app/src/main/kotlin/com/gbndt/shijiaoqi/data/update/UpdateManager.kt:57-61`、`:147-160`（API < 33 需对归档改用 GET_SIGNATURES 或 apksig 自行校验）。

## D-33 S4 WMESH_ADMIN_RESET 不是一次性的（W-05）
- 复现：以 WMESH_ADMIN_RESET=true 启动后管理员密码被打回 .env 值；随后改成新密码，再 restart（变量仍在）→ 又被打回 .env 值。
- 预期：一次性覆盖，之后变量不再生效（或启动后提示删除变量）。
- 位置：`global/server/cmd/server/main.go:273-277` 每次启动只看 `cfg.AdminReset` 就重置，没有记录「已执行过」。

## D-34（需求待定）臂网上连臂匹配不上报、回厂网也不补报（B-11）
- 臂网 826 匹配成功后回厂网回首页 20 秒，厂端 person_login_logs 该时段 0 条。厂端看不到操作工在哪台臂上作业（登录记录、最近使用设备只反映厂网登录）。

## D-35 S3（待真机定性）焊接时长严重偏短（B-14、R-10）
- 7 段起弧约 110 秒只记 3 秒；432 行工程记 2 秒；但断线重连那一次（N-33）记了 78 秒。示教器按「起弧行到收弧行之间每秒计 1」累计，依赖 8083 当前行语义。抓真机 8083 确认后，要么改假控制器，要么改为按起弧/收弧时刻差计时。
- 位置：`client/ShiJiaoQi-New/app/src/main/kotlin/com/gbndt/shijiaoqi/ui/welding/single/SingleWeldViewModel.kt:1052-1062、1141-1153`（多层、T 排同）。
