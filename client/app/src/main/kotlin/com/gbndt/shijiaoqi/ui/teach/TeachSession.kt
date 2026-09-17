package com.gbndt.shijiaoqi.ui.teach

import android.content.Context
import android.speech.tts.TextToSpeech
import android.widget.Toast
import com.gbndt.shijiaoqi.data.prefs.DeviceSettingsStore
import com.gbndt.shijiaoqi.data.repository.RobotRepository
import com.gbndt.shijiaoqi.data.repository.SessionRepository
import com.gbndt.shijiaoqi.data.robot.protocol.FrPacket
import com.gbndt.shijiaoqi.data.robot.protocol.RobotCommands
import com.gbndt.shijiaoqi.data.robot.protocol.RobotLink
import com.gbndt.shijiaoqi.model.Pose
import com.gbndt.shijiaoqi.data.log.PadLog
import dagger.hilt.android.qualifiers.ApplicationContext
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import java.util.Locale
import javax.inject.Inject
import javax.inject.Singleton

/** 示教：点动、手柄、工具、方位/速度/安装位、外轴、报警语音。不管焊道与开焊。 */
@Singleton
class TeachSession @Inject constructor(
    @param:ApplicationContext private val app: Context,
    private val robot: RobotRepository,
    private val session: SessionRepository,
    private val settings: DeviceSettingsStore,
) {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private var commandId = 1
    private var lastZero = false
    private var wireOn = false
    private var dragOn = false
    private var tts: TextToSpeech? = null
    private var alarmVoiceJob: Job? = null

    private val _uiState = MutableStateFlow(TeachUiState())
    val uiState: StateFlow<TeachUiState> = _uiState.asStateFlow()

    val connectionStatus: String get() = uiState.value.connectionStatus
    val alarmStatus: String get() = uiState.value.alarmStatus
    val pose: Pose get() = uiState.value.pose
    val extAxisPos: Double get() = uiState.value.extAxisPos
    val extAxisReady: Boolean get() = uiState.value.extAxisReady
    val machineCode: String get() = uiState.value.machineCode
    val isControllerActive: Boolean get() = uiState.value.isControllerActive
    val toolCoordinateSystem: String get() = uiState.value.toolCoordinateSystem
    val toolCoordinates: List<Pose?> get() = uiState.value.toolCoordinates
    val toolRemarks: List<String> get() = uiState.value.toolRemarks
    val speedMode: String get() = uiState.value.speedMode
    val positionMode: String get() = uiState.value.positionMode
    val installPos: Int get() = uiState.value.installPos
    val isExtAxisEnabled: Boolean get() = uiState.value.isExtAxisEnabled
    var isToolListDialogVisible: Boolean
        get() = uiState.value.isToolListDialogVisible
        set(v) { _uiState.update { it.copy(isToolListDialogVisible = v) } }
    var isToolEditDialogVisible: Boolean
        get() = uiState.value.isToolEditDialogVisible
        set(v) { _uiState.update { it.copy(isToolEditDialogVisible = v) } }
    var isPositionDialogVisible: Boolean
        get() = uiState.value.isPositionDialogVisible
        set(v) { _uiState.update { it.copy(isPositionDialogVisible = v) } }
    var isSpeedDialogVisible: Boolean
        get() = uiState.value.isSpeedDialogVisible
        set(v) { _uiState.update { it.copy(isSpeedDialogVisible = v) } }
    var isInstallPosDialogVisible: Boolean
        get() = uiState.value.isInstallPosDialogVisible
        set(v) { _uiState.update { it.copy(isInstallPosDialogVisible = v) } }
    var editingToolIndex: Int
        get() = uiState.value.editingToolIndex
        private set(_) {}
    var editingToolPose: Pose
        get() = uiState.value.editingToolPose
        set(v) { _uiState.update { it.copy(editingToolPose = v) } }
    var editingToolRemark: String
        get() = uiState.value.editingToolRemark
        set(v) { _uiState.update { it.copy(editingToolRemark = v) } }

    var joyX1 = 0f
    var joyY1 = 0f
    var joyX2 = 0f
    var joyY2 = 0f
    var btnUp = false
    var btnDown = false
    var btnLeft = false
    var btnRight = false
    var btnL1 = false
    var btnL2 = false
    var btnR2 = false

    /** 下发给控制器的工具号，1–14。 */
    val toolIndex: Int
        get() = toolCoordinateSystem.removePrefix("工具").toIntOrNull()?.coerceIn(1, 14) ?: 1

    init {
        robot.start()
        tts = TextToSpeech(app) { status ->
            if (status == TextToSpeech.SUCCESS) tts?.setLanguage(Locale.CHINA)
        }
        val snap = settings.loadAppSettings()
        val coords = MutableList<Pose?>(14) { snap.toolCoordinates.getOrNull(it) }
        val remarks = MutableList(14) { snap.toolRemarks.getOrNull(it) ?: "" }
        val toolName = if (snap.selectedToolIndex in 0 until 14) "工具${snap.selectedToolIndex + 1}" else "工具1"
        _uiState.value = TeachUiState(
            isExtAxisEnabled = snap.isExtAxisEnabled,
            toolCoordinates = coords,
            toolRemarks = remarks,
            toolCoordinateSystem = toolName,
            positionMode = snap.positionMode,
            speedMode = snap.speedMode,
            installPos = snap.installPos,
        )
        scope.launch {
            robot.connectionStatus.collect { status ->
                val was = uiState.value.connectionStatus
                _uiState.update { it.copy(connectionStatus = status) }
                if (was != RobotLink.UP && status == RobotLink.UP) {
                    delay(500)
                    sendCurrentTool()
                    delay(200)
                    sendInstallPos()
                    checkArmOnConnect()
                }
            }
        }
        scope.launch {
            session.state.collect { st ->
                if (st.loggedIn) checkArmOnConnect()
            }
        }
        scope.launch { robot.robotPose.collect { p -> if (p != null) _uiState.update { it.copy(pose = p) } } }
        scope.launch {
            robot.alarmStatus.collect { status ->
                val prev = uiState.value.alarmStatus
                if (prev != status) {
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
                _uiState.update { it.copy(alarmStatus = status) }
            }
        }
        scope.launch { robot.extAxisPos.collect { pos -> _uiState.update { it.copy(extAxisPos = pos) } } }
        scope.launch { robot.extAxisReady.collect { ready -> _uiState.update { it.copy(extAxisReady = ready) } } }
        scope.launch {
            robot.receivedText.collect { text ->
                val mac = Regex("/f/bIII\\d+III826III\\d+III(.+?)III/b/f").find(text)?.groupValues?.get(1)
                if (mac != null) {
                    _uiState.update { it.copy(machineCode = mac) }
                    session.setDeviceSerial(mac)
                    checkArmOnConnect()
                }
            }
        }
        scope.launch {
            robot.robotInputSignal.collect { combined ->
                when {
                    combined in 2690..2890 && !wireOn -> {
                        startWireFeed()
                        wireOn = true
                    }
                    combined > 4096 && wireOn -> {
                        stopWireFeed()
                        wireOn = false
                    }
                    combined in 754..954 && !dragOn -> {
                        dragTeach(true)
                        dragOn = true
                    }
                    combined > 4096 && dragOn -> {
                        dragTeach(false)
                        dragOn = false
                    }
                }
            }
        }
        scope.launch {
            while (isActive) {
                if (isControllerActive) sendServoCart()
                delay(200)
            }
        }
    }

    /** 手柄点动开关。 */
    fun toggleController() {
        _uiState.update { it.copy(isControllerActive = !it.isControllerActive) }
    }

    /** 开焊前关掉点动。 */
    fun stopController() {
        _uiState.update { it.copy(isControllerActive = false) }
    }

    fun openToolList() = _uiState.update { it.copy(isToolListDialogVisible = true) }
    fun dismissToolList() = _uiState.update { it.copy(isToolListDialogVisible = false) }
    fun openPositionDialog() = _uiState.update { it.copy(isPositionDialogVisible = true) }
    fun dismissPositionDialog() = _uiState.update { it.copy(isPositionDialogVisible = false) }
    fun openSpeedDialog() = _uiState.update { it.copy(isSpeedDialogVisible = true) }
    fun dismissSpeedDialog() = _uiState.update { it.copy(isSpeedDialogVisible = false) }
    fun openInstallDialog() = _uiState.update { it.copy(isInstallPosDialogVisible = true) }
    fun dismissInstallDialog() = _uiState.update { it.copy(isInstallPosDialogVisible = false) }

    fun setPosition(mode: String) {
        _uiState.update { it.copy(positionMode = mode, isPositionDialogVisible = false) }
        persistCell()
    }

    fun setSpeed(mode: String) {
        _uiState.update { it.copy(speedMode = mode, isSpeedDialogVisible = false) }
        persistCell()
    }

    fun updateInstallPos(pos: Int) {
        _uiState.update { it.copy(installPos = pos, isInstallPosDialogVisible = false) }
        persistCell()
        sendInstallPos()
    }

    fun toggleExtAxisEnabled() {
        PadLog.info("Teach", "ext axis enabled=${!isExtAxisEnabled}")
        _uiState.update { it.copy(isExtAxisEnabled = !it.isExtAxisEnabled) }
        persistCell()
    }

    fun enableExtAxisServo() {
        PadLog.info("Teach", "ext axis servo on")
        sendManualCommand(RobotCommands.TYPE_EXT_SERVO, "ExtAxisServoOn(1,1)")
    }

    fun startExtAxisJog(direction: Int) {
        if (!isControllerActive) return
        sendManualCommand(RobotCommands.TYPE_EXT_JOG, "ExtAxisStartJog(6,1,$direction,100,100,2000)")
    }

    fun stopExtAxisJog() {
        if (!isControllerActive) return
        sendManualCommand(RobotCommands.TYPE_EXT_JOG_STOP, "StopExtAxisJog")
    }

    fun selectTool(index: Int) {
        if (index !in 0 until 14) return
        val pose = toolCoordinates[index]
        if (pose == null) {
            openToolEdit(index)
            return
        }
        _uiState.update { it.copy(toolCoordinateSystem = "工具${index + 1}", isToolListDialogVisible = false) }
        PadLog.info("Teach", "select tool=${index + 1}")
        persistCell()
        sendTool(index + 1, pose)
    }

    fun openToolEdit(index: Int) {
        if (index !in 0 until 14) return
        _uiState.update {
            it.copy(
                editingToolIndex = index,
                editingToolPose = it.toolCoordinates[index] ?: Pose(0.0, 0.0, 0.0, 0.0, 0.0, 0.0),
                editingToolRemark = it.toolRemarks[index],
                isToolEditDialogVisible = true,
                isToolListDialogVisible = false,
            )
        }
    }

    fun saveToolEdit(pose: Pose, remark: String) {
        val index = editingToolIndex
        if (index !in 0 until 14) return
        _uiState.update {
            val coords = it.toolCoordinates.toMutableList()
            val remarks = it.toolRemarks.toMutableList()
            coords[index] = pose
            remarks[index] = remark
            it.copy(
                toolCoordinates = coords,
                toolRemarks = remarks,
                isToolEditDialogVisible = false,
                toolCoordinateSystem = "工具${index + 1}",
            )
        }
        persistCell()
        sendTool(index + 1, pose)
    }

    fun cancelToolEdit() {
        _uiState.update { it.copy(isToolEditDialogVisible = false, isToolListDialogVisible = true) }
    }

    fun sendManualCommand(type: Int, command: String) {
        robot.sendControlCommand(FrPacket.encode(commandId++, type, command))
    }

    fun resetAllError() {
        PadLog.info("Teach", "reset errors")
        robot.sendControlCommand(FrPacket.encode(7, 107, "ResetAllError()"))
    }

    fun reconnect() {
        PadLog.info("Teach", "reconnect")
        robot.restart()
    }

    /** 套接字起来且读到号时核本厂名录；过了保持连接，不过立刻断。 */
    private fun checkArmOnConnect() {
        if (!session.state.value.loggedIn) return
        if (uiState.value.connectionStatus != RobotLink.UP) return
        val serial = uiState.value.machineCode.trim()
        if (serial.isEmpty()) return
        scope.launch {
            try {
                session.matchArm(serial)
            } catch (e: Exception) {
                PadLog.warn("Teach", "arm check failed ${e.message}")
                dropLink()
                val msg = session.state.value.error ?: "机械臂校验失败"
                PadLog.toast(msg)
                Toast.makeText(app, msg, Toast.LENGTH_SHORT).show()
            }
        }
    }

    fun dropLink() {
        PadLog.warn("Teach", "drop link")
        robot.dropLink()
        _uiState.update { it.copy(connectionStatus = RobotLink.DOWN) }
    }

    fun startWireFeed() = sendManualCommand(268, "SetForwardWireFeed(0,1)")

    fun stopWireFeed() = sendManualCommand(268, "SetForwardWireFeed(0,0)")

    fun reverseWireFeed(on: Boolean) {
        sendManualCommand(269, if (on) "SetReverseWireFeed(0,1)" else "SetReverseWireFeed(0,0)")
    }

    private fun dragTeach(enable: Boolean) {
        sendManualCommand(333, "DragTeachSwitch(${if (enable) "1" else "0"})")
    }

    private fun sendServoCart() {
        val joyFwd = when {
            joyY2 < -0.5f -> 1
            joyY2 > 0.5f -> -1
            else -> 0
        }
        val joyLeft = when {
            joyX2 < -0.5f -> 1
            joyX2 > 0.5f -> -1
            else -> 0
        }
        val z = when {
            btnUp -> 1
            btnDown -> -1
            else -> 0
        }
        val rotFwd = when {
            joyY1 < -0.5f -> 1
            joyY1 > 0.5f -> -1
            else -> 0
        }
        val rotLeft = when {
            joyX1 < -0.5f -> 1
            joyX1 > 0.5f -> -1
            else -> 0
        }
        val rz = when {
            btnLeft -> 1
            btnRight -> -1
            else -> 0
        }
        val speedS = if (btnL1) 10.0 else 1.0
        val speedR = if (btnL1) 1.0 else 0.2
        val ext1 = when {
            btnR2 -> -5
            btnL2 -> 5
            else -> 0
        }
        val zero = joyFwd == 0 && joyLeft == 0 && z == 0 && rotFwd == 0 && rotLeft == 0 && rz == 0 && ext1 == 0
        if (zero) {
            if (lastZero) return
            lastZero = true
        } else {
            lastZero = false
        }
        robot.sendControlCommand(
            RobotCommands.servoCart(positionMode, joyFwd, joyLeft, z, rotFwd, rotLeft, rz, speedS, speedR, ext1),
        )
    }

    private fun sendCurrentTool() {
        val pose = toolCoordinates.getOrNull(toolIndex - 1) ?: return
        sendTool(toolIndex, pose)
    }

    private fun sendTool(index: Int, pose: Pose) {
        val cmd = "SetToolCoord($index,${pose.x},${pose.y},${pose.z},${pose.rx},${pose.ry},${pose.rz},0,0,0,0)"
        robot.sendControlCommand(FrPacket.encode(21, 316, cmd))
    }

    private fun sendInstallPos() {
        val cmd = "SetRobotInstallPos($installPos)"
        robot.sendControlCommand(FrPacket.encode(23, 337, cmd))
    }

    private fun persistCell() {
        val cur = settings.loadAppSettings()
        val s = uiState.value
        val index = (toolIndex - 1).coerceIn(0, 13)
        settings.saveAppSettings(
            cur.copy(
                selectedToolIndex = index,
                toolCoordinates = s.toolCoordinates,
                toolRemarks = s.toolRemarks,
                positionMode = s.positionMode,
                speedMode = s.speedMode,
                installPos = s.installPos,
                isExtAxisEnabled = s.isExtAxisEnabled,
            ),
        )
    }
}
