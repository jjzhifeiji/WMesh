package com.gbndt.shijiaoqi.ui.welding.tbar

import com.gbndt.shijiaoqi.data.log.PadLog
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.gbndt.shijiaoqi.data.prefs.DeviceSettingsStore
import com.gbndt.shijiaoqi.data.repository.PouchRepository
import com.gbndt.shijiaoqi.data.repository.RobotRepository
import com.gbndt.shijiaoqi.data.robot.protocol.FrPacket
import com.gbndt.shijiaoqi.data.robot.protocol.RobotLink
import com.gbndt.shijiaoqi.domain.shared.ScriptPoint
import com.gbndt.shijiaoqi.domain.shared.StopResume
import com.gbndt.shijiaoqi.domain.tbar.TBarLua
import com.gbndt.shijiaoqi.domain.tbar.TBarScriptPath
import com.gbndt.shijiaoqi.domain.shared.WeldLineFollow
import com.gbndt.shijiaoqi.domain.shared.WeldRun
import com.gbndt.shijiaoqi.domain.tbar.toLuaLine
import com.gbndt.shijiaoqi.domain.shared.Capture
import com.gbndt.shijiaoqi.domain.shared.ProcessRef
import com.gbndt.shijiaoqi.data.crypt.Wm2
import com.gbndt.shijiaoqi.domain.tbar.TBarGeometry
import com.gbndt.shijiaoqi.domain.tbar.TBarPass
import com.gbndt.shijiaoqi.domain.tbar.TBarProject
import com.gbndt.shijiaoqi.domain.tbar.TBarRun
import com.gbndt.shijiaoqi.domain.shared.WeldProgress
import com.gbndt.shijiaoqi.model.tbar.GapBand
import com.gbndt.shijiaoqi.model.Pose
import com.gbndt.shijiaoqi.model.RobotError
import com.gbndt.shijiaoqi.model.RobotErrorCodes
import com.gbndt.shijiaoqi.model.single.WeldPath
import com.gbndt.shijiaoqi.model.WeldPoint
import com.gbndt.shijiaoqi.model.WeldPointType
import com.gbndt.shijiaoqi.model.WeldProcess
import com.gbndt.shijiaoqi.model.tbar.isTBarCollectable
import com.gbndt.shijiaoqi.ui.teach.TeachSession
import com.gbndt.shijiaoqi.ui.welding.FineTuneSupport
import com.gbndt.shijiaoqi.ui.welding.TBarUiState
import com.gbndt.shijiaoqi.ui.welding.WeldPad
import com.gbndt.shijiaoqi.ui.welding.WeldRunUi
import com.gbndt.shijiaoqi.ui.welding.WeldShellHost
import com.gbndt.shijiaoqi.ui.welding.WeldShellUi
import com.gbndt.shijiaoqi.ui.welding.asUiPath
import com.gbndt.shijiaoqi.ui.welding.busy
import com.gbndt.shijiaoqi.ui.welding.isPaused
import com.gbndt.shijiaoqi.ui.welding.isSimulating
import com.gbndt.shijiaoqi.ui.welding.isWelding
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.async
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.merge
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeoutOrNull
import java.util.Locale
import java.util.UUID
import javax.inject.Inject

