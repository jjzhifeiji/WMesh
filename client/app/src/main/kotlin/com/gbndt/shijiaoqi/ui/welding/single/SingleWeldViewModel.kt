package com.gbndt.shijiaoqi.ui.welding.single

import com.gbndt.shijiaoqi.data.log.PadLog
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.gbndt.shijiaoqi.config.AppConfig
import com.gbndt.shijiaoqi.data.prefs.DeviceSettingsStore
import com.gbndt.shijiaoqi.data.repository.PouchRepository
import com.gbndt.shijiaoqi.data.repository.RobotRepository
import com.gbndt.shijiaoqi.data.robot.protocol.FrPacket
import com.gbndt.shijiaoqi.data.robot.protocol.RobotCommands
import com.gbndt.shijiaoqi.data.robot.protocol.RobotLink
import com.gbndt.shijiaoqi.domain.shared.ScriptPath
import com.gbndt.shijiaoqi.domain.shared.ScriptPoint
import com.gbndt.shijiaoqi.domain.single.SingleLayerLua
import com.gbndt.shijiaoqi.domain.shared.StopResume
import com.gbndt.shijiaoqi.domain.shared.WeldLineFollow
import com.gbndt.shijiaoqi.domain.shared.WeldRun
import com.gbndt.shijiaoqi.domain.shared.Capture
import com.gbndt.shijiaoqi.domain.single.CornerWeldGenerator
import com.gbndt.shijiaoqi.domain.shared.ProcessRef
import com.gbndt.shijiaoqi.domain.single.SingleLayerProject
import com.gbndt.shijiaoqi.data.crypt.Wm2
import com.gbndt.shijiaoqi.domain.shared.WeldProgress
import com.gbndt.shijiaoqi.domain.shared.toVec3
import com.gbndt.shijiaoqi.geom.Vec3
import com.gbndt.shijiaoqi.model.Pose
import com.gbndt.shijiaoqi.model.RefPoint
import com.gbndt.shijiaoqi.model.RobotError
import com.gbndt.shijiaoqi.model.RobotErrorCodes
import com.gbndt.shijiaoqi.model.single.WeldPath
import com.gbndt.shijiaoqi.model.single.WeldPathProcessSlot
import com.gbndt.shijiaoqi.model.WeldPoint
import com.gbndt.shijiaoqi.model.WeldPointType
import com.gbndt.shijiaoqi.model.WeldProcess
import com.gbndt.shijiaoqi.ui.teach.TeachSession
import com.gbndt.shijiaoqi.ui.welding.FineTuneSupport
import com.gbndt.shijiaoqi.ui.welding.SingleWeldUiState
import com.gbndt.shijiaoqi.ui.welding.WeldPad
import com.gbndt.shijiaoqi.ui.welding.WeldRunUi
import com.gbndt.shijiaoqi.ui.welding.WeldShellHost
import com.gbndt.shijiaoqi.ui.welding.WeldShellUi
import com.gbndt.shijiaoqi.ui.welding.asUiPath
import com.gbndt.shijiaoqi.ui.welding.copyableOf
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

