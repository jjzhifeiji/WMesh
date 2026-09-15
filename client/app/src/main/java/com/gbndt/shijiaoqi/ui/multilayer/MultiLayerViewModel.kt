package com.gbndt.shijiaoqi.ui.multilayer

import android.speech.tts.TextToSpeech
import android.app.Application
import android.util.Log
import android.app.DownloadManager
import android.content.Context
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
import com.gbndt.shijiaoqi.data.manager.ProcessManager
import com.gbndt.shijiaoqi.data.manager.ProjectManager
import com.gbndt.shijiaoqi.data.manager.SocketManager
import com.gbndt.shijiaoqi.robot.RobotCommands
import com.gbndt.shijiaoqi.ShiJiaoQiApp
import com.gbndt.shijiaoqi.platform.crypt.Wm2
import com.gbndt.shijiaoqi.platform.pouch.Pouch
import com.gbndt.shijiaoqi.platform.session.BagSession
import com.gbndt.shijiaoqi.weld.Capture
import com.gbndt.shijiaoqi.weld.MultiLayerProject
import com.gbndt.shijiaoqi.weld.MultiLayerRun
import com.gbndt.shijiaoqi.weld.PouchProcessSource
import com.gbndt.shijiaoqi.weld.PouchSave
import com.gbndt.shijiaoqi.weld.ProcessBind
import com.gbndt.shijiaoqi.weld.ProcessChoice
import com.gbndt.shijiaoqi.weld.ProcessJson
import com.gbndt.shijiaoqi.weld.ProcessRef
import com.gbndt.shijiaoqi.weld.ProjectChoice
import com.gbndt.shijiaoqi.weld.WeldRun
import com.gbndt.shijiaoqi.data.models.FileSystemItem
import com.gbndt.shijiaoqi.data.models.Pose
import com.gbndt.shijiaoqi.data.models.AppSettings
import com.gbndt.shijiaoqi.data.models.WeldPath
import com.gbndt.shijiaoqi.data.models.WeldPoint
import com.gbndt.shijiaoqi.data.models.WeldPointType
import com.gbndt.shijiaoqi.data.models.WeldProcess
import com.gbndt.shijiaoqi.data.models.MultiLayerWeldPath
import com.gbndt.shijiaoqi.data.models.WeldPassOffset
import com.gbndt.shijiaoqi.ui.components.FineTuneSupport
import java.io.File
import java.util.UUID
import java.util.Locale
import kotlin.math.sqrt
import kotlin.math.abs
import com.gbndt.shijiaoqi.data.manager.UpdateManager
import com.gbndt.shijiaoqi.data.manager.UpdateInfo
import android.os.Environment
import android.content.IntentFilter
import android.content.Intent
import android.content.BroadcastReceiver
import android.net.Uri
import com.gbndt.shijiaoqi.data.models.RefPoint
import com.gbndt.shijiaoqi.utils.CoordinateUtils
import com.gbndt.shijiaoqi.utils.CoordinateUtils.CoordinateSystem
import com.gbndt.shijiaoqi.utils.Point3D
import com.gbndt.shijiaoqi.utils.CoordinateUtils.toPoint3D
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlin.math.pow

import androidx.compose.runtime.toMutableStateList
import com.gbndt.shijiaoqi.ui.viewmodel.WeldViewModelInterface

import kotlinx.coroutines.flow.first
import kotlinx.coroutines.withTimeoutOrNull
import kotlinx.coroutines.async

class MultiLayerViewModel(application: Application) : AndroidViewModel(application), WeldViewModelInterface {

    // Helper class for Vector math
    // 向量数学辅助类
    private data class Vector3(val x: Double, val y: Double, val z: Double) {
        // 向量加法
        operator fun plus(other: Vector3) = Vector3(x + other.x, y + other.y, z + other.z)
        // 向量减法
        operator fun minus(other: Vector3) = Vector3(x - other.x, y - other.y, z - other.z)
        // 向量数乘
        operator fun times(scalar: Double) = Vector3(x * scalar, y * scalar, z * scalar)
        // 计算向量长度
        fun length() = sqrt(x * x + y * y + z * z)
        // 向量归一化
        fun normalize(): Vector3 {
            val len = length()
            return if (len > 0) Vector3(x / len, y / len, z / len) else Vector3(0.0, 0.0, 0.0)
        }
        // 向量叉积
        fun cross(other: Vector3) = Vector3(
            y * other.z - z * other.y,
            z * other.x - x * other.z,
            x * other.y - y * other.x
        )
        // 向量点积
        fun dot(other: Vector3) = x * other.x + y * other.y + z * other.z
    }

    private val processManager = ProcessManager(application)
    private val projectManager = ProjectManager(application)
    private val socketManager = SocketManager
    private val updateManager = UpdateManager(application)
    
    private fun handleCommandExecuted(id: Int) {
        val mapping = commandIdMap[id]
        if (mapping != null) {
            val (mIndex, pIndex, pointIndex) = mapping
            Log.d("CommandFeedback", "Executed Point: MultiPath=$mIndex, Pass=$pIndex, Point=$pointIndex")
            
            // Update UI Selection
            if (mIndex in multiLayerWeldPaths.indices) {
                selectedMultiLayerPathIndex = mIndex
                selectedPassIndex = pIndex
                
                val multiPath = multiLayerWeldPaths[mIndex]
                
                if (pIndex == -1) {
                    // Base Path
                    if (pointIndex < multiPath.basePath.points.size) {
                        val newBasePath = multiPath.basePath.copy(selectedPointIndex = pointIndex)
                        multiLayerWeldPaths[mIndex] = multiPath.copy(basePath = newBasePath)
                    }
                }
                
                // --- Stats Logic for Multi-Layer ---
                if ((isWelding || isSimulating) && pointIndex < multiPath.basePath.points.size) {
                    val currentPt = multiPath.basePath.points[pointIndex]
                    
                    // Case 1: Start Point Highlighted -> Start Timer
                    if (currentPt.type == WeldPointType.START) {
                        if (isWelding) {
                            if (!isWeldingStatsActive) {
                                isWeldingStatsActive = true
                                Log.d("WeldStats", "Multi-Layer Timer STARTED")
                            }
                        }
                    } else {
                        if (isWelding && isWeldingStatsActive && lastReachedPoint != null) {
                            val added = calculateSegmentLength(multiPath.basePath.points, lastReachedPointIndex, pointIndex)
                            weldingLength += added / 1000.0
                            saveAppSettings()
                        }
                    }
                    
                    // Track progress
                    lastReachedPoint = currentPt
                    lastReachedPointIndex = pointIndex
                    lastReachedMultiPathIndex = mIndex
                    lastReachedPassIndex = pIndex
                    
                    // Case 3: End Point Highlighted -> Stop Timer
                    if (currentPt.type == WeldPointType.END) {
                        if (isWelding) {
                            isWeldingStatsActive = false
                            Log.d("WeldStats", "Multi-Layer Timer STOPPED")
                        }
                        markPassCompleted(mIndex, pIndex)
                    }
                }
                // -----------------------------------
                
            }
        }
    }

    private fun markPassCompleted(mIndex: Int, pIndex: Int) {
        if (mIndex !in multiLayerWeldPaths.indices) return
        val multiPath = multiLayerWeldPaths[mIndex]
        if (pIndex == -1) {
            if (!multiPath.isBaseCompleted) {
                multiLayerWeldPaths[mIndex] = multiPath.copy(isBaseCompleted = true)
                saveCurrentProject()
            }
        } else if (pIndex in multiPath.passes.indices) {
            val pass = multiPath.passes[pIndex]
            if (!pass.isCompleted) {
                multiPath.passes[pIndex] = pass.copy(isCompleted = true)
                saveCurrentProject()
            }
        }
    }

    private fun resetWeldCompletionMarks() {
        for (i in multiLayerWeldPaths.indices) {
            val mp = multiLayerWeldPaths[i]
            if (mp.isBaseCompleted) {
                multiLayerWeldPaths[i] = mp.copy(isBaseCompleted = false)
            }
            val current = multiLayerWeldPaths[i]
            for (j in current.passes.indices) {
                val pass = current.passes[j]
                if (pass.isCompleted) {
                    current.passes[j] = pass.copy(isCompleted = false)
                }
            }
        }
    }

    init {
        socketManager.start()

        viewModelScope.launch {
            socketManager.robotErrorEvent.collect { errCode ->
                val errorInfo = com.gbndt.shijiaoqi.data.models.RobotErrorCodes.map[errCode]
                val errorToAdd = errorInfo ?: com.gbndt.shijiaoqi.data.models.RobotError(errCode, "未知故障", "请参考手册")
                if (!currentRobotErrors.contains(errorToAdd)) {
                    currentRobotErrors.add(errorToAdd)
                }
                isRobotErrorDialogVisible = true
            }
        }
    }


    private var tts: TextToSpeech? = null
    
    // Multi-Layer State
    val multiLayerWeldPaths = mutableStateListOf<MultiLayerWeldPath>()
    val pouchProjects = mutableStateListOf<ProjectChoice>()
    val pouchProcesses = mutableStateListOf<ProcessChoice>()
    private var pouchProjectId: UUID? = null
    var selectedMultiLayerPathIndex by mutableStateOf(0)
    var selectedPassIndex by mutableStateOf(-1) // -1 means Base Path, 0..N means Pass index

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

    override var positionMode by mutableStateOf("右")
    var simulationSpeed by mutableStateOf(100)
    override var speedMode by mutableStateOf("1倍")
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

    override var isRobotErrorDialogVisible by mutableStateOf(false)
    override val currentRobotErrors = mutableStateListOf<com.gbndt.shijiaoqi.data.models.RobotError>()

    var isDownloading by mutableStateOf(false)
    private var downloadId: Long = -1L

    // Dialog Visibility States
    override var isPositionDialogVisible by mutableStateOf(false)
    override var isSpeedDialogVisible by mutableStateOf(false)
    
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
    private var lastReachedMultiPathIndex: Int = -1
    private var lastReachedPassIndex: Int = -1

    // ID -> Triple(MultiPathIndex, PassIndex, PointIndex)
    private val commandIdMap = mutableMapOf<Int, Triple<Int, Int, Int>>()
    private val commandLineMap = mutableMapOf<Int, Int>()
    private var globalCommandId = 1000

    val fineTune = FineTuneSupport(
        scope = viewModelScope,
        socketManager = socketManager,
        nextCommandId = { globalCommandId++ },
        toast = { msg -> viewModelScope.launch { _toastEvent.emit(msg) } },
        currentProcess = {
            val mp = multiLayerWeldPaths.getOrNull(selectedMultiLayerPathIndex) ?: return@FineTuneSupport null
            if (selectedPassIndex == -1) {
                mp.basePath.process
            } else {
                mp.passes.getOrNull(selectedPassIndex)?.process
            }
        },
        onOscillationChanged = { osc ->
            val mp = multiLayerWeldPaths.getOrNull(selectedMultiLayerPathIndex) ?: return@FineTuneSupport
            if (selectedPassIndex == -1) {
                mp.basePath.process = mp.basePath.process.copy(oscillation = osc)
            } else if (selectedPassIndex in mp.passes.indices) {
                val pass = mp.passes[selectedPassIndex]
                mp.passes[selectedPassIndex] = pass.copy(process = pass.process.copy(oscillation = osc))
            }
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

    // 批量指令构建器
    private inner class BatchCommandBuilder(
        private val description: String
    ) {
        private var currentSb = StringBuilder()
        private var lastId = -1
        private var lineCount = 0

        // 追加纯文本指令，不再包装 /f/bIII
        fun appendCmd(cmd: String, id: Int? = null) {
            lineCount++
            currentSb.append(cmd).append("\r\n")
            if (id != null) {
                lastId = id
                commandLineMap[lineCount] = id
            }
        }

        // 刷新缓冲区并一次性下发 105 和 106
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
                
                // Keep for tracking or legacy if needed
                val groupDesc = "$description (All in one)"
                executionQueue.add(ExecutionGroup(
                    commandString = "106_BATCH_SENT", // Placeholder
                    lastCommandId = lastId,
                    description = groupDesc
                ))
                
                currentSb = StringBuilder()
            }
        }

        fun hasPending(): Boolean = currentSb.isNotEmpty()
    }

    // Interface Implementation
    // 选择工具坐标系
    override fun selectTool(index: Int) {
        if (index in 0 until 14) {
            val pose = toolCoordinates[index]
            if (pose == null) {
                openToolEdit(index)
            } else {
                toolCoordinateSystem = "工具${index + 1}"
                isToolListDialogVisible = false
                saveAppSettings()

                val cmd = "SetTool($index)"
                val msg = "/f/bIII1001III200III${cmd.length}III${cmd}III/b/f"
                socketManager.sendControlCommand(msg)
            }
        }
    }

    // 打开工具编辑对话框
    override fun openToolEdit(index: Int) {
        editingToolIndex = index
        editingToolPose = toolCoordinates[index] ?: Pose(0.0, 0.0, 0.0, 0.0, 0.0, 0.0)
        editingToolRemark = toolRemarks[index]
        isToolEditDialogVisible = true
    }

    // 取消工具编辑
    override fun cancelToolEdit() {
        isToolEditDialogVisible = false
    }

    // 保存工具编辑
    override fun saveToolEdit(pose: Pose, remark: String) {
        if (editingToolIndex in 0..13) {
            toolCoordinates[editingToolIndex] = pose
            toolRemarks[editingToolIndex] = remark
            saveAppSettings()
        }
        isToolEditDialogVisible = false
    }

    // 设置位置模式（前/后/左/右）
    override fun setPosition(mode: String) {
        positionMode = mode
        isPositionDialogVisible = false
        saveAppSettings()
    }

    // 设置速度倍率
    override fun setSpeed(mode: String) {
        speedMode = mode
        isSpeedDialogVisible = false
        saveAppSettings()
    }

    // 设置安装方式
    override fun updateInstallPos(pos: Int) {
        installPos = pos
        isInstallPosDialogVisible = false
        saveAppSettings()
        
        // Send command to robot
        val cmd = "SetRobotInstallPos($pos)"
        val msg = "/f/bIII23III337III${cmd.length}III${cmd}III/b/f"
        socketManager.sendControlCommand(msg)
    }

    // 切换手柄控制激活状态
    override fun toggleControllerActive() {
        isControllerActive = !isControllerActive
    }

    // 停止手柄控制
    override fun stopControllerActive() {
        isControllerActive = false
    }
    
    // Position History for Pause/Resume
    data class RobotState(val pose: Pose, val joints: List<Double>)
    private val positionHistory = ArrayDeque<RobotState>(500)
    var isPaused by mutableStateOf(false)
    private var pauseState: RobotState? = null
    private var pausePathIndex: Int = -1
    private var programHasStarted = false

    // Enum for Reference Point Teaching
    enum class RefPointType {
        NONE, START_X, START_Z, END_X, END_Z, MIDDLE_X, MIDDLE_Z
    }
    var selectedRefPointType by mutableStateOf(RefPointType.NONE)

    // 选择参考点类型
    fun selectRefPointType(type: RefPointType) {
        if (selectedRefPointType == type) {
             selectedRefPointType = RefPointType.NONE
        } else {
             selectedRefPointType = type
             // No need to deselect point index, we keep it as context or just ignore it
        }
    }

    // 记录参考点
    fun recordRefPoint(type: RefPointType) {
        if (selectedMultiLayerPathIndex !in multiLayerWeldPaths.indices) return

        val currentPose = operationPosition
        val currentJoints = socketManager.robotJoints.value
        val refPoint = RefPoint(currentPose, currentJoints)

        val path = multiLayerWeldPaths[selectedMultiLayerPathIndex]
        val updatedPath = when (type) {
            RefPointType.START_X -> path.copy(refPointX1 = refPoint)
            RefPointType.START_Z -> path.copy(refPointZ1 = refPoint)
            RefPointType.END_X -> path.copy(refPointXEnd = refPoint)
            RefPointType.END_Z -> path.copy(refPointZEnd = refPoint)
            RefPointType.MIDDLE_X -> path.copy(refPointXMiddle = refPoint)
            RefPointType.MIDDLE_Z -> path.copy(refPointZMiddle = refPoint)
            else -> path
        }
        multiLayerWeldPaths[selectedMultiLayerPathIndex] = updatedPath
        saveCurrentProject()
    }

