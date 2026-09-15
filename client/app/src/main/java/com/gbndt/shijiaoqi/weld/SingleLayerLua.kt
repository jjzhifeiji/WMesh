package com.gbndt.shijiaoqi.weld

import com.gbndt.shijiaoqi.data.models.Pose
import com.gbndt.shijiaoqi.data.models.WeldPointType
import com.gbndt.shijiaoqi.data.models.WeldProcess
import java.util.Locale

/** 一条单层焊道上的点：类型、位姿、关节。 */
data class ScriptPoint(
    val type: WeldPointType,
    val pose: Pose,
    val joints: List<Double> = listOf(0.0, 0.0, 0.0, 0.0, 0.0, 0.0),
)

/** 一条焊道：主工艺加附加工艺，几何共用。 */
data class ScriptPath(
    val points: List<ScriptPoint>,
    val process: WeldProcess,
    val extras: List<WeldProcess> = emptyList(),
    val enabled: Boolean = true,
)

/** 单层 Lua：全局速度、工艺参数、点运动后再起弧；模拟不加起收弧。 */
object SingleLayerLua {
    const val GLOBAL_SPEED = "SetSpeed(10)"

    fun job(
        paths: List<ScriptPath>,
        welding: Boolean,
        simulating: Boolean,
        speedMode: String = "1倍",
        toolIndex: Int = 1,
    ): List<String> {
        val out = mutableListOf(GLOBAL_SPEED)
        for (path in paths) {
            if (!path.enabled && path.extras.isEmpty()) continue
            if (path.enabled) {
                out += pathLines(path.points, path.process, welding && !simulating, simulating, speedMode, toolIndex)
            }
            for (extra in path.extras) {
                out += pathLines(path.points, extra, welding && !simulating, simulating, speedMode, toolIndex)
            }
        }
        return out
    }

    fun pathLines(
        points: List<ScriptPoint>,
        process: WeldProcess,
        isWelding: Boolean,
        simulating: Boolean,
        speedMode: String,
        toolIndex: Int,
    ): List<String> {
        val multiplier = if (simulating) {
            when (speedMode) {
                "3倍" -> 3
                "5倍" -> 5
                else -> 1
            }
        } else {
            1
        }
        val weldSpeed = (process.speed * multiplier).toInt()
        val out = mutableListOf<String>()
        out += "WeldingSetProcessParam(2,${process.startArcCurrent},${process.startArcVoltage},${process.startArcTime},${process.current},${process.voltage},${process.endArcCurrent},${process.endArcVoltage},${process.endArcTime})"
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
                else -> 0
            }
            val waitTimeCode = if (process.oscillation.waitTime == "不包括") 0 else 1
            val posWaitCode = if (process.oscillation.positionWait == "等待时间内位置继续移动") 0 else 1
            val o = process.oscillation
            out += "WeaveSetPara(3,$typeCode,${o.frequency},$waitTimeCode,${o.amplitude},${o.leftSideLength},${o.rightSideLength},${o.zeroTime},${o.leftStopTime},${o.rightStopTime},${o.callbackRatio},$posWaitCode,${o.azimuth},${o.inclination})"
        }
        var hasStartedArc = false
        var i = 0
        while (i < points.size) {
            val point = points[i]
            if (point.type == WeldPointType.ARC_MIDDLE && i + 1 < points.size) {
                val next = points[i + 1]
                out += moveC(point, next, weldSpeed, toolIndex)
                if (!hasStartedArc && oscType != "无摆动") {
                    out += "WeaveStart(3)"
                }
                if (next.type == WeldPointType.END) {
                    if (isWelding) out += "ARCEnd(0,2,10000)"
                    if (oscType != "无摆动") out += "WeaveEnd(0)"
                }
                i += 2
                continue
            }
            val moveSpeed = if (
                point.type == WeldPointType.START_SAFE ||
                point.type == WeldPointType.END_SAFE ||
                point.type == WeldPointType.START
            ) 100 else weldSpeed
            out += moveL(point, moveSpeed, toolIndex)
            if (point.type == WeldPointType.START && !hasStartedArc) {
                if (isWelding) out += "ARCStart(0,2,10000)"
                if (oscType != "无摆动") out += "WeaveStart(3)"
                hasStartedArc = true
            } else if (point.type == WeldPointType.END) {
                if (isWelding) out += "ARCEnd(0,2,10000)"
                if (oscType != "无摆动") out += "WeaveEnd(0)"
            }
            i++
        }
        return out
    }

    private fun moveL(point: ScriptPoint, speed: Int, toolIndex: Int): String {
        val pose = point.pose
        val joints = joints6(point.joints)
        val pos2 = joints.joinToString(",") { fmt(it) } + "," +
            listOf(pose.x, pose.y, pose.z, pose.rx, pose.ry, pose.rz).joinToString(",") { fmt(it) }
        val ext1 = fmt(pose.ext1)
        return "MoveL($pos2,$toolIndex,0,100,100,$speed,-1,0,$ext1,0.000,0.000,0.000,0,0,0,0,0,0,0,0,100,0)"
    }

    private fun moveC(mid: ScriptPoint, end: ScriptPoint, speed: Int, toolIndex: Int): String {
        val midPos = joints6(mid.joints).joinToString(",") { fmt(it) } + "," +
            listOf(mid.pose.x, mid.pose.y, mid.pose.z, mid.pose.rx, mid.pose.ry, mid.pose.rz).joinToString(",") { fmt(it) }
        val endPos = joints6(end.joints).joinToString(",") { fmt(it) } + "," +
            listOf(end.pose.x, end.pose.y, end.pose.z, end.pose.rx, end.pose.ry, end.pose.rz).joinToString(",") { fmt(it) }
        return "MoveC($midPos,$toolIndex,0,100,100,0,0,0,0,0,0,0,0,0,0,0,$endPos,$toolIndex,0,100,100,0,0,0,0,0,0,0,0,0,0,0,$speed,-1)"
    }

    private fun joints6(raw: List<Double>): List<Double> =
        if (raw.size >= 6) raw.take(6) else List(6) { raw.getOrElse(it) { 0.0 } }

    private fun fmt(v: Double): String = String.format(Locale.US, "%.3f", v)
}
