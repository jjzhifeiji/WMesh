package com.gbndt.shijiaoqi.model

import kotlinx.serialization.Serializable

/** 本机示教偏好：工具、速度、累计焊长。 */
@Serializable
data class AppSettings(
    val selectedToolIndex: Int = 0,
    val toolCoordinates: List<Pose?> = List(14) { null },
    val toolRemarks: List<String> = List(14) { "" },
    val positionMode: String = "前",
    val speedMode: String = "1倍",
    val totalWeldingLength: Double = 0.0,
    val totalWeldingDuration: Long = 0L,
    val lastConnectionTime: Long = 0L,
    val installPos: Int = 0, // 0-平装, 1-侧装, 2-挂装
    val weldingCurrent: Double = 0.0,
    val weldingVoltage: Double = 0.0,
    val isExtAxisEnabled: Boolean = true
)
/** 机器人测试页采到的一点。 */
@Serializable
data class CapturedPoint(
    val pose: Pose,
    val joints: List<Double>
)

/** 机器人测试页起终点与摆焊。 */
@Serializable
data class RobotTestSettings(
    val startPoint: CapturedPoint? = null,
    val endPoint: CapturedPoint? = null,
    val oscillation: Oscillation = Oscillation()
)
