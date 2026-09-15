package com.gbndt.shijiaoqi.data.manager

import android.util.Log
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import java.io.InputStream
import java.io.OutputStream
import java.net.Socket
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.asSharedFlow
import java.nio.ByteBuffer
import java.nio.ByteOrder
import com.gbndt.shijiaoqi.data.models.Pose
import java.net.InetSocketAddress

object SocketManager {
    private val scope = CoroutineScope(Dispatchers.IO + SupervisorJob())

    // Connection states for internal tracking
    private val _isConnected8080 = MutableStateFlow(false)
    private val _isConnected8082 = MutableStateFlow(false)
    private val _isConnected8083 = MutableStateFlow(false)

    // Public combined status
    private val _connectionStatus = MutableStateFlow("未连接")
    val connectionStatus: StateFlow<String> = _connectionStatus.asStateFlow()
    
    // Received Text Data (8080)
    private val _receivedText = MutableSharedFlow<String>()
    val receivedText: SharedFlow<String> = _receivedText.asSharedFlow()
    
    // Received Text Data (8082)
    private val _receivedText8082 = MutableSharedFlow<String>()
    val receivedText8082: SharedFlow<String> = _receivedText8082.asSharedFlow()
    
    // Robot Realtime Data
    private val _robotJoints = MutableStateFlow<List<Double>>(emptyList())
    val robotJoints: StateFlow<List<Double>> = _robotJoints.asStateFlow()
    
    private val _robotPose = MutableStateFlow<Pose?>(null)
    val robotPose: StateFlow<Pose?> = _robotPose.asStateFlow()

    private val _weldingBreakOffState = MutableStateFlow("正常")
    val weldingBreakOffState: StateFlow<String> = _weldingBreakOffState.asStateFlow()

    private val _weldArcState = MutableStateFlow("正常")
    val weldArcState: StateFlow<String> = _weldArcState.asStateFlow()
    
    // Alarm Status
    private val _alarmStatus = MutableStateFlow("无报警")
    val alarmStatus: StateFlow<String> = _alarmStatus.asStateFlow()

    // Robot Input Signal (from bytes 387-388)
    private val _robotInputSignal = MutableStateFlow(0)
    val robotInputSignal: StateFlow<Int> = _robotInputSignal.asStateFlow()

    // Program Status (8083)
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

    // Command Feedback Error (e.g. "robot errcode:46")
    private val _robotErrorEvent = MutableSharedFlow<Int>()
    val robotErrorEvent = _robotErrorEvent.asSharedFlow()

    private var socket8080: SocketClient? = null
    private var socket8082: SocketClient? = null
    private var socket8083: SocketClient? = null
    private var lastDataTime8083 = 0L
    private var lastExtLogTime = 0L

    private val SERVER_IP = "192.168.57.2"
    private val PORT_CONTROL = 8080
    private val PORT_BATCH = 8082
    private val PORT_DATA = 8083

    private var isStarted = false
    private var clientCount = 0

    init {
        // Monitor both statuses to update main status
        scope.launch {
            while (true) {
                val s1 = _isConnected8080.value
                val s2 = _isConnected8083.value
                val is8083Alive = s2 && (System.currentTimeMillis() - lastDataTime8083 <= 1000)
                _connectionStatus.value = if (s1 && is8083Alive) "已连接" else "未连接"
                delay(500)
            }
        }
    }

    private var stopJob: Job? = null

    fun start() {
        synchronized(this) {
            clientCount++
            if (stopJob?.isActive == true) {
                Log.d("SocketManager", "Cancelling pending stop, clientCount=$clientCount")
                stopJob?.cancel()
                stopJob = null
            }
        }
        startInternal()
    }