/** 单层焊道协调器：袋、采点、开焊。点动在示教，算法在 domain。 */
@HiltViewModel
class SingleWeldViewModel @Inject constructor(
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
    private var extraProcessEditIndex = -1
    private var recording = false
    private var timerJob: Job? = null
    private var programStarted = false
    private var stopResume: StopResume? = null
    private val follow = WeldLineFollow()

    val weldPaths = mutableStateListOf<WeldPath>()
    private val robotErrors = mutableStateListOf<RobotError>()
    private var starting = false

    private val _uiState = MutableStateFlow(SingleWeldUiState())
    val uiState: StateFlow<SingleWeldUiState> = combine(
        _uiState,
        pouch.projects,
        pouch.processes,
    ) { s, projects, processes ->
        s.copy(shell = s.shell.copy(pouchProjects = projects, pouchProcesses = processes))
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), SingleWeldUiState())
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
    var isCornerParamsUnlocked: Boolean
        get() = _uiState.value.isCornerParamsUnlocked
        set(v) { patch { it.copy(isCornerParamsUnlocked = v) } }
    var operationPosition by mutableStateOf(Pose(0.0, 0.0, 0.0, 0.0, 0.0, 0.0))

    val isControllerActive: Boolean get() = teach.isControllerActive
    val connectionStatus: String get() = teach.connectionStatus

    val fineTune = FineTuneSupport(
        scope = viewModelScope,
        socketManager = robot,
        nextCommandId = { commandId++ },
        toast = { msg -> viewModelScope.launch { _toast.emit(msg) } },
        currentProcess = { currentPath()?.process },
        onOscillationChanged = { osc ->
            currentPath()?.process?.oscillation = osc
            saveProject()
        },
        paramsVisible = { _uiState.value.shell.pouchProcesses.copyableOf(currentPath()?.processId.orEmpty()) },
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
            val loaded = runCatching { SingleLayerProject.parse(bytes) }.getOrElse {
                PadLog.error("SingleWeld", "active project parse failed", it)
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
        PadLog.info("SingleWeld", "activate project=$id")
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

    override suspend fun loadProcessFromPouch(id: UUID) = pouch.openProcess(id)

    override fun saveProcessFromPouch(id: UUID?, process: WeldProcess) {
        viewModelScope.launch {
            runCatching { pouch.saveProcess(id, process) }
                .onSuccess { refreshPouchLists() }
                .onFailure { e -> toast(if (e.message == "asset is not copyable") "保密工艺不能改" else (e.message ?: "无法保存工艺")) }
        }
    }

    override fun bindProcessFromPouch(processId: UUID?) {
        viewModelScope.launch {
            val path = currentPath() ?: run {
                extraProcessEditIndex = -1
                return@launch
            }
            val process = if (processId == null) WeldProcess() else {
                pouch.openProcess(processId) ?: run {
                    missingProcessMessage = "闭包里没有这条工艺"
                    isMissingProcessDialogVisible = true
                    extraProcessEditIndex = -1
                    return@launch
                }
            }
            val idStr = processId?.toString().orEmpty()
            when {
                extraProcessEditIndex == -2 -> {
                    if (processId != null) {
                        path.extraProcesses.add(
                            WeldPathProcessSlot(
                                id = UUID.randomUUID().toString(),
                                process = process,
                                processId = idStr,
                                isEnabled = true,
                            ),
                        )
                    }
                }
                extraProcessEditIndex >= 0 && extraProcessEditIndex < path.extraProcesses.size -> {
                    val slot = path.extraProcesses[extraProcessEditIndex]
                    path.extraProcesses[extraProcessEditIndex] = slot.copy(process = process, processId = idStr)
                }
                else -> {
                    weldPaths[selectedWeldPathIndex] = path.copy(process = process, processId = idStr)
                }
            }
            extraProcessEditIndex = -1
            saveProject()
        }
    }

    override fun cancelAddProcessVariant() {
        extraProcessEditIndex = -1
    }

    fun beginAddProcessVariant(pathIndex: Int) {
        extraProcessEditIndex = -2
        selectedWeldPathIndex = pathIndex
    }

    fun beginReplaceExtraProcess(pathIndex: Int, extraIndex: Int) {
        extraProcessEditIndex = extraIndex
        selectedWeldPathIndex = pathIndex
    }

    fun toggleExtraProcessEnabled(pathIndex: Int, extraIndex: Int) {
        val path = weldPaths.getOrNull(pathIndex) ?: return
        if (extraIndex !in path.extraProcesses.indices) return
        val slot = path.extraProcesses[extraIndex]
        path.extraProcesses[extraIndex] = slot.copy(isEnabled = !slot.isEnabled)
        saveProject()
    }

    fun deleteExtraProcess(pathIndex: Int, extraIndex: Int) {
        val path = weldPaths.getOrNull(pathIndex) ?: return
        if (extraIndex !in path.extraProcesses.indices) return
        path.extraProcesses.removeAt(extraIndex)
        saveProject()
    }

    fun addWeldPath() {
        val points = mutableStateListOf(
            WeldPoint(UUID.randomUUID().toString(), WeldPointType.START_SAFE),
            WeldPoint(UUID.randomUUID().toString(), WeldPointType.START),
            WeldPoint(UUID.randomUUID().toString(), WeldPointType.END),
            WeldPoint(UUID.randomUUID().toString(), WeldPointType.END_SAFE),
        )
        weldPaths.add(
            WeldPath(UUID.randomUUID().toString(), "焊道 ${weldPaths.size + 1}", points, WeldProcess()).asUiPath(),
        )
        selectedWeldPathIndex = weldPaths.lastIndex
        saveProject()
        viewModelScope.launch { _scrollToIndex.emit(weldPaths.lastIndex) }
    }

    fun deleteWeldPath(index: Int) {
        if (index !in weldPaths.indices) return
        val group = weldPaths[index].cornerGroupParams?.groupId
        if (group != null) weldPaths.removeAll { it.cornerGroupParams?.groupId == group }
        else weldPaths.removeAt(index)
        if (selectedWeldPathIndex >= weldPaths.size) selectedWeldPathIndex = weldPaths.lastIndex.coerceAtLeast(0)
        saveProject()
    }

    fun moveWeldPath(from: Int, to: Int) {
        weldPaths.add(to, weldPaths.removeAt(from))
        selectedWeldPathIndex = to
        saveProject()
    }

    fun renameWeldPath(name: String) {
        if (name.isEmpty() || selectedWeldPathIndex !in weldPaths.indices) return
        weldPaths[selectedWeldPathIndex] = weldPaths[selectedWeldPathIndex].copy(name = name)
        saveProject()
    }

    fun toggleWeldPathEnabled(index: Int) {
        val path = weldPaths.getOrNull(index) ?: return
        weldPaths[index] = path.copy(isEnabled = !path.isEnabled)
        saveProject()
    }

    fun addMiddlePoint() = insertBeforeEnd(WeldPointType.MIDDLE)

    fun addArcMiddlePoint() = insertBeforeEnd(WeldPointType.ARC_MIDDLE)

    fun selectPoint(pathIndex: Int, pointIndex: Int) {
        val path = weldPaths.getOrNull(pathIndex) ?: return
        if (pointIndex !in path.points.indices) return
        selectedWeldPathIndex = pathIndex
        if (path.selectedPointIndex != pointIndex) {
            weldPaths[pathIndex] = path.copy(selectedPointIndex = pointIndex)
        }
    }

    fun deletePoint(pathIndex: Int, pointIndex: Int) {
        val path = weldPaths.getOrNull(pathIndex) ?: return
        if (pointIndex < 1 || pointIndex >= path.points.size - 1) return
        val point = path.points[pointIndex]
        if (point.type != WeldPointType.MIDDLE && point.type != WeldPointType.ARC_MIDDLE) return
        path.points.removeAt(pointIndex)
        val sel = if (path.selectedPointIndex >= pointIndex) (path.selectedPointIndex - 1).coerceAtLeast(0) else path.selectedPointIndex
        weldPaths[pathIndex] = path.copy(selectedPointIndex = sel)
        saveProject()
    }

    override fun collectData() {
        PadLog.info("SingleWeld", "collect point")
        if (teach.connectionStatus == RobotLink.DOWN) {
            toast("设备未连接")
            return
        }
        val path = currentPath() ?: return
        val i = path.selectedPointIndex
        if (i !in path.points.indices) return
        val snap = Capture.snapshot(robot.robotPose.value, robot.robotJoints.value)
        val point = path.points[i]
        path.points[i] = if (snap != null) {
            point.copy(pose = snap.first, jointAngles = snap.second)
        } else {
            point.copy(pose = Pose(100.0, 200.0, 300.0, 0.0, 0.0, 0.0), jointAngles = List(6) { 0.0 })
        }
        val next = if (i < path.points.lastIndex) i + 1 else i
        if (next != i) {
            weldPaths[selectedWeldPathIndex] = path.copy(selectedPointIndex = next)
        } else if (selectedWeldPathIndex < weldPaths.lastIndex) {
            selectedWeldPathIndex += 1
            val np = weldPaths[selectedWeldPathIndex]
            if (np.selectedPointIndex != 0) weldPaths[selectedWeldPathIndex] = np.copy(selectedPointIndex = 0)
        }
        saveProject()
    }

    fun collectRefX() {
        if (teach.connectionStatus == RobotLink.DOWN) {
            toast("设备未连接")
            return
        }
        val path = currentPath() ?: return
        val i = path.selectedPointIndex
        if (i !in path.points.indices) return
        val pose = robot.robotPose.value
        val joints = robot.robotJoints.value
        if (pose == null || joints.isEmpty()) return
        path.points[i] = path.points[i].copy(refPointX = RefPoint(pose, joints))
        saveProject()
        toast("X方向参考点已记录")
    }

    fun clearRefX() {
        val path = currentPath() ?: return
        val i = path.selectedPointIndex
        if (i !in path.points.indices) return
        path.points[i] = path.points[i].copy(refPointX = null)
        saveProject()
        toast("X方向参考点已清除")
    }

    fun clearPointData() {
        val path = currentPath() ?: return
        val i = path.selectedPointIndex
        if (i !in path.points.indices) return
        path.points[i] = path.points[i].copy(pose = null, jointAngles = null)
        saveProject()
    }

    override fun sendMoveLCommand() {
        val point = currentPath()?.points?.getOrNull(currentPath()?.selectedPointIndex ?: -1) ?: return
        val pose = point.pose ?: return
        val joints = point.jointAngles?.takeIf { it.size >= 6 } ?: List(6) { 0.0 }
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

    override fun startSimulation() {
        PadLog.info("SingleWeld", "start simulation")
        startRun(welding = false, simulating = true)
    }

    override fun startArcWelding() {
        PadLog.info("SingleWeld", "start arc")
        startRun(welding = true, simulating = false)
    }

    fun pauseWelding() {
        PadLog.info("SingleWeld", "pause")
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
        PadLog.info("SingleWeld", "continue")
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
        PadLog.info("SingleWeld", "stop force=$force")
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

    fun unlockCornerParams(password: String): Boolean {
        if (password != AppConfig.CORNER_PASSWORD) return false
        isCornerParamsUnlocked = true
        return true
    }

    fun lockCornerParams() {
        isCornerParamsUnlocked = false
    }

    fun generateCornerWelds(
        refPathAIndex: Int,
        refPathBIndex: Int,
        layerCount: Int,
        initialLength: Double,
        upwardOffset: Double,
        lengthReduction: Double,
        updateGroupId: String? = null,
        torchRx: Double? = null,
        torchRy: Double? = null,
        torchRz: Double? = null,
        processId: String? = null,
    ) {
        val refA = weldPaths.getOrNull(refPathAIndex) ?: return
        val refB = weldPaths.getOrNull(refPathBIndex) ?: return
        val ptsA = refA.points.filter { it.type == WeldPointType.START || it.type == WeldPointType.MIDDLE || it.type == WeldPointType.END }
        val ptsB = refB.points.filter { it.type == WeldPointType.START || it.type == WeldPointType.MIDDLE || it.type == WeldPointType.END }
        if (ptsA.size < 2 || ptsB.size < 2) {
            toast("参考焊道缺少足够的点，无法计算交点！")
            return
        }
        var minD = Double.MAX_VALUE
        var bestI = 0
        var bestJ = 0
        for (i in ptsA.indices) for (j in ptsB.indices) {
            val pA = ptsA[i].pose?.toVec3() ?: continue
            val pB = ptsB[j].pose?.toVec3() ?: continue
            val d = pA.distanceTo(pB)
            if (d < minD) {
                minD = d
                bestI = i
                bestJ = j
            }
        }
        val pA1 = ptsA[bestI].pose!!.toVec3()
        val pA2 = if (bestI == 0) ptsA[1].pose!!.toVec3() else ptsA[bestI - 1].pose!!.toVec3()
        val pB1 = ptsB[bestJ].pose!!.toVec3()
        val pB2 = if (bestJ == 0) ptsB[1].pose!!.toVec3() else ptsB[bestJ - 1].pose!!.toVec3()
        val hit = CornerWeldGenerator.calculate3DIntersection(pA1, pA2, pB1, pB2)
        if (hit == null) {
            toast("无法计算交点，两条焊道平行或距离异常")
            return
        }
        val cornerPose = ptsB[bestJ].pose!!.copy(x = hit.x, y = hit.y, z = hit.z)
        val cornerPt = cornerPose.toVec3()
        val vecA = pathDir(refA, cornerPt) ?: return
        val vecB = pathDir(refB, cornerPt) ?: return
        val pid = processId?.takeIf { it.isNotEmpty() } ?: refA.processId
        viewModelScope.launch {
            val proc = pid.toUuidOrNull()?.let { pouch.openProcess(it) } ?: refA.process
            val orient = if (torchRx != null && torchRy != null && torchRz != null) {
                Pose(0.0, 0.0, 0.0, torchRx, torchRy, torchRz)
            } else null
            val safe = refB.points.firstOrNull { it.type == WeldPointType.START_SAFE }?.pose
            toast("正在通过逆运动学计算关节角度，请稍候...")
            val newPaths = CornerWeldGenerator.generateCornerPaths(
                baseProcess = proc,
                processId = pid,
                cornerPose = cornerPose,
                safeStartPose = safe,
                safeEndPose = safe,
                weldOrientation = orient,
                vecAIn = vecA,
                vecBIn = vecB,
                layerCount = layerCount,
                initialLength = initialLength,
                upwardOffset = upwardOffset,
                lengthReduction = lengthReduction,
                refPathAId = refA.id,
                refPathBId = refB.id,
            )
            for (path in newPaths) {
                for (pt in path.points) {
                    val pose = pt.pose ?: continue
                    if (pt.jointAngles == null) {
                        val joints = robot.getInverseKin(pose)
                        if (joints != null && joints.size >= 6) pt.jointAngles = joints
                    }
                }
            }
            if (updateGroupId != null) weldPaths.removeAll { it.cornerGroupParams?.groupId == updateGroupId }
            newPaths.forEachIndexed { i, p ->
                weldPaths.add(p.copy(name = "包角焊道 ${weldPaths.size + 1} (层 ${i + 1})").asUiPath())
            }
            saveProject()
            toast("成功生成 $layerCount 层包角工艺")
            _scrollToIndex.emit(weldPaths.lastIndex)
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
                teach.stopController()
                programStarted = false
                follow.reset()
                patch { it.copy(run = WeldRunUi.Active(welding = welding, paused = false)) }
                fineTune.resetOffsets()
                pouch.setWelding(true)
                if (welding) startTimer()
                withContext(Dispatchers.IO) {
                    val numbered = WeldRun.number(
                        SingleLayerLua.job(
                            scripts, welding, simulating, teach.speedMode, teach.toolIndex, teach.isExtAxisEnabled, resume,
                        ),
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

    private fun toScripts(): List<ScriptPath>? {
        val out = ArrayList<ScriptPath>()
        for (path in weldPaths) {
            if (!path.isEnabled && path.extraProcesses.none { it.isEnabled }) continue
            val pts = ArrayList<ScriptPoint>(path.points.size)
            for (p in path.points) {
                val pose = p.pose
                if (pose == null) {
                    toast("${path.name} 有未采集的点")
                    return null
                }
                pts += ScriptPoint(p.type, pose, p.jointAngles ?: List(6) { 0.0 }, refX = p.refPointX?.pose)
            }
            out += ScriptPath(
                points = pts,
                process = path.process,
                extras = path.extraProcesses.filter { it.isEnabled }.map { it.process },
                enabled = path.isEnabled,
            )
        }
        if (out.isEmpty()) {
            toast("没有可执行的焊道")
            return null
        }
        return out
    }

    private suspend fun bindProcesses(): Boolean {
        val refs = weldPaths.flatMap { path ->
            listOf(ProcessRef(path.name, path.processId, path.isEnabled)) +
                path.extraProcesses.map { ProcessRef("${path.name} 附加", it.processId, it.isEnabled) }
        }
        val outcome = pouch.resolveProcesses(refs)
        weldPaths.forEach { path ->
            path.processId.toUuidOrNull()?.let { id -> outcome.loaded[id]?.let { path.process = it } }
            path.extraProcesses.forEachIndexed { i, slot ->
                slot.processId.toUuidOrNull()?.let { id ->
                    outcome.loaded[id]?.let { path.extraProcesses[i] = slot.copy(process = it) }
                }
            }
        }
        if (outcome.missing.isNotEmpty()) {
            missingProcessMessage = "以下焊道的工艺不在当前闭包：\n" + outcome.missing.joinToString("\n")
            isMissingProcessDialogVisible = true
            return false
        }
        return true
    }

    private fun saveProject() {
        val id = pouchProjectId ?: return
        val paths = weldPaths.toList()
        publish()
        viewModelScope.launch {
            runCatching {
                pouch.saveWeldPaths(paths)
                pouch.saveProject(id, SingleLayerProject.encode(paths))
            }.onFailure { e -> toast(e.message ?: "无法保存工程") }
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

    private fun publish(transform: (SingleWeldUiState) -> SingleWeldUiState = { it }) {
        _uiState.update { s ->
            val n = transform(s)
            n.copy(
                weldPaths = weldPaths.toList(),
                currentRobotErrors = robotErrors.toList(),
            )
        }
    }

    private fun patch(transform: (SingleWeldUiState) -> SingleWeldUiState) {
        _uiState.update(transform)
    }

    private fun insertBeforeEnd(type: WeldPointType) {
        val path = currentPath() ?: return
        val end = path.points.indexOfFirst { it.type == WeldPointType.END }
        if (end <= 0) return
        path.points.add(end, WeldPoint(UUID.randomUUID().toString(), type))
        saveProject()
    }

    private fun currentPath(): WeldPath? = weldPaths.getOrNull(selectedWeldPathIndex)

    private fun pathDir(path: WeldPath, corner: Vec3): Vec3? {
        val pts = path.points.filter {
            it.type == WeldPointType.START || it.type == WeldPointType.MIDDLE || it.type == WeldPointType.END
        }
        if (pts.size < 2) return null
        var minD = Double.MAX_VALUE
        var best = 0
        for (i in pts.indices) {
            val p = pts[i].pose?.toVec3() ?: continue
            val d = p.distanceTo(corner)
            if (d < minD) {
                minD = d
                best = i
            }
        }
        val p1 = pts[best].pose!!.toVec3()
        val p2 = if (best == 0) pts[1].pose!!.toVec3() else pts[best - 1].pose!!.toVec3()
        return p2 - p1
    }

    private fun toast(msg: String) {
        PadLog.toast(msg)
        viewModelScope.launch { _toast.emit(msg) }
    }

    private fun String.toUuidOrNull(): UUID? = runCatching { UUID.fromString(trim()) }.getOrNull()
}
