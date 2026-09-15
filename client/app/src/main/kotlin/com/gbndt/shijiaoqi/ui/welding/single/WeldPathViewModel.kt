package com.gbndt.shijiaoqi.ui.welding.single

import android.speech.tts.TextToSpeech
import android.app.Application
import android.util.Log
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.launch
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.flow.collect
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.asSharedFlow
import com.gbndt.shijiaoqi.data.legacy.ProcessManager
import com.gbndt.shijiaoqi.data.legacy.ProjectManager
import com.gbndt.shijiaoqi.data.robot.link.SocketManager
import com.gbndt.shijiaoqi.data.robot.protocol.RobotCommands
import com.gbndt.shijiaoqi.ShiJiaoQiApp
import com.gbndt.shijiaoqi.data.crypt.Wm2
import com.gbndt.shijiaoqi.data.pouch.Pouch
import com.gbndt.shijiaoqi.data.session.BagSession
import com.gbndt.shijiaoqi.domain.weld.Capture
import com.gbndt.shijiaoqi.domain.weld.PouchProcessSource
import com.gbndt.shijiaoqi.data.pouch.PouchSave
import com.gbndt.shijiaoqi.domain.weld.ProcessBind
import com.gbndt.shijiaoqi.domain.weld.ProcessChoice
import com.gbndt.shijiaoqi.domain.weld.ProcessJson
import com.gbndt.shijiaoqi.domain.weld.ProcessRef
import com.gbndt.shijiaoqi.domain.weld.ProjectChoice
import com.gbndt.shijiaoqi.domain.weld.SingleLayerProject
import com.gbndt.shijiaoqi.domain.script.WeldRun
import com.gbndt.shijiaoqi.model.FileSystemItem
import com.gbndt.shijiaoqi.model.Pose
import com.gbndt.shijiaoqi.model.AppSettings
import com.gbndt.shijiaoqi.model.WeldPath
import com.gbndt.shijiaoqi.model.WeldPathProcessSlot
import com.gbndt.shijiaoqi.model.WeldPoint
import com.gbndt.shijiaoqi.model.WeldPointType
import com.gbndt.shijiaoqi.model.WeldProcess
import com.gbndt.shijiaoqi.ui.welding.FineTuneSupport
import java.io.File
import java.util.UUID
import java.util.Locale

import kotlin.math.sqrt
import kotlin.math.abs

import com.gbndt.shijiaoqi.data.update.UpdateManager
import com.gbndt.shijiaoqi.data.update.UpdateInfo
import android.content.IntentFilter
import android.content.Intent
import android.app.DownloadManager
import android.content.BroadcastReceiver
import android.content.Context
import android.net.Uri
import com.gbndt.shijiaoqi.model.RefPoint


import com.gbndt.shijiaoqi.domain.weld.CoordinateUtils
import com.gbndt.shijiaoqi.domain.weld.CoordinateUtils.CoordinateSystem
import com.gbndt.shijiaoqi.domain.weld.Point3D
import com.gbndt.shijiaoqi.domain.weld.CoordinateUtils.toPoint3D
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlin.math.pow
import kotlin.math.sqrt

import kotlinx.coroutines.flow.first
import kotlinx.coroutines.withTimeoutOrNull
import kotlinx.coroutines.async
import com.gbndt.shijiaoqi.ui.welding.*

class WeldPathViewModel(application: Application) : AndroidViewModel(application), WeldViewModelInterface {
    // ... existing properties ...

    // Helper class for Vector math
    private data class Vector3(val x: Double, val y: Double, val z: Double) {
        operator fun plus(other: Vector3) = Vector3(x + other.x, y + other.y, z + other.z)
        operator fun minus(other: Vector3) = Vector3(x - other.x, y - other.y, z - other.z)
        operator fun times(scalar: Double) = Vector3(x * scalar, y * scalar, z * scalar)
        fun length() = sqrt(x * x + y * y + z * z)
        fun normalize(): Vector3 {
            val len = length()
            return if (len > 0) Vector3(x / len, y / len, z / len) else Vector3(0.0, 0.0, 0.0)
        }
        fun cross(other: Vector3) = Vector3(
            y * other.z - z * other.y,
            z * other.x - x * other.z,
            x * other.y - y * other.x
        )
        fun dot(other: Vector3) = x * other.x + y * other.y + z * other.z
    }

    private val processManager = ProcessManager(application)
    private val projectManager = ProjectManager(application)
    private val socketManager = SocketManager
    private val updateManager = UpdateManager(application)
    
    private fun handleCommandExecuted(id: Int) {
        val mapping = commandIdMap[id]
        if (mapping != null) {
            val (pathIndex, pointIndex, extraProcessIndex) = mapping
            Log.d("CommandFeedback", "Executed Point: Path=$pathIndex, Point=$pointIndex, Extra=$extraProcessIndex")
            
            // Update UI Selection to show progress (Highlight Logic)
            if (pathIndex < weldPaths.size) {
                selectedWeldPathIndex = pathIndex
                val path = weldPaths[pathIndex]
                
                // Update the Highlighted Point
                if (pointIndex < path.points.size) {
                    weldPaths[pathIndex] = path.copy(selectedPointIndex = pointIndex)
                    
                    // Trigger Stats based on the Highlighted Point
                     if ((isWelding || isSimulating) && pathIndex == selectedWeldPathIndex) {
                         val currentPt = path.points[pointIndex]
                         
                         // Case 1: Start Point Highlighted -> Start Timer
                         if (currentPt.type == WeldPointType.START) {
                             if (isWelding) {
                                 if (!isWeldingStatsActive) {
                                     isWeldingStatsActive = true
                                     Log.d("WeldStats", "Timer STARTED (Highlight: Start Point)")
                                 }
                            }
                        } else {
                            // Accumulate Length (Only for Welding)
                            if (isWelding && isWeldingStatsActive && lastReachedPoint != null) {
                                val added = calculateSegmentLength(path.points, lastReachedPointIndex, pointIndex)
                                weldingLength += added / 1000.0
                                saveAppSettings()
                            }
                        }
                        
                        // Track progress for both Welding and Simulation
                        lastReachedPoint = currentPt
                        lastReachedPointIndex = pointIndex
                        lastReachedPathIndex = pathIndex
                         
                         // Case 3: End Point Highlighted -> Stop Timer & Auto Stop
                         if (currentPt.type == WeldPointType.END) {
                             if (isWelding) {
                                 isWeldingStatsActive = false
                                 Log.d("WeldStats", "Timer STOPPED (Highlight: End Point)")
                             }
                         }
                     }
                }
            }
        }
    }

    init {
        socketManager.start()

        viewModelScope.launch {
            socketManager.robotErrorEvent.collect { errCode ->
                val errorInfo = com.gbndt.shijiaoqi.model.RobotErrorCodes.map[errCode]
                val errorToAdd = errorInfo ?: com.gbndt.shijiaoqi.model.RobotError(errCode, "未知故障", "请参考手册")
                if (!currentRobotErrors.contains(errorToAdd)) {
                    currentRobotErrors.add(errorToAdd)
                }
                isRobotErrorDialogVisible = true
            }
        }
    }

    private var tts: TextToSpeech? = null
    
    val weldPaths = mutableStateListOf<WeldPath>()
    val pouchProjects = mutableStateListOf<ProjectChoice>()
    val pouchProcesses = mutableStateListOf<ProcessChoice>()
    private var pouchProjectId: UUID? = null
    var selectedWeldPathIndex by mutableStateOf(0)
    // -1 覆盖主工艺；-2 新增附加工艺；>=0 替换对应附加工艺
    var extraProcessEditIndex by mutableStateOf(-1)
    var isCornerParamsUnlocked by mutableStateOf(false)

    fun unlockCornerParams(password: String): Boolean {
        if (password == CORNER_PARAM_PASSWORD) {
            isCornerParamsUnlocked = true
            return true
        }
        return false
    }

    fun lockCornerParams() {
        isCornerParamsUnlocked = false
    }

    var isRenameDialogVisible by mutableStateOf(false)
    var newWeldPathName by mutableStateOf("")

    // Current/Voltage Dialog State
    var isCurrentVoltageDialogVisible by mutableStateOf(false)
    var inputCurrent by mutableStateOf("")
    var inputVoltage by mutableStateOf("")
    var savedCurrent by mutableStateOf(0.0)
    var savedVoltage by mutableStateOf(0.0)

    // List Scroll State
    var listScrollIndex by mutableStateOf(0)
    var listScrollOffset by mutableStateOf(0)

    // Missing Process Dialog State
    var isMissingProcessDialogVisible by mutableStateOf(false)
    var missingProcessMessage by mutableStateOf("")

    // 工程管理相关状态
    override var currentProjectName by mutableStateOf<String?>(null) // 存储工程的相对路径
    
    // Project Explorer State
    override var projectCurrentPath by mutableStateOf("")
    override val projectItems = mutableStateListOf<FileSystemItem>()

    // Process Explorer State
    override var processCurrentPath by mutableStateOf("")
    override val processItems = mutableStateListOf<FileSystemItem>()

    // Status Bar State
    override var toolCoordinateSystem by mutableStateOf("工具1")
    override var operationPosition by mutableStateOf(Pose(0.0, 0.0, 0.0, 0.0, 0.0, 0.0))
    
    // UI Events
    private val _toastEvent = MutableSharedFlow<String>()
    val toastEvent = _toastEvent.asSharedFlow()

    private val _scrollToIndexEvent = MutableSharedFlow<Int>()
    val scrollToIndexEvent = _scrollToIndexEvent.asSharedFlow()

    override var positionMode by mutableStateOf("右") // New
    var simulationSpeed by mutableStateOf(100)
    override var speedMode by mutableStateOf("1倍") // New
    override var isInstallPosDialogVisible by mutableStateOf(false)
    override var installPos by mutableStateOf(0) // 0-平装, 1-侧装, 2-挂装
    override var alarmStatus by mutableStateOf("无故障")
    override var connectionStatus by mutableStateOf("已连接")
    override var weldingLength by mutableStateOf(0.0)
    override var weldingDuration by mutableStateOf(0L)
    override var weldingBreakOffState by mutableStateOf("正常")
    private var isResuming = false
    override var weldArcState by mutableStateOf("正常")

    override var extAxisPos by mutableStateOf(0.0)
    override var extAxisReady by mutableStateOf(false)
    override var isExtAxisEnabled by mutableStateOf(true)
    
    override fun toggleExtAxisEnabled() {
        isExtAxisEnabled = !isExtAxisEnabled
    }

    // Registration State
    override var isRegistered by mutableStateOf(false)
    var lastConnectionTime: Long = 0L
    override var machineCode by mutableStateOf("")

    // Update State
    override var isUpdateDialogVisible by mutableStateOf(false)
    override var updateInfo by mutableStateOf<UpdateInfo?>(null)
    var isDownloading by mutableStateOf(false)
    private var downloadId: Long = -1L

    // Dialog Visibility States
    override var isPositionDialogVisible by mutableStateOf(false)
    override var isSpeedDialogVisible by mutableStateOf(false)

    override var isRobotErrorDialogVisible by mutableStateOf(false)
    override val currentRobotErrors = mutableStateListOf<com.gbndt.shijiaoqi.model.RobotError>()

    // Tool Coordinates State
    override val toolCoordinates = mutableStateListOf<Pose?>().apply {
        repeat(14) { add(null) }
    }
    override val toolRemarks = mutableStateListOf<String>().apply {
        repeat(14) { add("") }
    }
    override var isToolListDialogVisible by mutableStateOf(false)
    override var isToolEditDialogVisible by mutableStateOf(false)
    override var editingToolIndex by mutableStateOf(-1) // 0-13
    override var editingToolPose by mutableStateOf(Pose(0.0, 0.0, 0.0, 0.0, 0.0, 0.0))
    override var editingToolRemark by mutableStateOf("")

    // Joystick States
    override var isControllerActive by mutableStateOf(false)
    var joyX1 by mutableStateOf(0f)
    var joyY1 by mutableStateOf(0f)
    var joyX2 by mutableStateOf(0f)
    var joyY2 by mutableStateOf(0f)
    var btnUp by mutableStateOf(false)
    var btnDown by mutableStateOf(false)
    var btnLeft by mutableStateOf(false)
    var btnRight by mutableStateOf(false)
    var btnL1 by mutableStateOf(false)
    var btnL2 by mutableStateOf(false)
    var btnR2 by mutableStateOf(false)

    // Welding State
    var isWelding by mutableStateOf(false)
    var isSimulating by mutableStateOf(false)
    var isWeldingStatsActive by mutableStateOf(false)
    
    // External Control State (from Robot IO)
    var isWireFeeding by mutableStateOf(false)
    var isRecording by mutableStateOf(false)
    var isDragEnabled by mutableStateOf(false)

    private var weldingTimerJob: kotlinx.coroutines.Job? = null
    private var alarmVoiceJob: kotlinx.coroutines.Job? = null
    private var lastReachedPoint: WeldPoint? = null
    private var lastReachedPointIndex: Int = -1
    private var lastReachedPathIndex: Int = -1

    // ID -> Triple(PathIndex, PointIndex, ExtraProcessIndex) ExtraProcessIndex=-1 表示主工艺
    private val commandIdMap = mutableMapOf<Int, Triple<Int, Int, Int>>()
    private val commandLineMap = mutableMapOf<Int, Int>()
    private var globalCommandId = 1000

    val fineTune = FineTuneSupport(
        scope = viewModelScope,
        socketManager = socketManager,
        nextCommandId = { globalCommandId++ },
        toast = { msg -> viewModelScope.launch { _toastEvent.emit(msg) } },
        currentProcess = { currentActiveWeldPath?.process },
        onOscillationChanged = { osc ->
            val path = currentActiveWeldPath ?: return@FineTuneSupport
            path.process = path.process.copy(oscillation = osc)
        }
    )

    // Batch Execution Queue
    private data class ExecutionGroup(
        val commandString: String,
        val lastCommandId: Int,
        val description: String
    )
    private val executionQueue = ArrayDeque<ExecutionGroup>()
    private var currentExecutionGroup: ExecutionGroup? = null
    var isExecutingBatch by mutableStateOf(false)

    private inner class BatchCommandBuilder(
        private val description: String
    ) {
        private var currentSb = StringBuilder()
        private var lastId = -1
        private var lineCount = 0

        fun appendCmd(cmd: String, id: Int? = null) {
            lineCount++
            currentSb.append(cmd).append("\r\n")
            if (id != null) {
                lastId = id
                commandLineMap[lineCount] = id
            }
        }

        fun flush() {
            if (currentSb.isNotEmpty()) {
                val allCommands = currentSb.toString()
                
                viewModelScope.launch {
                    val id105 = globalCommandId++
                    val id106 = globalCommandId++
                    val plan = WeldRun.batch(id105, id106, allCommands)
                    socketManager.sendBatchCommandSync(plan.fileName)
                    delay(WeldRun.AFTER_FILENAME_MS)
                    val responseJob = async(Dispatchers.Default) {
                        withTimeoutOrNull(8000) {
                            kotlinx.coroutines.flow.merge(socketManager.receivedText8082, socketManager.receivedText)
                                .first { it.contains(id106.toString()) || it.contains("106") }
                        }
                    }
                    delay(100)
                    socketManager.sendBatchCommandSync(plan.body)
                    val ack = responseJob.await()
                    if (ack == null) {
                        Log.e("BatchCommand", "Timeout waiting for 106 ACK!")
                    } else {
                        Log.d("BatchCommand", "Received 106 ACK: $ack")
                    }
                    delay(WeldRun.AFTER_BODY_ACK_MS)
                    socketManager.sendControlCommand(plan.modeAuto)
                    delay(WeldRun.AFTER_MODE_MS)
                    socketManager.sendControlCommand(plan.start)
                }
                
                val groupDesc = "$description (All in one)"
                executionQueue.add(ExecutionGroup(
                    commandString = "106_BATCH_SENT",
                    lastCommandId = lastId,
                    description = groupDesc
                ))
                currentSb = StringBuilder()
            }
        }

        fun hasPending(): Boolean = currentSb.isNotEmpty()
    }

    fun startBatchExecution() {
        if (executionQueue.isEmpty()) {
            viewModelScope.launch { _toastEvent.emit("没有可执行的命令") }
            return
        }
        isExecutingBatch = true
        sendNextGroup()
    }

    private fun sendNextGroup() {
        if (executionQueue.isEmpty()) {
            isExecutingBatch = false
            currentExecutionGroup = null
            viewModelScope.launch { _toastEvent.emit("执行完成") }
            return
        }

        val group = executionQueue.removeFirst()
        currentExecutionGroup = group
        
        Log.d("BatchExecution", "Sending Group: ${group.description}")
        if (group.commandString != "106_BATCH_SENT") {
            socketManager.sendControlCommand(group.commandString)
        }
    }

    // Interface Implementation

