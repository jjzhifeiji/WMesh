package com.gbndt.shijiaoqi.data.robot.link

import android.util.Log
import com.gbndt.shijiaoqi.model.Pose
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.TimeoutCancellationException
import kotlinx.coroutines.delay
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.withTimeout
import java.io.OutputStream
import java.net.InetSocketAddress
import java.net.Socket
import com.gbndt.shijiaoqi.data.robot.protocol.FrPacket
import com.gbndt.shijiaoqi.data.robot.protocol.RobotCommands
import com.gbndt.shijiaoqi.data.robot.protocol.RobotLink
import com.gbndt.shijiaoqi.data.robot.protocol.Status8083

/** 三路 TCP 控制器适配：8080 点动/指令，8082 批量，8083 状态。不管身份。 */
object LiveRobotBus {
    private val scope = CoroutineScope(Dispatchers.IO + SupervisorJob())

    private val _isConnected8080 = MutableStateFlow(false)
    private val _isConnected8082 = MutableStateFlow(false)
    private val _isConnected8083 = MutableStateFlow(false)

    private val _connectionStatus = MutableStateFlow(RobotLink.DOWN)
    val connectionStatus: StateFlow<String> = _connectionStatus.asStateFlow()

    private val _receivedText = MutableSharedFlow<String>()
    val receivedText: SharedFlow<String> = _receivedText.asSharedFlow()

    private val _receivedText8082 = MutableSharedFlow<String>()
    val receivedText8082: SharedFlow<String> = _receivedText8082.asSharedFlow()

    private val _robotJoints = MutableStateFlow<List<Double>>(emptyList())
    val robotJoints: StateFlow<List<Double>> = _robotJoints.asStateFlow()

    private val _robotPose = MutableStateFlow<Pose?>(null)
    val robotPose: StateFlow<Pose?> = _robotPose.asStateFlow()

    private val _weldingBreakOffState = MutableStateFlow("正常")
    val weldingBreakOffState: StateFlow<String> = _weldingBreakOffState.asStateFlow()

    private val _weldArcState = MutableStateFlow("正常")
    val weldArcState: StateFlow<String> = _weldArcState.asStateFlow()

    private val _alarmStatus = MutableStateFlow("无报警")
    val alarmStatus: StateFlow<String> = _alarmStatus.asStateFlow()

    private val _robotInputSignal = MutableStateFlow(0)
    val robotInputSignal: StateFlow<Int> = _robotInputSignal.asStateFlow()

    private val _programState = MutableStateFlow(0)
    val programState: StateFlow<Int> = _programState.asStateFlow()

    private val _extAxisPos = MutableStateFlow(0.0)
    val extAxisPos: StateFlow<Double> = _extAxisPos.asStateFlow()

    private val _extAxisReady = MutableStateFlow(false)
    val extAxisReady: StateFlow<Boolean> = _extAxisReady.asStateFlow()

    private val _progTotalLine = MutableStateFlow(0)
    val progTotalLine: StateFlow<Int> = _progTotalLine.asStateFlow()

    private val _progCurLine = MutableStateFlow(0)
    val progCurLine: StateFlow<Int> = _progCurLine.asStateFlow()

    private val _weldCurrent = MutableStateFlow(0.0)
    val weldCurrent: StateFlow<Double> = _weldCurrent.asStateFlow()

    private val _weldVoltage = MutableStateFlow(0.0)
    val weldVoltage: StateFlow<Double> = _weldVoltage.asStateFlow()

    private val _robotErrorEvent = MutableSharedFlow<Int>()
    val robotErrorEvent = _robotErrorEvent.asSharedFlow()

    private var socket8080: SocketClient? = null
    private var socket8082: SocketClient? = null
    private var socket8083: SocketClient? = null
    private var lastDataTime8083 = 0L

    const val SERVER_IP = "192.168.57.2"
    const val PORT_CONTROL = 8080
    const val PORT_BATCH = 8082
    const val PORT_DATA = 8083

    private var isStarted = false
    private var clientCount = 0
    private var stopJob: Job? = null

    init {
        scope.launch {
            while (true) {
                _connectionStatus.value = RobotLink.status(
                    _isConnected8080.value,
                    _isConnected8083.value,
                    lastDataTime8083,
                    System.currentTimeMillis(),
                )
                delay(500)
            }
        }
    }

    fun start() {
        synchronized(this) {
            clientCount++
            if (stopJob?.isActive == true) {
                stopJob?.cancel()
                stopJob = null
            }
        }
        startInternal()
    }

    private fun startInternal() {
        if (isStarted) return
        isStarted = true
        socket8080 = SocketClient(scope, SERVER_IP, PORT_CONTROL, onStatusChange = { _isConnected8080.value = it }) { data, length ->
            val text = String(data, 0, length, Charsets.UTF_8)
            FrPacket.errorCode(text)?.let { code ->
                scope.launch { _robotErrorEvent.emit(code) }
            }
            scope.launch { _receivedText.emit(text) }
        }
        socket8082 = SocketClient(scope, SERVER_IP, PORT_BATCH, onStatusChange = { _isConnected8082.value = it }) { data, length ->
            val text = String(data, 0, length, Charsets.UTF_8)
            FrPacket.errorCode(text)?.let { code ->
                scope.launch { _robotErrorEvent.emit(code) }
            }
            scope.launch { _receivedText8082.emit(text) }
        }
        socket8083 = SocketClient(scope, SERVER_IP, PORT_DATA, onStatusChange = { up ->
            _isConnected8083.value = up
            if (up) lastDataTime8083 = System.currentTimeMillis()
        }) { data, length ->
            lastDataTime8083 = System.currentTimeMillis()
            val st = Status8083.parse(data, length) ?: return@SocketClient
            applyStatus(st)
        }
        socket8080?.start()
        socket8082?.start()
        socket8083?.start()
    }