    private fun startInternal() {
        if (isStarted) return
        isStarted = true

        socket8080 = SocketClient(scope, SERVER_IP, PORT_CONTROL, isText = true, onStatusChange = { isConnected ->
            _isConnected8080.value = isConnected
        }, onDataReceived = { data, length ->
            // Text data from 8080
            val text = String(data, 0, length, Charsets.UTF_8)
            Log.d("SocketClient", "8080 Recv: $text")
            
            // Check for robot errcode
            val errCodeRegex = Regex("robot errcode:(\\d+)")
            val match = errCodeRegex.find(text)
            if (match != null) {
                val errCode = match.groupValues[1].toIntOrNull()
                if (errCode != null && errCode != 0 && errCode != 18) {
                    scope.launch {
                        _robotErrorEvent.emit(errCode)
                    }
                }
            }
            
            scope.launch {
                _receivedText.emit(text)
            }
        })

        socket8082 = SocketClient(scope, SERVER_IP, PORT_BATCH, isText = true, onStatusChange = { isConnected ->
            _isConnected8082.value = isConnected
        }, onDataReceived = { data, length ->
            val text = String(data, 0, length, Charsets.UTF_8)
            Log.d("SocketClient", "8082 Recv: $text")
            
            // Check for robot errcode in 8082 as well
            val errCodeRegex = Regex("robot errcode:(\\d+)")
            val match = errCodeRegex.find(text)
            if (match != null) {
                val errCode = match.groupValues[1].toIntOrNull()
                if (errCode != null && errCode != 0 && errCode != 18) {
                    scope.launch {
                        _robotErrorEvent.emit(errCode)
                    }
                }
            }

            scope.launch {
                _receivedText8082.emit(text)
            }
        })

        socket8083 = SocketClient(scope, SERVER_IP, PORT_DATA, isText = false, onStatusChange = { isConnected ->
            _isConnected8083.value = isConnected
            if (isConnected) {
                lastDataTime8083 = System.currentTimeMillis()
            }
        }, onDataReceived = { data, length ->
             // Binary data from 8083 - Parse Robot Data
            lastDataTime8083 = System.currentTimeMillis()
//            Log.d("SocketManager", "8083 Recv: $data")
             try {
                 if (length >= 104 && data[0].toInt() == 0x5a && data[1].toInt() == 0x5a) {
                     // Parse Alarm Status (Byte 6)
                     val alarmCode = data[6].toInt()
                     val alarmText = when (alarmCode) {
                         0x00 -> "无故障"
                         0x01 -> "驱动器故障"
                         0x02 -> "超出软限位故障"
                         0x03 -> "碰撞故障"
                         0x04 -> "奇异位姿"
                         0x05 -> "从站错误"
                         0x06 -> "指令点错误"
                         0x07 -> "IO错误"
                         0x08 -> "夹爪错误"
                         0x09 -> "文件错误"
                         0x0a -> "参数错误"
                         0x0b -> "扩展轴超出软限位错误"
                         else -> "未知错误($alarmCode)"
                     }
                     _alarmStatus.value = alarmText

                     val buffer = ByteBuffer.wrap(data, 8, 96).order(ByteOrder.LITTLE_ENDIAN)
                     val doubles = DoubleArray(12)
                     for (i in 0 until 12) {
                         doubles[i] = buffer.double
                     }
                     
                     // 0-5: Joints
                     val joints = doubles.slice(0..5)
                     _robotJoints.value = joints
                     
                     // 6-8: X, Y, Z; 9-11: Rx, Ry, Rz
                     val pose = Pose(
                         x = doubles[6],
                         y = doubles[7],
                         z = doubles[8],
                         rx = doubles[9],
                         ry = doubles[10],
                         rz = doubles[11],
                         ext1 = _extAxisPos.value
                     )
                     _robotPose.value = pose
                     
                     // Log for debugging
                     // Log.d("SocketManager", "Parsed Robot Data: $pose")
                     
                     // Extract Robot Input Signal (User Request)
                     if (length > 388) {
                         val highByte = data[388].toInt() and 0xFF
                         val lowByte = data[387].toInt() and 0xFF
                         val combined = (highByte shl 8) or lowByte
                         _robotInputSignal.value = combined
                     }

                     // Extract Welding State (Index 69)
                     if (length >= 427) {
                         val breakOffState = data[425].toInt() and 0xFF
                         val arcState = data[426].toInt() and 0xFF
                         _weldingBreakOffState.value = if (breakOffState == 1) "中断" else "正常"
                         _weldArcState.value = if (arcState == 1) "中断" else "正常"
                     }

                     // Extract Program Status
                    if (length > 177) {
                        _programState.value = data[5].toInt() and 0xFF
                        _progTotalLine.value = data[176].toInt() and 0xFF
                        _progCurLine.value = data[177].toInt() and 0xFF
                    }
                    
                    if (length >= 387) {
                        val ai0 = (data[383].toInt() and 0xFF) or ((data[384].toInt() and 0xFF) shl 8)
                        val ai1 = (data[385].toInt() and 0xFF) or ((data[386].toInt() and 0xFF) shl 8)
                        // 控制箱模拟量输入 0-4095，按常见焊机模拟量量程换算
                        _weldCurrent.value = ai0 / 4095.0 * 500.0
                        _weldVoltage.value = ai1 / 4095.0 * 50.0
                    }

                    // Parse External Axis (Item 49, offset 265)
                    if (length >= 381) {
                        val bufferExt = ByteBuffer.wrap(data, 265, 29).order(ByteOrder.LITTLE_ENDIAN)
                        val pos = bufferExt.double
                        val speed = bufferExt.double
                        val errCode = bufferExt.int
                        val rdy = bufferExt.get().toInt() and 0xFF
                        
                        _extAxisPos.value = pos
                        _extAxisReady.value = rdy == 1
                        
                        val currentTime = System.currentTimeMillis()
                        if (currentTime - lastExtLogTime > 1000) { 
                            lastExtLogTime = currentTime
                            Log.d("SocketManager", "ExtAxis[0]: Pos=$pos, Speed=$speed, Err=$errCode, Rdy=$rdy")
                        }
                    }
                 }
             } catch (e: Exception) {
                 Log.e("SocketManager", "Parse Error: ${e.message}")
             }
        })

        socket8080?.start()
        socket8082?.start()
        socket8083?.start()
    }
    