    override fun toggleControllerActive() {
        isControllerActive = !isControllerActive
    }

    override fun stopControllerActive() {
        isControllerActive = false
    }

    // Position History for Pause/Resume
    data class RobotState(val pose: Pose, val joints: List<Double>)

    private val positionHistory = ArrayDeque<RobotState>(500)
    var isPaused by mutableStateOf(false)
    private var pauseState: RobotState? = null
    private var pausePathIndex: Int = -1
    // 必须先见到「运行」再把「停止」当成结束，否则上传/Start 前的空闲状态会把暂停按钮清掉
    private var programHasStarted = false

    init {
        // Initialize TTS
        tts = TextToSpeech(getApplication()) { status ->
            if (status == TextToSpeech.SUCCESS) {
                val result = tts?.setLanguage(Locale.CHINA)
                if (result == TextToSpeech.LANG_MISSING_DATA || result == TextToSpeech.LANG_NOT_SUPPORTED) {
                    Log.e("WeldPathViewModel", "TTS: Chinese language is not supported or missing data")
                }
            } else {
                Log.e("WeldPathViewModel", "TTS: Initialization failed")
            }
        }

        // Listen for Robot Pose (8083)
        viewModelScope.launch {
            socketManager.robotPose.collect { pose ->
                if (pose != null) {
                    operationPosition = pose
                    // Add to history only if moved > 0.1mm to preserve trajectory
                    val last = positionHistory.lastOrNull()
                    val shouldAdd = if (last == null) true else {
                        val dx = last.pose.x - pose.x
                        val dy = last.pose.y - pose.y
                        val dz = last.pose.z - pose.z
                        (dx*dx + dy*dy + dz*dz) > 0.01 // 0.1mm^2
                    }

                    if (shouldAdd) {
                        val joints = socketManager.robotJoints.value
                        if (positionHistory.size >= 500) {
                            positionHistory.removeFirst()
                        }
                        positionHistory.addLast(RobotState(pose, joints))
                    }
                }
            }
        }

        // Listen for Program State
        viewModelScope.launch {
            socketManager.programState.collect { state ->
                // 1-停止； 2-运行； 3-暂停； 4-拖动
                if (state == 2) {
                    programHasStarted = true
                    if (isPaused && !isResuming) isPaused = false
                } else if (state == 3) {
                    if (!isPaused && !isResuming) isPaused = true
                } else if (state == 1) {
                    if (isPaused || isResuming) {
                        // 点暂停后会切到手动，控制器报停止，不能当成焊接结束
                    } else if ((isWelding || isSimulating || isExecutingBatch) && programHasStarted) {
                        if (weldingBreakOffState == "中断") {
                            isPaused = true
                        } else {
                            programHasStarted = false
                            isWelding = false
                            isSimulating = false
                            isExecutingBatch = false
                            markPouchWelding(false)
                            weldingTimerJob?.cancel()
                            viewModelScope.launch { _toastEvent.emit("执行完成") }
                        }
                    }
                }
            }
        }

        // Listen for Current Line (8083)
        var lastProcessedLine = 0
        viewModelScope.launch {
            socketManager.progCurLine.collect { line ->
                if (!isExecutingBatch && !isWelding && !isSimulating) {
                    lastProcessedLine = 0
                }
                if (line > 0 && (isExecutingBatch || isWelding || isSimulating)) {
                    if (line < lastProcessedLine) {
                        lastProcessedLine = 0
                    }
                    if (line > lastProcessedLine) {
                        for (l in (lastProcessedLine + 1)..line) {
                            val id = commandLineMap[l]
                            if (id != null) {
                                handleCommandExecuted(id)
                            }
                        }
                        lastProcessedLine = line
                    }
                }
            }
        }

        // Listen for 8080 text feedback
        viewModelScope.launch {
            socketManager.receivedText.collect { text ->
                // Check for Registration Machine Code (ID 826)
                val regexMac = Regex("/f/bIII\\d+III826III\\d+III(.+?)III/b/f")
                val matchMac = regexMac.find(text)
                if (matchMac != null) {
                    val mac = matchMac.groupValues[1]
                    Log.d("Registration", "Received MAC: $mac")
                    machineCode = mac
                    
                    // Auto-login / Verification
                    val savedLicense = projectManager.loadLicense()
                    if (savedLicense.isNotEmpty()) {
                        val expectedCode = encryptMac("P${mac}L")
                        if (savedLicense == expectedCode) {
                            if (!isRegistered) {
                                isRegistered = true
                            }
                            lastConnectionTime = System.currentTimeMillis()
                            saveAppSettings()
                            Log.d("Registration", "License Verified")
                        } else {
                            // Validation Failed!
                            Log.w("Registration", "License Validation Failed! MAC mismatch or invalid key.")
                            if (isRegistered) {
                                isRegistered = false
                                saveAppSettings()
                                viewModelScope.launch {
                                    _toastEvent.emit("注册校验失败，请重新注册")
                                }
                            }
                        }
                    }
                }

                // Check for command feedback format: /f/bIII{ID}III{TYPE}III1III1III/b/f
                // Regex to extract ID. Modified to accept any command type (e.g. 201 for MoveL, 248 for ARCEnd)
                val regex = Regex("/f/bIII(\\d+)III\\d+III1III1III/b/f")
                val matchResult = regex.find(text)
                if (matchResult != null) {
                    val idString = matchResult.groupValues[1]
                    val id = idString.toIntOrNull()
                    if (id != null) {
                        // Check for Batch Execution Progress (Independent of Mapping)
                        // This ensures we proceed to next group even if the last command (e.g., ArcEnd) is not mapped.
                        if (isExecutingBatch && currentExecutionGroup != null && id == currentExecutionGroup!!.lastCommandId) {
                            Log.d("BatchExecution", "Group Finished: ${currentExecutionGroup!!.description}")
                            sendNextGroup()
                        }
                    }
                }
            }
        }

        // 初始化列表
        refreshProjectExplorer()
        refreshProcessExplorer()
        // 初始化一个默认焊道
        addWeldPath()

        // Load app settings
        val settings = projectManager.loadAppSettings()
        
        isExtAxisEnabled = settings.isExtAxisEnabled
        
        // Tool Coordinates
        toolCoordinates.clear()
        if (settings.toolCoordinates.size == 14) {
            toolCoordinates.addAll(settings.toolCoordinates)
        } else {
            repeat(14) { toolCoordinates.add(null) }
            settings.toolCoordinates.forEachIndexed { index, pose ->
                if (index < 14) toolCoordinates[index] = pose
            }
        }
        
        // Tool Remarks
        toolRemarks.clear()
        if (settings.toolRemarks.size == 14) {
            toolRemarks.addAll(settings.toolRemarks)
        } else {
            repeat(14) { toolRemarks.add("") }
            settings.toolRemarks.forEachIndexed { index, remark ->
                if (index < 14) toolRemarks[index] = remark
            }
        }
        
        if (settings.selectedToolIndex in 0 until 14) {
             toolCoordinateSystem = "工具${settings.selectedToolIndex + 1}"
        }
        
        // Position and Speed
        positionMode = settings.positionMode
        speedMode = settings.speedMode
        installPos = settings.installPos
        
        // Load Welding Stats
        weldingLength = settings.totalWeldingLength
        weldingDuration = settings.totalWeldingDuration
        
        // Load Welding Current/Voltage
        savedCurrent = settings.weldingCurrent
        savedVoltage = settings.weldingVoltage

        syncFromPouch()
        if (weldPaths.isEmpty()) addWeldPath()

        // Start Socket Manager
        // socketManager.start() - Moved to init


        viewModelScope.launch {
            socketManager.connectionStatus.collect { status ->
                val previousStatus = connectionStatus
                connectionStatus = status
                
                // When status changes from "未连接" to "已连接", send current tool coordinate command
                if (previousStatus != "已连接" && status == "已连接") {
                    val toolIndex = try {
                        toolCoordinateSystem.removePrefix("工具").toInt()
                    } catch (e: Exception) { 1 }
                    
                    if (toolIndex in 1..14) {
                        val pose = toolCoordinates[toolIndex - 1]
                        if (pose != null) {
                            Log.d("WeldPathViewModel", "Connection established, auto-sending Tool $toolIndex")
                            // Wait a bit to ensure socket is fully ready
                            delay(500) 
                            sendToolCoordCommand(toolIndex, pose)
                        }
                    }
                    
                    // Auto-send Installation Position
                    delay(200)
                    val installCmd = "SetRobotInstallPos($installPos)"
                    val installMsg = "/f/bIII23III337III${installCmd.length}III${installCmd}III/b/f"
                    socketManager.sendControlCommand(installMsg)
                }
            }
        }
        
        // Listen for Robot Data
        viewModelScope.launch {
            socketManager.robotPose.collect { pose ->
                if (pose != null) {
                    operationPosition = pose
                    // Record position history
                    // We need joints too. We assume robotJoints is updated synchronously or very close.
                    val joints = socketManager.robotJoints.value
                    if (joints.size >= 6) {
                        if (positionHistory.size >= 500) {
                            positionHistory.removeFirst()
                        }
                        positionHistory.addLast(RobotState(pose, joints))
                    }
                }
            }
        }

        // Listen for Welding State
        viewModelScope.launch {
            socketManager.weldingBreakOffState.collect { state ->
                weldingBreakOffState = state
            }
        }
        viewModelScope.launch {
            socketManager.weldArcState.collect { state ->
                weldArcState = state
            }
        }
        viewModelScope.launch {
            socketManager.extAxisPos.collect { pos ->
                extAxisPos = pos
            }
        }
        viewModelScope.launch {
            socketManager.extAxisReady.collect { ready ->
                extAxisReady = ready
            }
        }

        // Listen for Alarm Status
        viewModelScope.launch {
            socketManager.alarmStatus.collect { status ->
                if (alarmStatus != status) {
                    // Stop previous loop if any
                    alarmVoiceJob?.cancel()
                    alarmVoiceJob = null

                    if (status != "无故障" && status != "无报警") {
                         // Start continuous loop
                         alarmVoiceJob = launch {
                             while (isActive) {
                                 tts?.speak(status, TextToSpeech.QUEUE_FLUSH, null, null)
                                 delay(5000) // Repeat every 5 seconds
                             }
                         }
                    } else {
                        // Stop speaking immediately when cleared
                        tts?.stop()
                    }
                }
                alarmStatus = status
            }
        }
        
        // Listen for Robot Input Signal (Custom IO Logic)
        viewModelScope.launch {
            socketManager.robotInputSignal.collect { combined ->
                when {
                     // 2690 < combined < 2890 且当前未在送丝
                     combined in 2690..2890 && !isWireFeeding -> {
                         startWireFeed()
                         isWireFeeding = true
                     }
                     // combined > 4096 且当前正在送丝
                     combined > 4096 && isWireFeeding -> {
                         stopWireFeed()
                         isWireFeeding = false
                     }
                     // 2048 < combined < 2248 且当前未在录点
                     combined in 2048..2248 && !isRecording -> {
                         collectData()
                         isRecording = true
                     }
                     // combined > 4096 且当前正在录点
                     combined > 4096 && isRecording -> {
                         isRecording = false
                     }
                     // 754 < combined < 954 且当前未在拖动
                     combined in 754..954 && !isDragEnabled -> {
                         dragTeachSwitch(true)
                         isDragEnabled = true
                     }
                     // combined > 4096 且当前正在拖动
                     combined > 4096 && isDragEnabled -> {
                         dragTeachSwitch(false)
                         isDragEnabled = false
                     }
                }
            }
        }

        val currentTime = System.currentTimeMillis()
        val threeDaysInMillis = 3L * 24 * 60 * 60 * 1000
        
        lastConnectionTime = settings.lastConnectionTime
        
        if (lastConnectionTime > 0 && (currentTime - lastConnectionTime) > threeDaysInMillis) {
            // Expired
            isRegistered = false
            projectManager.clearLicense()
            saveAppSettings()
            viewModelScope.launch {
                _toastEvent.emit("注册已过期，请重新注册")
            }
        } else {
            isRegistered = settings.isRegistered
            // Optimistic Registration: If license exists, assume registered temporarily
            // Verification will happen asynchronously when MAC is received.
            if (!isRegistered && projectManager.hasLicense()) {
                isRegistered = true
            }
        }

        // Register Update Receiver
        registerDownloadReceiver()
        
        // Start Servo Cart Loop
        startServoCartLoop()
    }

    private fun startServoCartLoop() {
        viewModelScope.launch {
            while (isActive) {
                if (isControllerActive) {
                    sendServoCartCommand()
                }
                delay(200)
            }
        }
    }



    private var isLastCommandZero = false

    private fun sendServoCartCommand() {
        // Calculate Inputs
        // Right Stick Up(x)/Down(-x): joyY2 < -0.5 => 1, joyY2 > 0.5 => -1
        val joyFwd = when {
            joyY2 < -0.5 -> 1
            joyY2 > 0.5 -> -1
            else -> 0
        }
        
        // Right Stick Left(y)/Right(-y): joyX2 < -0.5 => 1, joyX2 > 0.5 => -1
        val joyLeft = when {
            joyX2 < -0.5 -> 1
            joyX2 > 0.5 -> -1
            else -> 0
        }

        // D-Pad Up(z)/Down(-z)
        val z = when {
            btnUp -> 1
            btnDown -> -1
            else -> 0
        }

        // Left Stick Up(rx)/Down(-rx): joyY1 < -0.5 => 1, joyY1 > 0.5 => -1
        val rotFwd = when {
            joyY1 < -0.5 -> 1
            joyY1 > 0.5 -> -1
            else -> 0
        }

        // Left Stick Left(ry)/Right(-ry): joyX1 < -0.5 => 1, joyX1 > 0.5 => -1
        val rotLeft = when {
            joyX1 < -0.5 -> 1
            joyX1 > 0.5 -> -1
            else -> 0
        }

        // D-Pad Left(rz)/Right(-rz)
        val rz = when {
            btnLeft -> 1
            btnRight -> -1
            else -> 0
        }

        val speed_s = if (btnL1) 10.0 else 1.0
        val speed_r = if (btnL1) 1.0 else 0.2
        val ext1 = when {
            btnR2 -> -5
            btnL2 -> 5
            else -> 0
        }

        val isZeroCommand = joyFwd == 0 && joyLeft == 0 && z == 0 && rotFwd == 0 && rotLeft == 0 && rz == 0 && ext1 == 0
        if (isZeroCommand) {
            if (isLastCommandZero) return
            isLastCommandZero = true
        } else {
            isLastCommandZero = false
        }

        socketManager.sendControlCommand(
            RobotCommands.servoCart(positionMode, joyFwd, joyLeft, z, rotFwd, rotLeft, rz, speed_s, speed_r, ext1),
        )
    }
    
    fun enableExtAxisServo() {
        val cmd = "ExtAxisServoOn(1,1)"
        sendManualCommand(RobotCommands.TYPE_EXT_SERVO, cmd)
    }

    fun startExtAxisJog(direction: Int) {
        if (!isControllerActive) return
        sendManualCommand(RobotCommands.TYPE_EXT_JOG, "ExtAxisStartJog(6,1,$direction,100,100,2000)")
    }

    fun stopExtAxisJog(direction: Int) {
        if (!isControllerActive) return
        sendManualCommand(RobotCommands.TYPE_EXT_JOG_STOP, "StopExtAxisJog")
    }

    fun sendMoveLCommand() {
        val currentPath = currentActiveWeldPath ?: return
        val point = currentPath.points.getOrNull(currentPath.selectedPointIndex) ?: return
        
        var targetPose = point.pose
        val targetJoints = point.jointAngles

        if (targetPose != null) {
            val toolIndex = try {
                toolCoordinateSystem.removePrefix("工具").toInt()
            } catch (e: Exception) { 1 }

            val finalJoints = if (targetJoints != null && targetJoints.size >= 6) targetJoints else List(6) { 0.0 }

            val pos2 = listOf(
                finalJoints[0], finalJoints[1], finalJoints[2], finalJoints[3], finalJoints[4], finalJoints[5],
                targetPose.x, targetPose.y, targetPose.z, targetPose.rx, targetPose.ry, targetPose.rz
            ).joinToString(",") { String.format(Locale.US, "%.3f", it) }
            val ext1Str = String.format(Locale.US, "%.3f", targetPose.ext1)
            
            if (isExtAxisEnabled) {
                val extAxisCmd = "ExtAxisMoveJ(1,$ext1Str,0.000,0.000,0.000,100,0)"
                val msgExt = "/f/bIII${66}1III201III${extAxisCmd.length}III${extAxisCmd}III/b/f"
                socketManager.sendControlCommand(msgExt)
                Thread.sleep(50)
            }

            val cmd2 = "MoveL($pos2,$toolIndex,0,100,100,100,-1,0,$ext1Str,0.000,0.000,0.000,0,0,0,0,0,0,0,0,100,0)"
            val msg2 = "/f/bIII${66}2III201III${cmd2.length}III${cmd2}III/b/f"
            Log.d("MoveLCommand", "Standard MoveL: $msg2")
            Log.e("sendMoveLCommand", "msg2: $msg2")

            socketManager.sendControlCommand(msg2)
        }
    }