    private fun applyStatus(st: Status8083) {
        _alarmStatus.value = st.alarmText
        _robotJoints.value = st.joints
        _robotPose.value = st.pose
        _robotInputSignal.value = st.inputSignal
        _weldingBreakOffState.value = if (st.weldingBreakOff) "中断" else "正常"
        _weldArcState.value = if (st.arcBreakOff) "中断" else "正常"
        _programState.value = st.programState
        _progTotalLine.value = st.progTotalLine
        _progCurLine.value = st.progCurLine
        _weldCurrent.value = st.weldCurrent
        _weldVoltage.value = st.weldVoltage
        _extAxisPos.value = st.extAxisPos
        _extAxisReady.value = st.extAxisReady
    }

    fun stop() {
        synchronized(this) {
            clientCount--
            if (clientCount < 0) clientCount = 0
            if (clientCount == 0) {
                stopJob?.cancel()
                stopJob = scope.launch {
                    delay(2000)
                    forceStop()
                }
            }
        }
    }

    private fun forceStop() {
        isStarted = false
        socket8080?.stop()
        socket8082?.stop()
        socket8083?.stop()
    }

    fun restart() {
        forceStop()
        scope.launch {
            delay(500)
            startInternal()
        }
    }

    fun sendControlCommand(command: String) {
        socket8080?.send(command)
    }

    fun sendBatchCommand(command: String) {
        socket8082?.send(command)
    }

    suspend fun sendBatchCommandSync(command: String) {
        socket8082?.sendSync(command)
    }

    fun sendJog(frame: String) {
        sendControlCommand(frame)
    }

    suspend fun getInverseKin(pose: Pose, config: Int = -1): List<Double>? {
        val x = String.format(java.util.Locale.US, "%.3f", pose.x)
        val y = String.format(java.util.Locale.US, "%.3f", pose.y)
        val z = String.format(java.util.Locale.US, "%.3f", pose.z)
        val rx = String.format(java.util.Locale.US, "%.3f", pose.rx)
        val ry = String.format(java.util.Locale.US, "%.3f", pose.ry)
        val rz = String.format(java.util.Locale.US, "%.3f", pose.rz)
        val cmd = "GetInverseKin(0,$x,$y,$z,$rx,$ry,$rz,$config)"
        val msg = FrPacket.encode(4, 375, cmd)
        var result: List<Double>? = null
        val job = scope.launch {
            try {
                withTimeout(2000) {
                    receivedText.collect { text ->
                        if (text.contains("III375III")) {
                            val parts = text.split("III")
                            if (parts.size >= 5) {
                                val joints = parts[4].split(",").mapNotNull { it.toDoubleOrNull() }
                                if (joints.size >= 6) {
                                    result = joints.take(6)
                                    this@launch.cancel()
                                }
                            }
                        }
                    }
                }
            } catch (_: TimeoutCancellationException) {
                Log.e("LiveRobotBus", "GetInverseKin timeout")
            }
        }
        sendControlCommand(msg)
        job.join()
        return result
    }

    private class SocketClient(
        val scope: CoroutineScope,
        val ip: String,
        val port: Int,
        val onStatusChange: (Boolean) -> Unit,
        val onDataReceived: (ByteArray, Int) -> Unit,
    ) {
        private var socket: Socket? = null
        private var outputStream: OutputStream? = null
        private var isRunning = false
        private var job: Job? = null

        fun start() {
            isRunning = true
            job = scope.launch {
                while (isRunning) {
                    try {
                        socket = Socket()
                        socket?.connect(InetSocketAddress(ip, port), 2000)
                        outputStream = socket?.getOutputStream()
                        val inputStream = socket?.getInputStream()
                        onStatusChange(true)
                        try {
                            outputStream?.write(RobotCommands.HANDSHAKE.toByteArray())
                            outputStream?.flush()
                        } catch (_: Exception) {
                        }
                        val buffer = ByteArray(1024)
                        while (isRunning && socket != null && socket!!.isConnected) {
                            val read = inputStream?.read(buffer) ?: -1
                            if (read == -1) break
                            if (read > 0) onDataReceived(buffer, read)
                        }
                    } catch (e: Exception) {
                        Log.e("LiveRobotBus", "Error on $port: ${e.message}")
                    } finally {
                        cleanup()
                        onStatusChange(false)
                        if (isRunning) delay(3000)
                    }
                }
            }
        }

        fun stop() {
            isRunning = false
            cleanup()
            job?.cancel()
        }

        fun send(msg: String) {
            scope.launch { sendSync(msg) }
        }

        suspend fun sendSync(msg: String) {
            kotlinx.coroutines.withContext(Dispatchers.IO) {
                try {
                    outputStream?.write(msg.toByteArray(Charsets.UTF_8))
                    outputStream?.flush()
                } catch (e: Exception) {
                    Log.e("LiveRobotBus", "Send error: ${e.message}")
                }
            }
        }

        private fun cleanup() {
            try {
                socket?.close()
            } catch (_: Exception) {
            }
            socket = null
            outputStream = null
        }
    }
}
