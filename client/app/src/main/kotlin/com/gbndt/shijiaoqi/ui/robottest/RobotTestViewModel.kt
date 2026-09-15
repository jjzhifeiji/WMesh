package com.gbndt.shijiaoqi.ui.robottest

import android.app.Application
import android.util.Log
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import com.gbndt.shijiaoqi.data.legacy.ProjectManager
import com.gbndt.shijiaoqi.data.robot.link.SocketManager
import com.gbndt.shijiaoqi.model.CapturedPoint
import com.gbndt.shijiaoqi.model.Oscillation
import com.gbndt.shijiaoqi.model.Pose
import com.gbndt.shijiaoqi.model.RobotTestSettings
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.util.Locale

class RobotTestViewModel(application: Application) : AndroidViewModel(application) {
    private val socketManager = SocketManager
    private val projectManager = ProjectManager(application)
    private var commandId = 1000

    var connectionStatus by mutableStateOf("未连接")
        private set
    var alarmStatus by mutableStateOf("无报警")
        private set
    var livePose by mutableStateOf(Pose(0.0, 0.0, 0.0, 0.0, 0.0, 0.0))
        private set
    var liveJoints by mutableStateOf(listOf(0.0, 0.0, 0.0, 0.0, 0.0, 0.0))
        private set

    var startPoint by mutableStateOf<CapturedPoint?>(null)
        private set
    var endPoint by mutableStateOf<CapturedPoint?>(null)
        private set

    var oscillation by mutableStateOf(Oscillation())
    var showOscillationDialog by mutableStateOf(false)

    var speedPercent by mutableStateOf(10)
        private set
    var offsetStepMm by mutableStateOf(1.0)
    var weaveOffsetX by mutableStateOf(0.0)
        private set
    var weaveOffsetY by mutableStateOf(0.0)
        private set
    var weaveOffsetZ by mutableStateOf(0.0)
        private set

    private val _toastEvent = MutableSharedFlow<String>()
    val toastEvent = _toastEvent.asSharedFlow()

    init {
        val saved = projectManager.loadRobotTestSettings()
        startPoint = saved.startPoint
        endPoint = saved.endPoint
        oscillation = saved.oscillation

        socketManager.start()
        viewModelScope.launch {
            socketManager.connectionStatus.collect { connectionStatus = it }
        }
        viewModelScope.launch {
            socketManager.alarmStatus.collect { alarmStatus = it }
        }
        viewModelScope.launch {
            socketManager.robotPose.collect { pose ->
                if (pose != null) livePose = pose
            }
        }
        viewModelScope.launch {
            socketManager.robotJoints.collect { joints ->
                if (joints.size >= 6) liveJoints = joints.take(6)
            }
        }
        viewModelScope.launch {
            socketManager.robotErrorEvent.collect { errCode ->
                if (errCode != 0 && errCode != 18) {
                    toast("机械臂错误码: $errCode")
                }
            }
        }
    }

    fun reconnect() {
        viewModelScope.launch {
            toast("正在尝试重新连接...")
            withContext(Dispatchers.IO) {
                try {
                    socketManager.restart()
                } catch (e: Exception) {
                    Log.e("RobotTest", "Reconnection failed", e)
                }
            }
        }
    }

    fun captureStart() {
        val point = snapshotCurrentPoint() ?: return
        startPoint = point
        persist()
        toast("已采集起点")
    }

    fun captureEnd() {
        val point = snapshotCurrentPoint() ?: return
        endPoint = point
        persist()
        toast("已采集终点")
    }