    override fun resetAllError() {
        val msg = "/f/bIII7III107III15IIIResetAllError()III/b/f"
        socketManager.sendControlCommand(msg)
    }

    override fun clearStats() {
        weldingLength = 0.0
        weldingDuration = 0L
        saveAppSettings()
    }



    // --- Multi-Layer Logic Removed ---


    // 刷新工程浏览器
    override fun refreshProjectExplorer() {
        projectItems.clear()
        projectItems.addAll(projectManager.listContents(projectCurrentPath, "single"))
    }
    
    // 导航到工程目录
    override fun navigateProject(item: FileSystemItem) {
        if (item.isDirectory) {
            projectCurrentPath = item.path
            refreshProjectExplorer()
        }
    }
    
    // 返回上一级工程目录
    override fun navigateProjectBack() {
        if (projectCurrentPath.isNotEmpty()) {
            val parent = File(projectCurrentPath).parent
            projectCurrentPath = parent?.replace("\\", "/") ?: ""
            refreshProjectExplorer()
        }
    }
    
    // 创建工程文件夹
    override fun createProjectFolder(name: String) {
        if (projectManager.createFolder(projectCurrentPath, name, "single")) {
            refreshProjectExplorer()
        }
    }

    // 创建新工程
    override fun createProject(name: String) {
        createNewProjectInCurrentPath(name)
    }
    
    fun createNewProjectInCurrentPath(name: String) {
        if (projectManager.createProject(projectCurrentPath, name, "single")) {
            val fullPath = if (projectCurrentPath.isEmpty()) name else "$projectCurrentPath/$name"
            currentProjectName = fullPath
            
            // Reset to default state
            weldPaths.clear()
            
            addWeldPath() // Adds a default single layer path with empty points
            saveCurrentProject()
            refreshProjectExplorer()
        }
    }

    // Helper to get current active path (Single or Multi Base)
    private val currentActiveWeldPath: WeldPath?
        get() {
            return if (selectedWeldPathIndex in weldPaths.indices) {
                weldPaths[selectedWeldPathIndex]
            } else null
        }
    
    // Helper to update current active path
    private fun updateCurrentActiveWeldPath(newPath: WeldPath) {
        if (selectedWeldPathIndex in weldPaths.indices) {
            weldPaths[selectedWeldPathIndex] = newPath
            saveCurrentProject()
        }
    }
    
    // ... existing init ...
    
    override fun openProject(path: String) {
        viewModelScope.launch { _toastEvent.emit("请从本机袋打开工程") }
    }

    private fun bag(): BagSession = (getApplication() as ShiJiaoQiApp).bag

    private fun markPouchWelding(on: Boolean) {
        runCatching { bag().setWelding(on) }
    }

    private fun processSource(): PouchProcessSource = PouchProcessSource(bag().pouch)

    private fun refsOf(paths: List<WeldPath>): List<ProcessRef> {
        val out = mutableListOf<ProcessRef>()
        paths.forEach { path ->
            out.add(ProcessRef(path.name, path.processId, path.isEnabled))
            path.extraProcesses.forEach { slot ->
                out.add(ProcessRef("${path.name} 附加", slot.processId, slot.isEnabled))
            }
        }
        return out
    }

    private fun applyLoaded(paths: List<WeldPath>, loaded: Map<UUID, WeldProcess>) {
        paths.forEach { path ->
            path.processId.toUuidOrNull()?.let { id -> loaded[id]?.let { path.process = it } }
            path.extraProcesses.forEachIndexed { extraIdx, slot ->
                slot.processId.toUuidOrNull()?.let { id ->
                    loaded[id]?.let { path.extraProcesses[extraIdx] = slot.copy(process = it) }
                }
            }
        }
    }

    private fun String.toUuidOrNull(): UUID? = try {
        UUID.fromString(trim())
    } catch (_: Exception) {
        null
    }

    private fun bindPouchProcesses(paths: List<WeldPath>): Boolean {
        val outcome = ProcessBind.resolve(refsOf(paths), processSource())
        applyLoaded(paths, outcome.loaded)
        if (outcome.missing.isNotEmpty()) {
            missingProcessMessage = "以下焊道的工艺不在当前闭包：\n" + outcome.missing.joinToString("\n")
            isMissingProcessDialogVisible = true
            return false
        }
        return true
    }

    fun syncFromPouch() {
        refreshPouchLists()
        val bag = bag()
        val id = bag.pouch.activeProject() ?: return
        val bytes = try {
            bag.open(id)
        } catch (_: Exception) {
            return
        }
        try {
            val loaded = try {
                SingleLayerProject.parse(bytes)
            } catch (e: Exception) {
                Log.e("WeldPathViewModel", "active project parse failed", e)
                emptyList()
            }
            weldPaths.clear()
            weldPaths.addAll(loaded)
            if (weldPaths.isEmpty()) addWeldPath()
            selectedWeldPathIndex = 0
            pouchProjectId = id
            currentProjectName = bag.pouch.exportClosures().firstOrNull { it.assetId == id }?.name
            bindPouchProcesses(weldPaths)
        } finally {
            Wm2.zero(bytes)
        }
        refreshPouchLists()
    }

    fun activatePouchProject(id: UUID) {
        try {
            bag().activate(id)
            syncFromPouch()
        } catch (e: Exception) {
            viewModelScope.launch { _toastEvent.emit(e.message ?: "无法激活工程") }
        }
    }

    fun refreshPouchLists() {
        val bag = runCatching { bag() }.getOrNull() ?: return
        val active = bag.pouch.activeProject()
        pouchProjects.clear()
        val self = bag.pouch.boundClient()
        bag.pouch.exportClosures().filter { it.kind == Pouch.KIND_PROJECT && self != null && it.targetClientId == self }.forEach {
            pouchProjects.add(ProjectChoice(it.assetId, it.name, it.revision, it.assetId == active))
        }
        pouchProcesses.clear()
        pouchProcesses.addAll(PouchProcessSource(bag.pouch).list())
    }

    fun bindProcessFromPouch(processId: UUID?) {
        if (weldPaths.isEmpty() || selectedWeldPathIndex !in weldPaths.indices) {
            extraProcessEditIndex = -1
            return
        }
        val currentPath = weldPaths[selectedWeldPathIndex]
        val process = if (processId == null) WeldProcess() else {
            processSource().open(processId) ?: run {
                missingProcessMessage = "闭包里没有这条工艺"
                isMissingProcessDialogVisible = true
                extraProcessEditIndex = -1
                return
            }
        }
        val idStr = processId?.toString().orEmpty()
        when {
            extraProcessEditIndex == -2 -> {
                if (processId != null) {
                    currentPath.extraProcesses.add(
                        WeldPathProcessSlot(
                            id = UUID.randomUUID().toString(),
                            process = process,
                            processId = idStr,
                            isEnabled = true
                        )
                    )
                }
            }
            extraProcessEditIndex >= 0 && extraProcessEditIndex < currentPath.extraProcesses.size -> {
                val slot = currentPath.extraProcesses[extraProcessEditIndex]
                currentPath.extraProcesses[extraProcessEditIndex] = slot.copy(
                    process = process,
                    processId = idStr
                )
            }
            else -> {
                weldPaths[selectedWeldPathIndex] = currentPath.copy(
                    process = process,
                    processId = idStr
                )
            }
        }
        extraProcessEditIndex = -1
        saveCurrentProject()
    }

    fun saveCurrentProject() {
        val id = pouchProjectId ?: return
        val bag = runCatching { bag() }.getOrNull() ?: return
        weldPaths.forEach { PouchSave.weldPath(bag, it) }
        PouchSave.project(bag, id, SingleLayerProject.encode(weldPaths.toList()))
    }

    fun copyCurrentProject(newName: String) {
        val currentPath = currentProjectName ?: return
        // 复制到当前所在目录（或者根目录？）
        // 简单起见，复制到同级目录
        val parentPath = File(currentPath).parent?.replace("\\", "/") ?: ""
        if (projectManager.copyProject(currentPath, parentPath, newName, "single")) {
            refreshProjectExplorer()
        }
    }
    
    override fun deleteProjectItem(item: FileSystemItem) {
        if (projectManager.deleteItem(item.path, "single")) {
            if (currentProjectName == item.path) {
                currentProjectName = null
                weldPaths.clear()
                addWeldPath()
            }
            refreshProjectExplorer()
        }
    }
    
    // --- Process Explorer Methods ---
    override fun refreshProcessExplorer() {
        processItems.clear()
        processItems.addAll(processManager.listContents(processCurrentPath))
    }
    
    override fun navigateProcess(item: FileSystemItem) {
        if (item.isDirectory) {
            processCurrentPath = item.path
            refreshProcessExplorer()
        }
    }
    
    override fun navigateProcessBack() {
        if (processCurrentPath.isNotEmpty()) {
            val parent = File(processCurrentPath).parent
            processCurrentPath = parent?.replace("\\", "/") ?: ""
            refreshProcessExplorer()
        }
    }
    
    override fun createProcessFolder(name: String) {
        if (processManager.createFolder(processCurrentPath, name)) {
            refreshProcessExplorer()
        }
    }

    override fun createProcess(name: String, process: WeldProcess) {
        val bag = runCatching { bag() }.getOrNull() ?: return
        val body = ProcessJson.encode(process.copy(name = name))
        try {
            bag.issuePersonal(Pouch.KIND_PROCESS, name, body)
            refreshPouchLists()
        } catch (_: Exception) {
        } finally {
            Wm2.zero(body)
        }
    }

    override fun updateProcess(item: FileSystemItem, process: WeldProcess) {
        saveProcess(process)
    }

    override fun importProcess(uri: Uri) {
        importProcessFile(uri, processCurrentPath, "")
    }

    override fun importProcessZip(uri: Uri) {
        unzipProcessFile(uri, processCurrentPath)
    }

    // Update Standard Process Library
    override fun updateStandardProcessLibrary(url: String) {
        viewModelScope.launch {
            _toastEvent.emit("本机不保存标准工艺库文件")
        }
    }

    override fun exportProcess(item: FileSystemItem): File? = null

    override fun exportProcessZip(item: FileSystemItem): File? = null

    override fun selectProcess(item: FileSystemItem) {
        loadProcessToCurrentWeldPath(item)
    }

    fun getProcessFile(relativePath: String): File {
        return processManager.getFile(relativePath)
    }

    fun zipProcessFolder(relativePath: String, zipFile: File): Boolean {
        return processManager.zipFileOrFolder(relativePath, zipFile)
    }

    fun unzipProcessFile(zipUri: Uri, destPath: String): Boolean {
        val result = processManager.unzip(zipUri, destPath)
        if (result) refreshProcessExplorer()
        return result
    }

    fun importProcessFile(uri: Uri, destPath: String, fileName: String): Boolean {
        val result = processManager.importFile(uri, destPath, fileName)
        if (result) refreshProcessExplorer()
        return result
    }

    override fun saveProcess(process: WeldProcess) {
        if (weldPaths.isNotEmpty() && selectedWeldPathIndex in weldPaths.indices) {
            val currentPath = weldPaths[selectedWeldPathIndex]
            if (currentPath.process.name == process.name) {
                weldPaths[selectedWeldPathIndex] = currentPath.copy(process = process)
            } else {
                val extraIdx = currentPath.extraProcesses.indexOfFirst { it.process.name == process.name }
                if (extraIdx >= 0) {
                    val slot = currentPath.extraProcesses[extraIdx]
                    currentPath.extraProcesses[extraIdx] = slot.copy(process = process)
                }
            }
        }
        saveCurrentProject()
    }
    
    override fun loadProcess(path: String): WeldProcess? {
        return processManager.loadProcess(path)
    }
    
    fun beginAddProcessVariant(pathIndex: Int) {
        extraProcessEditIndex = -2
        selectedWeldPathIndex = pathIndex
    }

    fun beginReplaceExtraProcess(pathIndex: Int, extraIndex: Int) {
        extraProcessEditIndex = extraIndex
        selectedWeldPathIndex = pathIndex
    }

    fun cancelAddProcessVariant() {
        extraProcessEditIndex = -1
    }

    fun toggleExtraProcessEnabled(pathIndex: Int, extraIndex: Int) {
        val path = weldPaths.getOrNull(pathIndex) ?: return
        if (extraIndex !in path.extraProcesses.indices) return
        val slot = path.extraProcesses[extraIndex]
        path.extraProcesses[extraIndex] = slot.copy(isEnabled = !slot.isEnabled)
        saveCurrentProject()
    }

    fun deleteExtraProcess(pathIndex: Int, extraIndex: Int) {
        val path = weldPaths.getOrNull(pathIndex) ?: return
        if (extraIndex !in path.extraProcesses.indices) return
        path.extraProcesses.removeAt(extraIndex)
        saveCurrentProject()
    }

    fun loadProcessToCurrentWeldPath(item: FileSystemItem) {
        extraProcessEditIndex = -1
    }
    
    override fun deleteProcessItem(item: FileSystemItem) {
        if (processManager.deleteItem(item.path)) {
            refreshProcessExplorer()
        }
    }

    override fun startUpdateDownload() {
        val info = updateInfo ?: return
        if (isDownloading) return
        isUpdateDialogVisible = false
        isDownloading = true
        
        viewModelScope.launch {
            _toastEvent.emit("开始下载更新...")
        }
        
        // We still provide a name hint, but we will query the actual name later
        val fileName = "ShiJiaoQi_v${info.versionCode}.apk"
        currentDownloadFileName = fileName
        downloadId = updateManager.downloadApk(info.downloadUrl, fileName)
        Log.d("UpdateManager", "Started download. ID: $downloadId, URL: ${info.downloadUrl}")
        
        // Start polling for download status
        startDownloadPolling(downloadId)
    }

    override fun checkRegistration() {
        val command = "GetControlBoxNetMacAddr(0)"
        val len = command.length
        val msg = "/f/bIII123III826III${len}III${command}III/b/f"
        Log.d("Registration", "Fetching Machine Code: $msg")
        socketManager.sendControlCommand(msg)
    }

    override fun register(code: String): Boolean {
        if (machineCode.isNotEmpty()) {
            val expectedCode = encryptMac("P${machineCode}L")
            if (code == expectedCode) {
                isRegistered = true
                lastConnectionTime = System.currentTimeMillis()
                saveAppSettings()
                projectManager.saveLicense(code)
                viewModelScope.launch {
                    _toastEvent.emit("注册成功")
                }
                return true
            } else {
                viewModelScope.launch {
                    _toastEvent.emit("注册码无效")
                }
            }
        } else {
             viewModelScope.launch {
                _toastEvent.emit("未获取到机器码")
            }
        }
        return false
    }
    
    private fun encryptMac(mac: String): String {
        try {
            val digest = java.security.MessageDigest.getInstance("SHA-256")
            val hash = digest.digest(mac.toByteArray(java.nio.charset.StandardCharsets.UTF_8))
            val hexString = StringBuilder()
            for (b in hash) {
                val hex = Integer.toHexString(0xff and b.toInt())
                if (hex.length == 1) hexString.append('0')
                hexString.append(hex)
            }
            val fullHash = hexString.toString()
            return if (fullHash.length > 12) fullHash.takeLast(12) else fullHash
        } catch (e: Exception) {
            e.printStackTrace()
            return ""
        }
    }

    // --- Existing Methods (kept for compatibility or logic) ---
    // Some are replaced by above.
    
    fun deleteCurrentProject() {
        val name = currentProjectName ?: return
        if (projectManager.deleteItem(name)) {
            currentProjectName = null
            weldPaths.clear()
            addWeldPath() 
            refreshProjectExplorer()
        }
    }

    fun addWeldPath() {
        val id = UUID.randomUUID().toString()
        val name = "焊道 ${weldPaths.size + 1}"
        val points = mutableStateListOf(
            WeldPoint(UUID.randomUUID().toString(), WeldPointType.START_SAFE),
            WeldPoint(UUID.randomUUID().toString(), WeldPointType.START),
            WeldPoint(UUID.randomUUID().toString(), WeldPointType.END),
            WeldPoint(UUID.randomUUID().toString(), WeldPointType.END_SAFE)
        )
        val process = WeldProcess()
        val weldPath = WeldPath(id, name, points, process)
        weldPaths.add(weldPath)
        selectedWeldPathIndex = weldPaths.lastIndex
        saveCurrentProject()

        // Scroll to new item
        viewModelScope.launch {
            _scrollToIndexEvent.emit(weldPaths.lastIndex)
        }
    }

