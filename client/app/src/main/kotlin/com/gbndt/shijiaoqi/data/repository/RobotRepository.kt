package com.gbndt.shijiaoqi.data.repository

import com.gbndt.shijiaoqi.data.robot.link.LiveRobotBus
import com.gbndt.shijiaoqi.model.Pose
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.StateFlow
import javax.inject.Inject
import javax.inject.Singleton

/** 控制器数据源：状态出流、指令进方法；协议细节只在 robot/protocol。 */
@Singleton
class RobotRepository @Inject constructor() {
    val connectionStatus: StateFlow<String> get() = LiveRobotBus.connectionStatus
    val receivedText: SharedFlow<String> get() = LiveRobotBus.receivedText
    val receivedText8082: SharedFlow<String> get() = LiveRobotBus.receivedText8082
    val robotJoints: StateFlow<List<Double>> get() = LiveRobotBus.robotJoints
    val robotPose: StateFlow<Pose?> get() = LiveRobotBus.robotPose
    val weldingBreakOffState: StateFlow<String> get() = LiveRobotBus.weldingBreakOffState
    val weldArcState: StateFlow<String> get() = LiveRobotBus.weldArcState
    val alarmStatus: StateFlow<String> get() = LiveRobotBus.alarmStatus
    val robotInputSignal: StateFlow<Int> get() = LiveRobotBus.robotInputSignal
    val programState: StateFlow<Int> get() = LiveRobotBus.programState
    val extAxisPos: StateFlow<Double> get() = LiveRobotBus.extAxisPos
    val extAxisReady: StateFlow<Boolean> get() = LiveRobotBus.extAxisReady
    val progTotalLine: StateFlow<Int> get() = LiveRobotBus.progTotalLine
    val progCurLine: StateFlow<Int> get() = LiveRobotBus.progCurLine
    val weldCurrent: StateFlow<Double> get() = LiveRobotBus.weldCurrent
    val weldVoltage: StateFlow<Double> get() = LiveRobotBus.weldVoltage
    val robotErrorEvent: SharedFlow<Int> get() = LiveRobotBus.robotErrorEvent

    fun start() = LiveRobotBus.start()
    fun stop() = LiveRobotBus.stop()
    fun restart() = LiveRobotBus.restart()
    fun sendControlCommand(command: String) = LiveRobotBus.sendControlCommand(command)
    fun sendBatchCommand(command: String) = LiveRobotBus.sendBatchCommand(command)
    suspend fun sendBatchCommandSync(command: String) = LiveRobotBus.sendBatchCommandSync(command)
    suspend fun getInverseKin(pose: Pose, config: Int = -1): List<Double>? =
        LiveRobotBus.getInverseKin(pose, config)
}