/** T 排焊道协调器：袋、采点、开焊。点动在示教，几何和 Lua 在 domain。 */
@HiltViewModel
class TBarWeldViewModel @Inject constructor(
    private val robot: RobotRepository,
    private val pouch: PouchRepository,
    private val settings: DeviceSettingsStore,
    private val teach: TeachSession,
) : ViewModel(), WeldPad, WeldShellHost {

    private val _toast = MutableSharedFlow<String>()
    override val toastEvent = _toast.asSharedFlow()
    private val _scrollToIndex = MutableSharedFlow<Int>()
    val scrollToIndexEvent = _scrollToIndex.asSharedFlow()

    private var commandId = 100
    private var pouchProjectId: UUID? = null
    private var recording = false
    private var timerJob: Job? = null
    private var programStarted = false
    private var stopResume: StopResume? = null
    private val follow = WeldLineFollow()

    val weldPaths = mutableStateListOf<WeldPath>()
    private val robotErrors = mutableStateListOf<RobotError>()
    private var starting = false

    private val _uiState = MutableStateFlow(TBarUiState())
    val uiState: StateFlow<TBarUiState> = combine(
        _uiState,
        pouch.projects,
        pouch.processes,
    ) { s, projects, processes ->
        s.copy(shell = s.shell.copy(pouchProjects = projects, pouchProcesses = processes))
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), TBarUiState())
    override val shellUi: StateFlow<WeldShellUi> =
        uiState.map { it.shell }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), WeldShellUi())

    var selectedWeldPathIndex: Int
        get() = _uiState.value.selectedWeldPathIndex
        set(v) { patch { it.copy(selectedWeldPathIndex = v) } }
    var listScrollIndex: Int
        get() = _uiState.value.listScrollIndex
        set(v) { patch { it.copy(listScrollIndex = v) } }
    var listScrollOffset: Int
        get() = _uiState.value.listScrollOffset
        set(v) { patch { it.copy(listScrollOffset = v) } }
    val isWelding: Boolean get() = _uiState.value.run.isWelding
    val isSimulating: Boolean get() = _uiState.value.run.isSimulating
    val isPaused: Boolean get() = _uiState.value.run.isPaused
    var weldingLength: Double
        get() = _uiState.value.weldingLength
        set(v) { patch { it.copy(weldingLength = v) } }
    var weldingDuration: Long
        get() = _uiState.value.weldingDuration
        set(v) { patch { it.copy(weldingDuration = v) } }
    var isMissingProcessDialogVisible: Boolean
        get() = _uiState.value.isMissingProcessDialogVisible
        set(v) { patch { it.copy(isMissingProcessDialogVisible = v) } }
    var missingProcessMessage: String
        get() = _uiState.value.missingProcessMessage
        set(v) { patch { it.copy(missingProcessMessage = v) } }
    var isRenameDialogVisible: Boolean
        get() = _uiState.value.isRenameDialogVisible
        set(v) { patch { it.copy(isRenameDialogVisible = v) } }
    var newWeldPathName: String
        get() = _uiState.value.newWeldPathName
        set(v) { patch { it.copy(newWeldPathName = v) } }
    var isCurrentVoltageDialogVisible: Boolean
        get() = _uiState.value.isCurrentVoltageDialogVisible
        set(v) { patch { it.copy(isCurrentVoltageDialogVisible = v) } }
    var inputCurrent: String
        get() = _uiState.value.inputCurrent
        set(v) { patch { it.copy(inputCurrent = v) } }
    var inputVoltage: String
        get() = _uiState.value.inputVoltage
        set(v) { patch { it.copy(inputVoltage = v) } }
    var savedCurrent: Double
        get() = _uiState.value.savedCurrent
        set(v) { patch { it.copy(savedCurrent = v) } }
    var savedVoltage: Double
        get() = _uiState.value.savedVoltage
        set(v) { patch { it.copy(savedVoltage = v) } }
    var isRobotErrorDialogVisible: Boolean
        get() = _uiState.value.isRobotErrorDialogVisible
        set(v) { patch { it.copy(isRobotErrorDialogVisible = v) } }
    val currentRobotErrors get() = robotErrors
    var operationPosition by mutableStateOf(Pose(0.0, 0.0, 0.0, 0.0, 0.0, 0.0))

    val isControllerActive: Boolean get() = teach.isControllerActive

    val fineTune = FineTuneSupport(
        scope = viewModelScope,
        socketManager = robot,
        nextCommandId = { commandId++ },
        toast = { msg -> viewModelScope.launch { _toast.emit(msg) } },
        currentProcess = { currentProcess() },
        onOscillationChanged = { osc ->
            currentProcess()?.oscillation = osc
            saveCurrentProject()
        },
    )

    init {
        val snap = settings.loadAppSettings()
        patch {
            it.copy(
                weldingLength = snap.totalWeldingLength,
                weldingDuration = snap.totalWeldingDuration,
                savedCurrent = snap.weldingCurrent,
                savedVoltage = snap.weldingVoltage,
            )
        }
        viewModelScope.launch { robot.robotPose.collect { p -> if (p != null) operationPosition = p } }
        viewModelScope.launch {
            robot.weldingBreakOffState.collect { v ->
                patch { it.copy(shell = it.shell.copy(weldingBreakOffState = v)) }
            }
        }
        viewModelScope.launch {
            robot.weldArcState.collect { v ->
                patch { it.copy(shell = it.shell.copy(weldArcState = v)) }
            }
        }
        viewModelScope.launch {
            robot.robotErrorEvent.collect { code ->
                val err = RobotErrorCodes.map[code] ?: RobotError(code, "未知故障", "请参考手册")
                if (!robotErrors.contains(err)) robotErrors.add(err)
                isRobotErrorDialogVisible = true
                publish()
            }
        }
        viewModelScope.launch {
            robot.robotInputSignal.collect { combined ->
                when {
                    combined in 2048..2248 && !recording -> {
                        collectData()
                        recording = true
                    }
                    combined > 4096 && recording -> recording = false
                }
            }
        }
        viewModelScope.launch {
            robot.progCurLine.collect { line ->
                applyHits(follow.onProgLine(line, _uiState.value.run.busy) { p, i ->
                    weldPaths.getOrNull(p)?.points?.getOrNull(i)?.type
                })
            }
        }
        viewModelScope.launch {
            robot.programState.collect { state ->
                val run = _uiState.value.run
                if (run !is WeldRunUi.Active) return@collect
                when (state) {
                    2 -> programStarted = true
                    4, 5 -> if (!run.paused) patch { it.copy(run = run.copy(paused = true)) }
                    1 -> if (programStarted && !run.paused) {
                        if (_uiState.value.shell.weldingBreakOffState == "中断") {
                            patch { it.copy(run = run.copy(paused = true)) }
                        } else {
                            finishRun()
                        }
                    }
                }
            }
        }
        addWeldPath()
    }

    override suspend fun syncFromPouch() {
        val id = pouch.activeProjectId() ?: return
        val bytes = pouch.openProjectBytes(id) ?: return
        try {
            val loaded = runCatching { TBarProject.parse(bytes) }.getOrElse {
                PadLog.error("TBarWeld", "active project parse failed", it)
                emptyList()
            }
            weldPaths.clear()
            weldPaths.addAll(loaded.map { it.asUiPath() })
            if (weldPaths.isEmpty()) addWeldPath()
            selectedWeldPathIndex = 0
            pouchProjectId = id
            patch { it.copy(shell = it.shell.copy(currentProjectName = pouch.projectName(id))) }
            bindProcesses()
        } finally {
            Wm2.zero(bytes)
        }
        publish()
    }

    override fun activatePouchProject(id: UUID) {
        PadLog.info("TBarWeld", "activate project=$id")
        viewModelScope.launch {
            runCatching {
                pouch.activate(id)
                syncFromPouch()
            }.onFailure { e -> toast(e.message ?: "无法激活工程") }
        }
    }

    override fun refreshPouchLists() {
        viewModelScope.launch { pouch.refresh() }
    }

    override fun bindProcessFromPouch(processId: UUID?) {
        // T 排工艺走间隙带，不绑焊道主工艺。
    }

    fun currentGapBands(): List<GapBand> =
        (currentPath()?.gapBands ?: emptyList()).sortedWith(compareBy({ it.layer }, { it.minGap }))

    fun bindGapBandProcess(bandIndex: Int, pass: TBarPass, processId: UUID?) {
        viewModelScope.launch {
            val path = currentPath() ?: return@launch
            val bands = path.gapBands.toMutableList()
            if (bandIndex !in bands.indices) return@launch
            val idStr = processId?.toString().orEmpty()
            val loaded = if (processId == null) WeldProcess() else {
                pouch.openProcess(processId) ?: run {
                    missingProcessMessage = "闭包里没有这条工艺"
                    isMissingProcessDialogVisible = true
                    return@launch
                }
            }
            val old = bands[bandIndex]
            bands[bandIndex] = if (pass == TBarPass.ROOT) {
                old.copy(rootProcessId = idStr, rootProcess = loaded)
            } else {
                old.copy(capProcessId = idStr, capProcess = loaded)
            }
            weldPaths[selectedWeldPathIndex] = path.copy(gapBands = bands)
            saveCurrentProject()
        }
    }

    fun addWeldPath() {
        val points = mutableStateListOf(
            WeldPoint(UUID.randomUUID().toString(), WeldPointType.START_SAFE),
            WeldPoint(UUID.randomUUID().toString(), WeldPointType.GROOVE_A_LOWER),
            WeldPoint(UUID.randomUUID().toString(), WeldPointType.GROOVE_B_LOWER),
            WeldPoint(UUID.randomUUID().toString(), WeldPointType.GROOVE_A_UPPER),
            WeldPoint(UUID.randomUUID().toString(), WeldPointType.GROOVE_B_UPPER),
            WeldPoint(UUID.randomUUID().toString(), WeldPointType.START),
            WeldPoint(UUID.randomUUID().toString(), WeldPointType.END),
            WeldPoint(UUID.randomUUID().toString(), WeldPointType.END_SAFE),
        )
        weldPaths.add(
            WeldPath(
                id = UUID.randomUUID().toString(),
                name = "焊道 ${weldPaths.size + 1}",
                points = points,
                process = WeldProcess(),
                gapBands = weldPaths.firstOrNull()?.gapBands.orEmpty(),
            ).asUiPath(),
        )
        selectedWeldPathIndex = weldPaths.lastIndex
        saveCurrentProject()
        viewModelScope.launch { _scrollToIndex.emit(weldPaths.lastIndex) }
    }

    fun deleteWeldPath(index: Int) {
        if (index !in weldPaths.indices) return
        weldPaths.removeAt(index)
        if (selectedWeldPathIndex >= weldPaths.size) {
            selectedWeldPathIndex = weldPaths.lastIndex.coerceAtLeast(0)
        }
        saveCurrentProject()
    }

    fun moveWeldPath(from: Int, to: Int) {
        if (from !in weldPaths.indices || to !in weldPaths.indices) return
        weldPaths.add(to, weldPaths.removeAt(from))
        selectedWeldPathIndex = to
        saveCurrentProject()
    }

    fun renameWeldPath(name: String) {
        if (name.isEmpty()) return
        val path = currentPath() ?: return
        weldPaths[selectedWeldPathIndex] = path.copy(name = name)
        saveCurrentProject()
    }

    fun toggleWeldPathEnabled(index: Int) {
        val path = weldPaths.getOrNull(index) ?: return
        weldPaths[index] = path.copy(isEnabled = !path.isEnabled)
        saveCurrentProject()
    }

    fun snapToCollectablePoint(weldPathIndex: Int) {
        val path = weldPaths.getOrNull(weldPathIndex) ?: return
        val current = path.points.getOrNull(path.selectedPointIndex)
        if (current != null && current.type.isTBarCollectable()) return
        val idx = path.points.indexOfFirst { it.type.isTBarCollectable() }
        if (idx >= 0 && idx != path.selectedPointIndex) {
            weldPaths[weldPathIndex] = path.copy(selectedPointIndex = idx)
        }
    }

    fun selectPoint(pathIndex: Int, pointIndex: Int) {
        val path = weldPaths.getOrNull(pathIndex) ?: return
        val point = path.points.getOrNull(pointIndex) ?: return
        if (!point.type.isTBarCollectable()) return
        selectedWeldPathIndex = pathIndex
        if (path.selectedPointIndex != pointIndex) {
            weldPaths[pathIndex] = path.copy(selectedPointIndex = pointIndex)
        }
    }

    fun deletePoint(pathIndex: Int, pointIndex: Int) {
        val path = weldPaths.getOrNull(pathIndex) ?: return
        if (pointIndex !in path.points.indices) return
        val point = path.points[pointIndex]
        if (point.type != WeldPointType.MIDDLE && point.type != WeldPointType.ARC_MIDDLE) return
        path.points.removeAt(pointIndex)
        saveCurrentProject()
    }

    fun tBarGapText(path: WeldPath?): String {
        if (path == null) return ""
        val aL = path.points.firstOrNull { it.type == WeldPointType.GROOVE_A_LOWER }?.pose
        val bL = path.points.firstOrNull { it.type == WeldPointType.GROOVE_B_LOWER }?.pose
        val aU = path.points.firstOrNull { it.type == WeldPointType.GROOVE_A_UPPER }?.pose
        val bU = path.points.firstOrNull { it.type == WeldPointType.GROOVE_B_UPPER }?.pose
        if (aL == null || bL == null || aU == null || bU == null) return "坡口点未采齐"
        val g0 = TBarGeometry.gapAt(aL, aU, bL, bU, 0.0)
        val g1 = TBarGeometry.gapAt(aL, aU, bL, bU, 1.0)
        val bands = path.gapBands
        val f0 = TBarRun.matchBand(g0, bands, 1)?.let { TBarRun.bandLabel(it) } ?: "无匹配"
        val f1 = TBarRun.matchBand(g1, bands, 1)?.let { TBarRun.bandLabel(it) } ?: "无匹配"
        return "起点间隙 ${"%.1f".format(g0)} mm($f0)  终点间隙 ${"%.1f".format(g1)} mm($f1)  先打底后盖面"
    }

    override fun collectData() {
        PadLog.info("TBarWeld", "collect point")
        if (teach.connectionStatus == RobotLink.DOWN) {
            toast("设备未连接")
            return
        }
        val path = currentPath() ?: return
        val i = path.selectedPointIndex
        if (i !in path.points.indices) return
        val point = path.points[i]
        if (!point.type.isTBarCollectable()) {
            toast("起点终点由坡口点计算，请采集起安、A下、B下、A上、B上、终安")
            return
        }
        val snap = Capture.snapshot(robot.robotPose.value, robot.robotJoints.value)
        path.points[i] = if (snap != null) {
            point.copy(pose = snap.first, jointAngles = snap.second)
        } else {
            point.copy(pose = Pose(100.0, 200.0, 300.0, 0.0, 0.0, 0.0), jointAngles = List(6) { 0.0 })
        }
        rebuildComputed(path)
        val next = (i + 1 until path.points.size).firstOrNull { path.points[it].type.isTBarCollectable() } ?: i
        if (next != i) {
            weldPaths[selectedWeldPathIndex] = path.copy(selectedPointIndex = next)
        } else {
            weldPaths[selectedWeldPathIndex] = path
        }
        saveCurrentProject()
    }

    fun clearPointData() {
        val path = currentPath() ?: return
        val i = path.selectedPointIndex
        if (i !in path.points.indices) return
        val point = path.points[i]
        if (!point.type.isTBarCollectable()) return
        path.points[i] = point.copy(pose = null, jointAngles = null)
        rebuildComputed(path)
        saveCurrentProject()
    }

    override fun sendMoveLCommand() {
        val point = currentPath()?.points?.getOrNull(currentPath()?.selectedPointIndex ?: -1) ?: return
        val pose = point.pose ?: return
        sendMoveL(pose, point.jointAngles)
    }

    override fun startSimulation() {
        PadLog.info("TBarWeld", "start simulation")
        startRun(welding = false, simulating = true)
    }

    override fun startArcWelding() {
        PadLog.info("TBarWeld", "start arc")
        startRun(welding = true, simulating = false)
    }

    fun pauseWelding() {
        PadLog.info("TBarWeld", "pause")
        if (!_uiState.value.run.busy) return
        viewModelScope.launch {
            WeldRun.pauseSeq().forEach {
                robot.sendControlCommand(it)
                delay(WeldRun.AFTER_PAUSE_MS)
            }
        }
        val run = _uiState.value.run
        if (run is WeldRunUi.Active) patch { it.copy(run = run.copy(paused = true)) }
    }

    fun continueWelding() {
        PadLog.info("TBarWeld", "continue")
        if (!_uiState.value.run.isPaused) return
        viewModelScope.launch {
            val run = _uiState.value.run
            if (run is WeldRunUi.Active) patch { it.copy(run = run.copy(paused = false)) }
            WeldRun.resumeSeq(_uiState.value.shell.weldingBreakOffState == "中断").forEach {
                robot.sendControlCommand(it)
                delay(50)
            }
        }
    }

    override fun stopWelding(force: Boolean) {
        PadLog.info("TBarWeld", "stop force=$force")
        val pose = robot.robotPose.value ?: operationPosition
        val joints = robot.robotJoints.value
        val pathIdx = if (follow.lastPath >= 0) follow.lastPath else selectedWeldPathIndex
        val pointIdx = if (follow.lastPoint >= 0) follow.lastPoint else currentPath()?.selectedPointIndex ?: -1
        if (joints.size >= 6) {
            stopResume = StopResume(pose, joints, pathIdx, pointIdx)
        }
        if (_uiState.value.run.isWelding && follow.statsActive) {
            val last = weldPaths.getOrNull(follow.lastPath)?.points?.getOrNull(follow.lastPoint)?.pose
            weldingLength += WeldProgress.linearMm(last, pose) / 1000.0
        }
        finishRun(sendStop = true)
    }

    fun sendManualCommand(type: Int, command: String) {
        robot.sendControlCommand(FrPacket.encode(commandId++, type, command))
    }

    fun setWeldingCurrentVoltage() {
        val c = inputCurrent.toDoubleOrNull()
        val v = inputVoltage.toDoubleOrNull()
        if (c == null || c !in 0.0..1000.0) {
            toast("电流值无效 (0-1000)")
            return
        }
        if (v == null || v !in 0.0..1000.0) {
            toast("电压值无效 (0-1000)")
            return
        }
        savedCurrent = c
        savedVoltage = v
        isCurrentVoltageDialogVisible = false
        persistStats()
    }

    fun clearStats() {
        weldingLength = 0.0
        weldingDuration = 0L
        persistStats()
    }

    fun beginAddProcessVariant(pathIndex: Int) {
        selectedWeldPathIndex = pathIndex
    }

    fun beginReplaceExtraProcess(pathIndex: Int, extraIndex: Int) {
        selectedWeldPathIndex = pathIndex
    }

    fun toggleExtraProcessEnabled(pathIndex: Int, extraIndex: Int) = Unit

    fun deleteExtraProcess(pathIndex: Int, extraIndex: Int) = Unit

    fun saveCurrentProject() {
        val id = pouchProjectId ?: return
        val paths = weldPaths.toList()
        publish()
        viewModelScope.launch {
            runCatching {
                pouch.saveWeldPaths(paths)
                pouch.saveProject(id, TBarProject.encode(paths))
            }.onFailure { e -> toast(e.message ?: "无法保存工程") }
        }
    }

    private fun startRun(welding: Boolean, simulating: Boolean) {
        if (_uiState.value.run.busy || starting) return
        starting = true
        viewModelScope.launch {
            try {
                if (!bindProcesses()) return@launch
                val scripts = toScripts() ?: return@launch
                pouch.factoryArmError()?.let { toast(it); return@launch }
                val resume = stopResume
                stopResume = null
                val lua = try {
                    TBarLua.job(
                        scripts,
                        welding,
                        simulating,
                        teach.speedMode,
                        teach.toolIndex,
                        teach.isExtAxisEnabled,
                        resume,
                    )
                } catch (e: Exception) {
                    missingProcessMessage = e.message ?: "无法生成 T 排指令"
                    isMissingProcessDialogVisible = true
                    return@launch
                }
                if (lua.size <= 1) {
                    toast("没有可执行的焊道")
                    return@launch
                }
                teach.stopController()
                programStarted = false
                follow.reset()
                patch { it.copy(run = WeldRunUi.Active(welding = welding, paused = false)) }
                fineTune.resetOffsets()
                pouch.setWelding(true)
                if (welding) startTimer()
                withContext(Dispatchers.IO) {
                    val numbered = WeldRun.number(
                        lua.map { line -> line.toLuaLine(weldPaths.getOrNull(line.pathIndex)?.points.orEmpty()) },
                    ) { commandId++ }
                    follow.load(numbered)
                    val id105 = commandId++
                    val id106 = commandId++
                    val plan = WeldRun.batch(id105, id106, numbered.body)
                    robot.sendBatchCommandSync(plan.fileName)
                    delay(WeldRun.AFTER_FILENAME_MS)
                    val ack = async {
                        withTimeoutOrNull(8000) {
                            merge(robot.receivedText8082, robot.receivedText).first {
                                it.contains(id106.toString()) || it.contains("106")
                            }
                        }
                    }
                    delay(100)
                    robot.sendBatchCommandSync(plan.body)
                    ack.await()
                    delay(WeldRun.AFTER_BODY_ACK_MS)
                    robot.sendControlCommand(plan.modeAuto)
                    delay(WeldRun.AFTER_MODE_MS)
                    robot.sendControlCommand(plan.start)
                }
            } finally {
                starting = false
            }
        }
    }

    private fun toScripts(): List<TBarScriptPath>? {
        val out = ArrayList<TBarScriptPath>()
        for (path in weldPaths) {
            if (!path.isEnabled) continue
            out += toScript(path) ?: return null
        }
        if (out.isEmpty()) {
            toast("没有可执行的焊道")
            return null
        }
        return out
    }

    private fun toScript(path: WeldPath): TBarScriptPath? {
        fun point(type: WeldPointType, missing: String): WeldPoint? {
            val p = path.points.firstOrNull { it.type == type }
            if (p == null) {
                toast("${path.name}: $missing")
                return null
            }
            return p
        }
        fun pose(type: WeldPointType, missing: String): Pose? {
            val p = point(type, missing) ?: return null
            val pose = p.pose
            if (pose == null) {
                toast("${path.name}: $missing")
                return null
            }
            return pose
        }
        val startSafe = point(WeldPointType.START_SAFE, "请先采集起安") ?: return null
        val endSafe = point(WeldPointType.END_SAFE, "请先采集终安") ?: return null
        val startSafePose = startSafe.pose ?: run { toast("${path.name}: 请先采集起安"); return null }
        val endSafePose = endSafe.pose ?: run { toast("${path.name}: 请先采集终安"); return null }
        val aL = pose(WeldPointType.GROOVE_A_LOWER, "请先采集A下") ?: return null
        val bL = pose(WeldPointType.GROOVE_B_LOWER, "请先采集B下") ?: return null
        val aU = pose(WeldPointType.GROOVE_A_UPPER, "请先采集A上") ?: return null
        val bU = pose(WeldPointType.GROOVE_B_UPPER, "请先采集B上") ?: return null
        val startPose = pose(WeldPointType.START, "起点未计算，请采齐坡口点和安全点") ?: return null
        val endPose = pose(WeldPointType.END, "终点未计算，请采齐坡口点和安全点") ?: return null
        if (path.gapBands.none { it.layer == 1 }) {
            missingProcessMessage = "${path.name}: 当前工程没有 1H 间隙带"
            isMissingProcessDialogVisible = true
            return null
        }
        return TBarScriptPath(
            startSafe = ScriptPoint(startSafe.type, startSafePose, startSafe.jointAngles.orEmpty()),
            endSafe = ScriptPoint(endSafe.type, endSafePose, endSafe.jointAngles.orEmpty()),
            aLower = aL,
            bLower = bL,
            aUpper = aU,
            bUpper = bU,
            startPose = startPose,
            endPose = endPose,
            bands = path.gapBands,
        )
    }

    private suspend fun bindProcesses(): Boolean {
        val refs = weldPaths.flatMap { path ->
            path.gapBands.map { band ->
                val label = TBarRun.bandLabel(band)
                listOf(
                    ProcessRef("${path.name} $label 打底", band.rootProcessId, path.isEnabled),
                    ProcessRef("${path.name} $label 盖面", band.capProcessId, path.isEnabled),
                )
            }.flatten()
        }
        val outcome = pouch.resolveProcesses(refs)
        weldPaths.forEachIndexed { i, path ->
            val bands = path.gapBands.map { band ->
                val rootId = band.rootProcessId.toUuidOrNull()
                val capId = band.capProcessId.toUuidOrNull()
                band.copy(
                    rootProcess = rootId?.let { outcome.loaded[it] } ?: band.rootProcess,
                    capProcess = capId?.let { outcome.loaded[it] } ?: band.capProcess,
                )
            }
            val firstRoot = bands.firstOrNull()?.rootProcess
            weldPaths[i] = path.copy(
                gapBands = bands,
                process = firstRoot ?: path.process,
            )
        }
        if (outcome.missing.isNotEmpty()) {
            missingProcessMessage = "以下焊道的工艺不在当前闭包：\n" + outcome.missing.joinToString("\n")
            isMissingProcessDialogVisible = true
            return false
        }
        return true
    }

    private fun rebuildComputed(path: WeldPath) {
        val startSafe = path.points.firstOrNull { it.type == WeldPointType.START_SAFE }?.pose
        val endSafe = path.points.firstOrNull { it.type == WeldPointType.END_SAFE }?.pose
        val aL = path.points.firstOrNull { it.type == WeldPointType.GROOVE_A_LOWER }?.pose
        val bL = path.points.firstOrNull { it.type == WeldPointType.GROOVE_B_LOWER }?.pose
        val aU = path.points.firstOrNull { it.type == WeldPointType.GROOVE_A_UPPER }?.pose
        val bU = path.points.firstOrNull { it.type == WeldPointType.GROOVE_B_UPPER }?.pose
        val computed = try {
            if (startSafe != null && endSafe != null && aL != null && bL != null && aU != null && bU != null) {
                TBarGeometry.buildWeldPoses(aL, bL, aU, bU, startSafe, endSafe)
            } else {
                null
            }
        } catch (t: Throwable) {
            PadLog.error("TBarWeld", "start/end pose failed", t)
            toast("起点终点计算失败")
            null
        }
        val startIdx = path.points.indices.firstOrNull { path.points[it].type == WeldPointType.START }
        val endIdx = path.points.indices.firstOrNull { path.points[it].type == WeldPointType.END }
        if (startIdx != null) {
            path.points[startIdx] = path.points[startIdx].copy(pose = computed?.first, jointAngles = null)
        }
        if (endIdx != null) {
            path.points[endIdx] = path.points[endIdx].copy(pose = computed?.second, jointAngles = null)
        }
    }

    private fun persistStats() {
        val cell = settings.loadAppSettings()
        settings.saveAppSettings(
            cell.copy(
                totalWeldingLength = weldingLength,
                totalWeldingDuration = weldingDuration,
                weldingCurrent = savedCurrent,
                weldingVoltage = savedVoltage,
            ),
        )
    }

    private fun startTimer() {
        timerJob?.cancel()
        timerJob = viewModelScope.launch {
            while (isActive && _uiState.value.run.isWelding) {
                delay(1000)
                val s = _uiState.value
                if (s.run.isWelding && !s.run.isPaused && follow.statsActive) {
                    patch { it.copy(weldingDuration = it.weldingDuration + 1) }
                    persistStats()
                }
            }
        }
    }

    private fun finishRun(sendStop: Boolean = false) {
        programStarted = false
        follow.statsActive = false
        timerJob?.cancel()
        teach.stopController()
        viewModelScope.launch { pouch.setWelding(false) }
        patch { it.copy(run = WeldRunUi.Idle) }
        if (sendStop) {
            viewModelScope.launch {
                WeldRun.stopSeq(_uiState.value.shell.weldingBreakOffState == "中断").forEachIndexed { i, cmd ->
                    robot.sendControlCommand(cmd)
                    if (i == 0) delay(1000)
                }
            }
        }
        persistStats()
        publish()
    }

    private fun applyHits(hits: List<WeldLineFollow.Hit>) {
        if (hits.isEmpty()) return
        var added = 0.0
        for (hit in hits) {
            val path = weldPaths.getOrNull(hit.pathIndex) ?: continue
            if (hit.pointIndex in path.points.indices) {
                weldPaths[hit.pathIndex] = path.copy(selectedPointIndex = hit.pointIndex)
            }
            selectedWeldPathIndex = hit.pathIndex
            if (hit.startStats && _uiState.value.run.isWelding) follow.statsActive = true
            if (hit.addFrom >= 0 && hit.countLength && _uiState.value.run.isWelding) {
                added += WeldProgress.segmentLength(path.points, hit.addFrom, hit.pointIndex)
            }
            if (hit.stopStats) follow.statsActive = false
        }
        if (added > 0) {
            patch { it.copy(weldingLength = it.weldingLength + added / 1000.0) }
            persistStats()
        }
        publish()
    }

    private fun publish(transform: (TBarUiState) -> TBarUiState = { it }) {
        _uiState.update { s ->
            val n = transform(s)
            n.copy(
                weldPaths = weldPaths.toList(),
                currentRobotErrors = robotErrors.toList(),
            )
        }
    }

    private fun patch(transform: (TBarUiState) -> TBarUiState) {
        _uiState.update(transform)
    }

    private fun sendMoveL(pose: Pose, jointsRaw: List<Double>?) {
        val joints = jointsRaw?.takeIf { it.size >= 6 } ?: List(6) { 0.0 }
        val tool = teach.toolIndex
        val ext = String.format(Locale.US, "%.3f", pose.ext1)
        val pos = (joints.take(6) + listOf(pose.x, pose.y, pose.z, pose.rx, pose.ry, pose.rz))
            .joinToString(",") { String.format(Locale.US, "%.3f", it) }
        if (teach.isExtAxisEnabled) {
            robot.sendControlCommand(FrPacket.encode(661, 201, "ExtAxisMoveJ(1,$ext,0.000,0.000,0.000,100,0)"))
        }
        robot.sendControlCommand(
            FrPacket.encode(662, 201, "MoveL($pos,$tool,0,100,100,100,-1,0,$ext,0.000,0.000,0.000,0,0,0,0,0,0,0,0,100,0)"),
        )
    }

    private fun currentProcess(): WeldProcess? {
        val path = currentPath() ?: return null
        val band = path.gapBands.firstOrNull()
        return if (band != null && band.rootProcessId.isNotBlank()) band.rootProcess else path.process
    }

    private fun currentPath(): WeldPath? = weldPaths.getOrNull(selectedWeldPathIndex)

    private fun toast(msg: String) {
        PadLog.toast(msg)
        viewModelScope.launch { _toast.emit(msg) }
    }

    private fun String.toUuidOrNull(): UUID? = runCatching { UUID.fromString(trim()) }.getOrNull()
}