    private fun getPathDirectionVector(path: WeldPath, cornerPoint: Point3D): Point3D? {
        val pts = path.points.filter { it.type == WeldPointType.START || it.type == WeldPointType.MIDDLE || it.type == WeldPointType.END }
        if (pts.size < 2) return null

        var minD = Double.MAX_VALUE
        var bestI = 0
        for (i in pts.indices) {
            val pt3D = pts[i].pose?.toPoint3D() ?: continue
            val d = pt3D.distanceTo(cornerPoint)
            if (d < minD) {
                minD = d
                bestI = i
            }
        }

        val p1 = pts[bestI].pose!!.toPoint3D()
        // 找到靠近角落的点后，使用相邻的点计算方向 (p2 - p1 指向远离角落的方向)
        val p2 = if (bestI == 0) {
            pts[1].pose!!.toPoint3D()
        } else {
            pts[bestI - 1].pose!!.toPoint3D()
        }

        return p2 - p1
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
        process: WeldProcess? = null,
        processId: String? = null
    ) {
        val refPathA = weldPaths.getOrNull(refPathAIndex) ?: return
        val refPathB = weldPaths.getOrNull(refPathBIndex) ?: return
        val baseProcessId = processId?.takeIf { it.isNotEmpty() } ?: refPathA.processId
        val baseProcess = baseProcessId.toUuidOrNull()?.let { processSource().open(it) }
            ?: process
            ?: refPathA.process

        val cornerPose: Pose
        
        val ptsA = refPathA.points.filter { it.type == WeldPointType.START || it.type == WeldPointType.MIDDLE || it.type == WeldPointType.END }
        val ptsB = refPathB.points.filter { it.type == WeldPointType.START || it.type == WeldPointType.MIDDLE || it.type == WeldPointType.END }
        
        if (ptsA.size < 2 || ptsB.size < 2) {
            viewModelScope.launch { _toastEvent.emit("参考焊道缺少足够的点，无法计算交点！") }
            return
        }
        
        // 找到两焊道中距离最近的两个点
        var minD = Double.MAX_VALUE
        var bestI = 0
        var bestJ = 0
        for (i in ptsA.indices) {
            for (j in ptsB.indices) {
                val pA = ptsA[i].pose?.toPoint3D() ?: continue
                val pB = ptsB[j].pose?.toPoint3D() ?: continue
                val d = pA.distanceTo(pB)
                if (d < minD) {
                    minD = d
                    bestI = i
                    bestJ = j
                }
            }
        }
        
        // 使用这两个最近点和它们相邻的点构造线段进行求交
        val pA1 = ptsA[bestI].pose!!.toPoint3D()
        val pA2 = if (bestI == 0) ptsA[1].pose!!.toPoint3D() else ptsA[bestI - 1].pose!!.toPoint3D()
        
        val pB1 = ptsB[bestJ].pose!!.toPoint3D()
        val pB2 = if (bestJ == 0) ptsB[1].pose!!.toPoint3D() else ptsB[bestJ - 1].pose!!.toPoint3D()
        
        val intersection = com.gbndt.shijiaoqi.domain.weld.CornerWeldGenerator.calculate3DIntersection(pA1, pA2, pB1, pB2)
        
        if (intersection == null) {
            viewModelScope.launch { _toastEvent.emit("无法计算交点，两条焊道平行或距离异常") }
            return
        }
        
        // 自动交点模式下，姿态继承自参考焊道B（立焊）的最近点，坐标用求出的交点
        cornerPose = ptsB[bestJ].pose!!.copy(x = intersection.x, y = intersection.y, z = intersection.z)

        val cornerPoint3D = cornerPose.toPoint3D()
        val vecA = getPathDirectionVector(refPathA, cornerPoint3D) ?: return
        val vecB = getPathDirectionVector(refPathB, cornerPoint3D) ?: return

        // 尝试获取安全起点和安全终点 (因为我们已经规定参考焊道B为立焊，所以直接提取B的安全点)
        val safeStartPose = refPathB.points.firstOrNull { it.type == WeldPointType.START_SAFE }?.pose
        val safeEndPose = refPathB.points.firstOrNull { it.type == WeldPointType.START_SAFE }?.pose

        val weldOrientation = if (torchRx != null && torchRy != null && torchRz != null) {
            Pose(0.0, 0.0, 0.0, torchRx, torchRy, torchRz)
        } else {
            null
        }

        val newPaths = com.gbndt.shijiaoqi.domain.weld.CornerWeldGenerator.generateCornerPaths(
            baseProcess = baseProcess,
            processId = baseProcessId,
            cornerPose = cornerPose,
            safeStartPose = safeStartPose,
            safeEndPose = safeEndPose,
            weldOrientation = weldOrientation,
            vecAIn = vecA,
            vecBIn = vecB,
            layerCount = layerCount,
            initialLength = initialLength,
            upwardOffset = upwardOffset,
            lengthReduction = lengthReduction,
            refPathAId = refPathA.id,
            refPathBId = refPathB.id
        )

        viewModelScope.launch {
            _toastEvent.emit("正在通过逆运动学计算关节角度，请稍候...")
            
            for (path in newPaths) {
                for (pt in path.points) {
                    if (pt.pose != null && pt.jointAngles == null) {
                        val joints = socketManager.getInverseKin(pt.pose!!)
                        if (joints != null && joints.size >= 6) {
                            pt.jointAngles = joints
                        }
                    }
                }
            }

            // Delete the old group if updating
            if (updateGroupId != null) {
                weldPaths.removeAll { it.cornerGroupParams?.groupId == updateGroupId }
            }

            // Add the generated paths to the end
            newPaths.forEachIndexed { i, p ->
                weldPaths.add(p.copy(name = "包角焊道 ${weldPaths.size + 1} (层 ${i + 1})"))
            }

            saveCurrentProject()
            _toastEvent.emit("成功生成 $layerCount 层包角工艺")
            _scrollToIndexEvent.emit(weldPaths.lastIndex)
        }
    }

    // --- Duplicates Removed ---


    fun deleteWeldPath(index: Int) {
        if (weldPaths.isNotEmpty() && index >= 0 && index < weldPaths.size) {
            val pathToDelete = weldPaths[index]
            val cornerGroupParams = pathToDelete.cornerGroupParams
            
            if (cornerGroupParams != null) {
                // Delete all paths in the same corner group
                val groupId = cornerGroupParams.groupId
                weldPaths.removeAll { it.cornerGroupParams?.groupId == groupId }
            } else {
                weldPaths.removeAt(index)
            }
            
            if (selectedWeldPathIndex >= weldPaths.size) {
                selectedWeldPathIndex = weldPaths.lastIndex
            }
            saveCurrentProject()
        }
    }

    fun moveWeldPath(fromIndex: Int, toIndex: Int) {
        val item = weldPaths.removeAt(fromIndex)
        weldPaths.add(toIndex, item)
        selectedWeldPathIndex = toIndex
        saveCurrentProject()
    }

    fun renameWeldPath(newName: String) {
        if (newName.isEmpty()) return
        
        if (weldPaths.isNotEmpty()) {
            weldPaths[selectedWeldPathIndex] = weldPaths[selectedWeldPathIndex].copy(name = newName)
            saveCurrentProject()
        }
    }

    fun toggleWeldPathEnabled(index: Int) {
        if (index >= 0 && index < weldPaths.size) {
            val path = weldPaths[index]
            weldPaths[index] = path.copy(isEnabled = !path.isEnabled)
            saveCurrentProject()
        }
    }

    fun addMiddlePoint() {
        val currentWeldPath = currentActiveWeldPath ?: return
        val endIndex = currentWeldPath.points.indexOfFirst { it.type == WeldPointType.END }
        
        if (endIndex > 0) {
            val middlePoints = currentWeldPath.points.filter { it.type == WeldPointType.MIDDLE }
            val newMiddlePoint = WeldPoint(
                id = UUID.randomUUID().toString(),
                type = WeldPointType.MIDDLE
            )
            currentWeldPath.points.add(endIndex, newMiddlePoint)
            saveCurrentProject()
        }
    }

    fun addArcMiddlePoint() {
        val currentWeldPath = currentActiveWeldPath ?: return
        val endIndex = currentWeldPath.points.indexOfFirst { it.type == WeldPointType.END }
        
        if (endIndex > 0) {
            val arcMiddlePoints = currentWeldPath.points.filter { it.type == WeldPointType.ARC_MIDDLE }
            val newArcMiddlePoint = WeldPoint(
                id = UUID.randomUUID().toString(),
                type = WeldPointType.ARC_MIDDLE
            )
            currentWeldPath.points.add(endIndex, newArcMiddlePoint)
            saveCurrentProject()
        }
    }

    fun movePoint(weldPathIndex: Int, fromIndex: Int, toIndex: Int) {
        val weldPath = if (weldPathIndex < weldPaths.size) weldPaths[weldPathIndex] else null
        ?: return
        
        val points = weldPath.points
        
        // 检查是否是可移动的中间点
        if (fromIndex < 1 || toIndex < 1 || fromIndex >= points.size - 1 || toIndex >= points.size - 1) {
            return
        }
        
        val point = points[fromIndex]
        if (point.type != WeldPointType.MIDDLE && point.type != WeldPointType.ARC_MIDDLE) {
            return
        }
        
        val temp = points.removeAt(fromIndex)
        points.add(toIndex, temp)
        
        // 更新选中的点位索引
        var newSelectedPointIndex = weldPath.selectedPointIndex
        if (newSelectedPointIndex == fromIndex) {
            newSelectedPointIndex = toIndex
        } else if (newSelectedPointIndex > fromIndex && newSelectedPointIndex <= toIndex) {
            newSelectedPointIndex--
        } else if (newSelectedPointIndex < fromIndex && newSelectedPointIndex >= toIndex) {
            newSelectedPointIndex++
        }
        
        if (newSelectedPointIndex != weldPath.selectedPointIndex) {
            weldPaths[weldPathIndex] = weldPath.copy(selectedPointIndex = newSelectedPointIndex)
        }
        saveCurrentProject()
    }

    fun selectPoint(weldPathIndex: Int, pointIndex: Int) {
        if (weldPathIndex < weldPaths.size && pointIndex < weldPaths[weldPathIndex].points.size) {
            // 切换到当前点击的焊道
            if (selectedWeldPathIndex != weldPathIndex) {
                selectedWeldPathIndex = weldPathIndex
            }
            
            val path = weldPaths[weldPathIndex]
            if (path.selectedPointIndex != pointIndex) {
                weldPaths[weldPathIndex] = path.copy(selectedPointIndex = pointIndex)
            }
        }
    }

    fun collectData() {
        if (connectionStatus == "未连接") {
            viewModelScope.launch {
                _toastEvent.emit("设备未连接")
            }
            return
        }

        val currentWeldPath = currentActiveWeldPath ?: return
        val pointIndex = currentWeldPath.selectedPointIndex
        if (pointIndex < 0 || pointIndex >= currentWeldPath.points.size) return

        val currentPoint = currentWeldPath.points[pointIndex]
        
        // Use real data if available, otherwise simulation data
        val currentPose = socketManager.robotPose.value
        val currentJoints = socketManager.robotJoints.value
        val snap = Capture.snapshot(currentPose, currentJoints)
        val updatedPoint = if (snap != null) {
            currentPoint.copy(pose = snap.first, jointAngles = snap.second)
        } else {
            currentPoint.copy(
                pose = Pose(100.0, 200.0, 300.0, 0.0, 0.0, 0.0),
                jointAngles = listOf(0.0, 0.0, 0.0, 0.0, 0.0, 0.0)
            )
        }
        
        currentWeldPath.points[pointIndex] = updatedPoint
        
        // Auto-select next point
        val nextPointIndex = if (pointIndex < currentWeldPath.points.size - 1) pointIndex + 1 else pointIndex
        
        if (nextPointIndex != pointIndex) {
            updateCurrentActiveWeldPath(currentWeldPath.copy(selectedPointIndex = nextPointIndex))
        } else {
            // Check if we need to jump to next path
            if (selectedWeldPathIndex < weldPaths.size - 1) {
                 val nextPathIndex = selectedWeldPathIndex + 1
                 selectedWeldPathIndex = nextPathIndex
                 val nextWeldPath = weldPaths[nextPathIndex]
                 if (nextWeldPath.selectedPointIndex != 0) {
                     weldPaths[nextPathIndex] = nextWeldPath.copy(selectedPointIndex = 0)
                 }
                 saveCurrentProject()
                 return
            }
            updateCurrentActiveWeldPath(currentWeldPath)
        }
    }

    // 新增：采集 X 方向参考点
    fun collectRefX() {
        if (connectionStatus == "未连接") {
            viewModelScope.launch { _toastEvent.emit("设备未连接") }
            return
        }

        val currentWeldPath = currentActiveWeldPath ?: return
        val pointIndex = currentWeldPath.selectedPointIndex
        if (pointIndex < 0 || pointIndex >= currentWeldPath.points.size) return

        val currentPoint = currentWeldPath.points[pointIndex]
        // 只能在 START 或 MIDDLE 点设置 RefX (用于定义该点开始的段的 X 方向)
        // 用户说 "采集完起点之后...用于定义x方向"。
        
        val currentPose = socketManager.robotPose.value
        val currentJoints = socketManager.robotJoints.value
        
        if (currentPose != null && currentJoints.isNotEmpty()) {
            val refPoint = RefPoint(currentPose, currentJoints)
            val updatedPoint = currentPoint.copy(refPointX = refPoint)
            currentWeldPath.points[pointIndex] = updatedPoint
            saveCurrentProject() // Save changes
            viewModelScope.launch { _toastEvent.emit("X方向参考点已记录") }
        }
    }
    
    // 新增：清除 X 方向参考点
    fun clearRefX() {
        val currentWeldPath = currentActiveWeldPath ?: return
        val pointIndex = currentWeldPath.selectedPointIndex
        if (pointIndex < 0 || pointIndex >= currentWeldPath.points.size) return

        val currentPoint = currentWeldPath.points[pointIndex]
        if (currentPoint.refPointX != null) {
            val updatedPoint = currentPoint.copy(refPointX = null)
            currentWeldPath.points[pointIndex] = updatedPoint
            saveCurrentProject()
            viewModelScope.launch { _toastEvent.emit("X方向参考点已清除") }
        }
    }

    fun clearPointData() {
        val currentWeldPath = currentActiveWeldPath ?: return
        val pointIndex = currentWeldPath.selectedPointIndex
        if (pointIndex < 0 || pointIndex >= currentWeldPath.points.size) return

        val currentPoint = currentWeldPath.points[pointIndex]
        
        // Clear Data
        val updatedPoint = currentPoint.copy(
            pose = null,
            jointAngles = null
        )
        currentWeldPath.points[pointIndex] = updatedPoint
        
        updateCurrentActiveWeldPath(currentWeldPath)
    }
    
    fun deletePoint(weldPathIndex: Int, pointIndex: Int) {
        val weldPath = if (weldPathIndex < weldPaths.size) weldPaths[weldPathIndex] else null
        ?: return
        
        val points = weldPath.points
        
        // Check valid index for deletion (not Start/End/Safe)
        if (pointIndex < 1 || pointIndex >= points.size - 1) {
            return
        }
        
        val point = points[pointIndex]
        if (point.type != WeldPointType.MIDDLE && point.type != WeldPointType.ARC_MIDDLE) {
            return
        }
        
        // Remove
        points.removeAt(pointIndex)
        
        // Update selection index if needed
        var newSelectedPointIndex = weldPath.selectedPointIndex
        if (newSelectedPointIndex >= pointIndex) {
            newSelectedPointIndex = (newSelectedPointIndex - 1).coerceAtLeast(0)
        }
        
        val newWeldPath = weldPath.copy(selectedPointIndex = newSelectedPointIndex)
        weldPaths[weldPathIndex] = newWeldPath
        saveCurrentProject()
    }

    // --- Tool Coordinate Methods ---
    private fun sendToolCoordCommand(toolIndex: Int, pose: Pose) {
        val cmd = "SetToolCoord($toolIndex,${pose.x},${pose.y},${pose.z},${pose.rx},${pose.ry},${pose.rz},0,0,0,0)"
        val len = cmd.length
        val msg = "/f/bIII21III316III${len}III${cmd}III/b/f"
        socketManager.sendControlCommand(msg)
    }

    fun openToolList() {
        isToolListDialogVisible = true
    }

    override fun selectTool(index: Int) {
        if (index in 0 until 14) {
            val pose = toolCoordinates[index]
            if (pose == null) {
                // Open edit dialog if empty
                openToolEdit(index)
            } else {
                // Select tool
                toolCoordinateSystem = "工具${index + 1}"
                isToolListDialogVisible = false
                saveAppSettings()
                
                // Send command
                sendToolCoordCommand(index + 1, pose)
            }
        }
    }

    override fun openToolEdit(index: Int) {
        if (index in 0 until 14) {
            editingToolIndex = index
            editingToolPose = toolCoordinates[index] ?: Pose(0.0, 0.0, 0.0, 0.0, 0.0, 0.0)
            editingToolRemark = toolRemarks[index]
            isToolEditDialogVisible = true
            isToolListDialogVisible = false // Close list dialog
        }
    }