    fun startRun() {
        val start = startPoint
        val end = endPoint
        if (start == null || end == null) {
            toast("请先采集起点和终点")
            return
        }
        if (!isConnected()) {
            toast("设备未连接")
            return
        }

        weaveOffsetX = 0.0
        weaveOffsetY = 0.0
        weaveOffsetZ = 0.0

        val lines = mutableListOf<String>()
        lines.add("SetSpeed($speedPercent)")
        val weave = buildWeaveSetPara()
        if (weave != null) {
            lines.add(weave)
        }
        lines.add(buildMoveL(start))
        if (weave != null) {
            lines.add("WeaveStart(3)")
        }
        lines.add(buildMoveL(end, moveSpeed = 10))
        if (weave != null) {
            lines.add("WeaveEnd(0)")
        }
        val lua = lines.joinToString("\r\n") + "\r\n"

        viewModelScope.launch {
            val luaName = "/fruser/test.lua"
            val id105 = nextId()
            val msg105 = "/f/bIII${id105}III105III${luaName.length}III${luaName}III/b/f"
            socketManager.sendBatchCommandSync(msg105)
            delay(50)

            val id106 = nextId()
            val msg106 = "/f/bIII${id106}III106III${lua.length}III${lua}III/b/f"
            socketManager.sendBatchCommandSync(msg106)
            delay(500)

            socketManager.sendControlCommand("/f/bIII20III303III7IIIMode(0)III/b/f")
            delay(100)
            socketManager.sendControlCommand("/f/bIII77III101III5IIIStartIII/b/f")
            toast("已通过 8082 下发并启动")
        }
    }

    fun stopRun() {
        socketManager.sendControlCommand("/f/bIII7III102III4IIISTOPIII/b/f")
        sendLuaOnce("Mode(1)", type = 303)
        toast("已停止")
    }

    fun updateOscillation(osc: Oscillation) {
        oscillation = osc
        persist()
    }

    fun sendOscillationParams() {
        val weave = buildWeaveSetPara()
        if (weave == null) {
            toast("当前为无摆动，未发送")
            return
        }
        sendLuaOnce(weave)
        toast("已发送摆动参数")
    }

    fun increaseAmplitude() = adjustOnlineWeave(amplitudeDelta = 1.0)

    fun decreaseAmplitude() = adjustOnlineWeave(amplitudeDelta = -1.0)

    fun increaseFrequency() = adjustOnlineWeave(frequencyDelta = 1.0)

    fun decreaseFrequency() = adjustOnlineWeave(frequencyDelta = -1.0)

    private fun adjustOnlineWeave(amplitudeDelta: Double = 0.0, frequencyDelta: Double = 0.0) {
        val osc = oscillation
        val newAmp = (osc.amplitude + amplitudeDelta).coerceAtLeast(1.0)
        val newFreq = (osc.frequency + frequencyDelta).coerceAtLeast(1.0)
        if (newAmp == osc.amplitude && newFreq == osc.frequency) {
            toast("已到最小值")
            return
        }
        oscillation = osc.copy(amplitude = newAmp, frequency = newFreq)
        persist()
        sendWeaveOnlineSetPara()
        toast(
            String.format(
                Locale.US,
                "摆幅 %.0f mm  频率 %.0f Hz",
                oscillation.amplitude,
                oscillation.frequency
            )
        )
    }

    private fun sendWeaveOnlineSetPara() {
        val osc = oscillation
        val typeCode = weaveTypeCode(osc.type)
        val waitTimeCode = if (osc.waitTime == "不包括") 0 else 1
        val posWaitCode = if (osc.positionWait == "等待时间内位置继续移动") 0 else 1
        val freq = formatNumber(osc.frequency)
        val amp = formatNumber(osc.amplitude)
        val leftStay = osc.leftStopTime.toInt()
        val rightStay = osc.rightStopTime.toInt()
        val ratio = osc.callbackRatio.toInt().coerceIn(0, 100)
        val cmd = "WeaveOnlineSetPara(3,$typeCode,$freq,$waitTimeCode,$amp,$leftStay,$rightStay,$ratio,$posWaitCode)"
        sendLuaOnce(cmd, type = 825)
    }

    private fun formatNumber(value: Double): String {
        return if (value == value.toLong().toDouble()) {
            value.toLong().toString()
        } else {
            String.format(Locale.US, "%.1f", value)
        }
    }

