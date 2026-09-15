package com.gbndt.shijiaoqi.weld

import com.gbndt.shijiaoqi.data.models.GapBand
import com.gbndt.shijiaoqi.data.models.Pose
import com.gbndt.shijiaoqi.data.models.WeldProcess
import com.gbndt.shijiaoqi.utils.TBarPass
import java.util.Locale

/** 一条 T 排焊缝：坡口几何加间隙带，工艺按身份取。 */
data class TBarScriptPath(
    val startSafe: ScriptPoint,
    val endSafe: ScriptPoint,
    val aLower: Pose,
    val bLower: Pose,
    val aUpper: Pose,
    val bUpper: Pose,
    val startPose: Pose,
    val endPose: Pose,
    val bands: List<GapBand>,
    val enabled: Boolean = true,
)

/** T 排 Lua：起安 → 打底 → 空走回起点 → 盖面 → 终安；段切换 WeaveOnlineSetPara。 */
object TBarLua {
    const val GLOBAL_SPEED = "SetSpeed(10)"

    fun job(
        paths: List<TBarScriptPath>,
        processes: Map<String, WeldProcess>,
        welding: Boolean,
        simulating: Boolean,
        speedMode: String = "1倍",
        toolIndex: Int = 1,
        extAxis: Boolean = false,
    ): List<String> {
        val out = mutableListOf(GLOBAL_SPEED)
        val isWeld = welding && !simulating
        for (path in paths) {
            if (!path.enabled) continue
            val root = TBarRun.buildSegments(
                path.aLower, path.bLower, path.aUpper, path.bUpper,
                path.startPose, path.endPose, path.bands, TBarPass.ROOT, 1,
            )
            val cap = TBarRun.buildSegments(
                path.aLower, path.bLower, path.aUpper, path.bUpper,
                path.startPose, path.endPose, path.bands, TBarPass.CAP, 1,
            )
            if (root.isEmpty() || cap.isEmpty()) {
                throw IllegalStateException("未能生成打底/盖面焊接段")
            }
            out += taughtMoveL(path.startSafe, 100, toolIndex, extAxis)
            out += passLines(root, processes, isWeld, simulating, speedMode, toolIndex, extAxis, returnToStart = true)
            out += passLines(cap, processes, isWeld, simulating, speedMode, toolIndex, extAxis, returnToStart = false)
            out += taughtMoveL(path.endSafe, 100, toolIndex, extAxis)
        }
        return out
    }

    fun passLines(
        segments: List<TBarSeg>,
        processes: Map<String, WeldProcess>,
        isWelding: Boolean,
        simulating: Boolean,
        speedMode: String,
        toolIndex: Int,
        extAxis: Boolean,
        returnToStart: Boolean,
    ): List<String> {
        val first = segments.first()
        val firstProc = processOf(first, processes)
        val out = mutableListOf<String>()
        out += processParams(firstProc, onlineWeave = false)
        out += ikMoveL(first.startPose, 100, toolIndex, extAxis)
        if (isWelding) out += "ARCStart(0,2,10000)"
        if (firstProc.oscillation.type != "无摆动") out += "WeaveStart(3)"
        segments.forEachIndexed { index, seg ->
            val proc = processOf(seg, processes)
            if (index > 0) out += processParams(proc, onlineWeave = true)
            out += ikMoveL(seg.endPose, weldSpeed(proc, simulating, speedMode), toolIndex, extAxis)
        }
        if (isWelding) out += "ARCEnd(0,2,10000)"
        if (segments.any { processOf(it, processes).oscillation.type != "无摆动" }) {
            out += "WeaveEnd(0)"
        }
        if (returnToStart) out += ikMoveL(first.startPose, 100, toolIndex, extAxis)
        return out
    }

    private fun processOf(seg: TBarSeg, processes: Map<String, WeldProcess>): WeldProcess {
        if (seg.processId.isBlank()) {
            throw IllegalStateException("间隙带 ${TBarRun.bandLabel(seg.band)} 未选工艺")
        }
        return processes[seg.processId]
            ?: throw IllegalStateException("闭包里没有间隙带 ${TBarRun.bandLabel(seg.band)} 的工艺")
    }

    private fun processParams(process: WeldProcess, onlineWeave: Boolean): List<String> {
        val out = mutableListOf(
            "WeldingSetProcessParam(2,${process.startArcCurrent},${process.startArcVoltage},${process.startArcTime},${process.current},${process.voltage},${process.endArcCurrent},${process.endArcVoltage},${process.endArcTime})",
            "WeldingSetCurrent(0,${fmtNum(process.current)},0)",
            "WeldingSetVoltage(0,${fmtNum(process.voltage)},1)",
        )
        val weave = if (onlineWeave) weaveOnline(process) else weaveSet(process)
        if (weave != null) out += weave
        return out
    }