    override fun saveToolEdit(pose: Pose, remark: String) {
        if (editingToolIndex in 0 until 14) {
            toolCoordinates[editingToolIndex] = pose
            toolRemarks[editingToolIndex] = remark
            isToolEditDialogVisible = false
            
            // Auto-select the edited tool
            toolCoordinateSystem = "工具${editingToolIndex + 1}"
            
            // Save settings
            saveAppSettings()
            
            // Send command
            sendToolCoordCommand(editingToolIndex + 1, pose)
        }
    }

    override fun cancelToolEdit() {
        isToolEditDialogVisible = false
        // Re-open list dialog so user can choose another or see list
        isToolListDialogVisible = true
    }

    fun setWeldingCurrentVoltage() {
        val current = inputCurrent.toDoubleOrNull()
        val voltage = inputVoltage.toDoubleOrNull()

        if (current == null || current < 0 || current > 1000) {
            viewModelScope.launch {
                _toastEvent.emit("电流值无效 (0-1000)")
            }
            return
        }
        if (voltage == null || voltage < 0 || voltage > 1000) {
            viewModelScope.launch {
                _toastEvent.emit("电压值无效 (0-1000)")
            }
            return
        }

        // Save values
        savedCurrent = current
        savedVoltage = voltage
        saveAppSettings()

        // Format values (remove decimal if integer) to ensure command compatibility
        val currentStr = if (current % 1.0 == 0.0) current.toInt().toString() else current.toString()
        val voltageStr = if (voltage % 1.0 == 0.0) voltage.toInt().toString() else voltage.toString()

        // Send Current
        // Use globalCommandId for packet ID to ensure uniqueness/sequence
        val cmdCurrent = "WeldingSetCurrent(0,$currentStr,0)"
        val lenCurrent = cmdCurrent.length
        val msgCurrent = "/f/bIII${globalCommandId++}III201III${lenCurrent}III${cmdCurrent}III/b/f"
        Log.d("WeldPathViewModel", "Sending Current: $msgCurrent")
        socketManager.sendControlCommand(msgCurrent)

        // Send Voltage
        val cmdVoltage = "WeldingSetVoltage(0,$voltageStr,1)"
        val lenVoltage = cmdVoltage.length
        val msgVoltage = "/f/bIII${globalCommandId++}III201III${lenVoltage}III${cmdVoltage}III/b/f"
        Log.d("WeldPathViewModel", "Sending Voltage: $msgVoltage")
        socketManager.sendControlCommand(msgVoltage)

        isCurrentVoltageDialogVisible = false
        
        viewModelScope.launch {
            _toastEvent.emit("电流电压设置指令已发送")
        }
    }

    private fun appendCommandsForPath(
        weldPath: WeldPath, 
        builder: BatchCommandBuilder, 
        pathIndexForMap: Int, 
        toolIndex: Int,
        startIndex: Int = 0,
        isResumeFromStop: Boolean = false,
        extraProcessIndex: Int = -1
    ) {
        val points = weldPath.points
        val process = weldPath.process

        // Calculate speed multiplier based on mode
        val multiplier = if (isSimulating) {
            when (speedMode) {
                "3倍" -> 3
                "5倍" -> 5
                else -> 1
            }
        } else {
            1
        }
        val finalSpeed = (process.speed * multiplier).toInt()
         
         // --- 1. Welding Process Parameters Command ---
         val paramCmd = "WeldingSetProcessParam(2,${process.startArcCurrent},${process.startArcVoltage},${process.startArcTime},${process.current},${process.voltage},${process.endArcCurrent},${process.endArcVoltage},${process.endArcTime})"
         val paramId = globalCommandId++
         builder.appendCmd(paramCmd, paramId)

          // --- 2. Oscillation Parameters Command ---
          val oscType = process.oscillation.type
          if (oscType != "无摆动") {
              val typeCode = when (oscType) {
                  "三角波摆动" -> 0
                  "直角L型三角波摆动" -> 1
                  "圆形摆动-顺时针" -> 2
                  "圆形摆动-逆时针" -> 3
                  "正弦波摆动" -> 4
                  "垂直L型正弦波摆动" -> 5
                  "立焊三角摆动" -> 6
                  else -> 0 // Default
              }
              
              val waitTimeCode = if (process.oscillation.waitTime == "不包括") 0 else 1
              val posWaitCode = if (process.oscillation.positionWait == "等待时间内位置继续移动") 0 else 1
              
              val weaveCmd = "WeaveSetPara(3,$typeCode,${process.oscillation.frequency},$waitTimeCode,${process.oscillation.amplitude},${process.oscillation.leftSideLength},${process.oscillation.rightSideLength},${process.oscillation.zeroTime},${process.oscillation.leftStopTime},${process.oscillation.rightStopTime},${process.oscillation.callbackRatio},$posWaitCode,${process.oscillation.azimuth},${process.oscillation.inclination})"
              val weaveId = globalCommandId++
              builder.appendCmd(weaveCmd, weaveId)
          }
          
          var pointIndex = startIndex
          var hasStartedArc = false
          
          if (isResumeFromStop && stopPointPose != null && stopPointJoints != null && stopPointJoints!!.size >= 6) {
              val vPose = stopPointPose!!
              val vJoints = stopPointJoints!!
              
              val pos2 = listOf(
                  vJoints[0], vJoints[1], vJoints[2], vJoints[3], vJoints[4], vJoints[5],
                  vPose.x, vPose.y, vPose.z, vPose.rx, vPose.ry, vPose.rz
              ).joinToString(",") { String.format(Locale.US, "%.3f", it) }
              
              val moveSpeed = 100 // 空走回到断点
              val ext1Str = String.format(Locale.US, "%.3f", vPose.ext1)
              
              if (isExtAxisEnabled) {
                  val extAxisCmd = "ExtAxisMoveJ(1,$ext1Str,0.000,0.000,0.000,$moveSpeed,-1)"
                  val extAxisId = globalCommandId++
                  builder.appendCmd(extAxisCmd, extAxisId)
              }

              val cmd2 = "MoveL($pos2,$toolIndex,0,100,100,$moveSpeed,-1,0,$ext1Str,0.000,0.000,0.000,0,0,0,0,0,0,0,0,100,0)"
              
              val id = globalCommandId++
              commandIdMap[id] = Triple(pathIndexForMap, stopPointIndex, extraProcessIndex)
              builder.appendCmd(cmd2, id)
              
              val startPointIndex = points.indexOfFirst { it.type == WeldPointType.START }
              
              if (stopPointIndex > startPointIndex && startPointIndex != -1) {
                  // Robot was already welding when it stopped. Resume welding from here.
                  if (isWelding) {
                      val arcStartCmd = "ARCStart(0,2,10000)"
                      val arcStartId = globalCommandId++
                      builder.appendCmd(arcStartCmd, arcStartId)
                  }
                  if (oscType != "无摆动") {
                      val weaveStartCmd = "WeaveStart(3)"
                      val weaveStartId = globalCommandId++
                      builder.appendCmd(weaveStartCmd, weaveStartId)
                  }
                  hasStartedArc = true
              }
              
              // Continue to the point we were moving towards when stopped
              pointIndex = stopPointIndex
              
              // NEW LOGIC: Arc Resume
              // If stopped during an arc, we dynamically calculate a new intermediate point
              // from the current physical position to properly resume the arc.
              var isArcResume = false
              var arcStartIndex = -1
              
              // Protect pointIndex from being negative when accessing points
              val safeIndex = if (pointIndex >= 0) pointIndex else 0
              
              android.util.Log.e("ArcDebug", "stopPointIndex: $stopPointIndex, pointIndex: $pointIndex, safeIndex: $safeIndex")
              
              // Find which segment of the arc we are in based on safeIndex (the point we are heading towards)
              if (safeIndex > 0 && safeIndex < points.size && points[safeIndex].type == WeldPointType.END && points[safeIndex - 1].type == WeldPointType.ARC_MIDDLE) {
                  // Heading towards END. The arc is START -> ARC_MIDDLE -> END
                  arcStartIndex = safeIndex - 2
                  isArcResume = true
                  android.util.Log.e("ArcDebug", "isArcResume: TRUE (Heading towards END)")
              } else if (safeIndex > 0 && safeIndex < points.size && points[safeIndex].type == WeldPointType.ARC_MIDDLE && points[safeIndex - 1].type == WeldPointType.START) {
                  // Heading towards ARC_MIDDLE. The arc is START -> ARC_MIDDLE -> END
                  arcStartIndex = safeIndex - 1
                  isArcResume = true
                  android.util.Log.e("ArcDebug", "isArcResume: TRUE (Heading towards ARC_MIDDLE)")
              } else if (safeIndex < points.size - 2 && points[safeIndex].type == WeldPointType.START && points[safeIndex + 1].type == WeldPointType.ARC_MIDDLE && points[safeIndex + 2].type == WeldPointType.END) {
                  // Heading towards START. We haven't even reached the start of the arc yet!
                  isArcResume = false
                  android.util.Log.e("ArcDebug", "isArcResume: FALSE (Heading towards START)")
              } else {
                  // EXTENDED LOGIC:
                  if (safeIndex < points.size && points[safeIndex].type == WeldPointType.ARC_MIDDLE && safeIndex > 0 && points[safeIndex - 1].type == WeldPointType.START) {
                      arcStartIndex = safeIndex - 1
                      isArcResume = true
                      android.util.Log.e("ArcDebug", "isArcResume: TRUE (Standing ON ARC_MIDDLE)")
                  } else if (safeIndex < points.size && points[safeIndex].type == WeldPointType.END && safeIndex > 1 && points[safeIndex - 1].type == WeldPointType.ARC_MIDDLE) {
                      arcStartIndex = safeIndex - 2
                      isArcResume = true
                      android.util.Log.e("ArcDebug", "isArcResume: TRUE (Standing ON END)")
                  } else {
                      android.util.Log.e("ArcDebug", "isArcResume: FALSE (Not in Arc Segment)")
                  }
              }
              
              if (isArcResume && arcStartIndex >= 0 && arcStartIndex + 2 < points.size) {
                  val pStartPt = points[arcStartIndex]
                  val pMidPt = points[arcStartIndex + 1]
                  val pEndPt = points[arcStartIndex + 2]
                  
                  val pStart = pStartPt.pose
                  val pMid = pMidPt.pose
                  val pEnd = pEndPt.pose
                  val jEnd = pEndPt.jointAngles ?: listOf(0.0, 0.0, 0.0, 0.0, 0.0, 0.0)
                  
                  if (pStart != null && pMid != null && pEnd != null) {
                      // 1. Calculate the new midpoint in Physical Space because stopPointPose is physical
                      val (startOffX, startOffY, startOffZ) = calculateOffset(pStartPt, points, arcStartIndex, process)
                      val physStart = pStart.copy(x = pStart.x + startOffX, y = pStart.y + startOffY, z = pStart.z + startOffZ)
                      
                      val (midOff, endOff) = calculateArcOffset(pMidPt, pEndPt, points, arcStartIndex + 1, process)
                      val physMid = pMid.copy(x = pMid.x + midOff.first, y = pMid.y + midOff.second, z = pMid.z + midOff.third)
                      val physEnd = pEnd.copy(x = pEnd.x + endOff.first, y = pEnd.y + endOff.second, z = pEnd.z + endOff.third)
                      
                      val newMidPose = calculateNewArcMidPoint(physStart, physMid, physEnd, stopPointPose!!)
                      
                      android.util.Log.e("ArcDebug", "========= SingleLayer Arc Debug =========")
                      android.util.Log.e("ArcDebug", "Original Start (Physical): ${physStart.x}, ${physStart.y}, ${physStart.z}")
                      android.util.Log.e("ArcDebug", "Original Mid (Physical):   ${physMid.x}, ${physMid.y}, ${physMid.z}")
                      android.util.Log.e("ArcDebug", "Original End (Physical):   ${physEnd.x}, ${physEnd.y}, ${physEnd.z}")
                      android.util.Log.e("ArcDebug", "Stop Point (Current):      ${stopPointPose!!.x}, ${stopPointPose!!.y}, ${stopPointPose!!.z}")
                      android.util.Log.e("ArcDebug", "New Mid Point calculated:  ${newMidPose.x}, ${newMidPose.y}, ${newMidPose.z}")
                      android.util.Log.e("ArcDebug", "=========================================")

                      val newMidJoints = interpolateJoints(stopPointJoints!!, jEnd)
                      
                      // 2. For the midpoint, pass the Physical Cartesian Pose and interpolated joints
                      //    And disable offset for Midpoint (Enable=0) since it is already physical.
                      val midPosStr = listOf(
                          newMidJoints[0], newMidJoints[1], newMidJoints[2], newMidJoints[3], newMidJoints[4], newMidJoints[5],
                          newMidPose.x, newMidPose.y, newMidPose.z, newMidPose.rx, newMidPose.ry, newMidPose.rz
                      ).joinToString(",") { String.format(Locale.US, "%.3f", it) }
                      
                      // 3. For the endpoint, pass the ORIGINAL Virtual Pose and ORIGINAL Joint Angles
                      //    And let the controller apply the offset (Enable=1) natively!
                      val endPosStr = listOf(
                          jEnd[0], jEnd[1], jEnd[2], jEnd[3], jEnd[4], jEnd[5],
                          pEnd.x, pEnd.y, pEnd.z, pEnd.rx, pEnd.ry, pEnd.rz
                      ).joinToString(",") { String.format(Locale.US, "%.3f", it) }
                      
                      // CRITICAL FIX: Even if there is no offset, we MUST force Enable=1 (1,0,0,0,0,0,0) for the midpoint.
                      // Why? Because newMidPose (Cartesian) and newMidJoints (Interpolated) do NOT mathematically match perfectly.
                      // If Enable=0, the controller strictly verifies Joints vs Cartesian and throws "Instruction Point Error".
                      // If Enable=1, the controller runs Inverse Kinematics (IK) to recalculate the exact joints for the Cartesian point, masking our interpolation error!
                      val midOffsetStr = "1,0.000,0.000,0.000,0.000,0.000,0.000"
                      val endOffsetStr = if (endOff.first == 0.0 && endOff.second == 0.0 && endOff.third == 0.0) {
                          "0,0,0,0,0,0,0"
                      } else {
                          "3,${String.format(Locale.US, "%.3f", endOff.first)},${String.format(Locale.US, "%.3f", endOff.second)},${String.format(Locale.US, "%.3f", endOff.third)},0,0,0"
                      }
                      
                      val moveC = "MoveC($midPosStr,$toolIndex,0,100,100,0,0,0,0,$midOffsetStr,$endPosStr,$toolIndex,0,100,100,0,0,0,0,$endOffsetStr,$finalSpeed,-1)"
                      
                      val arcId = globalCommandId++
                      commandIdMap[arcId] = Triple(pathIndexForMap, arcStartIndex + 2, extraProcessIndex)
                      builder.appendCmd(moveC, arcId)
                      
                      if (pEndPt.type == WeldPointType.END) {
                          if (isWelding) {
                              val arcEndCmd = "ARCEnd(0,2,10000)"
                              val arcEndId = globalCommandId++
                              builder.appendCmd(arcEndCmd, arcEndId)
                          }
                          if (oscType != "无摆动") {
                              val weaveEndCmd = "WeaveEnd(0)"
                              val weaveEndId = globalCommandId++
                              builder.appendCmd(weaveEndCmd, weaveEndId)
                          }
                      }
                      
                      // Skip all arc points, go to the point AFTER the arc END
                      pointIndex = arcStartIndex + 3
                  }
              }
          } else {
              // Not an arc resume, just normal resume point
              pointIndex = if (stopPointIndex >= 0) stopPointIndex else 0
          }
          
          while (pointIndex >= 0 && pointIndex < points.size) {
            val point = points[pointIndex]
            val pose = point.pose
            val joints = point.jointAngles
            
            if (pose != null) {
                val jointsStr = if (joints != null && joints.size >= 6) {
                    joints.take(6).joinToString(",") { String.format(Locale.US, "%.3f", it) }
                } else "0,0,0,0,0,0"

                // Check for MoveC (ARC_MIDDLE)
                if (point.type == WeldPointType.ARC_MIDDLE && pointIndex + 1 < points.size) {
                    val nextPoint = points[pointIndex + 1]
                    val nextPose = nextPoint.pose
                    val nextJoints = nextPoint.jointAngles
                    
                    if (nextPose != null) {
                        val nextJointsStr = if (nextJoints != null && nextJoints.size >= 6) {
                            nextJoints.take(6).joinToString(",") { String.format(Locale.US, "%.3f", it) }
                        } else "0,0,0,0,0,0"

                        // Prepare Mid Point Data
                        val midPosStr = "${jointsStr},${String.format(Locale.US, "%.3f", pose.x)},${String.format(Locale.US, "%.3f", pose.y)},${String.format(Locale.US, "%.3f", pose.z)},${String.format(Locale.US, "%.3f", pose.rx)},${String.format(Locale.US, "%.3f", pose.ry)},${String.format(Locale.US, "%.3f", pose.rz)}"
                        
                        // Prepare End Point Data
                        val endPosStr = "${nextJointsStr},${String.format(Locale.US, "%.3f", nextPose.x)},${String.format(Locale.US, "%.3f", nextPose.y)},${String.format(Locale.US, "%.3f", nextPose.z)},${String.format(Locale.US, "%.3f", nextPose.rx)},${String.format(Locale.US, "%.3f", nextPose.ry)},${String.format(Locale.US, "%.3f", nextPose.rz)}"
                        
                        // Calculate Arc Offsets
                        val (midOff, endOff) = calculateArcOffset(point, nextPoint, points, pointIndex, process)
                        
                        val midOffsetStr = if (midOff.first == 0.0 && midOff.second == 0.0 && midOff.third == 0.0) {
                            "0,0,0,0,0,0,0"
                        } else {
                            "3,${String.format(Locale.US, "%.3f", midOff.first)},${String.format(Locale.US, "%.3f", midOff.second)},${String.format(Locale.US, "%.3f", midOff.third)},0,0,0"
                        }
                        
                        val endOffsetStr = if (endOff.first == 0.0 && endOff.second == 0.0 && endOff.third == 0.0) {
                            "0,0,0,0,0,0,0"
                        } else {
                            "3,${String.format(Locale.US, "%.3f", endOff.first)},${String.format(Locale.US, "%.3f", endOff.second)},${String.format(Locale.US, "%.3f", endOff.third)},0,0,0"
                        }
                        
                        val ext1Str = String.format(Locale.US, "%.3f", nextPose.ext1)
                        if (isExtAxisEnabled) {
                            val extAxisCmd = "ExtAxisMoveJ(1,$ext1Str,0.000,0.000,0.000,$finalSpeed,-1)"
                            val extAxisId = globalCommandId++
                            builder.appendCmd(extAxisCmd, extAxisId)
                        }

                        val moveC = "MoveC($midPosStr,$toolIndex,0,100,100,0,0,0,0,$midOffsetStr,$endPosStr,$toolIndex,0,100,100,0,0,0,0,$endOffsetStr,$finalSpeed,-1)"
                        
                        val id = globalCommandId++
                        // Map to the END point of the arc
                        commandIdMap[id] = Triple(pathIndexForMap, pointIndex + 1, extraProcessIndex)
                        
                        builder.appendCmd(moveC, id)
                        
                        if (pointIndex == startIndex && oscType != "无摆动" && !hasStartedArc) {
                            val weaveStartCmd = "WeaveStart(3)"
                            val weaveStartId = globalCommandId++
                            builder.appendCmd(weaveStartCmd, weaveStartId)
                        }

                        // Logic for End of Arc if it's the actual END point
                        if (nextPoint.type == WeldPointType.END) {
                            if (isWelding) {
                                // Arc End
                                val arcEndCmd = "ARCEnd(0,2,10000)"
                                val arcEndId = globalCommandId++
                                builder.appendCmd(arcEndCmd, arcEndId)
                            }
                            
                            if (oscType != "无摆动") {
                                // Weave End
                                val weaveEndCmd = "WeaveEnd(0)"
                                val weaveEndId = globalCommandId++
                                builder.appendCmd(weaveEndCmd, weaveEndId)
                            }
                        }
                        
                        // Skip next point as it is consumed by MoveC
                        pointIndex += 2
                        continue
                    }
                }
                
                // Normal MoveL Logic
                // Calculate Offsets
                val (offX, offY, offZ) = calculateOffset(point, points, pointIndex, process)
                val hasOffset = abs(offX) >= 1e-5 || abs(offY) >= 1e-5 || abs(offZ) >= 1e-5
                
                // 空走（起安、起点、终安）ovl=100，焊接段用工艺速度
                val moveSpeed = if (point.type == WeldPointType.START_SAFE || point.type == WeldPointType.END_SAFE || point.type == WeldPointType.START) 100 else finalSpeed
                
                val ext1Str = String.format(Locale.US, "%.3f", pose.ext1)

                // 1. Insert ExtAxisMoveJ
                if (isExtAxisEnabled) {
                    val extAxisCmd = "ExtAxisMoveJ(1,$ext1Str,0.000,0.000,0.000,$moveSpeed,-1)"
                    val extAxisId = globalCommandId++
                    builder.appendCmd(extAxisCmd, extAxisId)
                }

                val id = globalCommandId++
                commandIdMap[id] = Triple(pathIndexForMap, pointIndex, extraProcessIndex)

                // 第七轴开启时，MoveL 的 offset_flag≠0 会在导轨到位后做笛卡尔逆解，终点报 112。
                // GetInverseKinRef 是六轴逆解，终点笛卡尔在扩展轴下会与关节对不上（74）。
                // 改用协议 6.3.28 GetInverseKinExaxis，把扩展轴位置一起代入再发 offset_flag=0 的 MoveL。
                if (hasOffset && isExtAxisEnabled && joints != null && joints.size >= 6) {
                    val nx = String.format(Locale.US, "%.3f", pose.x + offX)
                    val ny = String.format(Locale.US, "%.3f", pose.y + offY)
                    val nz = String.format(Locale.US, "%.3f", pose.z + offZ)
                    val rx = String.format(Locale.US, "%.3f", pose.rx)
                    val ry = String.format(Locale.US, "%.3f", pose.ry)
                    val rz = String.format(Locale.US, "%.3f", pose.rz)
                    val ikCmd = "j1,j2,j3,j4,j5,j6=GetInverseKinExaxis(0,{$nx,$ny,$nz,$rx,$ry,$rz},{$ext1Str,0.000,0.000,0.000},$toolIndex,0)"
                    builder.appendCmd(ikCmd)
                    val cmd2 = "MoveL(j1,j2,j3,j4,j5,j6,$nx,$ny,$nz,$rx,$ry,$rz,$toolIndex,0,100,100,$moveSpeed,-1,0,$ext1Str,0.000,0.000,0.000,0,0,0,0,0,0,0,0,100,0)"
                    builder.appendCmd(cmd2, id)
                } else {
                    val pos2 = "${jointsStr},${String.format(Locale.US, "%.3f", pose.x)},${String.format(Locale.US, "%.3f", pose.y)},${String.format(Locale.US, "%.3f", pose.z)},${String.format(Locale.US, "%.3f", pose.rx)},${String.format(Locale.US, "%.3f", pose.ry)},${String.format(Locale.US, "%.3f", pose.rz)}"
                    // 无第七轴时 offset_flag=3（基座标系）可用；有第七轴无偏移则 flag=0。
                    val userParams = if (!hasOffset) {
                        "0,0,0,0,0,0,0"
                    } else {
                        "3,${String.format(Locale.US, "%.3f", offX)},${String.format(Locale.US, "%.3f", offY)},${String.format(Locale.US, "%.3f", offZ)},0,0,0"
                    }
                    val cmd2 = "MoveL($pos2,$toolIndex,0,100,100,$moveSpeed,-1,0,$ext1Str,0.000,0.000,0.000,0,$userParams,100,0)"
                    builder.appendCmd(cmd2, id)
                }

                // --- INSERT Start/End Logic Here ---
                if (point.type == WeldPointType.START) {
                    if (!hasStartedArc) {
                        if (isWelding) {
                            // Arc Start
                            val arcStartCmd = "ARCStart(0,2,10000)"
                            val arcStartId = globalCommandId++
                            builder.appendCmd(arcStartCmd, arcStartId)
                        }
                        
                        if (oscType != "无摆动") {
                            // Weave Start
                            val weaveStartCmd = "WeaveStart(3)"
                            val weaveStartId = globalCommandId++
                            builder.appendCmd(weaveStartCmd, weaveStartId)
                        }
                        hasStartedArc = true
                    }
                } else {
                    // If it is the first point of the path (but not START), send WeaveStart to apply parameters
                    // This ensures parameters take effect for continuous welding segments
                    // But exclude START_SAFE and END_SAFE points
                    if (pointIndex == startIndex && oscType != "无摆动" && point.type != WeldPointType.START_SAFE && point.type != WeldPointType.END_SAFE && !hasStartedArc) {
                        val weaveStartCmd = "WeaveStart(3)"
                        val weaveStartId = globalCommandId++
                        builder.appendCmd(weaveStartCmd, weaveStartId)
                    }

                    if (point.type == WeldPointType.END) {
                        if (isWelding) {
                            // Arc End
                            val arcEndCmd = "ARCEnd(0,2,10000)"
                            val arcEndId = globalCommandId++
                            builder.appendCmd(arcEndCmd, arcEndId)
                        }
                        
                        if (oscType != "无摆动") {
                            // Weave End
                            val weaveEndCmd = "WeaveEnd(0)"
                            val weaveEndId = globalCommandId++
                            builder.appendCmd(weaveEndCmd, weaveEndId)
                        }
                    }
                }
            }
            pointIndex++
        }
    }