    // 删除参考点
    fun deleteRefPoint(type: RefPointType) {
        if (selectedMultiLayerPathIndex !in multiLayerWeldPaths.indices) return

        val path = multiLayerWeldPaths[selectedMultiLayerPathIndex]
        val updatedPath = when (type) {
            RefPointType.START_X -> path.copy(refPointX1 = null)
            RefPointType.START_Z -> path.copy(refPointZ1 = null)
            RefPointType.END_X -> path.copy(refPointXEnd = null)
            RefPointType.END_Z -> path.copy(refPointZEnd = null)
            RefPointType.MIDDLE_X -> path.copy(refPointXMiddle = null)
            RefPointType.MIDDLE_Z -> path.copy(refPointZMiddle = null)
            else -> path
        }
        multiLayerWeldPaths[selectedMultiLayerPathIndex] = updatedPath
        saveCurrentProject()
    }

    // 更新焊道偏移量
    fun updatePassOffset(multiPathIndex: Int, passIndex: Int, field: String, value: String) {
        if (multiPathIndex !in multiLayerWeldPaths.indices) return
        
        // Auto-switch selection if user edits a different path
        if (selectedMultiLayerPathIndex != multiPathIndex) {
            selectedMultiLayerPathIndex = multiPathIndex
            selectedPassIndex = passIndex
        }
        
        val path = multiLayerWeldPaths[multiPathIndex]
        if (passIndex !in path.passes.indices) return

        val doubleVal = value.toDoubleOrNull() ?: 0.0
        val pass = path.passes[passIndex]
        val updatedPass = when (field) {
            "valX" -> pass.copy(valX = doubleVal)
            "valYLeft" -> pass.copy(valYLeft = doubleVal)
            "valYRight" -> pass.copy(valYRight = doubleVal)
            "valZ" -> pass.copy(valZ = doubleVal)
            "valR" -> pass.copy(valR = doubleVal)
            else -> pass
        }
        path.passes[passIndex] = updatedPass
        saveCurrentProject()
    }