    fun stop() {
        synchronized(this) {
            clientCount--
            if (clientCount < 0) clientCount = 0
            Log.d("SocketManager", "Stop requested, clientCount=$clientCount")
            
            if (clientCount == 0) {
                stopJob?.cancel() // Cancel any previous pending stop
                stopJob = scope.launch {
                    delay(2000) // Wait 2 seconds before actually stopping
                    Log.d("SocketManager", "Executing forceStop")
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
        // Wait a bit to ensure sockets are closed, then start again
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

    suspend fun getInverseKin(pose: Pose, config: Int = -1): List<Double>? {
        val x = String.format(java.util.Locale.US, "%.3f", pose.x)
        val y = String.format(java.util.Locale.US, "%.3f", pose.y)
        val z = String.format(java.util.Locale.US, "%.3f", pose.z)
        val rx = String.format(java.util.Locale.US, "%.3f", pose.rx)
        val ry = String.format(java.util.Locale.US, "%.3f", pose.ry)
        val rz = String.format(java.util.Locale.US, "%.3f", pose.rz)
        
        val cmd = "GetInverseKin(0,$x,$y,$z,$rx,$ry,$rz,$config)"
        val msg = "/f/bIII4III375III${cmd.length}III${cmd}III/b/f"
        
        var result: List<Double>? = null
        val job = scope.launch {
            try {
                withTimeout(2000) {
                    receivedText.collect { text ->
                        if (text.contains("III375III")) {
                            val parts = text.split("III")
                            if (parts.size >= 5) {
                                val dataPart = parts[4]
                                val joints = dataPart.split(",").mapNotNull { it.toDoubleOrNull() }
                                if (joints.size >= 6) {
                                    result = joints.take(6)
                                    this@launch.cancel()
                                }
                            }
                        }
                    }
                }
            } catch (e: TimeoutCancellationException) {
                Log.e("SocketManager", "GetInverseKin timeout")
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
        val isText: Boolean,
        val onStatusChange: (Boolean) -> Unit,
        val onDataReceived: (ByteArray, Int) -> Unit
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
                        Log.d("SocketClient", "Connecting to $ip:$port...")
                        socket = Socket()
                        socket?.connect(InetSocketAddress(ip, port), 2000) // 2 seconds timeout
                        
                        outputStream = socket?.getOutputStream()
                        val inputStream = socket?.getInputStream()
                        
                        Log.d("SocketClient", "Connected to $ip:$port")
                        onStatusChange(true)

                        // Send handshake/initialization command if needed immediately after connection
                        // This helps to "wake up" the server or register this client
                        try {
                            // Example: Send an empty line or a specific "hello" packet
                            // Adjust this based on your specific robot protocol requirements
                            val handshake = "connect".toByteArray() 
                            outputStream?.write(handshake)
                            outputStream?.flush()
                        } catch (e: Exception) {
                            Log.w("SocketClient", "Handshake failed: ${e.message}")
                        }

                        val buffer = ByteArray(1024)
                        while (isRunning && socket != null && socket!!.isConnected) {
                            val read = inputStream?.read(buffer) ?: -1
                            if (read == -1) break
                            
                            if (read > 0) {
                                onDataReceived(buffer, read)
                            }
                        }
                    } catch (e: Exception) {
                        Log.e("SocketClient", "Error on $port: ${e.message}")
                    } finally {
                        cleanup()
                        onStatusChange(false)
                        if (isRunning) {
                            delay(3000) // Reconnect delay
                            Log.d("SocketClient", "Attempting auto-reconnect to $ip:$port...")
                        }
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
            scope.launch {
                sendSync(msg)
            }
        }

        suspend fun sendSync(msg: String) {
            kotlinx.coroutines.withContext(kotlinx.coroutines.Dispatchers.IO) {
                try {
                    outputStream?.write(msg.toByteArray(Charsets.UTF_8))
                    outputStream?.flush()
                    Log.d("SocketClient", "Send ${msg}")
                } catch (e: Exception) {
                    Log.e("SocketClient", "Send error: ${e.message}")
                }
            }
        }

        private fun cleanup() {
            try {
                socket?.close()
            } catch (e: Exception) {}
            socket = null
            outputStream = null
        }
        
        private fun bytesToHex(bytes: ByteArray, length: Int): String {
            val sb = StringBuilder()
            for (i in 0 until length) {
                sb.append(String.format("%02X ", bytes[i]))
            }
            return sb.toString().trim()
        }
    }
}