    // --- Batch MoveL Command ---
    fun sendBatchMoveLCommands(resumePathIndex: Int = -1, resumePointIndex: Int = -1, isResumeFromStop: Boolean = false) {
        stopControllerActive()
        if (weldPaths.isEmpty()) return

        val missingOk = bindPouchProcesses(weldPaths)
        if (!missingOk) return

        val toolIndex = try {
            toolCoordinateSystem.removePrefix("工具").toInt()
        } catch (e: Exception) { 1 }

        executionQueue.clear()
        
        // Clear previous mapping
        commandIdMap.clear()
        commandLineMap.clear()
        
        // --- Global Speed Command ---
        // SetSpeed(10) - Fixed command at the start
        val globalSpeedCmd = "SetSpeed(10)"
        
        // Use a single BatchCommandBuilder for all paths
        val batchBuilder = BatchCommandBuilder("All Paths")
        batchBuilder.appendCmd(globalSpeedCmd)
        
        weldPaths.forEachIndexed { pathIndex, weldPath ->
            val runMain = weldPath.isEnabled
            val runExtras = weldPath.extraProcesses.any { it.isEnabled && it.processId.isNotBlank() }
            if (!runMain && !runExtras) {
                return@forEachIndexed
            }

            if (resumePathIndex != -1 && pathIndex < resumePathIndex) {
                return@forEachIndexed
            }

            val startIndex = if (resumePathIndex != -1 && pathIndex == resumePathIndex && resumePointIndex != -1) {
                resumePointIndex
            } else {
                0
            }

            if (runMain) {
                appendCommandsForPath(
                    weldPath,
                    batchBuilder,
                    pathIndex,
                    toolIndex,
                    startIndex,
                    if (pathIndex == resumePathIndex) isResumeFromStop else false
                )
            }
            weldPath.extraProcesses.forEachIndexed { extraIdx, slot ->
                if (slot.isEnabled && slot.processId.isNotBlank()) {
                    val extraPath = weldPath.copy(process = slot.process, processId = slot.processId)
                    appendCommandsForPath(extraPath, batchBuilder, pathIndex, toolIndex, 0, false, extraIdx)
                }
            }
        }
        
        batchBuilder.flush()

        startBatchExecution()
    }

    private fun calculateOffset(
        currentPoint: WeldPoint, 
        allPoints: List<WeldPoint>, 
        index: Int, 
        process: WeldProcess
    ): Triple<Double, Double, Double> {
        val type = currentPoint.type
        if (type == WeldPointType.START_SAFE || type == WeldPointType.END_SAFE) {
            return Triple(0.0, 0.0, 0.0)
        }

        val offXRaw = process.offsetX.toDoubleOrNull() ?: 0.0
        val offX = if (isExtAxisEnabled) -offXRaw else offXRaw
        val offY = process.offsetY.toDoubleOrNull() ?: 0.0
        val offZ = process.offsetZ.toDoubleOrNull() ?: 0.0

        if (offX == 0.0 && offY == 0.0 && offZ == 0.0) {
            return Triple(0.0, 0.0, 0.0)
        }

        val pCurr = currentPoint.pose?.let { Vector3(it.x, it.y, it.z) } ?: return Triple(0.0, 0.0, 0.0)
        val globalZ = Vector3(0.0, 0.0, 1.0)

        // Find Prev Point
        var vPrev: Vector3? = null
        var prevIndex = -1
        for (i in index - 1 downTo 0) {
            val pt = allPoints[i]
            if (pt.type != WeldPointType.START_SAFE && pt.type != WeldPointType.END_SAFE) {
                pt.pose?.let { vPrev = Vector3(it.x, it.y, it.z) }
                prevIndex = i
                break
            }
        }

        // Find Next Point
        var vNext: Vector3? = null
        var nextPt: WeldPoint? = null
        var nextIndex = -1
        for (i in index + 1 until allPoints.size) {
            val pt = allPoints[i]
            if (pt.type != WeldPointType.START_SAFE && pt.type != WeldPointType.END_SAFE) {
                pt.pose?.let { vNext = Vector3(it.x, it.y, it.z) }
                nextPt = pt
                nextIndex = i
                break
            }
        }

        // Helper to get Weld Frame (N=Horizontal, B=Perp)
        fun getFrame(p1: Vector3, p2: Vector3, refX: Vector3? = null): Pair<Vector3, Vector3> {
            val t = (p2 - p1).normalize()
            
            var n: Vector3
            if (refX != null) {
                // Use Ref Point logic:
                // X direction is perpendicular to T, in the plane defined by T and RefPoint.
                // Actually user said: "这个点和起点连线不一定垂直于焊缝，只是给出一个x方向，真正的x方向还需要起点和终点连线后，然后垂直于这条连线的才是x方向"
                // So, define plane using T and (Ref - P1). Then N = vector in that plane, perp to T.
                // Or simply: N_raw = Ref - P1. Then N = Project N_raw onto plane perp to T.
                // N = (N_raw - (N_raw.dot(T)) * T).normalize()
                
                // Let's use the Ref point relative to p1
                val vRef = (refX - p1)
                
                // Project vRef onto plane perpendicular to T to get X direction (N)
                // N = vRef - (vRef . T) * T
                val projection = vRef - t * vRef.dot(t)
                
                if (projection.length() > 1e-3) {
                    n = projection.normalize()
                } else {
                    // Ref point is collinear with T, fallback to global Z logic
                    n = t.cross(globalZ).normalize()
                    if (n.length() < 1e-3) n = Vector3(1.0, 0.0, 0.0)
                }
            } else {
                // Default logic: N = T x Z (Horizontal)
                n = t.cross(globalZ).normalize()
                if (n.length() < 1e-3) n = Vector3(1.0, 0.0, 0.0) 
            }
            
            // B = N x T (Z offset dir) - User said: "z方向就是垂直于x，y方向" => B = N x T
            val b = n.cross(t).normalize() 
            return Pair(n, b)
        }
        
        // Helper to solve Line-Circle Intersection for Line->Arc Transition
        fun solveLineArcIntersect(
            lineStart: Vector3, lineEnd: Vector3,
            arcStart: Vector3, arcMid: Vector3, arcEnd: Vector3,
            refX: Vector3? = null
        ): Vector3? {
            // Line Params
            val tLine = (lineEnd - lineStart).normalize()
            val (nLine, bLine) = getFrame(lineStart, lineEnd, refX)
            val lineOffsetOrigin = lineEnd + nLine * offX + tLine * offY + bLine * offZ // End of offset line
            
            // Arc Params
            val (center, normal, radius) = getCircleParams(arcStart, arcMid, arcEnd) ?: return null
            val offsetRadius = radius + offX // Radial offset
            val offsetCenter = center + normal * offZ
            
            // Solve |(lineOffsetOrigin + t*tLine) - offsetCenter|^2 = offsetRadius^2
            // Let V = lineOffsetOrigin - offsetCenter
            val V = lineOffsetOrigin - offsetCenter
            // t^2 + 2(V.tLine)t + (V.V - R^2) = 0
            val a = 1.0
            val b = 2 * V.dot(tLine)
            val c = V.dot(V) - offsetRadius * offsetRadius
            
            val delta = b * b - 4 * a * c
            if (delta < 0) return null // No intersection
            
            // Two solutions, pick closest to 0 (closest to original transition point)
            val t1 = (-b - sqrt(delta)) / (2 * a)
            val t2 = (-b + sqrt(delta)) / (2 * a)
            
            val t = if (kotlin.math.abs(t1) < kotlin.math.abs(t2)) t1 else t2
            return lineOffsetOrigin + tLine * t
        }

        val offsetVec: Vector3 = when {
            // Case 1: Line -> Arc Transition
            // Current is Line End. Next is Arc Middle.
            vPrev != null && vNext != null && nextPt?.type == WeldPointType.ARC_MIDDLE -> {
                // We need Arc End (NextNext)
                var vNextNext: Vector3? = null
                for (i in nextIndex + 1 until allPoints.size) {
                    val pt = allPoints[i]
                    if (pt.type != WeldPointType.START_SAFE && pt.type != WeldPointType.END_SAFE) {
                        pt.pose?.let { vNextNext = Vector3(it.x, it.y, it.z) }
                        break
                    }
                }
                
                // Use RefX from Prev Point (Start of the line segment)
                // Actually, currentPoint is the END of the line segment.
                // The RefPoint should be associated with the segment.
                // Typically RefPoint is stored on the START point of a segment.
                // For segment Prev -> Curr, check Prev.refPointX
                val refX = allPoints[prevIndex].refPointX?.pose?.let { Vector3(it.x, it.y, it.z) }
                
                if (vNextNext != null) {
                    val intersect = solveLineArcIntersect(vPrev!!, pCurr, pCurr, vNext!!, vNextNext!!, refX)
                    if (intersect != null) {
                         intersect - pCurr
                    } else {
                        // Fallback to Miter if no intersection
                        // Standard Miter Logic...
                         val tIn = (pCurr - vPrev!!).normalize()
                         val tOut = (vNext!! - pCurr).normalize()
                         val (nIn, bIn) = getFrame(vPrev!!, pCurr, refX)
                         // For Arc Start (pCurr -> vNext), we might need another frame or same?
                         // Arc frame is usually Frenet or based on Plane.
                         // But Miter requires two frames.
                         // Let's assume standard Arc frame logic for the outgoing part if no ref there.
                         val (nOut, bOut) = getFrame(pCurr, vNext!!) // TODO: Arc frame might need Ref too?
                         
                         val nAvg = (nIn + nOut).normalize()
                         val dotN = nIn.dot(nOut)
                         val kX = if (dotN > -0.99) 1.0 / sqrt((1 + dotN) / 2) else 1.0
                         val bAvg = (bIn + bOut).normalize()
                         val dotB = bIn.dot(bOut)
                         val kZ = if (dotB > -0.99) 1.0 / sqrt((1 + dotB) / 2) else 1.0
                         val tAvg = (tIn + tOut).normalize()
                         nAvg * (offX * kX) + bAvg * (offZ * kZ) + tAvg * offY
                    }
                } else {
                     Vector3(0.0,0.0,0.0)
                }
            }
            
            // Case 2: Middle Point: Miter (Bisector) - Has both Prev and Next (Standard Line-Line)
            vPrev != null && vNext != null -> {
                val tIn = (pCurr - vPrev!!).normalize()
                val tOut = (vNext!! - pCurr).normalize()
                
                // RefX for In segment (Prev -> Curr)
                val refXIn = allPoints[prevIndex].refPointX?.pose?.let { Vector3(it.x, it.y, it.z) }
                // RefX for Out segment (Curr -> Next)
                val refXOut = currentPoint.refPointX?.pose?.let { Vector3(it.x, it.y, it.z) }
                
                val (nIn, bIn) = getFrame(vPrev!!, pCurr, refXIn)
                val (nOut, bOut) = getFrame(pCurr, vNext!!, refXOut)
                
                // Miter for N (X offset)
                val nAvg = (nIn + nOut).normalize()
                val dotN = nIn.dot(nOut)
                val kX = if (dotN > -0.99) 1.0 / sqrt((1 + dotN) / 2) else 1.0
                
                // Miter for B (Z offset)
                val bAvg = (bIn + bOut).normalize()
                val dotB = bIn.dot(bOut)
                val kZ = if (dotB > -0.99) 1.0 / sqrt((1 + dotB) / 2) else 1.0
                
                val tAvg = (tIn + tOut).normalize()
                 
                nAvg * (offX * kX) + bAvg * (offZ * kZ) + tAvg * offY
            }
            // Start Point (or First Point) - No Prev, Has Next
            vNext != null -> {
                val t = (vNext!! - pCurr).normalize()
                // RefX for this segment (Curr -> Next)
                val refX = currentPoint.refPointX?.pose?.let { Vector3(it.x, it.y, it.z) }
                val (n, b) = getFrame(pCurr, vNext!!, refX)
                n * offX + t * offY + b * offZ
            }
            // End Point (or Last Point) - Has Prev, No Next
            vPrev != null -> {
                val t = (pCurr - vPrev!!).normalize()
                // RefX for incoming segment (Prev -> Curr)
                val refX = allPoints[prevIndex].refPointX?.pose?.let { Vector3(it.x, it.y, it.z) }
                val (n, b) = getFrame(vPrev!!, pCurr, refX)
                n * offX + t * offY + b * offZ
            }
            else -> Vector3(0.0, 0.0, 0.0)
        }

        return Triple(offsetVec.x, offsetVec.y, offsetVec.z)
    }