    private fun weaveSet(process: WeldProcess): String? {
        val osc = process.oscillation
        if (osc.type == "无摆动") return null
        val typeCode = weaveType(osc.type)
        val waitTimeCode = if (osc.waitTime == "不包括") 0 else 1
        val posWaitCode = if (osc.positionWait == "等待时间内位置继续移动") 0 else 1
        return "WeaveSetPara(3,$typeCode,${osc.frequency},$waitTimeCode,${osc.amplitude},${osc.leftSideLength},${osc.rightSideLength},${osc.zeroTime},${osc.leftStopTime},${osc.rightStopTime},${osc.callbackRatio},$posWaitCode,${osc.azimuth},${osc.inclination})"
    }

    private fun weaveOnline(process: WeldProcess): String? {
        val osc = process.oscillation
        if (osc.type == "无摆动") return null
        val typeCode = weaveType(osc.type)
        val waitTimeCode = if (osc.waitTime == "不包括") 0 else 1
        val posWaitCode = if (osc.positionWait == "等待时间内位置继续移动") 0 else 1
        return "WeaveOnlineSetPara(3,$typeCode,${fmtNum(osc.frequency)},$waitTimeCode,${fmtNum(osc.amplitude)},${osc.leftStopTime.toInt()},${osc.rightStopTime.toInt()},${osc.callbackRatio.toInt().coerceIn(0, 100)},$posWaitCode)"
    }

    private fun weaveType(type: String): Int = when (type) {
        "三角波摆动" -> 0
        "直角L型三角波摆动" -> 1
        "圆形摆动-顺时针" -> 2
        "圆形摆动-逆时针" -> 3
        "正弦波摆动" -> 4
        "垂直L型正弦波摆动" -> 5
        "立焊三角摆动" -> 6
        else -> 0
    }

    private fun weldSpeed(process: WeldProcess, simulating: Boolean, speedMode: String): Int {
        val multiplier = if (simulating) {
            when (speedMode) {
                "3倍" -> 3
                "5倍" -> 5
                else -> 1
            }
        } else {
            1
        }
        return (process.speed * multiplier).toInt().coerceAtLeast(1)
    }

    private fun taughtMoveL(point: ScriptPoint, speed: Int, toolIndex: Int, extAxis: Boolean): List<String> {
        if (point.joints.size >= 6) {
            val pose = point.pose
            val joints = point.joints.take(6).joinToString(",") { fmt(it) }
            val pos = "$joints,${fmt(pose.x)},${fmt(pose.y)},${fmt(pose.z)},${fmt(pose.rx)},${fmt(pose.ry)},${fmt(pose.rz)}"
            val ext1 = fmt(pose.ext1)
            val out = mutableListOf<String>()
            if (extAxis) out += "ExtAxisMoveJ(1,$ext1,0.000,0.000,0.000,$speed,-1)"
            out += "MoveL($pos,$toolIndex,0,100,100,$speed,-1,0,$ext1,0.000,0.000,0.000,0,0,0,0,0,0,0,0,100,0)"
            return out
        }
        return ikMoveL(point.pose, speed, toolIndex, extAxis)
    }

    private fun ikMoveL(pose: Pose, speed: Int, toolIndex: Int, extAxis: Boolean): List<String> {
        val nx = fmt(pose.x)
        val ny = fmt(pose.y)
        val nz = fmt(pose.z)
        val rx = fmt(pose.rx)
        val ry = fmt(pose.ry)
        val rz = fmt(pose.rz)
        val ext1 = fmt(pose.ext1)
        val out = mutableListOf<String>()
        if (extAxis) out += "ExtAxisMoveJ(1,$ext1,0.000,0.000,0.000,$speed,-1)"
        out += "j1,j2,j3,j4,j5,j6=GetInverseKinExaxis(0,{$nx,$ny,$nz,$rx,$ry,$rz},{$ext1,0.000,0.000,0.000},$toolIndex,0)"
        out += "MoveL(j1,j2,j3,j4,j5,j6,$nx,$ny,$nz,$rx,$ry,$rz,$toolIndex,0,100,100,$speed,-1,0,$ext1,0.000,0.000,0.000,0,0,0,0,0,0,0,0,100,0)"
        return out
    }

    private fun fmt(value: Double): String = String.format(Locale.US, "%.3f", value)

    private fun fmtNum(value: Double): String =
        if (value == value.toLong().toDouble()) value.toLong().toString()
        else String.format(Locale.US, "%.1f", value)
}