    init {
        // Initialize TTS
        tts = TextToSpeech(getApplication()) { status ->
            if (status == TextToSpeech.SUCCESS) {
                val result = tts?.setLanguage(Locale.CHINA)
                if (result == TextToSpeech.LANG_MISSING_DATA || result == TextToSpeech.LANG_NOT_SUPPORTED) {
                    Log.e("MultiLayerViewModel", "TTS: Chinese language is not supported or missing data")
                }
            } else {
                Log.e("MultiLayerViewModel", "TTS: Initialization failed")
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
                            weldingTimerJob?.cancel()
                            markPouchWelding(false)
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
                val regex = Regex("/f/bIII(\\d+)III\\d+III1III1III/b/f")
                val matchResult = regex.find(text)
                if (matchResult != null) {
                    val idString = matchResult.groupValues[1]
                    val id = idString.toIntOrNull()
                    if (id != null) {
                        // Check for Batch Execution Progress
                        if (isExecutingBatch && currentExecutionGroup != null && id == currentExecutionGroup!!.lastCommandId) {
                            Log.d("BatchExecution", "Group Finished: ${currentExecutionGroup!!.description}")
                            sendNextGroup()
                        }
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
                    alarmVoiceJob?.cancel()
                    alarmVoiceJob = null

                    if (status != "无故障" && status != "无报警") {
                         alarmVoiceJob = launch {
                             while (isActive) {
                                 tts?.speak(status, TextToSpeech.QUEUE_FLUSH, null, null)
                                 delay(5000)
                             }
                         }
                    } else {
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
                     combined in 2690..2890 && !isWireFeeding -> {
                         startWireFeed()
                         isWireFeeding = true
                     }
                     combined > 4096 && isWireFeeding -> {
                         stopWireFeed()
                         isWireFeeding = false
                     }
                     combined in 2048..2248 && !isRecording -> {
                         collectData()
                         isRecording = true
                     }
                     combined > 4096 && isRecording -> {
                         isRecording = false
                     }
                     combined in 754..954 && !isDragEnabled -> {
                         dragTeachSwitch(true)
                         isDragEnabled = true
                     }
                     combined > 4096 && isDragEnabled -> {
                         dragTeachSwitch(false)
                         isDragEnabled = false
                     }
                }
            }
        }

        // Load app settings
        val settings = projectManager.loadAppSettings()
        
        isExtAxisEnabled = settings.isExtAxisEnabled
        
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
            
            if (!isRegistered && projectManager.hasLicense()) {
                isRegistered = true
            }
        }

        registerDownloadReceiver()
        
        startServoCartLoop()

        // Initialize Lists
        refreshProjectExplorer()
        refreshProcessExplorer()
        
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
        
        positionMode = settings.positionMode
        speedMode = settings.speedMode
        installPos = settings.installPos
        
        weldingLength = settings.totalWeldingLength
        weldingDuration = settings.totalWeldingDuration
        
        savedCurrent = settings.weldingCurrent
        savedVoltage = settings.weldingVoltage

        syncFromPouch()
        if (multiLayerWeldPaths.isEmpty()) addLinearWeldPath()

        // Start Socket Manager
        // socketManager.start() - Moved to init


        viewModelScope.launch {
            socketManager.connectionStatus.collect { status ->
                val previousStatus = connectionStatus
                connectionStatus = status
                
                if (previousStatus != "已连接" && status == "已连接") {
                    val toolIndex = try {
                        toolCoordinateSystem.removePrefix("工具").toInt()
                    } catch (e: Exception) { 1 }
                    
                    if (toolIndex in 1..14) {
                        val pose = toolCoordinates[toolIndex - 1]
                        if (pose != null) {
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
    }

    // 启动伺服控制循环
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

    // 发送伺服笛卡尔运动指令
    private fun sendServoCartCommand() {
        val joyFwd = when {
            joyY2 < -0.5 -> 1
            joyY2 > 0.5 -> -1
            else -> 0
        }
        
        val joyLeft = when {
            joyX2 < -0.5 -> 1
            joyX2 > 0.5 -> -1
            else -> 0
        }

        val z = when {
            btnUp -> 1
            btnDown -> -1
            else -> 0
        }

        val rotFwd = when {
            joyY1 < -0.5 -> 1
            joyY1 > 0.5 -> -1
            else -> 0
        }

        val rotLeft = when {
            joyX1 < -0.5 -> 1
            joyX1 > 0.5 -> -1
            else -> 0
        }

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

    // 发送MoveL直线运动指令
    fun sendMoveLCommand() {
        val currentPath = currentActiveWeldPath ?: return
        
        // Handle Reference Point Moving for Multi-Layer
        if (selectedRefPointType != RefPointType.NONE && selectedMultiLayerPathIndex in multiLayerWeldPaths.indices) {
            val multiPath = multiLayerWeldPaths[selectedMultiLayerPathIndex]
            val targetRefPoint = when (selectedRefPointType) {
                RefPointType.START_X -> multiPath.refPointX1
                RefPointType.START_Z -> multiPath.refPointZ1
                RefPointType.END_X -> multiPath.refPointXEnd
                RefPointType.END_Z -> multiPath.refPointZEnd
                RefPointType.MIDDLE_X -> multiPath.refPointXMiddle
                RefPointType.MIDDLE_Z -> multiPath.refPointZMiddle
                else -> null
            }

            if (targetRefPoint != null) {
                val toolIndex = try {
                    toolCoordinateSystem.removePrefix("工具").toInt()
                } catch (e: Exception) { 1 }
                
                val targetPose = targetRefPoint.pose
                val storedJoints = targetRefPoint.jointAngles

                val jointsStr = if (storedJoints.size >= 6) {
                    storedJoints.take(6).joinToString(",") { String.format(Locale.US, "%.3f", it) }
                } else {
                    val currentJoints = socketManager.robotJoints.value
                    if (currentJoints.size >= 6) {
                        currentJoints.take(6).joinToString(",") { String.format(Locale.US, "%.3f", it) }
                    } else {
                        "0,0,0,0,0,0"
                    }
                }

                val pos2 = "$jointsStr,${targetPose.x},${targetPose.y},${targetPose.z},${targetPose.rx},${targetPose.ry},${targetPose.rz}"
                
                val ext1Str = String.format(Locale.US, "%.3f", targetPose.ext1)
                
                if (isExtAxisEnabled) {
                    val extAxisCmd = "ExtAxisMoveJ(1,$ext1Str,0.000,0.000,0.000,100,0)"
                    val msgExt = "/f/bIII${66}1III201III${extAxisCmd.length}III${extAxisCmd}III/b/f"
                    socketManager.sendControlCommand(msgExt)
                    Thread.sleep(50)
                }

                val cmd2 = "MoveL($pos2,$toolIndex,0,100,100,100,-1,0,$ext1Str,0.000,0.000,0.000,0,0,0,0,0,0,0,0,100,0)"
                val msg2 = "/f/bIII${66}2III201III${cmd2.length}III${cmd2}III/b/f"
                Log.d("MoveLCommand", "RefPoint MoveL: $msg2")
                socketManager.sendControlCommand(msg2)
                return
            }
        }

        // Validate Process
        val point = currentPath.points.getOrNull(currentPath.selectedPointIndex) ?: return
        
        var targetPose = point.pose
        val targetJoints = point.jointAngles

        // If Multi-Layer Pass is selected, apply offset
        if (selectedPassIndex >= 0 && selectedMultiLayerPathIndex in multiLayerWeldPaths.indices) {
            val multiPath = multiLayerWeldPaths[selectedMultiLayerPathIndex]
            if (selectedPassIndex < multiPath.passes.size && targetPose != null) {
                val pass = multiPath.passes[selectedPassIndex]
                if (point.type != WeldPointType.START_SAFE && point.type != WeldPointType.END_SAFE) {
                    val startIdx = currentPath.points.indexOfFirst { it.type == WeldPointType.START }
                    val endIdx = currentPath.points.indexOfLast { it.type == WeldPointType.END }
                    val currentIdx = currentPath.selectedPointIndex

                    if (multiPath.refPointX1 != null && multiPath.refPointZ1 != null &&
                        multiPath.refPointXEnd != null && multiPath.refPointZEnd != null &&
                        startIdx != -1 && endIdx != -1 && currentIdx >= startIdx && currentIdx <= endIdx) {
                        
                        var totalLen = 0.0
                        var currentLen = 0.0
                        for (i in startIdx until endIdx) {
                            val p1 = currentPath.points[i].pose
                            val p2 = currentPath.points[i+1].pose
                            if (p1 != null && p2 != null) {
                                val d = sqrt((p2.x - p1.x).pow(2) + (p2.y - p1.y).pow(2) + (p2.z - p1.z).pow(2))
                                totalLen += d
                                if (i < currentIdx) currentLen += d
                            }
                        }
                        val t = if (totalLen > 1e-6) currentLen / totalLen else 0.0
                        
                        val interpolatedY = when (currentIdx) {
                            startIdx -> pass.valYLeft
                            endIdx -> pass.valYRight
                            else -> 0.0
                        }

                        val cs = CoordinateUtils.interpolateCoordinateSystem(
                            originStart = currentPath.points[startIdx].pose!!.toPoint3D(),
                            xRefStart = multiPath.refPointX1!!.pose.toPoint3D(),
                            zRefStart = multiPath.refPointZ1!!.pose.toPoint3D(),
                            originEnd = currentPath.points[endIdx].pose!!.toPoint3D(),
                            xRefEnd = multiPath.refPointXEnd!!.pose.toPoint3D(),
                            zRefEnd = multiPath.refPointZEnd!!.pose.toPoint3D(),
                            t = t
                        )
                        
                        targetPose = CoordinateUtils.computeTargetPose(
                            basePose = targetPose!!,
                            cs = cs,
                            offsetX = signedPassX(pass.valX),
                            offsetY = interpolatedY,
                            offsetZ = pass.valZ,
                            offsetRx = pass.valR,
                            offsetRy = 0.0,
                            offsetRz = 0.0
                        )
                    } else {
                        val offsetX = signedPassX(pass.valX)
                        val offsetY = when (currentIdx) {
                            startIdx -> pass.valYLeft
                            endIdx -> pass.valYRight
                            else -> 0.0
                        }
                        val offsetZ = pass.valZ
                        targetPose = targetPose!!.copy(
                            x = targetPose!!.x + offsetX,
                            y = targetPose!!.y + offsetY,
                            z = targetPose!!.z + offsetZ
                        )
                    }
                }
            }
        }
        
        if (targetPose == null) return

        val toolIndex = try {
            toolCoordinateSystem.removePrefix("工具").toInt()
        } catch (e: Exception) { 1 }

        val jointsStr = if (targetJoints != null && targetJoints.size >= 6) {
             targetJoints.take(6).joinToString(",") { String.format(Locale.US, "%.3f", it) }
        } else {
            val currentJoints = socketManager.robotJoints.value
            if (currentJoints.size >= 6) {
                 currentJoints.take(6).joinToString(",") { String.format(Locale.US, "%.3f", it) }
            } else {
                 "0,0,0,0,0,0"
            }
        }

        val pos = "$jointsStr,${targetPose.x},${targetPose.y},${targetPose.z},${targetPose.rx},${targetPose.ry},${targetPose.rz}"
        
        val ext1Str = String.format(Locale.US, "%.3f", targetPose.ext1)
        
        if (isExtAxisEnabled) {
            val extAxisCmd = "ExtAxisMoveJ(1,$ext1Str,0.000,0.000,0.000,100,0)"
            val msgExt = "/f/bIII${66}1III201III${extAxisCmd.length}III${extAxisCmd}III/b/f"
            socketManager.sendControlCommand(msgExt)
            Thread.sleep(50)
        }

        val cmd = "MoveL($pos,$toolIndex,0,100,100,100,-1,0,$ext1Str,0.000,0.000,0.000,0,0,0,0,0,0,0,0,100,0)"
        val msg = "/f/bIII${66}2III201III${cmd.length}III${cmd}III/b/f"
        
        Log.d("MoveLCommand", "Sending: $msg")
        socketManager.sendControlCommand(msg)
    }

    // --- Multi-Layer Logic ---

    // 添加多层直线焊缝
    fun addLinearWeldPath() {
        val id = UUID.randomUUID().toString()
        val name = "多层直线 ${multiLayerWeldPaths.size + 1}"
        
        val basePath = WeldPath(
            id = UUID.randomUUID().toString(),
            name = "Base Path",
            points = mutableStateListOf(
                WeldPoint(UUID.randomUUID().toString(), WeldPointType.START_SAFE),
                WeldPoint(UUID.randomUUID().toString(), WeldPointType.START),
                WeldPoint(UUID.randomUUID().toString(), WeldPointType.END),
                WeldPoint(UUID.randomUUID().toString(), WeldPointType.END_SAFE)
            ),
            process = WeldProcess()
        )
        
        val newMultiPath = MultiLayerWeldPath(
            id = id,
            name = name,
            basePath = basePath,
            passes = mutableStateListOf()
        )
        
        multiLayerWeldPaths.add(newMultiPath)
        selectedMultiLayerPathIndex = multiLayerWeldPaths.size - 1
        selectedPassIndex = -1 // Default to Base Path
        saveCurrentProject()
        
        viewModelScope.launch {
            _scrollToIndexEvent.emit(multiLayerWeldPaths.lastIndex)
        }
    }

    // 添加多层圆弧焊缝
    fun addCircularWeldPath() {
        val id = UUID.randomUUID().toString()
        val name = "多层圆弧 ${multiLayerWeldPaths.size + 1}"
        
        val basePath = WeldPath(
            id = UUID.randomUUID().toString(),
            name = "Base Path",
            points = mutableStateListOf(
                WeldPoint(UUID.randomUUID().toString(), WeldPointType.START_SAFE),
                WeldPoint(UUID.randomUUID().toString(), WeldPointType.START),
                WeldPoint(UUID.randomUUID().toString(), WeldPointType.ARC_MIDDLE),
                WeldPoint(UUID.randomUUID().toString(), WeldPointType.END),
                WeldPoint(UUID.randomUUID().toString(), WeldPointType.END_SAFE)
            ),
            process = WeldProcess()
        )
        
        val newMultiPath = MultiLayerWeldPath(
            id = id,
            name = name,
            basePath = basePath,
            passes = mutableStateListOf()
        )
        
        multiLayerWeldPaths.add(newMultiPath)
        selectedMultiLayerPathIndex = multiLayerWeldPaths.size - 1
        selectedPassIndex = -1 // Default to Base Path
        saveCurrentProject()
        
        viewModelScope.launch {
            _scrollToIndexEvent.emit(multiLayerWeldPaths.lastIndex)
        }
    }

    // 添加焊道偏移
    fun addWeldPassOffset() {
        if (selectedMultiLayerPathIndex !in multiLayerWeldPaths.indices) return
        val currentMultiPath = multiLayerWeldPaths[selectedMultiLayerPathIndex]
        
        val newPass = WeldPassOffset(
            id = UUID.randomUUID().toString(),
            name = "第 ${currentMultiPath.passes.size + 1} 道",
            process = WeldProcess()
        )
        
        currentMultiPath.passes.add(newPass)
        saveCurrentProject()
    }

    // 删除焊道偏移
    fun deleteWeldPassOffset() {
        if (selectedMultiLayerPathIndex !in multiLayerWeldPaths.indices) return
        val currentMultiPath = multiLayerWeldPaths[selectedMultiLayerPathIndex]
        
        if (selectedPassIndex >= 0 && selectedPassIndex < currentMultiPath.passes.size) {
            currentMultiPath.passes.removeAt(selectedPassIndex)
            selectedPassIndex = -1
            saveCurrentProject()
        }
    }



    // --- Batch Execution Logic ---

    // 获取多层焊接的所有执行路径
    fun getMultiLayerExecutionPaths(): List<Pair<WeldPath, Int>> {
        val paths = mutableListOf<Pair<WeldPath, Int>>()
        if (selectedMultiLayerPathIndex !in multiLayerWeldPaths.indices) return paths
        
        val multiPath = multiLayerWeldPaths[selectedMultiLayerPathIndex]
        
        // 1. Base Path (Index -1)
        if (multiPath.basePath.isEnabled) {
            paths.add(Pair(multiPath.basePath, -1))
        }
        
        // 2. Passes (Index 0..N)
        multiPath.passes.forEachIndexed { index, pass ->
            if (pass.isEnabled) {
                // Generate a temporary WeldPath for the pass with offsets applied
                val passPath = generatePassPath(multiPath.basePath, pass, multiPath)
                if (passPath != null) {
                    paths.add(Pair(passPath, index))
                }
            }
        }
        return paths
    }

    // 位姿线性插值
    private fun subtractPoses(p1: Pose, p2: Pose): Pose {
        return Pose(
            x = p1.x - p2.x,
            y = p1.y - p2.y,
            z = p1.z - p2.z,
            rx = p1.rx - p2.rx,
            ry = p1.ry - p2.ry,
            rz = p1.rz - p2.rz
        )
    }

    private fun lerpPose(p1: Pose, p2: Pose, t: Double): Pose {
        return Pose(
            x = p1.x + (p2.x - p1.x) * t,
            y = p1.y + (p2.y - p1.y) * t,
            z = p1.z + (p2.z - p1.z) * t,
            rx = p1.rx + (p2.rx - p1.rx) * t,
            ry = p1.ry + (p2.ry - p1.ry) * t,
            rz = p1.rz + (p2.rz - p1.rz) * t
        )
    }

    // 参考点线性插值
    private fun lerpRefPoint(r1: RefPoint, r2: RefPoint, t: Double): RefPoint {
        val p1 = r1.pose
        val p2 = r2.pose
        val lerpedPose = lerpPose(p1, p2, t)
        
        val j1 = r1.jointAngles
        val j2 = r2.jointAngles
        val lerpedJoints = if (j1.size == j2.size) {
            j1.zip(j2).map { (a, b) -> a + (b - a) * t }
        } else {
            j1
        }
        
        return RefPoint(lerpedPose, lerpedJoints)
    }

    // 生成焊道路径（应用偏移和变换）
    // Helper to calculate CAD-like offset vector at a given point
    private fun getOffsetForPoint(
        currentIdx: Int,
        allPoints: List<WeldPoint>,
        refXVec: Vector3,
        refZVec: Vector3,
        offX: Double,
        offY: Double,
        offZ: Double
    ): Vector3 {
        val currentPt = allPoints[currentIdx]
        val pCurr = currentPt.pose!!.let { Vector3(it.x, it.y, it.z) }

        var prevPt: WeldPoint? = null
        var vPrev: Vector3? = null
        var prevIndex = -1
        for (i in currentIdx - 1 downTo 0) {
            val pt = allPoints[i]
            if (pt.type != WeldPointType.START_SAFE && pt.type != WeldPointType.END_SAFE) {
                prevPt = pt
                vPrev = pt.pose?.let { Vector3(it.x, it.y, it.z) }
                prevIndex = i
                break
            }
        }

        var nextPt: WeldPoint? = null
        var vNext: Vector3? = null
        var nextIndex = -1
        for (i in currentIdx + 1 until allPoints.size) {
            val pt = allPoints[i]
            if (pt.type != WeldPointType.START_SAFE && pt.type != WeldPointType.END_SAFE) {
                nextPt = pt
                vNext = pt.pose?.let { Vector3(it.x, it.y, it.z) }
                nextIndex = i
                break
            }
        }

        fun getFrame(p1: Vector3, p2: Vector3, rx: Vector3, rz: Vector3): Pair<Vector3, Vector3> {
            val t = (p2 - p1).normalize()
            val projX = rx - t * rx.dot(t)
            var n = if (projX.length() > 1e-3) projX.normalize() else t.cross(Vector3(0.0, 0.0, 1.0)).normalize()
            if (n.length() < 1e-3) n = Vector3(1.0, 0.0, 0.0)
            
            val projZ = rz - t * rz.dot(t)
            val b = if (projZ.length() > 1e-3) projZ.normalize() else n.cross(t).normalize()
            return Pair(n, b)
        }
        
        fun getCircleParams(p1: Vector3, p2: Vector3, p3: Vector3): Triple<Vector3, Vector3, Double>? {
            val v1 = p2 - p1
            val v2 = p3 - p2
            var normal = v1.cross(v2)
            if (normal.length() < 1e-3) return null
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
        
        fun solveLineArcIntersect(
            lineStart: Vector3, lineEnd: Vector3,
            arcStart: Vector3, arcMid: Vector3, arcEnd: Vector3,
            rx: Vector3, rz: Vector3,
            isArcFirst: Boolean
        ): Vector3? {
            val tLine = (lineEnd - lineStart).normalize()
            val (nLine, bLine) = getFrame(lineStart, lineEnd, rx, rz)
            
            val pTransition = if (isArcFirst) lineStart else lineEnd
            val lineOffsetOrigin = pTransition + nLine * offX + bLine * offZ
            
            val params = getCircleParams(arcStart, arcMid, arcEnd) ?: return null
            val (center, arcNormal, radius) = params
            
            val radial_mid = (arcMid - center).normalize()
            var tMid = arcNormal.cross(radial_mid).normalize()
            if ((arcEnd - arcStart).dot(tMid) < 0) tMid = tMid * -1.0
            
            val (nMid, bMid) = getFrame(arcMid, arcMid + tMid, rx, rz)
            val vOffMid = nMid * offX + bMid * offZ
            val delta_R = vOffMid.dot(radial_mid)
            val shift = vOffMid - radial_mid * delta_R
            
            val offsetRadius = radius + delta_R
            val offsetCenter = center + shift
            
            val V = lineOffsetOrigin - offsetCenter
            val a = 1.0
            val b = 2 * V.dot(tLine)
            val c = V.dot(V) - offsetRadius * offsetRadius
            val delta = b * b - 4 * a * c
            if (delta < 0) return lineOffsetOrigin
            
            val t1 = (-b - sqrt(delta)) / (2 * a)
            val t2 = (-b + sqrt(delta)) / (2 * a)
            
            val p1 = lineOffsetOrigin + tLine * t1
            val p2 = lineOffsetOrigin + tLine * t2
            val dist1 = kotlin.math.abs(t1)
            val dist2 = kotlin.math.abs(t2)
            
            return if (dist1 < dist2) p1 else p2
        }

        val offsetVec = when {
            // 1. ARC MIDDLE
            currentPt.type == WeldPointType.ARC_MIDDLE -> {
                if (vPrev != null && vNext != null) {
                    val params = getCircleParams(vPrev, pCurr, vNext)
                    if (params != null) {
                        val (center, arcNormal, _) = params
                        val radial_mid = (pCurr - center).normalize()
                        var tMid = arcNormal.cross(radial_mid).normalize()
                        if ((vNext - vPrev).dot(tMid) < 0) tMid = tMid * -1.0
                        
                        val (nMid, bMid) = getFrame(pCurr, pCurr + tMid, refXVec, refZVec)
                        val vOffMid = nMid * offX + bMid * offZ
                        val delta_R = vOffMid.dot(radial_mid)
                        val shift = vOffMid - radial_mid * delta_R
                        
                        radial_mid * delta_R + shift + tMid * offY
                    } else {
                        // Collinear fallback
                        val tIn = (pCurr - vPrev).normalize()
                        val tOut = (vNext - pCurr).normalize()
                        val (nIn, bIn) = getFrame(vPrev, pCurr, refXVec, refZVec)
                        val (nOut, bOut) = getFrame(pCurr, vNext, refXVec, refZVec)
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
                    Vector3(0.0, 0.0, 0.0)
                }
            }

            // 2. LINE -> ARC Transition (Arc Start)
            vPrev != null && vNext != null && nextPt?.type == WeldPointType.ARC_MIDDLE -> {
                var vNextNext: Vector3? = null
                for (i in nextIndex + 1 until allPoints.size) {
                    val pt = allPoints[i]
                    if (pt.type != WeldPointType.START_SAFE && pt.type != WeldPointType.END_SAFE) {
                        pt.pose?.let { vNextNext = Vector3(it.x, it.y, it.z) }
                        break
                    }
                }
                val localVNextNext = vNextNext
                if (localVNextNext != null) {
                    val intersect = solveLineArcIntersect(
                        lineStart = vPrev, lineEnd = pCurr,
                        arcStart = pCurr, arcMid = vNext, arcEnd = localVNextNext,
                        rx = refXVec, rz = refZVec,
                        isArcFirst = false
                    )
                    if (intersect != null) {
                        intersect - pCurr
                    } else {
                        val tIn = (pCurr - vPrev).normalize()
                        val tOut = (vNext - pCurr).normalize()
                        val (nIn, bIn) = getFrame(vPrev, pCurr, refXVec, refZVec)
                        val (nOut, bOut) = getFrame(pCurr, vNext, refXVec, refZVec)
                        val nAvg = (nIn + nOut).normalize()
                        val kX = if (nIn.dot(nOut) > -0.99) 1.0 / sqrt((1 + nIn.dot(nOut)) / 2) else 1.0
                        val bAvg = (bIn + bOut).normalize()
                        val kZ = if (bIn.dot(bOut) > -0.99) 1.0 / sqrt((1 + bIn.dot(bOut)) / 2) else 1.0
                        nAvg * (offX * kX) + bAvg * (offZ * kZ) + ((tIn + tOut).normalize()) * offY
                    }
                } else {
                    Vector3(0.0, 0.0, 0.0)
                }
            }

            // 3. ARC -> LINE Transition (Arc End)
            vPrev != null && vNext != null && prevPt?.type == WeldPointType.ARC_MIDDLE -> {
                var vPrevPrev: Vector3? = null
                for (i in prevIndex - 1 downTo 0) {
                    val pt = allPoints[i]
                    if (pt.type != WeldPointType.START_SAFE && pt.type != WeldPointType.END_SAFE) {
                        pt.pose?.let { vPrevPrev = Vector3(it.x, it.y, it.z) }
                        break
                    }
                }
                val localVPrevPrev = vPrevPrev
                if (localVPrevPrev != null) {
                    val intersect = solveLineArcIntersect(
                        lineStart = pCurr, lineEnd = vNext,
                        arcStart = localVPrevPrev, arcMid = vPrev, arcEnd = pCurr,
                        rx = refXVec, rz = refZVec,
                        isArcFirst = true
                    )
                    if (intersect != null) {
                        intersect - pCurr
                    } else {
                        val tIn = (pCurr - vPrev).normalize()
                        val tOut = (vNext - pCurr).normalize()
                        val (nIn, bIn) = getFrame(vPrev, pCurr, refXVec, refZVec)
                        val (nOut, bOut) = getFrame(pCurr, vNext, refXVec, refZVec)
                        val nAvg = (nIn + nOut).normalize()
                        val kX = if (nIn.dot(nOut) > -0.99) 1.0 / sqrt((1 + nIn.dot(nOut)) / 2) else 1.0
                        val bAvg = (bIn + bOut).normalize()
                        val kZ = if (bIn.dot(bOut) > -0.99) 1.0 / sqrt((1 + bIn.dot(bOut)) / 2) else 1.0
                        nAvg * (offX * kX) + bAvg * (offZ * kZ) + ((tIn + tOut).normalize()) * offY
                    }
                } else {
                    Vector3(0.0, 0.0, 0.0)
                }
            }

            // 4. LINE -> LINE Transition (Miter Joint)
            vPrev != null && vNext != null -> {
                val tIn = (pCurr - vPrev).normalize()
                val tOut = (vNext - pCurr).normalize()
                
                val crossT = tIn.cross(tOut)
                val sinT = crossT.length()
                val refXOut = if (sinT > 1e-6) {
                    val axis = crossT.normalize()
                    val cosT = tIn.dot(tOut)
                    refXVec * cosT + axis.cross(refXVec) * sinT + axis * (axis.dot(refXVec)) * (1.0 - cosT)
                } else refXVec
                
                val refZOut = if (sinT > 1e-6) {
                    val axis = crossT.normalize()
                    val cosT = tIn.dot(tOut)
                    refZVec * cosT + axis.cross(refZVec) * sinT + axis * (axis.dot(refZVec)) * (1.0 - cosT)
                } else refZVec
                
                val (nIn, bIn) = getFrame(vPrev, pCurr, refXVec, refZVec)
                val (nOut, bOut) = getFrame(pCurr, vNext, refXOut, refZOut)
                val nAvg = (nIn + nOut).normalize()
                val dotN = nIn.dot(nOut)
                val kX = if (dotN > -0.99) 1.0 / sqrt((1 + dotN) / 2) else 1.0
                val bAvg = (bIn + bOut).normalize()
                val dotB = bIn.dot(bOut)
                val kZ = if (dotB > -0.99) 1.0 / sqrt((1 + dotB) / 2) else 1.0
                val tAvg = (tIn + tOut).normalize()
                nAvg * (offX * kX) + bAvg * (offZ * kZ) + tAvg * offY
            }

            // 5. START POINT
            vNext != null -> {
                if (nextPt?.type == WeldPointType.ARC_MIDDLE) {
                    var vNextNext: Vector3? = null
                    for (i in nextIndex + 1 until allPoints.size) {
                        val pt = allPoints[i]
                        if (pt.type != WeldPointType.START_SAFE && pt.type != WeldPointType.END_SAFE) {
                            pt.pose?.let { vNextNext = Vector3(it.x, it.y, it.z) }
                            break
                        }
                    }
                    val localVNextNext = vNextNext
                    if (localVNextNext != null) {
                        val params = getCircleParams(pCurr, vNext, localVNextNext)
                        if (params != null) {
                            val (center, normal, _) = params
                            val radial = (pCurr - center).normalize()
                            var tStart = normal.cross(radial).normalize()
                            if ((vNext - pCurr).dot(tStart) < 0) tStart = tStart * -1.0
                            val (n, b) = getFrame(pCurr, pCurr + tStart, refXVec, refZVec)
                            return n * offX + tStart * offY + b * offZ
                        }
                    }
                }
                val t = (vNext - pCurr).normalize()
                val (n, b) = getFrame(pCurr, vNext, refXVec, refZVec)
                n * offX + t * offY + b * offZ
            }

            // 6. END POINT
            vPrev != null -> {
                if (prevPt?.type == WeldPointType.ARC_MIDDLE) {
                    var vPrevPrev: Vector3? = null
                    for (i in prevIndex - 1 downTo 0) {
                        val pt = allPoints[i]
                        if (pt.type != WeldPointType.START_SAFE && pt.type != WeldPointType.END_SAFE) {
                            pt.pose?.let { vPrevPrev = Vector3(it.x, it.y, it.z) }
                            break
                        }
                    }
                    val localVPrevPrev = vPrevPrev
                    if (localVPrevPrev != null) {
                        val params = getCircleParams(localVPrevPrev, vPrev, pCurr)
                        if (params != null) {
                            val (center, normal, _) = params
                            val radial = (pCurr - center).normalize()
                            var tEnd = normal.cross(radial).normalize()
                            if ((pCurr - vPrev).dot(tEnd) < 0) tEnd = tEnd * -1.0
                            val (n, b) = getFrame(pCurr, pCurr + tEnd, refXVec, refZVec)
                            return n * offX + tEnd * offY + b * offZ
                        }
                    }
                }
                val t = (pCurr - vPrev).normalize()
                val (n, b) = getFrame(vPrev, pCurr, refXVec, refZVec)
                n * offX + t * offY + b * offZ
            }

            else -> Vector3(0.0, 0.0, 0.0)
        }
        return offsetVec
    }

    private fun generatePassPath(basePath: WeldPath, pass: WeldPassOffset, multiPath: MultiLayerWeldPath): WeldPath? {
        val startIdx = basePath.points.indexOfFirst { it.type == WeldPointType.START }
        val endIdx = basePath.points.indexOfLast { it.type == WeldPointType.END }
        val middleIdx = basePath.points.indexOfFirst { it.type == WeldPointType.ARC_MIDDLE }

        // Calculate total length and segment lengths
        var totalLen = 0.0
        var lenToMiddle = 0.0
        
        if (startIdx != -1 && endIdx != -1) {
            for (i in startIdx until endIdx) {
                val p1 = basePath.points[i].pose
                val p2 = basePath.points[i+1].pose
                if (p1 != null && p2 != null) {
                    val d = sqrt((p2.x - p1.x).pow(2) + (p2.y - p1.y).pow(2) + (p2.z - p1.z).pow(2))
                    totalLen += d
                    if (middleIdx != -1 && i < middleIdx) {
                        lenToMiddle += d
                    }
                }
            }
        }

        val hasMiddleRef = multiPath.refPointXMiddle != null && multiPath.refPointZMiddle != null && 
                           middleIdx != -1 && middleIdx > startIdx && middleIdx < endIdx

        val newPoints = basePath.points.mapIndexed { index, point ->
            var newPose = point.pose ?: return null
            
            if (point.type != WeldPointType.START_SAFE && point.type != WeldPointType.END_SAFE) {
                 val currentIdx = index

                 if (multiPath.refPointX1 != null && multiPath.refPointZ1 != null &&
                     multiPath.refPointXEnd != null && multiPath.refPointZEnd != null &&
                     startIdx != -1 && endIdx != -1 && currentIdx >= startIdx && currentIdx <= endIdx) {
                     
                     // Calculate current length
                     var currentLen = 0.0
                     for (i in startIdx until currentIdx) {
                         val p1 = basePath.points[i].pose
                         val p2 = basePath.points[i+1].pose
                         if (p1 != null && p2 != null) {
                             currentLen += sqrt((p2.x - p1.x).pow(2) + (p2.y - p1.y).pow(2) + (p2.z - p1.z).pow(2))
                         }
                     }
                     
                     val t = if (totalLen > 1e-6) currentLen / totalLen else 0.0
                     val interpolatedY = when (currentIdx) {
                         startIdx -> pass.valYLeft
                         endIdx -> pass.valYRight
                         else -> 0.0
                     }

                     val originStart = basePath.points[startIdx].pose!!
                     val originEnd = basePath.points[endIdx].pose!!

                     // Origin A is simply the current point's pose on the base path
                     val originA = point.pose!!

                     // Interpolate Reference Points (B and C) using RELATIVE vectors to OriginA
                     // This mathematically guarantees:
                     // 1. Perfect parallel offset for mixed paths (when taught with fixed posture).
                     // 2. Perfect concentric circles for pure arcs (when taught with rotating posture).
                     val (refX, refZ) = if (hasMiddleRef) {
                         if (currentLen <= lenToMiddle) {
                             val segmentT = if (lenToMiddle > 1e-6) currentLen / lenToMiddle else 0.0
                             
                             val vecXStart = subtractPoses(multiPath.refPointX1!!.pose!!, originStart)
                             val vecXMid = subtractPoses(multiPath.refPointXMiddle!!.pose!!, basePath.points[middleIdx].pose!!)
                             val vecXCurrent = lerpPose(vecXStart, vecXMid, segmentT)
                             val b = originA.copy(
                                 x = originA.x + vecXCurrent.x,
                                 y = originA.y + vecXCurrent.y,
                                 z = originA.z + vecXCurrent.z
                             )
                             
                             val vecZStart = subtractPoses(multiPath.refPointZ1!!.pose!!, originStart)
                             val vecZMid = subtractPoses(multiPath.refPointZMiddle!!.pose!!, basePath.points[middleIdx].pose!!)
                             val vecZCurrent = lerpPose(vecZStart, vecZMid, segmentT)
                             val c = originA.copy(
                                 x = originA.x + vecZCurrent.x,
                                 y = originA.y + vecZCurrent.y,
                                 z = originA.z + vecZCurrent.z
                             )
                             
                             Pair(multiPath.refPointX1!!.copy(pose = b), multiPath.refPointZ1!!.copy(pose = c))
                         } else {
                             val segmentLen = totalLen - lenToMiddle
                             val distFromMiddle = currentLen - lenToMiddle
                             val segmentT = if (segmentLen > 1e-6) distFromMiddle / segmentLen else 0.0
                             
                             val vecXMid = subtractPoses(multiPath.refPointXMiddle!!.pose!!, basePath.points[middleIdx].pose!!)
                             val vecXEnd = subtractPoses(multiPath.refPointXEnd!!.pose!!, originEnd)
                             val vecXCurrent = lerpPose(vecXMid, vecXEnd, segmentT)
                             val b = originA.copy(
                                 x = originA.x + vecXCurrent.x,
                                 y = originA.y + vecXCurrent.y,
                                 z = originA.z + vecXCurrent.z
                             )
                             
                             val vecZMid = subtractPoses(multiPath.refPointZMiddle!!.pose!!, basePath.points[middleIdx].pose!!)
                             val vecZEnd = subtractPoses(multiPath.refPointZEnd!!.pose!!, originEnd)
                             val vecZCurrent = lerpPose(vecZMid, vecZEnd, segmentT)
                             val c = originA.copy(
                                 x = originA.x + vecZCurrent.x,
                                 y = originA.y + vecZCurrent.y,
                                 z = originA.z + vecZCurrent.z
                             )
                             
                             Pair(multiPath.refPointXMiddle!!.copy(pose = b), multiPath.refPointZMiddle!!.copy(pose = c))
                         }
                     } else {
                        val vecXStart = subtractPoses(multiPath.refPointX1!!.pose!!, originStart)
                        val vecZStart = subtractPoses(multiPath.refPointZ1!!.pose!!, originStart)

                        var vx = Vector3(vecXStart.x, vecXStart.y, vecXStart.z)
                        var vz = Vector3(vecZStart.x, vecZStart.y, vecZStart.z)

                        // Bishop transport for multi-segment paths to perfectly follow corners
                       for (idx in startIdx until currentIdx - 1) {
                           val p0 = basePath.points[idx].pose ?: continue
                            val p1 = basePath.points[idx+1].pose ?: continue
                            var p2: Pose? = null
                            for (j in idx+2 until basePath.points.size) {
                                val pt = basePath.points[j]
                                if (pt.type != WeldPointType.START_SAFE && pt.type != WeldPointType.END_SAFE) {
                                    p2 = pt.pose
                                    break
                                }
                            }
                            if (p2 != null) {
                                val t0 = Vector3(p1.x - p0.x, p1.y - p0.y, p1.z - p0.z).normalize()
                                val t1 = Vector3(p2.x - p1.x, p2.y - p1.y, p2.z - p1.z).normalize()
                                val crossT = t0.cross(t1)
                                val sinT = crossT.length()
                                if (sinT > 1e-6) {
                                    val axis = crossT.normalize()
                                    val cosT = t0.dot(t1)
                                    vx = vx * cosT + axis.cross(vx) * sinT + axis * (axis.dot(vx)) * (1.0 - cosT)
                                    vz = vz * cosT + axis.cross(vz) * sinT + axis * (axis.dot(vz)) * (1.0 - cosT)
                                }
                            }
                        }

                        val b = originA.copy(
                            x = originA.x + vx.x,
                            y = originA.y + vx.y,
                            z = originA.z + vx.z
                        )
                        val c = originA.copy(
                            x = originA.x + vz.x,
                            y = originA.y + vz.y,
                            z = originA.z + vz.z
                        )

                        Pair(multiPath.refPointX1!!.copy(pose = b), multiPath.refPointZ1!!.copy(pose = c))
                    }

                     val refXVec = Vector3(refX.pose.x - originA.x, refX.pose.y - originA.y, refX.pose.z - originA.z)
                     val refZVec = Vector3(refZ.pose.x - originA.x, refZ.pose.y - originA.y, refZ.pose.z - originA.z)

                     val offsetVec = getOffsetForPoint(
                         currentIdx = currentIdx,
                         allPoints = basePath.points,
                         refXVec = refXVec,
                         refZVec = refZVec,
                         offX = signedPassX(pass.valX),
                         offY = interpolatedY,
                         offZ = pass.valZ
                     )

                     val dcddA = com.gbndt.shijiaoqi.utils.DcddPoint(originA.x, originA.y, originA.z)
                     val dcddB = com.gbndt.shijiaoqi.utils.DcddPoint(refX.pose.x, refX.pose.y, refX.pose.z)
                     val dcddC = com.gbndt.shijiaoqi.utils.DcddPoint(refZ.pose.x, refZ.pose.y, refZ.pose.z)
                     val coordSys = com.gbndt.shijiaoqi.utils.DcddCoordinateSystem(dcddA, dcddB, dcddC)
                     val (_, euler) = coordSys.rotateBAByAngle(pass.valR)
                     
                     val rotXOffset = Math.toDegrees(euler.first)
                     val rotYOffset = Math.toDegrees(euler.second) - 90
                     val rotZOffset = Math.toDegrees(euler.third)
                     
                     // Calculate Offsets relative to OriginA using CAD Miter Joint logic
                     val offX = offsetVec.x
                     val offY = offsetVec.y
                     val offZ = offsetVec.z

                     // Use OriginA as the base pose
                     newPose = originA
                     
                     // Store execution offsets
                     val offsets = listOf(offX, offY, offZ, rotXOffset, rotYOffset, rotZOffset)
                     
                     // Update point with new pose and offsets
                     // Note: We keep the original joint angles or interpolate them if needed. 
                     // For simplicity and since MoveL uses cartesian, we can keep point.jointAngles if originA is close to point.pose
                     // Or we can try to use lerped joints if available. 
                     // But WeldPoint doesn't support storing lerped joints easily unless we overwrite jointAngles.
                     // Since originA is lerped, we should probably not overwrite jointAngles with old values if they are far.
                     // But we don't have lerped joints here (lerpRefPoint does it but lerpPose doesn't).
                     // Let's stick with point.jointAngles as seed.
                     
                     val newPoint = point.copy(pose = newPose, executionOffsets = offsets)
                     return@mapIndexed newPoint // We need to return the point for the map
                 } else {
                     // Simple Offset (Fallback or non-main points)
                     // If we want to support offsets here too, we can.
                     // But for now, let's keep the old behavior for "Simple Offset" but utilizing executionOffsets?
                     // Or just absolute. The user specifically asked for "duocengduodao" which applies to the main segment.
                     
                     newPose = MultiLayerRun.simpleOffset(
                         newPose,
                         point.type,
                         currentIdx == startIdx,
                         currentIdx == endIdx,
                         signedPassX(pass.valX),
                         pass.valYLeft,
                         pass.valYRight,
                         pass.valZ,
                     )
                     val newPoint = point.copy(pose = newPose, executionOffsets = null)
                     return@mapIndexed newPoint
                 }
            }
            // For START_SAFE and END_SAFE, just copy without offsets (use Base Path's points)
            val newPoint = point.copy(pose = newPose, executionOffsets = null)
            newPoint
        }.toMutableList()
        
        return basePath.copy(
            id = UUID.randomUUID().toString(), // Temp ID
            name = "${basePath.name} - ${pass.name}",
            points = mutableStateListOf<WeldPoint>().apply { addAll(newPoints) },
            process = pass.process,
            processId = pass.processId
        )
    }

    // 开始批量执行
    private fun startBatchExecution() {
        if (executionQueue.isNotEmpty()) {
            isExecutingBatch = true
            sendNextGroup()
        }
    }

    // 发送下一组指令
    private fun sendNextGroup() {
        currentExecutionGroup = executionQueue.removeFirstOrNull()
        if (currentExecutionGroup != null) {
            Log.d("BatchExecution", "Sending Group: ${currentExecutionGroup!!.description}")
            val msg = currentExecutionGroup!!.commandString
            if (msg != "106_BATCH_SENT") {
                socketManager.sendControlCommand(msg)
            }
        } else {
            isExecutingBatch = false
            Log.d("BatchExecution", "All Groups Finished")
        }
    }
    
    // 继续焊接（断点续焊）
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
            kotlinx.coroutines.delay(2000)
            isResuming = false
        }
    }

    // 恢复批量执行
    private fun resumeBatchExecution() {
        if (multiLayerWeldPaths.isEmpty()) return
        
        executionQueue.clear()
        commandIdMap.clear()
        commandLineMap.clear()
        
        // Initial Speed
        val globalSpeedCmd = "SetSpeed(10)"
        
        val builder = BatchCommandBuilder("Multi-Layer Resume")
        builder.appendCmd(globalSpeedCmd)
        
        val allPaths = getAllInterleavedExecutionPaths()
        var foundResumePoint = false
        
        allPaths.forEach { (path, mIndex, pIndex) ->
            if (!foundResumePoint) {
                // Check if this is the path where we stopped
                if (mIndex == lastReachedMultiPathIndex && pIndex == lastReachedPassIndex) {
                    foundResumePoint = true
                    // Resume from next point
                    val startPointIndex = lastReachedPointIndex + 1
                    
                    if (startPointIndex < path.points.size) {
                        // Pass specific params
                        val process = path.process
                        val paramCmd = "WeldingSetProcessParam(2,${process.startArcCurrent},${process.startArcVoltage},${process.startArcTime},${process.current},${process.voltage},${process.endArcCurrent},${process.endArcVoltage},${process.endArcTime})"
                        val paramId = globalCommandId++
                        builder.appendCmd(paramCmd, paramId)
                        
                        val toolIndex = try {
                            toolCoordinateSystem.removePrefix("工具").toInt()
                        } catch (e: Exception) { 1 }
                        
                        appendCommandsForPath(path, builder, mIndex, pIndex, toolIndex, startPointIndex)
                    }
                }
            } else {
                // Subsequent paths: Full execution
                val process = path.process
                val paramCmd = "WeldingSetProcessParam(2,${process.startArcCurrent},${process.startArcVoltage},${process.startArcTime},${process.current},${process.voltage},${process.endArcCurrent},${process.endArcVoltage},${process.endArcTime})"
                val paramId = globalCommandId++
                builder.appendCmd(paramCmd, paramId)
                
                val toolIndex = try {
                    toolCoordinateSystem.removePrefix("工具").toInt()
                } catch (e: Exception) { 1 }
                
                appendCommandsForPath(path, builder, mIndex, pIndex, toolIndex)
            }
        }
        
        builder.flush()
        
        // Restore state if needed, though they should be preserved
        // isWelding/isSimulating should be true if we are resuming
        
        startBatchExecution()
    }

    private fun findRetreatPoint(currentState: RobotState): RobotState {
        // 1. Try History
        if (positionHistory.isNotEmpty()) {
             for (i in positionHistory.lastIndex downTo 0) {
                 val p = positionHistory[i]
                 val dist = sqrt((currentState.pose.x - p.pose.x).pow(2) + (currentState.pose.y - p.pose.y).pow(2) + (currentState.pose.z - p.pose.z).pow(2))
                 if (dist >= 5.0) { // 5mm
                     return p
                 }
             }
        }
        
        // 2. Fallback: Return current state if history is not available
        return currentState
    }

    // 暂停焊接
    override fun pauseWelding() {
        if (!isWelding && !isSimulating) return

        viewModelScope.launch {
            socketManager.sendControlCommand(WeldRun.pause())
            kotlinx.coroutines.delay(WeldRun.AFTER_PAUSE_MS)
            socketManager.sendControlCommand(RobotCommands.modeManual())
        }

        isPaused = true
    }

    // 停止焊接
    // Tracks exact robot position and state when STOP is pressed
    var stopPointPose: Pose? = null
    var stopPointJoints: List<Double>? = null
    var stopMultiPathIndex: Int = -1
    var stopPassIndex: Int = -1
    var stopPointIndex: Int = -1

    override fun stopWelding(force: Boolean) {
        // Record EXACT position and tracking data when STOP is pressed
        stopPointPose = socketManager.robotPose.value
        stopPointJoints = socketManager.robotJoints.value
        stopMultiPathIndex = lastReachedMultiPathIndex
        stopPassIndex = lastReachedPassIndex
        stopPointIndex = lastReachedPointIndex



        isWelding = false
        isSimulating = false
        isPaused = false
        programHasStarted = false
        weldingTimerJob?.cancel()
        markPouchWelding(false)
        
        viewModelScope.launch {
            socketManager.sendControlCommand(WeldRun.stop())
            kotlinx.coroutines.delay(1000)
            socketManager.sendControlCommand(RobotCommands.modeManual())
        }

        if (weldingBreakOffState == "中断") {
            socketManager.sendControlCommand(WeldRun.abortAfterBreak())
        }
        
        // Arc End
        val arcEndCmd = "ARCEnd(0,2,10000)"
        val arcEndId = globalCommandId++
        val arcEndMsg = "/f/bIII${arcEndId}III248III${arcEndCmd.length}III${arcEndCmd}III/b/f"
        socketManager.sendControlCommand(arcEndMsg)
        
        executionQueue.clear()
        isExecutingBatch = false
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

    // 多层 X 按工艺填写方向使用，不再随第七轴取反。
    private fun signedPassX(valX: Double): Double = valX

    private fun fmtPose(v: Double): String = String.format(Locale.US, "%.3f", v)

    private fun hasNonZeroOffsets(offsets: List<Double>?): Boolean =
        offsets != null && offsets.size >= 6 && offsets.any { abs(it) >= 1e-5 }

    private fun bakePoseWithOffsets(pose: Pose, offsets: List<Double>?): Pose {
        if (!hasNonZeroOffsets(offsets)) return pose
        val o = offsets!!
        return pose.copy(
            x = pose.x + o[0],
            y = pose.y + o[1],
            z = pose.z + o[2],
            rx = pose.rx + o[3],
            ry = pose.ry + o[4],
            rz = pose.rz + o[5]
        )
    }

    // 第七轴 + 偏移：控制器 offset_flag≠0 会在导轨到位后笛卡尔逆解报 112。
    // 把偏移加进笛卡尔，用 GetInverseKinExaxis 再发 offset_flag=0 的 MoveL。
    private fun appendExtAxisIkMoveL(
        builder: BatchCommandBuilder,
        pose: Pose,
        toolIndex: Int,
        moveSpeed: Int,
        cmdId: Int
    ) {
        val nx = fmtPose(pose.x)
        val ny = fmtPose(pose.y)
        val nz = fmtPose(pose.z)
        val rx = fmtPose(pose.rx)
        val ry = fmtPose(pose.ry)
        val rz = fmtPose(pose.rz)
        val ext1Str = fmtPose(pose.ext1)
        val ikCmd = "j1,j2,j3,j4,j5,j6=GetInverseKinExaxis(0,{$nx,$ny,$nz,$rx,$ry,$rz},{$ext1Str,0.000,0.000,0.000},$toolIndex,0)"
        builder.appendCmd(ikCmd)
        val cmd = "MoveL(j1,j2,j3,j4,j5,j6,$nx,$ny,$nz,$rx,$ry,$rz,$toolIndex,0,100,100,$moveSpeed,-1,0,$ext1Str,0.000,0.000,0.000,0,0,0,0,0,0,0,0,100,0)"
        builder.appendCmd(cmd, cmdId)
    }

    private fun appendExtAxisIkMoveC(
        builder: BatchCommandBuilder,
        midPose: Pose,
        endPose: Pose,
        toolIndex: Int,
        speed: Int,
        cmdId: Int
    ) {
        val mx = fmtPose(midPose.x)
        val my = fmtPose(midPose.y)
        val mz = fmtPose(midPose.z)
        val mrx = fmtPose(midPose.rx)
        val mry = fmtPose(midPose.ry)
        val mrz = fmtPose(midPose.rz)
        val extMid = fmtPose(midPose.ext1)
        val ex = fmtPose(endPose.x)
        val ey = fmtPose(endPose.y)
        val ez = fmtPose(endPose.z)
        val erx = fmtPose(endPose.rx)
        val ery = fmtPose(endPose.ry)
        val erz = fmtPose(endPose.rz)
        val extEnd = fmtPose(endPose.ext1)
        builder.appendCmd("m1,m2,m3,m4,m5,m6=GetInverseKinExaxis(0,{$mx,$my,$mz,$mrx,$mry,$mrz},{$extMid,0.000,0.000,0.000},$toolIndex,0)")
        builder.appendCmd("e1,e2,e3,e4,e5,e6=GetInverseKinExaxis(0,{$ex,$ey,$ez,$erx,$ery,$erz},{$extEnd,0.000,0.000,0.000},$toolIndex,0)")
        val cmd = "MoveC(m1,m2,m3,m4,m5,m6,$mx,$my,$mz,$mrx,$mry,$mrz,$toolIndex,0,100,100,0,0,0,0,0,0,0,0,0,0,0,e1,e2,e3,e4,e5,e6,$ex,$ey,$ez,$erx,$ery,$erz,$toolIndex,0,100,100,0,0,0,0,0,0,0,0,0,0,0,$speed,-1)"
        builder.appendCmd(cmd, cmdId)
    }

    // 为路径追加指令到构建器
    private fun appendCommandsForPath(weldPath: WeldPath, builder: BatchCommandBuilder, multiPathIndex: Int, passIndex: Int, toolIndex: Int, startPointIndex: Int = 0, isResumeFromStop: Boolean = false) {
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

        // --- Oscillation Parameters Command ---
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
             
             val weaveParaCmd = "WeaveSetPara(3,$typeCode,${process.oscillation.frequency},$waitTimeCode,${process.oscillation.amplitude},${process.oscillation.leftSideLength},${process.oscillation.rightSideLength},${process.oscillation.zeroTime},${process.oscillation.leftStopTime},${process.oscillation.rightStopTime},${process.oscillation.callbackRatio},$posWaitCode,${process.oscillation.azimuth},${process.oscillation.inclination})"
             val weaveParaId = globalCommandId++
             builder.appendCmd(weaveParaCmd, weaveParaId)
        }

        var index = startPointIndex
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
            commandIdMap[id] = Triple(multiPathIndex, passIndex, stopPointIndex)
            builder.appendCmd(cmd2, id)
            
            val arcStartIdx = points.indexOfFirst { it.type == WeldPointType.START }
            if (stopPointIndex > arcStartIdx && arcStartIdx != -1) {
                // Resume welding
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
            index = stopPointIndex
            
            // NEW LOGIC: Arc Resume
            var isArcResume = false
            var arcStartIndex = -1
            
            // Protect index from being negative when accessing points
            val safeIndex = if (index >= 0) index else 0
            
            android.util.Log.e("ArcDebug", "MultiLayer - stopPointIndex: $stopPointIndex, index: $index, safeIndex: $safeIndex")
            
            // Find which segment of the arc we are in based on safeIndex (the point we are heading towards)
            if (safeIndex > 0 && safeIndex < points.size && points[safeIndex].type == WeldPointType.END && points[safeIndex - 1].type == WeldPointType.ARC_MIDDLE) {
                // Heading towards END. The arc is START -> ARC_MIDDLE -> END
                arcStartIndex = safeIndex - 2
                isArcResume = true
                android.util.Log.e("ArcDebug", "MultiLayer - isArcResume: TRUE (Heading towards END)")
            } else if (safeIndex > 0 && safeIndex < points.size && points[safeIndex].type == WeldPointType.ARC_MIDDLE && points[safeIndex - 1].type == WeldPointType.START) {
                // Heading towards ARC_MIDDLE. The arc is START -> ARC_MIDDLE -> END
                arcStartIndex = safeIndex - 1
                isArcResume = true
                android.util.Log.e("ArcDebug", "MultiLayer - isArcResume: TRUE (Heading towards ARC_MIDDLE)")
            } else if (safeIndex < points.size - 2 && points[safeIndex].type == WeldPointType.START && points[safeIndex + 1].type == WeldPointType.ARC_MIDDLE && points[safeIndex + 2].type == WeldPointType.END) {
                // Heading towards START. We haven't even reached the start of the arc yet!
                isArcResume = false
                android.util.Log.e("ArcDebug", "MultiLayer - isArcResume: FALSE (Heading towards START)")
            } else {
                // EXTENDED LOGIC: What if safeIndex is pointing EXACTLY at the ARC_MIDDLE or START itself?
                // Let's check the surrounding points to see if we are standing on an arc point
                if (safeIndex < points.size && points[safeIndex].type == WeldPointType.ARC_MIDDLE && safeIndex > 0 && points[safeIndex - 1].type == WeldPointType.START) {
                    arcStartIndex = safeIndex - 1
                    isArcResume = true
                    android.util.Log.e("ArcDebug", "MultiLayer - isArcResume: TRUE (Standing ON ARC_MIDDLE)")
                } else if (safeIndex < points.size && points[safeIndex].type == WeldPointType.END && safeIndex > 1 && points[safeIndex - 1].type == WeldPointType.ARC_MIDDLE) {
                    arcStartIndex = safeIndex - 2
                    isArcResume = true
                    android.util.Log.e("ArcDebug", "MultiLayer - isArcResume: TRUE (Standing ON END)")
                } else {
                    android.util.Log.e("ArcDebug", "MultiLayer - isArcResume: FALSE (Not in Arc Segment)")
                }
            }
            
            if (isArcResume && arcStartIndex >= 0 && arcStartIndex + 2 < points.size) {
                val pStartPt = points[arcStartIndex]
                val pMidPt = points[arcStartIndex + 1]
                val pEndPt = points[arcStartIndex + 2]
                
                val pStart = pStartPt.pose
                val pMid = pMidPt.pose
                val pEnd = pEndPt.pose
                val jEnd = pEndPt.jointAngles
                
                if (pStart != null && pMid != null && pEnd != null && jEnd != null) {
                    fun getPhysicalPose(pt: WeldPoint): Pose {
                        val p = pt.pose!!
                        val off = pt.executionOffsets
                        return if (off != null && off.size >= 6) {
                            p.copy(
                                x = p.x + off[0],
                                y = p.y + off[1],
                                z = p.z + off[2],
                                rx = p.rx + off[3],
                                ry = p.ry + off[4],
                                rz = p.rz + off[5]
                            )
                        } else {
                            p
                        }
                    }
                    
                    // 1. Calculate the new midpoint in Physical Space
                    val physStart = getPhysicalPose(pStartPt)
                    val physMid = getPhysicalPose(pMidPt)
                    val physEnd = getPhysicalPose(pEndPt)
                    
                    val newMidPose = calculateNewArcMidPoint(physStart, physMid, physEnd, stopPointPose!!)
                    
                    android.util.Log.e("ArcDebug", "========= MultiLayer Arc Debug =========")
                    android.util.Log.e("ArcDebug", "Original Start (Physical): ${physStart.x}, ${physStart.y}, ${physStart.z}")
                    android.util.Log.e("ArcDebug", "Original Mid (Physical):   ${physMid.x}, ${physMid.y}, ${physMid.z}")
                    android.util.Log.e("ArcDebug", "Original End (Physical):   ${physEnd.x}, ${physEnd.y}, ${physEnd.z}")
                    android.util.Log.e("ArcDebug", "Stop Point (Current):      ${stopPointPose!!.x}, ${stopPointPose!!.y}, ${stopPointPose!!.z}")
                    android.util.Log.e("ArcDebug", "New Mid Point calculated:  ${newMidPose.x}, ${newMidPose.y}, ${newMidPose.z}")
                    android.util.Log.e("ArcDebug", "========================================")

                    val newMidJoints = interpolateJoints(stopPointJoints!!, jEnd)
                    
                    // 2. Midpoint uses Physical Coordinates and interpolated joints (Enable=0)
                    val midPosStr = listOf(
                        newMidJoints[0], newMidJoints[1], newMidJoints[2], newMidJoints[3], newMidJoints[4], newMidJoints[5],
                        newMidPose.x, newMidPose.y, newMidPose.z, newMidPose.rx, newMidPose.ry, newMidPose.rz
                    ).joinToString(",") { String.format(Locale.US, "%.3f", it) }
                    
                    // 3. Endpoint uses Original Virtual Coordinates & Original Joints, and let controller apply offset!
                    val endPosStr = listOf(
                        jEnd[0], jEnd[1], jEnd[2], jEnd[3], jEnd[4], jEnd[5],
                        pEnd.x, pEnd.y, pEnd.z, pEnd.rx, pEnd.ry, pEnd.rz
                    ).joinToString(",") { String.format(Locale.US, "%.3f", it) }
                    
                    // CRITICAL FIX: Force Enable=1 for midpoint to make controller run Inverse Kinematics!
                    val midOffsetStr = "1,0.000,0.000,0.000,0.000,0.000,0.000"
                    
                    val nextOffsets = pEndPt.executionOffsets
                    val isZeroOffset = nextOffsets != null && nextOffsets.all { kotlin.math.abs(it) < 1e-5 }
                    val endOffsetStr = if (nextOffsets != null && nextOffsets.size >= 6 && !isZeroOffset) {
                        val offX = String.format(Locale.US, "%.3f", nextOffsets[0])
                        val offY = String.format(Locale.US, "%.3f", nextOffsets[1])
                        val offZ = String.format(Locale.US, "%.3f", nextOffsets[2])
                        val offRx = String.format(Locale.US, "%.3f", nextOffsets[3])
                        val offRy = String.format(Locale.US, "%.3f", nextOffsets[4])
                        val offRz = String.format(Locale.US, "%.3f", nextOffsets[5])
                        "3,$offX,$offY,$offZ,$offRx,$offRy,$offRz"
                    } else {
                        "0,0,0,0,0,0,0"
                    }
                    
                    val ext1StrEnd = String.format(Locale.US, "%.3f", pEndPt.pose?.ext1 ?: 0.0)
                    if (isExtAxisEnabled) {
                        val extAxisCmd = "ExtAxisMoveJ(1,$ext1StrEnd,0.000,0.000,0.000,$finalSpeed,-1)"
                        val extAxisId = globalCommandId++
                        builder.appendCmd(extAxisCmd, extAxisId)
                    }

                    val arcId = globalCommandId++
                    commandIdMap[arcId] = Triple(multiPathIndex, passIndex, arcStartIndex + 2)
                    if (isExtAxisEnabled) {
                        val bakedEnd = bakePoseWithOffsets(pEnd, nextOffsets)
                        appendExtAxisIkMoveC(builder, newMidPose, bakedEnd, toolIndex, finalSpeed, arcId)
                    } else {
                        val moveC = "MoveC($midPosStr,$toolIndex,0,100,100,0,0,0,0,$midOffsetStr,$endPosStr,$toolIndex,0,100,100,0,0,0,0,$endOffsetStr,$finalSpeed,-1)"
                        builder.appendCmd(moveC, arcId)
                    }
                    
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
                    index = arcStartIndex + 3
                }
            }
        } else {
            // Not an arc resume, just normal resume point
            index = if (stopPointIndex >= 0) stopPointIndex else 0
        }

        while (index >= 0 && index < points.size) {
            val point = points[index]
            
            // Skip safe points if they are not relevant for welding logic, but usually we need them for approach.
            // Generate command for each point
            
            val pose = point.pose
            if (pose == null) {
                index++
                continue
            }
            val joints = point.jointAngles
            
            val jointsStr = if (joints != null && joints.size >= 6) {
                 joints.take(6).joinToString(",") { String.format(Locale.US, "%.3f", it) }
            } else "0,0,0,0,0,0"
            
            val pos = "${jointsStr},${String.format(Locale.US, "%.3f", pose.x)},${String.format(Locale.US, "%.3f", pose.y)},${String.format(Locale.US, "%.3f", pose.z)},${String.format(Locale.US, "%.3f", pose.rx)},${String.format(Locale.US, "%.3f", pose.ry)},${String.format(Locale.US, "%.3f", pose.rz)}"
            
            // Assign ID for feedback mapping
            val cmdId = globalCommandId++
            commandIdMap[cmdId] = Triple(multiPathIndex, passIndex, index)
            
            // Handle Offsets
            val offsets = point.executionOffsets
            val isOffsetsZero = offsets != null && offsets.all { kotlin.math.abs(it) < 1e-5 }
            val userParams = if (offsets != null && offsets.size >= 6 && !isOffsetsZero) {
                 val offX = String.format(Locale.US, "%.3f", offsets[0])
                 val offY = String.format(Locale.US, "%.3f", offsets[1])
                 val offZ = String.format(Locale.US, "%.3f", offsets[2])
                 val offRx = String.format(Locale.US, "%.3f", offsets[3])
                 val offRy = String.format(Locale.US, "%.3f", offsets[4])
                 val offRz = String.format(Locale.US, "%.3f", offsets[5])
                 "3,$offX,$offY,$offZ,$offRx,$offRy,$offRz"
            } else {
                 "0,0,0,0,0,0,0"
            }
            
            val moveSpeed = if (point.type == WeldPointType.START_SAFE || point.type == WeldPointType.END_SAFE || point.type == WeldPointType.START) 100 else finalSpeed

            val ext1Str = String.format(Locale.US, "%.3f", point.pose?.ext1 ?: 0.0)
            
            // Insert ExtAxisMoveJ
            if (isExtAxisEnabled) {
                val extAxisCmd = "ExtAxisMoveJ(1,$ext1Str,0.000,0.000,0.000,$moveSpeed,-1)"
                val extAxisId = globalCommandId++
                builder.appendCmd(extAxisCmd, extAxisId)
            }

            // Determine Command Type based on Point Type
            when (point.type) {
                WeldPointType.START_SAFE, WeldPointType.END_SAFE -> {
                    val cmd = "MoveL($pos,$toolIndex,0,100,100,$moveSpeed,-1,0,$ext1Str,0.000,0.000,0.000,0,0,0,0,0,0,0,0,100,0)"
                    builder.appendCmd(cmd, cmdId)
                }
                WeldPointType.START -> {
                    if (isExtAxisEnabled && hasNonZeroOffsets(offsets) && joints != null && joints.size >= 6) {
                        appendExtAxisIkMoveL(builder, bakePoseWithOffsets(pose, offsets), toolIndex, moveSpeed, cmdId)
                    } else {
                        val cmd = "MoveL($pos,$toolIndex,0,100,100,$moveSpeed,-1,0,$ext1Str,0.000,0.000,0.000,0,$userParams,100,0)"
                        builder.appendCmd(cmd, cmdId)
                    }
                    
                    if (!hasStartedArc) {
                        // Arc Start
                        if (isWelding) {
                             val arcCmd = "ARCStart(0,2,10000)"
                             val arcId = globalCommandId++
                             builder.appendCmd(arcCmd, arcId)
                        }
                        
                        // Weave Start
                        val oscType = process.oscillation.type
                        if (oscType != "无摆动") {
                            val weaveStartCmd = "WeaveStart(3)"
                            val weaveStartId = globalCommandId++
                            builder.appendCmd(weaveStartCmd, weaveStartId)
                        }
                        hasStartedArc = true
                    }
                }
                WeldPointType.MIDDLE -> {
                    if (isExtAxisEnabled && hasNonZeroOffsets(offsets) && joints != null && joints.size >= 6) {
                        appendExtAxisIkMoveL(builder, bakePoseWithOffsets(pose, offsets), toolIndex, finalSpeed, cmdId)
                    } else {
                        val cmd = "MoveL($pos,$toolIndex,0,100,100,$moveSpeed,-1,0,$ext1Str,0.000,0.000,0.000,0,$userParams,100,0)"
                        builder.appendCmd(cmd, cmdId)
                    }
                    
                    if (index == startPointIndex && oscType != "无摆动" && !hasStartedArc) {
                        val weaveStartCmd = "WeaveStart(3)"
                        val weaveStartId = globalCommandId++
                        builder.appendCmd(weaveStartCmd, weaveStartId)
                    }
                }
                WeldPointType.ARC_MIDDLE -> {
                    // Check if next point exists for MoveC
                    if (index + 1 < points.size) {
                        val nextPoint = points[index + 1]
                        val nextPose = nextPoint.pose
                        val nextJoints = nextPoint.jointAngles
                        
                        if (nextPose != null && nextJoints != null) {
                            val nextJointsStr = if (nextJoints.size >= 6) {
                                nextJoints.take(6).joinToString(",") { String.format(Locale.US, "%.3f", it) }
                            } else "0,0,0,0,0,0"
                            
                            val nextPos = "${nextJointsStr},${String.format(Locale.US, "%.3f", nextPose.x)},${String.format(Locale.US, "%.3f", nextPose.y)},${String.format(Locale.US, "%.3f", nextPose.z)},${String.format(Locale.US, "%.3f", nextPose.rx)},${String.format(Locale.US, "%.3f", nextPose.ry)},${String.format(Locale.US, "%.3f", nextPose.rz)}"
                            
                            // Prepare next user params (offsets for next point)
                            val nextOffsets = nextPoint.executionOffsets
                            val isNextOffsetsZero = nextOffsets != null && nextOffsets.all { kotlin.math.abs(it) < 1e-5 }
                            val nextUserParams = if (nextOffsets != null && nextOffsets.size >= 6 && !isNextOffsetsZero) {
                                val offX = String.format(Locale.US, "%.3f", nextOffsets[0])
                                val offY = String.format(Locale.US, "%.3f", nextOffsets[1])
                                val offZ = String.format(Locale.US, "%.3f", nextOffsets[2])
                                val offRx = String.format(Locale.US, "%.3f", nextOffsets[3])
                                val offRy = String.format(Locale.US, "%.3f", nextOffsets[4])
                                val offRz = String.format(Locale.US, "%.3f", nextOffsets[5])
                                "3,$offX,$offY,$offZ,$offRx,$offRy,$offRz"
                            } else {
                                "0,0,0,0,0,0,0"
                            }

                            val ext1StrNext = String.format(Locale.US, "%.3f", nextPoint.pose?.ext1 ?: 0.0)
                            if (isExtAxisEnabled) {
                                val extAxisCmd = "ExtAxisMoveJ(1,$ext1StrNext,0.000,0.000,0.000,$finalSpeed,-1)"
                                val extAxisId = globalCommandId++
                                builder.appendCmd(extAxisCmd, extAxisId)
                            }

                            if (isExtAxisEnabled && (hasNonZeroOffsets(offsets) || hasNonZeroOffsets(nextOffsets))) {
                                val bakedMid = bakePoseWithOffsets(pose, offsets)
                                val bakedEnd = bakePoseWithOffsets(nextPose, nextOffsets)
                                appendExtAxisIkMoveC(builder, bakedMid, bakedEnd, toolIndex, finalSpeed, cmdId)
                            } else {
                                val cmd = "MoveC($pos,$toolIndex,0,100,100,0,0,0,0,$userParams,$nextPos,$toolIndex,0,100,100,0,0,0,0,$nextUserParams,$finalSpeed,-1)"
                                builder.appendCmd(cmd, cmdId)
                            }
                            
                            if (index == startPointIndex && oscType != "无摆动" && !hasStartedArc) {
                                val weaveStartCmd = "WeaveStart(3)"
                                val weaveStartId = globalCommandId++
                                builder.appendCmd(weaveStartCmd, weaveStartId)
                            }
                            
                            // Check if next point is END, handle End Logic (ArcEnd, WeaveEnd)
                            if (nextPoint.type == WeldPointType.END) {
                                if (isWelding) {
                                    val arcCmd = "ARCEnd(0,2,10000)"
                                    val arcId = globalCommandId++
                                    builder.appendCmd(arcCmd, arcId)
                                }
                                val oscType = process.oscillation.type
                                if (oscType != "无摆动") {
                                    val weaveEndCmd = "WeaveEnd(0)"
                                    val weaveEndId = globalCommandId++
                                    builder.appendCmd(weaveEndCmd, weaveEndId)
                                }
                            }
                            
                            // Skip next point
                            index += 2
                            continue
                        }
                    }
                    // Fallback if no next point (should not happen in valid arc)
                    if (isExtAxisEnabled && hasNonZeroOffsets(offsets) && joints != null && joints.size >= 6) {
                        appendExtAxisIkMoveL(builder, bakePoseWithOffsets(pose, offsets), toolIndex, finalSpeed, cmdId)
                    } else {
                        val cmd = "MoveL($pos,$toolIndex,0,100,100,$moveSpeed,-1,0,$ext1Str,0.000,0.000,0.000,0,$userParams,100,0)"
                        builder.appendCmd(cmd, cmdId)
                    }
                }
                WeldPointType.END -> {
                    // For Arc End, usually MoveL then ArcEnd
                    if (isExtAxisEnabled && hasNonZeroOffsets(offsets) && joints != null && joints.size >= 6) {
                        appendExtAxisIkMoveL(builder, bakePoseWithOffsets(pose, offsets), toolIndex, finalSpeed, cmdId)
                    } else {
                        val cmd = "MoveL($pos,$toolIndex,0,100,100,$moveSpeed,-1,0,$ext1Str,0.000,0.000,0.000,0,$userParams,100,0)"
                        builder.appendCmd(cmd, cmdId)
                    }
                    
                    if (isWelding) {
                        val arcCmd = "ARCEnd(0,2,10000)"
                        val arcId = globalCommandId++
                        builder.appendCmd(arcCmd, arcId)
                    }
                    val oscType = process.oscillation.type
                    if (oscType != "无摆动") {
                        val weaveEndCmd = "WeaveEnd(0)"
                        val weaveEndId = globalCommandId++
                        builder.appendCmd(weaveEndCmd, weaveEndId)
                    }
                }
                else -> {
                    val cmd = "MoveL($pos,$toolIndex,0,100,100,$moveSpeed,-1,0,$ext1Str,0.000,0.000,0.000,0,0,0,0,0,0,0,0,100,0)"
                    builder.appendCmd(cmd, cmdId)
                }
            }
            index++
        }
    }

    // 计算偏移后的路径
    private fun calculateArcOffset(
        midPoint: WeldPoint,
        endPoint: WeldPoint,
        allPoints: List<WeldPoint>,
        midIndex: Int,
        process: WeldProcess
    ): Pair<Triple<Double, Double, Double>, Triple<Double, Double, Double>> {
        val offXRaw = process.offsetX.toDoubleOrNull() ?: 0.0
        val offX = offXRaw
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

        val v1_circle = pMid - pStart!!
        val v2_circle = pEnd - pMid
        var normal_circle = v1_circle.cross(v2_circle)
        if (normal_circle.length() < 1e-3) return Pair(Triple(0.0,0.0,0.0), Triple(0.0,0.0,0.0))
        normal_circle = normal_circle.normalize()
        val m1_circle = (pStart!! + pMid) * 0.5
        val m2_circle = (pMid + pEnd) * 0.5
        val d1_circle = v1_circle.cross(normal_circle).normalize()
        val d2_circle = v2_circle.cross(normal_circle).normalize()
        val det_circle = d1_circle.cross(d2_circle).dot(normal_circle)
        if (kotlin.math.abs(det_circle) < 1e-3) return Pair(Triple(0.0,0.0,0.0), Triple(0.0,0.0,0.0))
        val t_circle = (m2_circle - m1_circle).cross(d2_circle).dot(normal_circle) / det_circle
        val center = m1_circle + d1_circle * t_circle
        val radius = (pStart!! - center).length()
        val normal = normal_circle

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
                    val t1 = (-b - kotlin.math.sqrt(delta)) / (2 * a)
                    val t2 = (-b + kotlin.math.sqrt(delta)) / (2 * a)
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

    private fun calculateOffsetPath(basePath: WeldPath, pass: WeldPassOffset): WeldPath {
        // Use the sophisticated generatePassPath logic instead of the simple one.
        // The original calculateOffsetPath was a simplified placeholder I added earlier.
        // But `generatePassPath` (which I saw in the file content) already implements the complex 
        // "duocengduodao" logic with reference points (A, B, C) and coordinate transformation.
        
        // We need to find the parent MultiLayerWeldPath to pass to generatePassPath.
        // Since we are inside MultiLayerViewModel, we can find it.
        // `pass` belongs to one of the paths.
        
        val multiPath = multiLayerWeldPaths.find { it.passes.contains(pass) } ?: return basePath
        
        val complexPath = generatePassPath(basePath, pass, multiPath)
        
        return complexPath ?: basePath
    }

    // 生成焊道路径（应用偏移和变换）

    // 获取多层焊接的所有执行路径（交错执行）
    // Return: List<Triple<WeldPath, MultiPathIndex, PassIndex>>
    // PassIndex: -1 for Base, 0..N for Pass
    private fun getAllInterleavedExecutionPaths(): List<Triple<WeldPath, Int, Int>> {
        val counts = multiLayerWeldPaths.map { it.passes.size }
        val steps = MultiLayerRun.interleave(counts) { mIndex, layer ->
            val p = multiLayerWeldPaths[mIndex]
            if (layer == -1) p.basePath.isEnabled else p.passes[layer].isEnabled
        }
        return steps.mapNotNull { step ->
            val p = multiLayerWeldPaths[step.pathIndex]
            if (step.passIndex == -1) {
                Triple(p.basePath, step.pathIndex, -1)
            } else {
                val passPath = generatePassPath(p.basePath, p.passes[step.passIndex], p) ?: return@mapNotNull null
                Triple(passPath, step.pathIndex, step.passIndex)
            }
        }
    }

    // 发送多层批量指令
    fun sendMultiLayerBatchCommands(resumeMultiPathIndex: Int = -1, resumePassIndex: Int = -1, resumePointIndex: Int = -1, isResumeFromStop: Boolean = false) {
        if (multiLayerWeldPaths.isEmpty()) return
        if (!bindPouchProcesses()) return
        
        executionQueue.clear()
        commandIdMap.clear()
        commandLineMap.clear()
        
        // Initial Speed
        val globalSpeedCmd = "SetSpeed(10)"
        
        // Use a single BatchCommandBuilder for the entire multi-layer operation
        val builder = BatchCommandBuilder("Multi-Layer Welding")
        builder.appendCmd(globalSpeedCmd)
        
        val allPaths = getAllInterleavedExecutionPaths()
        
        var resumeFound = (resumeMultiPathIndex == -1) // Change: only check if path index is -1 to determine if it's not a resume. PassIndex can be -1 for base layer.
        
        allPaths.forEach { (path, mIndex, pIndex) ->
            if (!resumeFound) {
                if (mIndex == resumeMultiPathIndex && pIndex == resumePassIndex) {
                    resumeFound = true
                } else {
                    return@forEach
                }
            }

            val toolIndex = try {
                toolCoordinateSystem.removePrefix("工具").toInt()
            } catch (e: Exception) { 1 }
            
            // Send Process Params
            val process = path.process
            val paramCmd = "WeldingSetProcessParam(2,${process.startArcCurrent},${process.startArcVoltage},${process.startArcTime},${process.current},${process.voltage},${process.endArcCurrent},${process.endArcVoltage},${process.endArcTime})"
            val paramId = globalCommandId++
            builder.appendCmd(paramCmd, paramId)
            
            val startIndex = if (mIndex == resumeMultiPathIndex && pIndex == resumePassIndex && resumePointIndex != -1) {
                resumePointIndex
            } else {
                0
            }
            
            val shouldResume = (mIndex == resumeMultiPathIndex && pIndex == resumePassIndex && isResumeFromStop)
            appendCommandsForPath(path, builder, mIndex, pIndex, toolIndex, startIndex, shouldResume)
        }
        
        builder.flush()
        
        isExecutingBatch = true
        isPaused = false
        startBatchExecution()
    }
    
    // 开始模拟
    override fun startSimulation() {
        if (isWelding || isSimulating) return
        if (!bindPouchProcesses()) return
        isSimulating = true
        markPouchWelding(true)
        programHasStarted = false
        fineTune.resetOffsets()
        
        // Reset Tracking
        lastReachedPoint = null
        lastReachedPointIndex = -1
        lastReachedMultiPathIndex = -1
        lastReachedPassIndex = -1
        
        resetWeldCompletionMarks()
        
        // Clear Stop Point records to ensure it starts from the beginning
        stopPointPose = null
        stopPointJoints = null
        stopMultiPathIndex = -1
        stopPassIndex = -1
        stopPointIndex = -1
        
        sendMultiLayerBatchCommands()
    }
    

    // 开始起弧焊接
    override fun startArcWelding() {
        if (isWelding || isSimulating) return
        if (!bindPouchProcesses()) return
        isWelding = true
        markPouchWelding(true)
        isWeldingStatsActive = false 
        programHasStarted = false
        fineTune.resetOffsets()
        
        // Reset Tracking
        lastReachedPoint = null
        lastReachedPointIndex = -1
        lastReachedMultiPathIndex = -1
        lastReachedPassIndex = -1
        
        resetWeldCompletionMarks()
        
        // Clear Stop Point records to ensure it starts from the beginning
        stopPointPose = null
        stopPointJoints = null
        stopMultiPathIndex = -1
        stopPassIndex = -1
        stopPointIndex = -1
        
        weldingTimerJob?.cancel()
        weldingTimerJob = viewModelScope.launch {
            var timerRunning = false
            var lastTick = 0L
            while (isActive && isWelding) {
                delay(50) 
                if (isWeldingStatsActive) {
                    if (!timerRunning) {
                         timerRunning = true
                         lastTick = System.currentTimeMillis()
                    }
                    val now = System.currentTimeMillis()
                    if (now - lastTick >= 1000) {
                        weldingDuration += 1
                        lastTick += 1000
                    }
                } else {
                    timerRunning = false
                }
            }
        }
        
        sendMultiLayerBatchCommands()
    }

    
    // 刷新工程浏览器
    override fun refreshProjectExplorer() {
        projectItems.clear()
        projectItems.addAll(projectManager.listContents(projectCurrentPath, "multi"))
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
        if (projectManager.createFolder(projectCurrentPath, name, "multi")) {
            refreshProjectExplorer()
        }
    }
    
    // 创建新工程
    override fun createProject(name: String) {
        if (projectManager.createProject(projectCurrentPath, name, "multi")) {
            val fullPath = if (projectCurrentPath.isEmpty()) name else "$projectCurrentPath/$name"
            currentProjectName = fullPath
            
            multiLayerWeldPaths.clear()
            addLinearWeldPath() 
            saveCurrentProject()
            refreshProjectExplorer()
        }
    }

    // Helper to get current active path (Multi Base)
    private val currentActiveWeldPath: WeldPath?
        get() {
            return if (selectedMultiLayerPathIndex in multiLayerWeldPaths.indices) {
                multiLayerWeldPaths[selectedMultiLayerPathIndex].basePath
            } else null
        }
    
    // 打开工程
    override fun openProject(path: String) {
        viewModelScope.launch { _toastEvent.emit("请从本机袋打开工程") }
    }

    private fun bag(): BagSession = (getApplication() as ShiJiaoQiApp).bag

    private fun markPouchWelding(on: Boolean) {
        runCatching { bag().setWelding(on) }
    }

    private fun processSource(): PouchProcessSource = PouchProcessSource(bag().pouch)

    private fun String.toUuidOrNull(): UUID? = try {
        UUID.fromString(trim())
    } catch (_: Exception) {
        null
    }

    private fun refsOf(): List<ProcessRef> {
        val out = mutableListOf<ProcessRef>()
        multiLayerWeldPaths.forEach { mp ->
            out.add(ProcessRef("${mp.name} 基准", mp.basePath.processId, mp.basePath.isEnabled))
            mp.passes.forEach { pass ->
                out.add(ProcessRef("${mp.name} ${pass.name}", pass.processId, pass.isEnabled))
            }
        }
        return out
    }

    private fun applyLoaded(loaded: Map<UUID, WeldProcess>) {
        multiLayerWeldPaths.forEachIndexed { i, mp ->
            mp.basePath.processId.toUuidOrNull()?.let { id ->
                loaded[id]?.let { mp.basePath.process = it }
            }
            mp.passes.forEachIndexed { j, pass ->
                pass.processId.toUuidOrNull()?.let { id ->
                    loaded[id]?.let { mp.passes[j] = pass.copy(process = it) }
                }
            }
            multiLayerWeldPaths[i] = mp.copy(basePath = mp.basePath)
        }
    }

    private fun bindPouchProcesses(): Boolean {
        val outcome = ProcessBind.resolve(refsOf(), processSource())
        applyLoaded(outcome.loaded)
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
                MultiLayerProject.parse(bytes)
            } catch (e: Exception) {
                Log.e("MultiLayerViewModel", "active project parse failed", e)
                emptyList()
            }
            multiLayerWeldPaths.clear()
            multiLayerWeldPaths.addAll(loaded)
            if (multiLayerWeldPaths.isEmpty()) addLinearWeldPath()
            selectedMultiLayerPathIndex = 0
            selectedPassIndex = -1
            pouchProjectId = id
            currentProjectName = bag.pouch.exportClosures().firstOrNull { it.assetId == id }?.name
            bindPouchProcesses()
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
        bag.pouch.exportClosures().filter { it.kind == Pouch.KIND_PROJECT }.forEach {
            pouchProjects.add(ProjectChoice(it.assetId, it.name, it.revision, it.assetId == active))
        }
        pouchProcesses.clear()
        pouchProcesses.addAll(PouchProcessSource(bag.pouch).list())
    }

    fun bindProcessFromPouch(processId: UUID?) {
        if (selectedMultiLayerPathIndex !in multiLayerWeldPaths.indices) return
        val multiPath = multiLayerWeldPaths[selectedMultiLayerPathIndex]
        val process = if (processId == null) WeldProcess() else {
            processSource().open(processId) ?: run {
                missingProcessMessage = "闭包里没有这条工艺"
                isMissingProcessDialogVisible = true
                return
            }
        }
        val idStr = processId?.toString().orEmpty()
        if (selectedPassIndex == -1) {
            multiLayerWeldPaths[selectedMultiLayerPathIndex] = multiPath.copy(
                basePath = multiPath.basePath.copy(process = process, processId = idStr)
            )
        } else if (selectedPassIndex in multiPath.passes.indices) {
            val pass = multiPath.passes[selectedPassIndex]
            multiPath.passes[selectedPassIndex] = pass.copy(process = process, processId = idStr)
        }
        saveCurrentProject()
    }

    fun saveCurrentProject() {
        val id = pouchProjectId ?: return
        val bag = runCatching { bag() }.getOrNull() ?: return
        multiLayerWeldPaths.forEach { PouchSave.multi(bag, it) }
        PouchSave.project(bag, id, MultiLayerProject.encode(multiLayerWeldPaths.toList()))
    }

    // 复制当前工程
    fun copyCurrentProject(newName: String) {
        val currentPath = currentProjectName ?: return
        val parentPath = File(currentPath).parent?.replace("\\", "/") ?: ""
        if (projectManager.copyProject(currentPath, parentPath, newName, "multi")) {
            refreshProjectExplorer()
        }
    }
    
    // 删除工程项目
    override fun deleteProjectItem(item: FileSystemItem) {
        if (projectManager.deleteItem(item.path, "multi")) {
            if (currentProjectName == item.path) {
                currentProjectName = null
                multiLayerWeldPaths.clear()
                addLinearWeldPath()
            }
            refreshProjectExplorer()
        }
    }
    
    // --- Process Explorer Methods ---
    // 刷新工艺文件浏览器
    override fun refreshProcessExplorer() {
        processItems.clear()
        processItems.addAll(processManager.listContents(processCurrentPath))
    }
    
    // 导航到工艺文件目录
    override fun navigateProcess(item: FileSystemItem) {
        if (item.isDirectory) {
            processCurrentPath = item.path
            refreshProcessExplorer()
        }
    }
    
    // 返回上一级工艺目录
    override fun navigateProcessBack() {
        if (processCurrentPath.isNotEmpty()) {
            val parent = File(processCurrentPath).parent
            processCurrentPath = parent?.replace("\\", "/") ?: ""
            refreshProcessExplorer()
        }
    }
    
    // 创建工艺文件夹
    override fun createProcessFolder(name: String) {
        if (processManager.createFolder(processCurrentPath, name)) {
            refreshProcessExplorer()
        }
    }

    // 获取工艺文件对象
    fun getProcessFile(relativePath: String): File {
        return processManager.getFile(relativePath)
    }

    // 压缩工艺文件夹
    fun zipProcessFolder(relativePath: String, zipFile: File): Boolean {
        return processManager.zipFileOrFolder(relativePath, zipFile)
    }

    // 解压工艺文件
    fun unzipProcessFile(zipUri: Uri, destPath: String): Boolean {
        val result = processManager.unzip(zipUri, destPath)
        if (result) refreshProcessExplorer()
        return result
    }

    // 导入工艺文件
    fun importProcessFile(uri: Uri, destPath: String, fileName: String): Boolean {
        val result = processManager.importFile(uri, destPath, fileName)
        if (result) refreshProcessExplorer()
        return result
    }

    // 保存工艺
    override fun saveProcess(process: WeldProcess) {
        if (selectedMultiLayerPathIndex in multiLayerWeldPaths.indices) {
            val multiPath = multiLayerWeldPaths[selectedMultiLayerPathIndex]
            if (multiPath.basePath.process.name == process.name) {
                val newBasePath = multiPath.basePath.copy(process = process)
                multiLayerWeldPaths[selectedMultiLayerPathIndex] = multiPath.copy(basePath = newBasePath)
            }
            for (i in multiPath.passes.indices) {
                val pass = multiPath.passes[i]
                if (pass.process.name == process.name) {
                    multiPath.passes[i] = pass.copy(process = process)
                }
            }
            saveCurrentProject()
        }
    }
    
    // 加载工艺
    override fun loadProcess(path: String): WeldProcess? {
        return processManager.loadProcess(path)
    }
    
    // 加载工艺到当前焊道
    fun loadProcessToCurrentWeldPath(item: FileSystemItem) {}
    
    // 删除工艺项
    override fun deleteProcessItem(item: FileSystemItem) {
        if (processManager.deleteItem(item.path)) {
            refreshProcessExplorer()
        }
    }
    
    // 删除当前工程
    fun deleteCurrentProject() {
        val name = currentProjectName ?: return
        if (projectManager.deleteItem(name)) {
            currentProjectName = null
            multiLayerWeldPaths.clear()
            addLinearWeldPath() 
            refreshProjectExplorer()
        }
    }

    // 删除焊接路径
    fun deleteWeldPath(index: Int) {
        if (multiLayerWeldPaths.isNotEmpty() && index >= 0 && index < multiLayerWeldPaths.size) {
            multiLayerWeldPaths.removeAt(index)
            if (selectedMultiLayerPathIndex >= multiLayerWeldPaths.size) {
                selectedMultiLayerPathIndex = multiLayerWeldPaths.lastIndex.coerceAtLeast(0)
            }
            saveCurrentProject()
        }
    }

    // 移动焊接路径
    fun moveWeldPath(fromIndex: Int, toIndex: Int) {
        if (fromIndex in multiLayerWeldPaths.indices && toIndex in multiLayerWeldPaths.indices) {
            val item = multiLayerWeldPaths.removeAt(fromIndex)
            multiLayerWeldPaths.add(toIndex, item)
            selectedMultiLayerPathIndex = toIndex
            saveCurrentProject()
        }
    }

    // 重命名焊接路径
    fun renameWeldPath(newName: String) {
        if (newName.isEmpty()) return
        
        if (multiLayerWeldPaths.isNotEmpty()) {
            multiLayerWeldPaths[selectedMultiLayerPathIndex] = multiLayerWeldPaths[selectedMultiLayerPathIndex].copy(name = newName)
            saveCurrentProject()
        }
    }

    // 切换焊接路径启用状态
    fun toggleWeldPathEnabled(index: Int) {
        if (index >= 0 && index < multiLayerWeldPaths.size) {
            val multiPath = multiLayerWeldPaths[index]
            // Toggle Base Path Enabled
            val newBasePath = multiPath.basePath.copy(isEnabled = !multiPath.basePath.isEnabled)
            multiLayerWeldPaths[index] = multiPath.copy(basePath = newBasePath)
            saveCurrentProject()
        }
    }

    // 添加中间点
    fun addMiddlePoint() {
        val currentWeldPath = currentActiveWeldPath ?: return
        val endIndex = currentWeldPath.points.indexOfFirst { it.type == WeldPointType.END }
        
        if (endIndex > 0) {
            val newMiddlePoint = WeldPoint(
                id = UUID.randomUUID().toString(),
                type = WeldPointType.MIDDLE
            )
            currentWeldPath.points.add(endIndex, newMiddlePoint)
            saveCurrentProject()
        }
    }

    // 添加圆弧中间点
    fun addArcMiddlePoint() {
        val currentWeldPath = currentActiveWeldPath ?: return
        val endIndex = currentWeldPath.points.indexOfFirst { it.type == WeldPointType.END }
        
        if (endIndex > 0) {
            val newArcMiddlePoint = WeldPoint(
                id = UUID.randomUUID().toString(),
                type = WeldPointType.ARC_MIDDLE
            )
            currentWeldPath.points.add(endIndex, newArcMiddlePoint)
            saveCurrentProject()
        }
    }

    // 移动点位
    fun movePoint(weldPathIndex: Int, fromIndex: Int, toIndex: Int) {
        val weldPath = if (weldPathIndex < multiLayerWeldPaths.size) multiLayerWeldPaths[weldPathIndex].basePath else null ?: return
        
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
             val newBasePath = weldPath.copy(selectedPointIndex = newSelectedPointIndex)
             multiLayerWeldPaths[weldPathIndex] = multiLayerWeldPaths[weldPathIndex].copy(basePath = newBasePath)
        }
        saveCurrentProject()
    }

    // 选择点位
    fun selectPoint(weldPathIndex: Int, pointIndex: Int) {
        if (weldPathIndex < multiLayerWeldPaths.size) {
             if (selectedMultiLayerPathIndex != weldPathIndex) {
                 selectedMultiLayerPathIndex = weldPathIndex
             }
             selectedPassIndex = -1 // Reset pass selection
             
             // Auto-clear Ref Point Selection if switching to Point
             selectedRefPointType = RefPointType.NONE
             
             val multiPath = multiLayerWeldPaths[weldPathIndex]
             val basePath = multiPath.basePath
             if (pointIndex < basePath.points.size && basePath.selectedPointIndex != pointIndex) {
                 val newBasePath = basePath.copy(selectedPointIndex = pointIndex)
                 multiLayerWeldPaths[weldPathIndex] = multiPath.copy(basePath = newBasePath)
                 saveCurrentProject()
             }
        }
    }

    // 删除点位
    fun deletePoint(weldPathIndex: Int, pointIndex: Int) {
        val weldPath = if (weldPathIndex < multiLayerWeldPaths.size) multiLayerWeldPaths[weldPathIndex].basePath else null ?: return
        
        val points = weldPath.points
        // Only allow deleting MIDDLE and ARC_MIDDLE points
        if (pointIndex >= 0 && pointIndex < points.size) {
            val point = points[pointIndex]
            if (point.type == WeldPointType.MIDDLE || point.type == WeldPointType.ARC_MIDDLE) {
                points.removeAt(pointIndex)
                
                // Update selection
                if (weldPath.selectedPointIndex >= points.size) {
                    val newBasePath = weldPath.copy(selectedPointIndex = points.lastIndex)
                    multiLayerWeldPaths[weldPathIndex] = multiLayerWeldPaths[weldPathIndex].copy(basePath = newBasePath)
                }
                saveCurrentProject()
            }
        }
    }
    
    // 计算段长度辅助方法
    private fun calculateSegmentLength(points: List<WeldPoint>, startIdx: Int, endIdx: Int): Double {
        if (startIdx < 0 || endIdx < 0 || startIdx >= points.size || endIdx >= points.size) return 0.0
        
        var length = 0.0
        val min = minOf(startIdx, endIdx)
        val max = maxOf(startIdx, endIdx)
        
        for (i in min until max) {
            val p1 = points[i].pose
            val p2 = points[i+1].pose
            if (p1 != null && p2 != null) {
                val dx = p1.x - p2.x
                val dy = p1.y - p2.y
                val dz = p1.z - p2.z
                length += sqrt(dx*dx + dy*dy + dz*dz)
            }
        }
        return length
    }

    // --- Commands ---
    
    // 发送工具坐标指令
    fun sendToolCoordCommand(toolIndex: Int, pose: Pose) {
        val cmd = "SetToolCoord($toolIndex,${pose.x},${pose.y},${pose.z},${pose.rx},${pose.ry},${pose.rz})"
        val msg = "/f/bIII123III205III${cmd.length}III${cmd}III/b/f"
        socketManager.sendControlCommand(msg)
    }

    // 采集数据
    fun collectData() {
        if (selectedPassIndex >= 0) return

        val currentPath = currentActiveWeldPath ?: return
        
        // 1. Handle Ref Point Recording
        if (selectedRefPointType != RefPointType.NONE) {
            recordRefPoint(selectedRefPointType)
            
            // Auto-advance logic for RefPoints
            val nextRefType = when (selectedRefPointType) {
                RefPointType.START_X -> RefPointType.START_Z
                RefPointType.START_Z -> RefPointType.NONE
                RefPointType.MIDDLE_X -> RefPointType.MIDDLE_Z
                RefPointType.MIDDLE_Z -> RefPointType.NONE
                RefPointType.END_X -> RefPointType.END_Z
                RefPointType.END_Z -> RefPointType.NONE
                else -> RefPointType.NONE
            }

            if (nextRefType != RefPointType.NONE) {
                selectedRefPointType = nextRefType
            } else {
                // Ref Point sequence finished for this WeldPoint, advance to next WeldPoint
                selectedRefPointType = RefPointType.NONE
                if (currentPath.selectedPointIndex < currentPath.points.size - 1) {
                    val newBasePath = currentPath.copy(selectedPointIndex = currentPath.selectedPointIndex + 1)
                    updateCurrentActiveWeldPath(newBasePath)
                } else {
                    saveCurrentProject()
                }
            }
            
            viewModelScope.launch {
                _toastEvent.emit("参考点已记录")
            }
            return
        }
        
        // 2. Handle Weld Point Recording
        val point = currentPath.points.getOrNull(currentPath.selectedPointIndex) ?: return
        val snap = Capture.snapshot(operationPosition, socketManager.robotJoints.value)
        val newPoint = if (snap != null) {
            point.copy(pose = snap.first, jointAngles = snap.second)
        } else {
            point.copy(pose = operationPosition, jointAngles = socketManager.robotJoints.value)
        }
        currentPath.points[currentPath.selectedPointIndex] = newPoint
        
        // Auto-advance logic for WeldPoints (Check if we need to enter RefPoint sequence)
        val nextRefTypeForCurrentPoint = when (point.type) {
            WeldPointType.START -> RefPointType.START_X
            WeldPointType.ARC_MIDDLE -> RefPointType.MIDDLE_X
            WeldPointType.END -> RefPointType.END_X
            else -> RefPointType.NONE
        }

        if (nextRefTypeForCurrentPoint != RefPointType.NONE) {
            // Stay on current point index, but enter RefPoint selection
            selectedRefPointType = nextRefTypeForCurrentPoint
            // We need to update the path with the recorded point, but NOT advance index
            updateCurrentActiveWeldPath(currentPath) 
        } else {
            // Standard WeldPoint advance
            if (currentPath.selectedPointIndex < currentPath.points.size - 1) {
                val newBasePath = currentPath.copy(selectedPointIndex = currentPath.selectedPointIndex + 1)
                updateCurrentActiveWeldPath(newBasePath)
            } else {
                updateCurrentActiveWeldPath(currentPath) // Just save the last point
                saveCurrentProject()
            }
        }
        
        viewModelScope.launch {
            _toastEvent.emit("点位已记录: ${point.type}")
        }
    }
    
    // 更新当前激活焊接路径辅助方法
    private fun updateCurrentActiveWeldPath(newBasePath: WeldPath) {
        if (selectedMultiLayerPathIndex in multiLayerWeldPaths.indices) {
            val currentMultiPath = multiLayerWeldPaths[selectedMultiLayerPathIndex]
            multiLayerWeldPaths[selectedMultiLayerPathIndex] = currentMultiPath.copy(basePath = newBasePath)
            saveCurrentProject()
        }
    }

    // Helper property to store current download file name
    private var currentDownloadFileName: String? = null
    private var pollingJob: kotlinx.coroutines.Job? = null

    // 开始下载更新
    override fun startUpdateDownload() {
        val info = updateInfo ?: return
        isUpdateDialogVisible = false
        isDownloading = true
        
        viewModelScope.launch {
            _toastEvent.emit("开始下载更新...")
        }
        
        // We still provide a name hint, but we will query the actual name later
        val fileName = "ShiJiaoQi_v${info.versionCode}.apk"
        currentDownloadFileName = fileName
        downloadId = updateManager.downloadApk(info.downloadUrl, fileName)
        
        // Start polling for download status
        startDownloadPolling(downloadId)
    }

    // 开始下载轮询
    private fun startDownloadPolling(id: Long) {
        pollingJob?.cancel()
        pollingJob = viewModelScope.launch {
            while (isActive && isDownloading) {
                delay(2000) // Check every 2 seconds
                val uri = updateManager.getDownloadedUri(id)
                if (uri != null) {
                    isDownloading = false
                    _toastEvent.emit("下载完成，正在准备安装...")
                    updateManager.installApk(uri)
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

    // 检查下载状态
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
            e.printStackTrace()
        } finally {
            cursor.close()
        }
        return status
    }

    private val downloadReceiver = object : BroadcastReceiver() {
        override fun onReceive(context: Context?, intent: Intent?) {
            val id = intent?.getLongExtra(DownloadManager.EXTRA_DOWNLOAD_ID, -1)
            if (id == downloadId) {
                if (!isDownloading) return 
                isDownloading = false
                pollingJob?.cancel()
                
                viewModelScope.launch {
                     _toastEvent.emit("下载完成，正在准备安装...")
                }
                
                val uri = updateManager.getDownloadedUri(downloadId)
                if (uri != null) {
                    updateManager.installApk(uri)
                } else {
                    viewModelScope.launch {
                        _toastEvent.emit("安装失败：无法找到下载文件")
                    }
                }
            }
        }
    }

    // 注册下载接收器
    private fun registerDownloadReceiver() {
        val filter = IntentFilter(DownloadManager.ACTION_DOWNLOAD_COMPLETE)
        if (android.os.Build.VERSION.SDK_INT >= android.os.Build.VERSION_CODES.TIRAMISU) {
            getApplication<Application>().registerReceiver(downloadReceiver, filter, Context.RECEIVER_EXPORTED)
        } else {
            getApplication<Application>().registerReceiver(downloadReceiver, filter)
        }
    }

    // 检查注册状态
    override fun checkRegistration() {
        val command = "GetControlBoxNetMacAddr(0)"
        val len = command.length
        val msg = "/f/bIII123III826III${len}III${command}III/b/f"
        Log.d("Registration", "Fetching Machine Code: $msg")
        socketManager.sendControlCommand(msg)
    }

    // 注册设备
    override fun register(code: String): Boolean {
        val expectedCode = encryptMac("P${machineCode}L")
        if (code == expectedCode) {
            projectManager.saveLicense(code)
            isRegistered = true
            lastConnectionTime = System.currentTimeMillis()
            saveAppSettings()
            viewModelScope.launch {
                _toastEvent.emit("注册成功")
            }
            return true
        } else {
            viewModelScope.launch {
                _toastEvent.emit("注册码无效")
            }
            return false
        }
    }
    
    // --- Ref Point Teaching ---
    // (Removed duplicate selectRefPointType)

    // --- Data Collection / Teaching ---
    // 清除点位数据
    fun clearPointData() {
        if (selectedRefPointType != RefPointType.NONE) {
            deleteRefPoint(selectedRefPointType)
            viewModelScope.launch {
                _toastEvent.emit("参考点数据已清除")
            }
            return
        }

        if (selectedMultiLayerPathIndex !in multiLayerWeldPaths.indices) return
        
        val multiPath = multiLayerWeldPaths[selectedMultiLayerPathIndex]
        val basePath = multiPath.basePath
        
        // Only allow for Base Path points
        if (selectedPassIndex != -1) return
        
        if (basePath.selectedPointIndex in basePath.points.indices) {
            val point = basePath.points[basePath.selectedPointIndex]
            val newPoint = point.copy(pose = null, jointAngles = null)
            
            val newBasePath = basePath.copy()
            newBasePath.points[basePath.selectedPointIndex] = newPoint
            
            multiLayerWeldPaths[selectedMultiLayerPathIndex] = multiPath.copy(basePath = newBasePath)
            saveCurrentProject()
            
            viewModelScope.launch {
                _toastEvent.emit("点位数据已清除")
            }
        }
    }

    // --- Current/Voltage Control ---
    // 设置焊接电流电压
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

        // Format values (remove decimal if integer)
        val currentStr = if (current % 1.0 == 0.0) current.toInt().toString() else current.toString()
        val voltageStr = if (voltage % 1.0 == 0.0) voltage.toInt().toString() else voltage.toString()

        // Send Current
        val cmdCurrent = "WeldingSetCurrent(0,$currentStr,0)"
        val lenCurrent = cmdCurrent.length
        val msgCurrent = "/f/bIII${globalCommandId++}III201III${lenCurrent}III${cmdCurrent}III/b/f"
        socketManager.sendControlCommand(msgCurrent)

        // Send Voltage
        val cmdVoltage = "WeldingSetVoltage(0,$voltageStr,1)"
        val lenVoltage = cmdVoltage.length
        val msgVoltage = "/f/bIII${globalCommandId++}III201III${lenVoltage}III${cmdVoltage}III/b/f"
        socketManager.sendControlCommand(msgVoltage)

        isCurrentVoltageDialogVisible = false
        
        viewModelScope.launch {
            _toastEvent.emit("电流电压设置指令已发送")
        }
    }

    // --- Interface Implementation (Missing Methods) ---

    // 发送手动指令
    override fun sendManualCommand(type: Int, command: String) {
        val len = command.length
        // Using generic type 200 for manual commands if not specified, but usually ID is passed in msg
        // The interface says type: Int. 
        // Protocol format: /f/bIII{ID}III{TYPE}III{LEN}III{CMD}III/b/f
        val msg = "/f/bIII${globalCommandId++}III${type}III${len}III${command}III/b/f"
        socketManager.sendControlCommand(msg)
    }

    // 清除统计数据
    override fun clearStats() {
        weldingLength = 0.0
        weldingDuration = 0L
        saveAppSettings()
    }

    // 重置所有错误
    override fun resetAllError() {
        val cmd = "ResetAllError()"
        val msg = "/f/bIII${globalCommandId++}III107III${cmd.length}III${cmd}III/b/f"
        socketManager.sendControlCommand(msg)
    }

    // 重连
    override fun reconnect() {
        viewModelScope.launch {
            withContext(Dispatchers.IO) {
                try {
                    socketManager.restart()
                } catch (e: Exception) {
                    Log.e("MultiLayerViewModel", "Reconnection failed", e)
                }
            }
            _toastEvent.emit("正在尝试重新连接...")
        }
    }

    // 检查更新
    override fun checkForUpdate() {
        viewModelScope.launch {
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

    // 加密MAC地址
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

    // 创建工艺
    override fun createProcess(name: String, process: WeldProcess) {
        val cleanName = if (name.endsWith(".json")) name.substringBeforeLast(".json") else name
        val bag = runCatching { bag() }.getOrNull() ?: return
        val body = ProcessJson.encode(process.copy(name = cleanName))
        try {
            bag.issuePersonal(Pouch.KIND_PROCESS, cleanName, body)
            refreshPouchLists()
        } catch (_: Exception) {
        } finally {
            Wm2.zero(body)
        }
    }

    // 更新工艺
    override fun updateProcess(item: FileSystemItem, process: WeldProcess) {
        saveProcess(process)
    }

    // 导入工艺
    override fun importProcess(uri: Uri) {
        val result = processManager.importFile(uri, processCurrentPath, "imported_process.json") // Name might be an issue
        if (result) refreshProcessExplorer()
    }

    // 导入工艺压缩包
    override fun importProcessZip(uri: Uri) {
        if (processManager.unzip(uri, processCurrentPath)) {
            refreshProcessExplorer()
        }
    }

    // 导出工艺
    // Update Standard Process Library
    override fun updateStandardProcessLibrary(url: String) {
        viewModelScope.launch {
            _toastEvent.emit("本机不保存标准工艺库文件")
        }
    }

    override fun exportProcess(item: FileSystemItem): File? = null

    override fun exportProcessZip(item: FileSystemItem): File? = null

    // 选择工艺
    override fun selectProcess(item: FileSystemItem) {
        loadProcessToCurrentWeldPath(item)
    }

    // 保存应用设置
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

    // 开启送丝
    fun startWireFeed(isForward: Boolean = true) {
        val type = if (isForward) 202 else 203
        sendManualCommand(type, "1")
    }

    // 停止送丝
    fun stopWireFeed() {
        sendManualCommand(204, "0")
    }

    // 拖拽示教开关
    fun dragTeachSwitch(enabled: Boolean) {
        val type = 205
        val cmd = if (enabled) "1" else "0"
        sendManualCommand(type, cmd)
    }

    // 重启应用
    fun restart() {
        val intent = getApplication<Application>().packageManager
            .getLaunchIntentForPackage(getApplication<Application>().packageName)
        intent?.addFlags(Intent.FLAG_ACTIVITY_CLEAR_TOP)
        intent?.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        getApplication<Application>().startActivity(intent)
        android.os.Process.killProcess(android.os.Process.myPid())
    }

    override fun showToast(message: String) {
        viewModelScope.launch {
            _toastEvent.emit(message)
        }
    }

    override fun onCleared() {
        super.onCleared()
        socketManager.stop()
    }
}