    private fun getCircleParams(p1: Vector3, p2: Vector3, p3: Vector3): Triple<Vector3, Vector3, Double>? {
        val v1 = p2 - p1
        val v2 = p3 - p2
        var normal = v1.cross(v2)
        if (normal.length() < 1e-3) return null // Collinear
        normal = normal.normalize()

        val m1 = (p1 + p2) * 0.5
        val m2 = (p2 + p3) * 0.5
        val d1 = v1.cross(normal).normalize()
        val d2 = v2.cross(normal).normalize()
        
        val det = d1.cross(d2).dot(normal)
        if (kotlin.math.abs(det) < 1e-3) return null
        
        val t = (m2 - m1).cross(d2).dot(normal) / det
        val center = m1 + d1 * t
        val radius = (p1 - center).length()
        return Triple(center, normal, radius)
    }

    private fun calculateArcOffset(
        midPoint: WeldPoint,
        endPoint: WeldPoint,
        allPoints: List<WeldPoint>,
        midIndex: Int,
        process: WeldProcess
    ): Pair<Triple<Double, Double, Double>, Triple<Double, Double, Double>> {
        val offXRaw = process.offsetX.toDoubleOrNull() ?: 0.0
        val offX = if (isExtAxisEnabled) -offXRaw else offXRaw
        val offY = process.offsetY.toDoubleOrNull() ?: 0.0
        val offZ = process.offsetZ.toDoubleOrNull() ?: 0.0

        if (offX == 0.0 && offY == 0.0 && offZ == 0.0) {
            return Pair(Triple(0.0, 0.0, 0.0), Triple(0.0, 0.0, 0.0))
        }

        var pStart: Vector3? = null
        for (i in midIndex - 1 downTo 0) {
             val pt = allPoints[i]
             if (pt.type != WeldPointType.START_SAFE && pt.type != WeldPointType.END_SAFE) {
                 pt.pose?.let { pStart = Vector3(it.x, it.y, it.z) }
                 break
             }
        }
        
        val pMid = midPoint.pose?.let { Vector3(it.x, it.y, it.z) }
        val pEnd = endPoint.pose?.let { Vector3(it.x, it.y, it.z) }

        if (pStart == null || pMid == null || pEnd == null) {
            return Pair(Triple(0.0, 0.0, 0.0), Triple(0.0, 0.0, 0.0))
        }

        val params = getCircleParams(pStart!!, pMid, pEnd)
        val (center, normal, radius) = params ?: return Pair(Triple(0.0,0.0,0.0), Triple(0.0,0.0,0.0))

        // Check Next Point for Arc->Line Intersection
        var vNext: Vector3? = null
        // Search after endPoint (which is midIndex + 1)
        for (i in midIndex + 2 until allPoints.size) {
            val pt = allPoints[i]
            if (pt.type != WeldPointType.START_SAFE && pt.type != WeldPointType.END_SAFE) {
                pt.pose?.let { vNext = Vector3(it.x, it.y, it.z) }
                break
            }
        }

        fun getOffset(p: Vector3, isEndPoint: Boolean): Triple<Double, Double, Double> {
            // Arc -> Line Intersection Logic for End Point
            if (isEndPoint && vNext != null) {
                // Line: pEnd -> vNext
                val tLine = (vNext!! - pEnd).normalize()
                val globalZ = Vector3(0.0, 0.0, 1.0)
                var nLine = tLine.cross(globalZ).normalize()
                if (nLine.length() < 1e-3) nLine = Vector3(1.0, 0.0, 0.0)
                val bLine = nLine.cross(tLine).normalize()
                
                val lineOffsetOrigin = pEnd + nLine * offX + tLine * offY + bLine * offZ
                
                val offsetRadius = radius + offX
                val offsetCenter = center + normal * offZ
                
                val V = lineOffsetOrigin - offsetCenter
                val a = 1.0
                val b = 2 * V.dot(tLine)
                val c = V.dot(V) - offsetRadius * offsetRadius
                val delta = b * b - 4 * a * c
                
                if (delta >= 0) {
                    val t1 = (-b - sqrt(delta)) / (2 * a)
                    val t2 = (-b + sqrt(delta)) / (2 * a)
                    // Pick closest to 0
                    val t = if (kotlin.math.abs(t1) < kotlin.math.abs(t2)) t1 else t2
                    val intersect = lineOffsetOrigin + tLine * t
                    val vec = intersect - pEnd
                    return Triple(vec.x, vec.y, vec.z)
                }
            }
            
            // Standard Arc Offset
            val radial = if ((p - center).length() < 1e-3) Vector3(1.0, 0.0, 0.0) else (p - center).normalize()
            val tangent = normal.cross(radial)
            val vec = radial * offX + tangent * offY + normal * offZ
            return Triple(vec.x, vec.y, vec.z)
        }
        
        return Pair(getOffset(pMid, false), getOffset(pEnd, true))
    }


    
    // Tracks exact robot position and state when STOP is pressed
    var stopPointPose: Pose? = null
    var stopPointJoints: List<Double>? = null
    var stopPointPathIndex: Int = -1
    var stopPointIndex: Int = -1
    
    override fun startSimulation() {
        if (isWelding || isSimulating) return
        if (!bindPouchProcesses(weldPaths)) return
        isSimulating = true
        markPouchWelding(true)
        programHasStarted = false
        fineTune.resetOffsets()
        
        // Reset Tracking
        lastReachedPoint = null
        lastReachedPointIndex = -1
        lastReachedPathIndex = -1
        
        // Clear Stop Point records to ensure it starts from the beginning
        stopPointPose = null
        stopPointJoints = null
        stopPointPathIndex = -1
        stopPointIndex = -1
        
        sendBatchMoveLCommands()
    }
    
    


    override fun startArcWelding() {
        if (isWelding || isSimulating) return
        if (!bindPouchProcesses(weldPaths)) return
        isWelding = true
        markPouchWelding(true)
        isWeldingStatsActive = false // Ensure stats are OFF initially
        programHasStarted = false
        fineTune.resetOffsets()
        
        // Reset Tracking
        lastReachedPoint = null
        lastReachedPointIndex = -1
        lastReachedPathIndex = -1
        
        // Clear Stop Point records to ensure it starts from the beginning
        stopPointPose = null
        stopPointJoints = null
        stopPointPathIndex = -1
        stopPointIndex = -1
        
        // Initialize Start Point (assume first non-safe point is where we start, or wait for feedback)
        // We wait for feedback to be safe, but we can try to find the logical start.
        // Actually, the feedback logic handles "lastReachedPoint == null" by just setting it.
        // So the first segment (Start -> Next) will be calculated when Next is reached.
        // But what about distance 0 (Start -> Start)? Handled correctly (0 length).

        // Start Timer Job but DO NOT increment yet
        // The increment will happen only when isWeldingStatsActive becomes true
        weldingTimerJob?.cancel()
        weldingTimerJob = viewModelScope.launch {
            var timerRunning = false
            var lastTick = 0L
            while (isActive && isWelding) {
                delay(50) // Check frequently for responsiveness
                if (isWeldingStatsActive) {
                    if (!timerRunning) {
                         timerRunning = true
                         lastTick = System.currentTimeMillis()
                    }
                    val now = System.currentTimeMillis()
                    if (now - lastTick >= 1000) {
                        weldingDuration += 1
                        lastTick += 1000 // Keep alignment to avoid drift
                    }
                } else {
                    timerRunning = false
                }
            }
        }

        sendBatchMoveLCommands()
    }


    // --- Pause/Resume Logic ---

    override fun pauseWelding() {
        if (!isWelding && !isSimulating) return

        viewModelScope.launch {
            socketManager.sendControlCommand(WeldRun.pause())
            kotlinx.coroutines.delay(WeldRun.AFTER_PAUSE_MS)
            socketManager.sendControlCommand(RobotCommands.modeManual())
        }

        isPaused = true
    }

    private fun distance(p1: Pose, p2: Pose): Double {
        val dx = p1.x - p2.x
        val dy = p1.y - p2.y
        val dz = p1.z - p2.z
        return sqrt(dx*dx + dy*dy + dz*dz)
    }

    private fun findRetreatPoint(currentState: RobotState): RobotState {
        // 1. Try History (Primary method: captures actual trajectory)
        // Check if history has enough data
        if (positionHistory.isNotEmpty()) {
             for (i in positionHistory.lastIndex downTo 0) {
                 val p = positionHistory[i]
                 val dist = distance(currentState.pose, p.pose)
                 if (dist >= 5.0) { // 5mm
                     return p
                 }
             }
        }
        
        // 2. Fallback: Analytic Retreat along Path (if history is insufficient)
        val (pathIndex, pointIndex) = findClosestPathSegment(currentState.pose)
        
        val pathsToCheck = weldPaths

        if (pathIndex != -1 && pathIndex < pathsToCheck.size) {
            val path = pathsToCheck[pathIndex]
            if (pointIndex >= 0 && pointIndex < path.points.size - 1) {
                val p1 = path.points[pointIndex].pose
                val p2 = path.points[pointIndex + 1].pose
                
                if (p1 != null && p2 != null) {
                    val v1 = Vector3(p1.x, p1.y, p1.z)
                    val v2 = Vector3(p2.x, p2.y, p2.z)
                    val direction = (v2 - v1).normalize()
                    
                    if (direction.length() > 0.0) {
                        val curr = Vector3(currentState.pose.x, currentState.pose.y, currentState.pose.z)
                        val retreatVec = curr - (direction * 5.0)
                        
                        // Construct Retreat Pose (Keep orientation)
                        val rPose = currentState.pose.copy(
                            x = retreatVec.x,
                            y = retreatVec.y,
                            z = retreatVec.z
                        )
                        return RobotState(rPose, currentState.joints)
                    }
                }
            }
        }
        
        // 3. Last Resort: Current State (0 distance)
        return currentState
    }

    private fun findClosestPathSegment(pose: Pose): Pair<Int, Int> {
        var minDesc = Double.MAX_VALUE
        var bestPath = -1
        var bestPoint = -1

        val currentPos = Vector3(pose.x, pose.y, pose.z)

        val pathsToCheck: List<WeldPath> = weldPaths

        pathsToCheck.forEachIndexed { pIdx, path ->
            if (path.isEnabled) {
                for (i in 0 until path.points.size - 1) {
                    val p1 = path.points[i].pose
                    val p2 = path.points[i+1].pose
                    if (p1 != null && p2 != null) {
                        val v1 = Vector3(p1.x, p1.y, p1.z)
                        val v2 = Vector3(p2.x, p2.y, p2.z)
                        val dist = distancePointToSegment(currentPos, v1, v2)
                        if (dist < minDesc) {
                            minDesc = dist
                            bestPath = pIdx
                            bestPoint = i
                        }
                    }
                }
            }
        }
        return Pair(bestPath, bestPoint)
    }

    private fun findClosestPointIndexOnPath(path: WeldPath, pose: Pose): Int {
        var minDesc = Double.MAX_VALUE
        var bestPoint = -1
        val currentPos = Vector3(pose.x, pose.y, pose.z)
        
        if (path.isEnabled) {
            for (i in 0 until path.points.size - 1) {
                val p1 = path.points[i].pose
                val p2 = path.points[i+1].pose
                if (p1 != null && p2 != null) {
                    val v1 = Vector3(p1.x, p1.y, p1.z)
                    val v2 = Vector3(p2.x, p2.y, p2.z)
                    val dist = distancePointToSegment(currentPos, v1, v2)
                    if (dist < minDesc) {
                        minDesc = dist
                        bestPoint = i
                    }
                }
            }
        }
        return bestPoint
    }

    private fun distancePointToSegment(p: Vector3, a: Vector3, b: Vector3): Double {
        val ab = b - a
        val ap = p - a
        val t = ap.dot(ab) / ab.dot(ab)
        val closest = when {
            t < 0.0 -> a
            t > 1.0 -> b
            else -> a + ab * t
        }
        return (p - closest).length()
    }

    override fun continueWelding() {
        if (!isPaused) return
        
        viewModelScope.launch {
            isResuming = true
            isPaused = false
            
            sendManualCommand(303, "Mode(0)")
            kotlinx.coroutines.delay(50)

            if (weldingBreakOffState == "中断") {
                socketManager.sendControlCommand(WeldRun.reWeldAfterBreak())
                kotlinx.coroutines.delay(100)
                socketManager.sendControlCommand(WeldRun.resume())
            } else {
                socketManager.sendControlCommand(WeldRun.resume())
            }
            
            // Give hardware time to transition to running state
            kotlinx.coroutines.delay(2000)
            isResuming = false
        }
    }

