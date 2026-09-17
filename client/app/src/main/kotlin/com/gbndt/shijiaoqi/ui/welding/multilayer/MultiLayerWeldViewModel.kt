package com.gbndt.shijiaoqi.ui.welding.multilayer

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
import com.gbndt.shijiaoqi.domain.multilayer.MultiLayerLua
import com.gbndt.shijiaoqi.domain.shared.StopResume
import com.gbndt.shijiaoqi.domain.shared.WeldLineFollow
import com.gbndt.shijiaoqi.domain.shared.WeldRun
import com.gbndt.shijiaoqi.domain.shared.Capture
import com.gbndt.shijiaoqi.domain.multilayer.MultiLayerPass
import com.gbndt.shijiaoqi.domain.multilayer.MultiLayerProject
import com.gbndt.shijiaoqi.domain.shared.ProcessRef
import com.gbndt.shijiaoqi.data.crypt.Wm2
import com.gbndt.shijiaoqi.domain.shared.WeldProgress
import com.gbndt.shijiaoqi.model.multilayer.MultiLayerWeldPath
import com.gbndt.shijiaoqi.model.Pose
import com.gbndt.shijiaoqi.model.RefPoint
import com.gbndt.shijiaoqi.model.RobotError
import com.gbndt.shijiaoqi.model.RobotErrorCodes
import com.gbndt.shijiaoqi.model.multilayer.WeldPassOffset
import com.gbndt.shijiaoqi.model.single.WeldPath
import com.gbndt.shijiaoqi.model.WeldPoint
import com.gbndt.shijiaoqi.model.WeldPointType
import com.gbndt.shijiaoqi.model.WeldProcess
import com.gbndt.shijiaoqi.ui.teach.TeachSession
import com.gbndt.shijiaoqi.ui.welding.FineTuneSupport
import com.gbndt.shijiaoqi.ui.welding.MultiLayerUiState
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

