package com.gbndt.shijiaoqi.model

import kotlinx.serialization.Serializable

/** 焊点类型；坡口点只出现在 T 排。 */
@Serializable
enum class WeldPointType {
    START_SAFE,
    START,
    MIDDLE,
    ARC_MIDDLE,
    END,
    END_SAFE,
    GROOVE_A_LOWER,
    GROOVE_B_LOWER,
    GROOVE_A_UPPER,
    GROOVE_B_UPPER
}

/** 界面点名。 */
fun WeldPointType.displayName(): String {
    return when (this) {
        WeldPointType.START_SAFE -> "起安"
        WeldPointType.START -> "起点"
        WeldPointType.MIDDLE -> "中间"
        WeldPointType.ARC_MIDDLE -> "圆中"
        WeldPointType.END -> "终点"
        WeldPointType.END_SAFE -> "终安"
        WeldPointType.GROOVE_A_LOWER -> "A下"
        WeldPointType.GROOVE_B_LOWER -> "B下"
        WeldPointType.GROOVE_A_UPPER -> "A上"
        WeldPointType.GROOVE_B_UPPER -> "B上"
    }
}
/** 笛卡尔位姿；角度为度，ext1 为导轨。 */
@Serializable
data class Pose(
    val x: Double,
    val y: Double,
    val z: Double,
    val rx: Double,
    val ry: Double,
    val rz: Double,
    val ext1: Double = 0.0
)
/** 参考点：笛卡尔加关节，给偏移坐标系用。 */
@Serializable
data class RefPoint(
    val pose: Pose,
    val jointAngles: List<Double>
)
/** 一条焊道上的示教点。 */
@Serializable
data class WeldPoint(
    val id: String,
    val type: WeldPointType,
    var pose: Pose? = null,
    var jointAngles: List<Double>? = null,
    var executionOffsets: List<Double>? = null, // [offX, offY, offZ, offRx, offRy, offRz]
    var refPointX: RefPoint? = null, // X 向参考
)
/** 摆焊参数；type 为中文枚举名。 */
@Serializable
data class Oscillation(
    var type: String = "无摆动",
    var waitTime: String = "不包括",
    var positionWait: String = "等待时间内位置继续移动",
    var frequency: Double = 5.0,
    var amplitude: Double = 1.0,
    var leftStopTime: Double = 100.0,
    var rightStopTime: Double = 100.0,
    var leftSideLength: Double = 1.0,
    var rightSideLength: Double = 1.0,
    var zeroTime: Double = 20.0,
    var callbackRatio: Double = 10.0,
    var azimuth: Double = 0.0,
    var inclination: Double = 0.0
)
/** 焊接工艺参数；身份在袋里，不在这份正文。 */
@Serializable
data class WeldProcess(
    var name: String = "默认工艺",
    var offsetX: String = "0",
    var offsetY: String = "0",
    var offsetZ: String = "0",
    var current: Double = 170.0,
    var voltage: Double = 20.0,
    var speed: Double = 10.0,
    var startArcTime: Double = 400.0,
    var endArcTime: Double = 400.0,
    var startArcCurrent: Double = 180.0,
    var endArcCurrent: Double = 160.0,
    var startArcVoltage: Double = 20.0,
    var endArcVoltage: Double = 20.0,
    var oscillation: Oscillation = Oscillation()
)
