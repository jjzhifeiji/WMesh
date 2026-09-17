package com.gbndt.shijiaoqi.data.repository

import com.gbndt.shijiaoqi.data.robot.link.LiveRobotBus
import com.gbndt.shijiaoqi.model.Pose
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.StateFlow
import javax.inject.Inject
import javax.inject.Singleton

/** 控制器数据源：状态出流、指令进方法；协议细节只在 robot/protocol。 */
@Singleton
class RobotRepository @Inject constructor(
    private val bus: LiveRobotBus,
) {
    val connectionStatus: StateFlow<String> get() = bus.connectionStatus
    val receivedText: SharedFlow<String> get() = bus.receivedText
    val receivedText8082: SharedFlow<String> get() = bus.receivedText8082
    val robotJoints: StateFlow<List<Double>> get() = bus.robotJoints
    val robotPose: StateFlow<Pose?> get() = bus.robotPose
    val weldingBreakOffState: StateFlow<String> get() = bus.weldingBreakOffState
    val weldArcState: StateFlow<String> get() = bus.weldArcState
    val alarmStatus: StateFlow<String> get() = bus.alarmStatus
    val robotInputSignal: StateFlow<Int> get() = bus.robotInputSignal
    val programState: StateFlow<Int> get() = bus.programState
    val extAxisPos: StateFlow<Double> get() = bus.extAxisPos
    val extAxisReady: StateFlow<Boolean> get() = bus.extAxisReady
    val progTotalLine: StateFlow<Int> get() = bus.progTotalLine
    val progCurLine: StateFlow<Int> get() = bus.progCurLine
    val weldCurrent: StateFlow<Double> get() = bus.weldCurrent
    val weldVoltage: StateFlow<Double> get() = bus.weldVoltage
    val robotErrorEvent: SharedFlow<Int> get() = bus.robotErrorEvent

    fun start() = bus.start()
    fun stop() = bus.stop()
    fun restart() = bus.restart()
    fun dropLink() = bus.dropLink()
    fun sendControlCommand(command: String) = bus.sendControlCommand(command)
    fun sendBatchCommand(command: String) = bus.sendBatchCommand(command)
    suspend fun sendBatchCommandSync(command: String) = bus.sendBatchCommandSync(command)
    suspend fun getInverseKin(pose: Pose, config: Int = -1): List<Double>? =
        bus.getInverseKin(pose, config)
}