/** 多层焊道协调器：袋、采点、开焊。点动在示教，几何和 Lua 在 domain。 */
@HiltViewModel
class MultiLayerWeldViewModel @Inject constructor(
    private val robot: RobotRepository,
    private val pouch: PouchRepository,
    private val settings: DeviceSettingsStore,
    private val teach: TeachSession,
) : ViewModel(), WeldPad, WeldShellHost {

    enum class RefPointType {
        NONE, START_X, START_Z, END_X, END_Z, MIDDLE_X, MIDDLE_Z
    }

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

    val multiLayerWeldPaths = mutableStateListOf<MultiLayerWeldPath>()
    private val robotErrors = mutableStateListOf<RobotError>()
    private var starting = false

    private val _uiState = MutableStateFlow(MultiLayerUiState())
    val uiState: StateFlow<MultiLayerUiState> = combine(
        _uiState,
        pouch.projects,
        pouch.processes,
    ) { s, projects, processes ->
        s.copy(shell = s.shell.copy(pouchProjects = projects, pouchProcesses = processes))
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), MultiLayerUiState())
    override val shellUi: StateFlow<WeldShellUi> =
        uiState.map { it.shell }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), WeldShellUi())

    var selectedMultiLayerPathIndex: Int
        get() = _uiState.value.selectedMultiLayerPathIndex
        set(v) { patch { it.copy(selectedMultiLayerPathIndex = v) } }
    var selectedPassIndex: Int
        get() = _uiState.value.selectedPassIndex
        set(v) { patch { it.copy(selectedPassIndex = v) } }
    var selectedRefPointType by mutableStateOf(RefPointType.NONE)
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
                    multiLayerWeldPaths.getOrNull(p)?.basePath?.points?.getOrNull(i)?.type
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
    }

    override suspend fun syncFromPouch() {
        val id = pouch.activeProjectId() ?: return
        val bytes = pouch.openProjectBytes(id) ?: return
        try {
            val loaded = runCatching { MultiLayerProject.parse(bytes) }.getOrElse {
                PadLog.error("MultiWeld", "active project parse failed", it)
                emptyList()
            }
            multiLayerWeldPaths.clear()
            multiLayerWeldPaths.addAll(loaded.map { it.asUiPath() })
            if (multiLayerWeldPaths.isEmpty()) addLinearWeldPath()
            selectedMultiLayerPathIndex = 0
            selectedPassIndex = -1
            selectedRefPointType = RefPointType.NONE
            pouchProjectId = id
            patch { it.copy(shell = it.shell.copy(currentProjectName = pouch.projectName(id))) }
            bindProcesses()
        } finally {
            Wm2.zero(bytes)
        }
        publish()
    }

    override fun activatePouchProject(id: UUID) {
        PadLog.info("MultiWeld", "activate project=$id")
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
        viewModelScope.launch {
            val mp = currentMulti() ?: return@launch
            val process = if (processId == null) WeldProcess() else {
                pouch.openProcess(processId) ?: run {
                    missingProcessMessage = "闭包里没有这条工艺"
                    isMissingProcessDialogVisible = true
                    return@launch
                }
            }
            val idStr = processId?.toString().orEmpty()
            if (selectedPassIndex == -1) {
                multiLayerWeldPaths[selectedMultiLayerPathIndex] = mp.copy(
                    basePath = mp.basePath.copy(process = process, processId = idStr),
                )
            } else if (selectedPassIndex in mp.passes.indices) {
                val pass = mp.passes[selectedPassIndex]
                mp.passes[selectedPassIndex] = pass.copy(process = process, processId = idStr)
            }
            saveCurrentProject()
        }
    }

    fun addLinearWeldPath() = addPath("多层直线", withArc = false)

    fun addCircularWeldPath() = addPath("多层圆弧", withArc = true)

    fun deleteWeldPath(index: Int) {
        if (index !in multiLayerWeldPaths.indices) return
        multiLayerWeldPaths.removeAt(index)
        if (selectedMultiLayerPathIndex >= multiLayerWeldPaths.size) {
            selectedMultiLayerPathIndex = multiLayerWeldPaths.lastIndex.coerceAtLeast(0)
        }
        selectedPassIndex = -1
        saveCurrentProject()
    }

    fun moveWeldPath(from: Int, to: Int) {
        if (from !in multiLayerWeldPaths.indices || to !in multiLayerWeldPaths.indices) return
        multiLayerWeldPaths.add(to, multiLayerWeldPaths.removeAt(from))
        selectedMultiLayerPathIndex = to
        saveCurrentProject()
    }

    fun renameWeldPath(name: String) {
        if (name.isEmpty()) return
        val mp = currentMulti() ?: return
        multiLayerWeldPaths[selectedMultiLayerPathIndex] = mp.copy(name = name)
        saveCurrentProject()
    }

    fun toggleWeldPathEnabled(index: Int) {
        val mp = multiLayerWeldPaths.getOrNull(index) ?: return
        multiLayerWeldPaths[index] = mp.copy(basePath = mp.basePath.copy(isEnabled = !mp.basePath.isEnabled))
        saveCurrentProject()
    }

    fun togglePassEnabled(pathIndex: Int, passIndex: Int) {
        val mp = multiLayerWeldPaths.getOrNull(pathIndex) ?: return
        if (passIndex !in mp.passes.indices) return
        val pass = mp.passes[passIndex]
        mp.passes[passIndex] = pass.copy(isEnabled = !pass.isEnabled)
        saveCurrentProject()
    }

    fun addWeldPassOffset() {
        val mp = currentMulti() ?: return
        mp.passes.add(
            WeldPassOffset(
                id = UUID.randomUUID().toString(),
                name = "第 ${mp.passes.size + 1} 道",
                process = WeldProcess(),
            ),
        )
        saveCurrentProject()
    }

    fun deleteWeldPassOffset() {
        val mp = currentMulti() ?: return
        if (selectedPassIndex !in mp.passes.indices) return
        mp.passes.removeAt(selectedPassIndex)
        selectedPassIndex = -1
        saveCurrentProject()
    }

    fun updatePassOffset(multiPathIndex: Int, passIndex: Int, field: String, value: String) {
        if (multiPathIndex !in multiLayerWeldPaths.indices) return
        if (selectedMultiLayerPathIndex != multiPathIndex) {
            selectedMultiLayerPathIndex = multiPathIndex
            selectedPassIndex = passIndex
        }
        val path = multiLayerWeldPaths[multiPathIndex]
        if (passIndex !in path.passes.indices) return
        val doubleVal = value.toDoubleOrNull() ?: 0.0
        val pass = path.passes[passIndex]
        path.passes[passIndex] = when (field) {
            "valX" -> pass.copy(valX = doubleVal)
            "valYLeft" -> pass.copy(valYLeft = doubleVal)
            "valYRight" -> pass.copy(valYRight = doubleVal)
            "valZ" -> pass.copy(valZ = doubleVal)
            "valR" -> pass.copy(valR = doubleVal)
            else -> pass
        }
        saveCurrentProject()
    }

    fun addMiddlePoint() = insertBeforeEnd(WeldPointType.MIDDLE)

    fun addArcMiddlePoint() = insertBeforeEnd(WeldPointType.ARC_MIDDLE)

    fun selectPoint(pathIndex: Int, pointIndex: Int) {
        val mp = multiLayerWeldPaths.getOrNull(pathIndex) ?: return
        if (pointIndex !in mp.basePath.points.indices) return
        selectedMultiLayerPathIndex = pathIndex
        selectedPassIndex = -1
        selectedRefPointType = RefPointType.NONE
        if (mp.basePath.selectedPointIndex != pointIndex) {
            multiLayerWeldPaths[pathIndex] = mp.copy(basePath = mp.basePath.copy(selectedPointIndex = pointIndex))
        }
    }

    fun deletePoint(pathIndex: Int, pointIndex: Int) {
        val mp = multiLayerWeldPaths.getOrNull(pathIndex) ?: return
        val points = mp.basePath.points
        if (pointIndex !in points.indices) return
        val point = points[pointIndex]
        if (point.type != WeldPointType.MIDDLE && point.type != WeldPointType.ARC_MIDDLE) return
        points.removeAt(pointIndex)
        val sel = if (mp.basePath.selectedPointIndex >= points.size) points.lastIndex else mp.basePath.selectedPointIndex
        multiLayerWeldPaths[pathIndex] = mp.copy(basePath = mp.basePath.copy(selectedPointIndex = sel))
        saveCurrentProject()
    }

    fun selectRefPointType(type: RefPointType) {
        selectedRefPointType = if (selectedRefPointType == type) RefPointType.NONE else type
    }

    fun deleteRefPoint(type: RefPointType) {
        val mp = currentMulti() ?: return
        multiLayerWeldPaths[selectedMultiLayerPathIndex] = when (type) {
            RefPointType.START_X -> mp.copy(refPointX1 = null)
            RefPointType.START_Z -> mp.copy(refPointZ1 = null)
            RefPointType.END_X -> mp.copy(refPointXEnd = null)
            RefPointType.END_Z -> mp.copy(refPointZEnd = null)
            RefPointType.MIDDLE_X -> mp.copy(refPointXMiddle = null)
            RefPointType.MIDDLE_Z -> mp.copy(refPointZMiddle = null)
            else -> mp
        }
        saveCurrentProject()
    }

    override fun collectData() {
        PadLog.info("MultiWeld", "collect point")
        if (selectedPassIndex >= 0) return
        if (teach.connectionStatus == RobotLink.DOWN) {
            toast("设备未连接")
            return
        }
        val path = currentBase() ?: return
        if (selectedRefPointType != RefPointType.NONE) {
            recordRefPoint(selectedRefPointType)
            val nextRef = when (selectedRefPointType) {
                RefPointType.START_X -> RefPointType.START_Z
                RefPointType.MIDDLE_X -> RefPointType.MIDDLE_Z
                RefPointType.END_X -> RefPointType.END_Z
                else -> RefPointType.NONE
            }
            if (nextRef != RefPointType.NONE) {
                selectedRefPointType = nextRef
            } else {
                selectedRefPointType = RefPointType.NONE
                advancePoint(path)
            }
            toast("参考点已记录")
            return
        }
        val i = path.selectedPointIndex
        if (i !in path.points.indices) return
        val point = path.points[i]
        val snap = Capture.snapshot(robot.robotPose.value, robot.robotJoints.value)
        path.points[i] = if (snap != null) {
            point.copy(pose = snap.first, jointAngles = snap.second)
        } else {
            point.copy(pose = Pose(100.0, 200.0, 300.0, 0.0, 0.0, 0.0), jointAngles = List(6) { 0.0 })
        }
        val nextRef = when (point.type) {
            WeldPointType.START -> RefPointType.START_X
            WeldPointType.ARC_MIDDLE -> RefPointType.MIDDLE_X
            WeldPointType.END -> RefPointType.END_X
            else -> RefPointType.NONE
        }
        if (nextRef != RefPointType.NONE) {
            selectedRefPointType = nextRef
            updateBase(path)
        } else {
            advancePoint(path)
        }
        toast("点位已记录: ${point.type}")
    }

    fun clearPointData() {
        if (selectedRefPointType != RefPointType.NONE) {
            deleteRefPoint(selectedRefPointType)
            toast("参考点数据已清除")
            return
        }
        if (selectedPassIndex != -1) return
        val path = currentBase() ?: return
        val i = path.selectedPointIndex
        if (i !in path.points.indices) return
        path.points[i] = path.points[i].copy(pose = null, jointAngles = null)
        saveCurrentProject()
        toast("点位数据已清除")
    }

    override fun sendMoveLCommand() {
        val mp = currentMulti() ?: return
        if (selectedRefPointType != RefPointType.NONE) {
            val ref = when (selectedRefPointType) {
                RefPointType.START_X -> mp.refPointX1
                RefPointType.START_Z -> mp.refPointZ1
                RefPointType.END_X -> mp.refPointXEnd
                RefPointType.END_Z -> mp.refPointZEnd
                RefPointType.MIDDLE_X -> mp.refPointXMiddle
                RefPointType.MIDDLE_Z -> mp.refPointZMiddle
                else -> null
            } ?: return
            sendMoveL(ref.pose, ref.jointAngles)
            return
        }
        val base = mp.basePath
        val i = base.selectedPointIndex
        if (i !in base.points.indices) return
        if (selectedPassIndex >= 0) {
            val pass = mp.passes.getOrNull(selectedPassIndex) ?: return
            val pts = MultiLayerPass.generate(base, pass, mp) ?: return
            val pt = pts.getOrNull(i) ?: return
            sendMoveL(pt.world(), pt.joints)
            return
        }
        val point = base.points[i]
        val pose = point.pose ?: return
        sendMoveL(pose, point.jointAngles)
    }

    override fun startSimulation() {
        PadLog.info("MultiWeld", "start simulation")
        startRun(welding = false, simulating = true)
    }

    override fun startArcWelding() {
        PadLog.info("MultiWeld", "start arc")
        startRun(welding = true, simulating = false)
    }

    fun pauseWelding() {
        PadLog.info("MultiWeld", "pause")
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
        PadLog.info("MultiWeld", "continue")
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
        PadLog.info("MultiWeld", "stop force=$force")
        val pose = robot.robotPose.value ?: operationPosition
        val joints = robot.robotJoints.value
        val pathIdx = if (follow.lastPath >= 0) follow.lastPath else selectedMultiLayerPathIndex
        val pointIdx = if (follow.lastPoint >= 0) follow.lastPoint else currentBase()?.selectedPointIndex ?: -1
        if (joints.size >= 6) {
            stopResume = StopResume(pose, joints, pathIdx, pointIdx, selectedPassIndex)
        }
        if (_uiState.value.run.isWelding && follow.statsActive) {
            val last = multiLayerWeldPaths.getOrNull(follow.lastPath)?.basePath?.points?.getOrNull(follow.lastPoint)?.pose
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

    fun saveCurrentProject() {
        val id = pouchProjectId ?: return
        val paths = multiLayerWeldPaths.toList()
        publish()
        viewModelScope.launch {
            runCatching {
                pouch.saveMultiLayerPaths(paths)
                pouch.saveProject(id, MultiLayerProject.encode(paths))
            }.onFailure { e -> toast(e.message ?: "无法保存工程") }
        }
    }

    private fun startRun(welding: Boolean, simulating: Boolean) {
        if (_uiState.value.run.busy || starting) return
        starting = true
        viewModelScope.launch {
            try {
                if (!bindProcesses()) return@launch
                val resume = stopResume
                stopResume = null
                val luaLines = MultiLayerLua.job(
                    multiLayerWeldPaths.toList(),
                    welding,
                    simulating,
                    teach.speedMode,
                    teach.toolIndex,
                    teach.isExtAxisEnabled,
                    resume,
                )
                if (luaLines == null) {
                    toast("有未采集的点")
                    return@launch
                }
                if (luaLines.isEmpty() || luaLines.size == 1) {
                    toast("没有可执行的焊道")
                    return@launch
                }
                pouch.factoryArmError()?.let { toast(it); return@launch }
                teach.stopController()
                programStarted = false
                follow.reset()
                patch { it.copy(run = WeldRunUi.Active(welding = welding, paused = false)) }
                fineTune.resetOffsets()
                pouch.setWelding(true)
                if (welding) startTimer()
                withContext(Dispatchers.IO) {
                    val numbered = WeldRun.number(luaLines) { commandId++ }
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

    private suspend fun bindProcesses(): Boolean {
        val refs = multiLayerWeldPaths.flatMap { mp ->
            listOf(ProcessRef("${mp.name} 基准", mp.basePath.processId, mp.basePath.isEnabled)) +
                mp.passes.map { ProcessRef("${mp.name} ${it.name}", it.processId, it.isEnabled) }
        }
        val outcome = pouch.resolveProcesses(refs)
        multiLayerWeldPaths.forEach { mp ->
            mp.basePath.processId.toUuidOrNull()?.let { id -> outcome.loaded[id]?.let { mp.basePath.process = it } }
            mp.passes.forEachIndexed { i, pass ->
                pass.processId.toUuidOrNull()?.let { id ->
                    outcome.loaded[id]?.let { mp.passes[i] = pass.copy(process = it) }
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
            val mp = multiLayerWeldPaths.getOrNull(hit.pathIndex) ?: continue
            val base = mp.basePath
            if (hit.pointIndex in base.points.indices) {
                multiLayerWeldPaths[hit.pathIndex] = mp.copy(basePath = base.copy(selectedPointIndex = hit.pointIndex))
            }
            selectedMultiLayerPathIndex = hit.pathIndex
            selectedPassIndex = hit.extraIndex
            if (hit.startStats && _uiState.value.run.isWelding) follow.statsActive = true
            if (hit.addFrom >= 0 && hit.countLength && _uiState.value.run.isWelding) {
                added += WeldProgress.segmentLength(base.points, hit.addFrom, hit.pointIndex)
            }
            if (hit.stopStats) follow.statsActive = false
        }
        if (added > 0) {
            patch { it.copy(weldingLength = it.weldingLength + added / 1000.0) }
            persistStats()
        }
        publish()
    }

    private fun publish(transform: (MultiLayerUiState) -> MultiLayerUiState = { it }) {
        _uiState.update { s ->
            val n = transform(s)
            n.copy(
                paths = multiLayerWeldPaths.toList(),
                currentRobotErrors = robotErrors.toList(),
            )
        }
    }

    private fun patch(transform: (MultiLayerUiState) -> MultiLayerUiState) {
        _uiState.update(transform)
    }

    private fun addPath(prefix: String, withArc: Boolean) {
        val points = mutableStateListOf(
            WeldPoint(UUID.randomUUID().toString(), WeldPointType.START_SAFE),
            WeldPoint(UUID.randomUUID().toString(), WeldPointType.START),
        )
        if (withArc) points.add(WeldPoint(UUID.randomUUID().toString(), WeldPointType.ARC_MIDDLE))
        points.add(WeldPoint(UUID.randomUUID().toString(), WeldPointType.END))
        points.add(WeldPoint(UUID.randomUUID().toString(), WeldPointType.END_SAFE))
        val base = WeldPath(
            id = UUID.randomUUID().toString(),
            name = "Base Path",
            points = points,
            process = WeldProcess(),
        )
        multiLayerWeldPaths.add(
            MultiLayerWeldPath(
                id = UUID.randomUUID().toString(),
                name = "$prefix ${multiLayerWeldPaths.size + 1}",
                basePath = base,
                passes = mutableStateListOf(),
            ).asUiPath(),
        )
        selectedMultiLayerPathIndex = multiLayerWeldPaths.lastIndex
        selectedPassIndex = -1
        saveCurrentProject()
        viewModelScope.launch { _scrollToIndex.emit(multiLayerWeldPaths.lastIndex) }
    }

    private fun insertBeforeEnd(type: WeldPointType) {
        val path = currentBase() ?: return
        val end = path.points.indexOfFirst { it.type == WeldPointType.END }
        if (end <= 0) return
        path.points.add(end, WeldPoint(UUID.randomUUID().toString(), type))
        saveCurrentProject()
    }

    private fun recordRefPoint(type: RefPointType) {
        val mp = currentMulti() ?: return
        val snap = Capture.snapshot(robot.robotPose.value, robot.robotJoints.value) ?: return
        val ref = RefPoint(snap.first, snap.second)
        multiLayerWeldPaths[selectedMultiLayerPathIndex] = when (type) {
            RefPointType.START_X -> mp.copy(refPointX1 = ref)
            RefPointType.START_Z -> mp.copy(refPointZ1 = ref)
            RefPointType.END_X -> mp.copy(refPointXEnd = ref)
            RefPointType.END_Z -> mp.copy(refPointZEnd = ref)
            RefPointType.MIDDLE_X -> mp.copy(refPointXMiddle = ref)
            RefPointType.MIDDLE_Z -> mp.copy(refPointZMiddle = ref)
            else -> mp
        }
        saveCurrentProject()
    }

    private fun advancePoint(path: WeldPath) {
        val next = if (path.selectedPointIndex < path.points.lastIndex) path.selectedPointIndex + 1 else path.selectedPointIndex
        if (next != path.selectedPointIndex) {
            updateBase(path.copy(selectedPointIndex = next))
        } else {
            updateBase(path)
        }
    }

    private fun updateBase(base: WeldPath) {
        val mp = currentMulti() ?: return
        multiLayerWeldPaths[selectedMultiLayerPathIndex] = mp.copy(basePath = base)
        saveCurrentProject()
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
        val mp = currentMulti() ?: return null
        return if (selectedPassIndex == -1) mp.basePath.process else mp.passes.getOrNull(selectedPassIndex)?.process
    }

    private fun currentMulti(): MultiLayerWeldPath? = multiLayerWeldPaths.getOrNull(selectedMultiLayerPathIndex)

    private fun currentBase(): WeldPath? = currentMulti()?.basePath

    private fun toast(msg: String) {
        PadLog.toast(msg)
        viewModelScope.launch { _toast.emit(msg) }
    }

    private fun String.toUuidOrNull(): UUID? = runCatching { UUID.fromString(trim()) }.getOrNull()
}