    fun increaseSpeed() {
        speedPercent = (speedPercent + 5).coerceAtMost(100)
        sendLuaOnce("SetSpeed($speedPercent)", type = 206)
        toast("SetSpeed($speedPercent)")
    }

    fun decreaseSpeed() {
        speedPercent = (speedPercent - 5).coerceAtLeast(1)
        sendLuaOnce("SetSpeed($speedPercent)", type = 206)
        toast("SetSpeed($speedPercent)")
    }

    fun sendOffset(axis: Char, positive: Boolean) {
        val delta = if (positive) offsetStepMm else -offsetStepMm
        when (axis) {
            'X' -> weaveOffsetX += delta
            'Y' -> weaveOffsetY += delta
            'Z' -> weaveOffsetZ += delta
        }
        val cmd = String.format(
            Locale.US,
            "SetWeaveOffsetRT(%.1f,%.1f,%.1f,0.0,0.0,0.0)",
            weaveOffsetX, weaveOffsetY, weaveOffsetZ
        )
        sendLuaOnce(cmd, type = 1368)
        toast(cmd)
    }

    fun onReservedButton(index: Int) {
        toast("预留$index：待配置指令")
    }

    private fun persist() {
        projectManager.saveRobotTestSettings(
            RobotTestSettings(
                startPoint = startPoint,
                endPoint = endPoint,
                oscillation = oscillation
            )
        )
    }

    private fun snapshotCurrentPoint(): CapturedPoint? {
        if (!isConnected()) {
            toast("设备未连接，无法采集")
            return null
        }
        val joints = liveJoints
        if (joints.size < 6) {
            toast("关节数据未就绪")
            return null
        }
        return CapturedPoint(livePose, joints)
    }

    private fun buildMoveL(point: CapturedPoint, moveSpeed: Int = 100): String {
        val j = point.joints
        val p = point.pose
        val pos = listOf(j[0], j[1], j[2], j[3], j[4], j[5], p.x, p.y, p.z, p.rx, p.ry, p.rz)
            .joinToString(",") { String.format(Locale.US, "%.3f", it) }
        val ext1 = String.format(Locale.US, "%.3f", p.ext1)
        return "MoveL($pos,2,0,100,100,$moveSpeed,-1,0,$ext1,0.000,0.000,0.000,0,0,0,0,0,0,0,0,100,0)"
    }

    private fun buildWeaveSetPara(): String? {
        val osc = oscillation
        if (osc.type == "无摆动") return null
        val typeCode = weaveTypeCode(osc.type)
        val waitTimeCode = if (osc.waitTime == "不包括") 0 else 1
        val posWaitCode = if (osc.positionWait == "等待时间内位置继续移动") 0 else 1
        return "WeaveSetPara(3,$typeCode,${osc.frequency},$waitTimeCode,${osc.amplitude},${osc.leftSideLength},${osc.rightSideLength},${osc.zeroTime},${osc.leftStopTime},${osc.rightStopTime},${osc.callbackRatio},$posWaitCode,${osc.azimuth},${osc.inclination})"
    }

    private fun weaveTypeCode(type: String): Int {
        return when (type) {
            "三角波摆动" -> 0
            "直角L型三角波摆动" -> 1
            "圆形摆动-顺时针" -> 2
            "圆形摆动-逆时针" -> 3
            "正弦波摆动" -> 4
            "垂直L型正弦波摆动" -> 5
            "立焊三角摆动" -> 6
            else -> 0
        }
    }

    private fun sendLuaOnce(cmd: String, type: Int = 201) {
        val id = nextId()
        val msg = "/f/bIII${id}III${type}III${cmd.length}III${cmd}III/b/f"
        Log.d("RobotTest", "Send: $msg")
        socketManager.sendControlCommand(msg)
    }

    private fun isConnected(): Boolean = connectionStatus == "已连接"

    private fun nextId(): Int = commandId++

    private fun toast(message: String) {
        viewModelScope.launch { _toastEvent.emit(message) }
    }

    override fun onCleared() {
        super.onCleared()
        socketManager.stop()
    }
}