    override fun stopWelding(force: Boolean) {
        
        // Record EXACT position and tracking data when STOP is pressed
        stopPointPose = socketManager.robotPose.value
        stopPointJoints = socketManager.robotJoints.value
        stopPointPathIndex = lastReachedPathIndex
        stopPointIndex = lastReachedPointIndex



        // Only calculate partial length if stats were active (between Start and End)
        if (isWelding && isWeldingStatsActive && lastReachedPoint != null) {
             val vLast = lastReachedPoint!!.pose?.let { Vector3(it.x, it.y, it.z) }
             val vCurr = Vector3(operationPosition.x, operationPosition.y, operationPosition.z)
             
             if (vLast != null) {
                 // Use linear distance for the stop segment
                 val len = (vCurr - vLast).length()
                 weldingLength += len / 1000.0
             }
        }
        
        isWelding = false
        isSimulating = false
        isControllerActive = false // Disable manual control
        isWeldingStatsActive = false
        isPaused = false
        programHasStarted = false
        pauseState = null
        weldingTimerJob?.cancel()
        markPouchWelding(false)
        
        // Send STOP and switch to Manual Mode
        viewModelScope.launch {
            socketManager.sendControlCommand(WeldRun.stop())
            kotlinx.coroutines.delay(1000)
            socketManager.sendControlCommand(RobotCommands.modeManual())
        }
        
        if (weldingBreakOffState == "中断") {
            socketManager.sendControlCommand(WeldRun.abortAfterBreak())
        }
        
        saveAppSettings()
    }

    private fun calculateSegmentLength(points: List<WeldPoint>, fromIndex: Int, toIndex: Int): Double {
        if (fromIndex == -1 || toIndex == -1) return 0.0
        val pFrom = points[fromIndex]
        val pTo = points[toIndex]
        
        // Check for Arc
        // If pTo is ARC_MIDDLE => Arc Segment 1 (Start -> Middle)
        if (pTo.type == WeldPointType.ARC_MIDDLE) {
             val pEnd = points.getOrNull(toIndex + 1)
             if (pEnd != null) {
                 return calculateArcPart(pFrom, pTo, pEnd, isFirstHalf = true)
             }
        }
        
        // If pFrom is ARC_MIDDLE => Arc Segment 2 (Middle -> End)
        if (pFrom.type == WeldPointType.ARC_MIDDLE) {
            val pStart = points.getOrNull(fromIndex - 1)
            if (pStart != null) {
                return calculateArcPart(pStart, pFrom, pTo, isFirstHalf = false)
            }
        }
        
        // Linear
        val v1 = pFrom.pose?.let { Vector3(it.x, it.y, it.z) }
        val v2 = pTo.pose?.let { Vector3(it.x, it.y, it.z) }
        return if (v1 != null && v2 != null) (v2 - v1).length() else 0.0
    }

    private fun calculateArcPart(pStart: WeldPoint, pMid: WeldPoint, pEnd: WeldPoint, isFirstHalf: Boolean): Double {
        val v1 = pStart.pose?.let { Vector3(it.x, it.y, it.z) }
        val v2 = pMid.pose?.let { Vector3(it.x, it.y, it.z) }
        val v3 = pEnd.pose?.let { Vector3(it.x, it.y, it.z) }
        
        if (v1 == null || v2 == null || v3 == null) return 0.0

        val v12 = v2 - v1
        val v23 = v3 - v2
        
        // Collinear check
        if (v12.cross(v23).length() < 1e-3) {
            return if (isFirstHalf) v12.length() else v23.length()
        }
        
        // Circle Center
        val normal = v12.cross(v23).normalize()
        val m1 = (v1 + v2) * 0.5
        val m2 = (v2 + v3) * 0.5
        val d1 = v12.cross(normal).normalize()
        val d2 = v23.cross(normal).normalize()
        
        val det = d1.cross(d2).dot(normal)
        if (abs(det) < 1e-3) return if (isFirstHalf) v12.length() else v23.length()
        
        val t = (m2 - m1).cross(d2).dot(normal) / det
        val center = m1 + d1 * t
        val r = (v1 - center).length()
        
        // Angles
        val cp1 = v1 - center
        val cp2 = v2 - center
        val cp3 = v3 - center
        
        val ang12 = angleBetween(cp1, cp2, normal)
        val ang23 = angleBetween(cp2, cp3, normal)
        
        return if (isFirstHalf) r * ang12 else r * ang23
    }

    private fun isSafePoint(point: WeldPoint): Boolean {
        return point.type == WeldPointType.START_SAFE || point.type == WeldPointType.END_SAFE
    }

    private fun calculateNewArcMidPoint(pStart: Pose, pMid: Pose, pEnd: Pose, pCurrent: Pose): Pose {
        val v1 = Vector3(pStart.x, pStart.y, pStart.z)
        val v2 = Vector3(pMid.x, pMid.y, pMid.z)
        val v3 = Vector3(pEnd.x, pEnd.y, pEnd.z)
        val vC = Vector3(pCurrent.x, pCurrent.y, pCurrent.z)

        // 1. Calculate Original Arc Center and Normal
        val v12 = v2 - v1
        val v23 = v3 - v2
        var normal = v12.cross(v23)
        
        if (normal.length() < 1e-6) {
            // Provide a tiny offset to prevent perfect collinearity which causes "Instruction Point Error"
            val fallback = (vC + v3) * 0.5
            val vC3 = v3 - vC
            var arbitraryPerp = Vector3(1.0, 0.0, 0.0)
            if (kotlin.math.abs(vC3.normalize().x) > 0.9) {
                arbitraryPerp = Vector3(0.0, 1.0, 0.0)
            }
            val tinyOffset = vC3.cross(arbitraryPerp).normalize() * 0.01 // 0.01mm offset
            return pCurrent.copy(x = fallback.x + tinyOffset.x, y = fallback.y + tinyOffset.y, z = fallback.z + tinyOffset.z)
        }
        normal = normal.normalize()

        val m1 = (v1 + v2) * 0.5
        val m2 = (v2 + v3) * 0.5
        val d1 = v12.cross(normal).normalize()
        val d2 = v23.cross(normal).normalize()

        val det = d1.cross(d2).dot(normal)
        if (kotlin.math.abs(det) < 1e-6) {
            val fallback = (vC + v3) * 0.5
            val vC3 = v3 - vC
            val tinyOffset = normal * 0.01 // 0.01mm offset along normal
            return pCurrent.copy(x = fallback.x + tinyOffset.x, y = fallback.y + tinyOffset.y, z = fallback.z + tinyOffset.z)
        }

        val t = (m2 - m1).cross(d2).dot(normal) / det
        val center = m1 + d1 * t

        val radius = (v1 - center).length()

        // 2. Project current point onto the circle plane to eliminate physical offsets
        val vC_to_center = vC - center
        val distToPlane = vC_to_center.dot(normal)
        val vC_proj = vC - normal * distToPlane

        val cp1 = (v1 - center).normalize()
        val cp2 = (v2 - center).normalize()
        val cp3 = (v3 - center).normalize()
        val cpC = (vC_proj - center).normalize()
        
        // 3. Determine position along the arc using 2D Cross Product
        val xAxis = cp1
        val yAxis = normal.cross(cp1).normalize()

        fun getAngle(v: Vector3): Double {
            var angle = kotlin.math.atan2(v.dot(yAxis), v.dot(xAxis))
            if (angle < 0) angle += 2 * Math.PI
            return angle
        }

        val angleM = getAngle(cp2)
        var angleE = getAngle(cp3)
        var aC = getAngle(cpC)

        if (angleE <= 1e-5) angleE = 2 * Math.PI

        if (aC > angleE) {
            if (2 * Math.PI - aC < aC - angleE) {
                aC = 0.0
            } else {
                aC = angleE - 0.001
            }
        }

        var newMid: Vector3
        if (aC < angleM) {
            // Stopped BEFORE the original midpoint.
            newMid = v2
        } else {
            // Stopped AFTER the original midpoint.
            val midAngle = (aC + angleE) / 2.0
            val newDir = xAxis * kotlin.math.cos(midAngle) + yAxis * kotlin.math.sin(midAngle)
            newMid = center + newDir * radius
        }
        
        fun shortestAngle(a1: Double, a2: Double): Double {
            var diff = (a2 - a1) % 360.0
            if (diff > 180.0) diff -= 360.0
            if (diff < -180.0) diff += 360.0
            return a1 + diff / 2.0
        }
        
        return pCurrent.copy(
            x = newMid.x,
            y = newMid.y,
            z = newMid.z,
            rx = shortestAngle(pCurrent.rx, pEnd.rx),
            ry = shortestAngle(pCurrent.ry, pEnd.ry),
            rz = shortestAngle(pCurrent.rz, pEnd.rz)
        )
    }

    private fun interpolateJoints(jCurrent: List<Double>, jEnd: List<Double>): List<Double> {
        if (jCurrent.size < 6 || jEnd.size < 6) return jCurrent
        return List(6) { i -> (jCurrent[i] + jEnd[i]) / 2.0 }
    }

    private fun angleBetween(v1: Vector3, v2: Vector3, normal: Vector3): Double {
        val dot = v1.dot(v2)
        val det = v1.cross(v2).dot(normal)
        var angle = kotlin.math.atan2(det, dot)
        if (angle < 0) angle += 2 * Math.PI
        return angle
    }



    // --- Position and Speed Methods ---
    fun openPositionDialog() {
        isPositionDialogVisible = true
    }

    // --- Registration Methods ---
    fun fetchMachineCode() {
        // Send command to get MAC address: GetControlBoxNetMacAddr(0), ID 826
        // Command format: /f/bIII826III{len}III{command}III/b/f
        val command = "GetControlBoxNetMacAddr(0)"
        val len = command.length
        val msg = "/f/bIII123III826III${len}III${command}III/b/f"
        Log.d("Registration", "Fetching Machine Code: $msg")
        socketManager.sendControlCommand(msg)
    }



    override fun setPosition(mode: String) {
        positionMode = mode
        isPositionDialogVisible = false
        saveAppSettings()
    }



    // --- Manual Control ---
    override fun sendManualCommand(type: Int, command: String) {
        val id = globalCommandId++
        val len = command.length
        val msg = "/f/bIII${id}III${type}III${len}III${command}III/b/f"
        Log.d("ManualCommand", "Sending: $msg")
        socketManager.sendControlCommand(msg)
    }



    // --- IO Actions ---
    private fun startWireFeed() {
        sendManualCommand(268, "SetForwardWireFeed(0,1)")
    }

    private fun stopWireFeed() {
        sendManualCommand(268, "SetForwardWireFeed(0,0)")
    }


    private fun dragTeachSwitch(enable: Boolean) {
        val valStr = if (enable) "1" else "0"
        sendManualCommand(333, "DragTeachSwitch($valStr)")
    }

    fun openSpeedDialog() {
        isSpeedDialogVisible = true
    }

    override fun setSpeed(mode: String) {
        speedMode = mode
        isSpeedDialogVisible = false
        saveAppSettings()
    }

    override fun updateInstallPos(pos: Int) {
        installPos = pos
        isInstallPosDialogVisible = false
        saveAppSettings()
        
        // Send command to robot
        val cmd = "SetRobotInstallPos($pos)"
        val msg = "/f/bIII23III337III${cmd.length}III${cmd}III/b/f"
        socketManager.sendControlCommand(msg)
    }

    private fun saveAppSettings() {
        val index = try {
            toolCoordinateSystem.removePrefix("工具").toInt() - 1
        } catch (e: Exception) { 0 }
        
        val settings = AppSettings(
            selectedToolIndex = index,
            toolCoordinates = toolCoordinates.toList(),
            toolRemarks = toolRemarks.toList(),
            positionMode = positionMode,
            speedMode = speedMode,
            lastOpenedProjectPath = currentProjectName,
            totalWeldingLength = weldingLength,
            totalWeldingDuration = weldingDuration,
            isRegistered = isRegistered,
            lastConnectionTime = lastConnectionTime,
            installPos = installPos,
            weldingCurrent = savedCurrent,
            weldingVoltage = savedVoltage,
            isExtAxisEnabled = isExtAxisEnabled
        )
        projectManager.saveAppSettings(settings)
    }

    // --- Update Methods ---
    override fun checkForUpdate() {
        viewModelScope.launch {
            // TODO: Replace with your actual server URL
            val updateUrl = "http://cdn.gbndt.com/sjqapk/update.json"
            val info = updateManager.checkUpdate(updateUrl)
            if (info != null) {
                updateInfo = info
                isUpdateDialogVisible = true
            } else {
                _toastEvent.emit("当前已是最新版本")
            }
        }
    }

    // Helper property to store current download file name
    private var currentDownloadFileName: String? = null

    private var pollingJob: kotlinx.coroutines.Job? = null

    private fun startDownloadPolling(id: Long) {
        pollingJob?.cancel()
        pollingJob = viewModelScope.launch {
            var isChecked = false
            while (isActive && isDownloading) {
                delay(2000) // Check every 2 seconds
                val uri = updateManager.getDownloadedUri(id)
                if (uri != null) {
                    Log.d("UpdateManager", "Polling found URI: $uri")
                    isDownloading = false
                    _toastEvent.emit("下载完成，正在准备安装...")
                    updateManager.installApk(uri)
                    isChecked = true
                    break
                } else {
                    // Check if failed
                    val status = checkDownloadStatus(id)
                    if (status == DownloadManager.STATUS_FAILED) {
                        isDownloading = false
                        _toastEvent.emit("更新下载失败")
                        break
                    }
                }
            }
        }
    }

    private fun checkDownloadStatus(id: Long): Int {
        val downloadManager = getApplication<Application>().getSystemService(Context.DOWNLOAD_SERVICE) as DownloadManager
        val query = DownloadManager.Query().setFilterById(id)
        val cursor = downloadManager.query(query)
        var status = -1
        try {
            if (cursor.moveToFirst()) {
                status = cursor.getInt(cursor.getColumnIndex(DownloadManager.COLUMN_STATUS))
            }
        } catch (e: Exception) {
            Log.e("UpdateManager", "Check status failed", e)
        } finally {
            cursor.close()
        }
        return status
    }

    private val downloadReceiver = object : BroadcastReceiver() {
        override fun onReceive(context: Context?, intent: Intent?) {
            val id = intent?.getLongExtra(DownloadManager.EXTRA_DOWNLOAD_ID, -1)
            Log.d("UpdateManager", "Broadcast Received. ID: $id, Expected: $downloadId")
            if (id == downloadId) {
                // If polling already handled it, isDownloading might be false, but let's double check
                if (!isDownloading) return 
                
                isDownloading = false
                pollingJob?.cancel() // Stop polling if broadcast received
                
                viewModelScope.launch {
                     _toastEvent.emit("下载完成，正在准备安装...")
                }
                
                // Query the actual file path from DownloadManager to handle renaming (e.g., -1.apk)
                val uri = updateManager.getDownloadedUri(downloadId)
                if (uri != null) {
                    Log.d("UpdateManager", "Found downloaded URI: $uri")
                    updateManager.installApk(uri)
                } else {
                    Log.e("UpdateManager", "Could not find downloaded file URI for ID: $downloadId")
                    viewModelScope.launch {
                        _toastEvent.emit("安装失败：无法找到下载文件")
                    }
                }
            }
        }
    }

    private fun registerDownloadReceiver() {
        val filter = IntentFilter(DownloadManager.ACTION_DOWNLOAD_COMPLETE)
        if (android.os.Build.VERSION.SDK_INT >= android.os.Build.VERSION_CODES.TIRAMISU) {
            getApplication<Application>().registerReceiver(downloadReceiver, filter, Context.RECEIVER_EXPORTED)
        } else {
            getApplication<Application>().registerReceiver(downloadReceiver, filter)
        }
    }

    override fun reconnect() {
        viewModelScope.launch {
            _toastEvent.emit("正在尝试重新连接...")
            withContext(Dispatchers.IO) {
                try {
                    socketManager.restart()
                } catch (e: Exception) {
                    Log.e("WeldPathViewModel", "Reconnection failed", e)
                }
            }
        }
    }

    override fun showToast(message: String) {
        viewModelScope.launch {
            _toastEvent.emit(message)
        }
    }

    override fun onCleared() {
        super.onCleared()
        alarmVoiceJob?.cancel()
        tts?.stop()
        tts?.shutdown()
        socketManager.stop()
        try {
            getApplication<Application>().unregisterReceiver(downloadReceiver)
        } catch (e: Exception) {
            // Ignore if not registered
        }
    }

    companion object {
        const val CORNER_PARAM_PASSWORD = "bd888888"
    }
}
