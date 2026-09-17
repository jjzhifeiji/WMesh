package com.gbndt.shijiaoqi.domain.tbar

import com.gbndt.shijiaoqi.domain.shared.LuaLine
import com.gbndt.shijiaoqi.domain.shared.ScriptPoint
import com.gbndt.shijiaoqi.domain.shared.StopResume
import com.gbndt.shijiaoqi.model.tbar.GapBand
import com.gbndt.shijiaoqi.model.Pose
import com.gbndt.shijiaoqi.model.WeldPoint
import com.gbndt.shijiaoqi.model.WeldPointType
import com.gbndt.shijiaoqi.model.WeldProcess
import com.gbndt.shijiaoqi.domain.tbar.TBarPass
import java.util.Locale
import com.gbndt.shijiaoqi.domain.tbar.TBarRun
import com.gbndt.shijiaoqi.domain.tbar.TBarSeg

/** 一条 T 排焊缝：坡口几何加间隙带，工艺已合进间隙带。 */
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

/** 一条 T 排指令到达的示教点；点位反馈按它回填，非运动指令为 null。 */
enum class TBarPoint { START_SAFE, START, END, END_SAFE }

/**
 * 一行 T 排指令。`point` 非空表示这行 MoveL 到达该示教点，要挂可映射的指令号；
 * `withId` 为假的行（求逆解）不挂号，与老项目一致。
 */
data class TBarLine(
    val text: String,
    val point: TBarPoint? = null,
    val withId: Boolean = true,
    val pathIndex: Int = -1, // 界面焊道
)

/** 转成统一行；点号按焊道里该类型的下标。 */
fun TBarLine.toLuaLine(points: List<WeldPoint> = emptyList()): LuaLine {
    val type = when (point) {
        TBarPoint.START_SAFE -> WeldPointType.START_SAFE
        TBarPoint.START -> WeldPointType.START
        TBarPoint.END -> WeldPointType.END
        TBarPoint.END_SAFE -> WeldPointType.END_SAFE
        null -> null
    }
    val idx = if (type == null) -1 else points.indexOfFirst { it.type == type }
    return LuaLine(text, withId, pathIndex, idx)
}

/** T 排 Lua：起安 → 打底 → 空走回起点 → 盖面 → 终安；段切换 WeaveOnlineSetPara。 */
object TBarLua {
    const val GLOBAL_SPEED = "SetSpeed(10)"

    fun job(
        paths: List<TBarScriptPath>,
        welding: Boolean,
        simulating: Boolean,
        speedMode: String = "1倍",
        toolIndex: Int = 1,
        extAxis: Boolean = false,
        resume: StopResume? = null,
    ): List<TBarLine> {
        val out = mutableListOf(TBarLine(GLOBAL_SPEED))
        val isWeld = welding && !simulating
        for ((pathIndex, path) in paths.withIndex()) {
            if (resume != null && resume.pathIndex >= 0 && pathIndex < resume.pathIndex) continue
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
            val here = resume?.takeIf { it.pathIndex == pathIndex && it.joints.size >= 6 }
            if (here != null) {
                out += taughtMoveL(
                    ScriptPoint(WeldPointType.START, here.pose, here.joints),
                    100, toolIndex, extAxis, TBarPoint.START,
                ).onPath(pathIndex)
            } else {
                out += taughtMoveL(path.startSafe, 100, toolIndex, extAxis, TBarPoint.START_SAFE).onPath(pathIndex)
            }
            out += passLines(root, isWeld, simulating, speedMode, toolIndex, extAxis, returnToStart = true).onPath(pathIndex)
            out += passLines(cap, isWeld, simulating, speedMode, toolIndex, extAxis, returnToStart = false).onPath(pathIndex)
            out += taughtMoveL(path.endSafe, 100, toolIndex, extAxis, TBarPoint.END_SAFE).onPath(pathIndex)
        }
        return out
    }

    private fun List<TBarLine>.onPath(pathIndex: Int) = map { it.copy(pathIndex = pathIndex) }

    fun passLines(
        segments: List<TBarSeg>,
        isWelding: Boolean,
        simulating: Boolean,
        speedMode: String,
        toolIndex: Int,
        extAxis: Boolean,
        returnToStart: Boolean,
    ): List<TBarLine> {
        val first = segments.first()
        val firstProc = processOf(first)
        val out = mutableListOf<TBarLine>()
        out += processParams(firstProc, onlineWeave = false).map { TBarLine(it) }
        out += ikMoveL(first.startPose, 100, toolIndex, extAxis, TBarPoint.START)
        if (isWelding) out += TBarLine("ARCStart(0,2,10000)")
        if (firstProc.oscillation.type != "无摆动") out += TBarLine("WeaveStart(3)")
        segments.forEachIndexed { index, seg ->
            val proc = processOf(seg)
            if (index > 0) out += processParams(proc, onlineWeave = true).map { TBarLine(it) }
            // 最后一段走到终点，其余段的落点仍算在起点上，与老项目的点位回填一致
            val reached = if (index == segments.lastIndex) TBarPoint.END else TBarPoint.START
            out += ikMoveL(seg.endPose, weldSpeed(proc, simulating, speedMode), toolIndex, extAxis, reached)
        }
        if (isWelding) out += TBarLine("ARCEnd(0,2,10000)")
        if (segments.any { processOf(it).oscillation.type != "无摆动" }) {
            out += TBarLine("WeaveEnd(0)")
        }
        if (returnToStart) out += ikMoveL(first.startPose, 100, toolIndex, extAxis, TBarPoint.START)
        return out
    }

    private fun processOf(seg: TBarSeg): WeldProcess {
        if (seg.processId.isBlank()) {
            throw IllegalStateException("间隙带 ${TBarRun.bandLabel(seg.band)} 未选工艺")
        }
        return seg.process
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

    private fun taughtMoveL(
        point: ScriptPoint,
        speed: Int,
        toolIndex: Int,
        extAxis: Boolean,
        reached: TBarPoint?,
    ): List<TBarLine> {
        if (point.joints.size >= 6) {
            val pose = point.pose
            val joints = point.joints.take(6).joinToString(",") { fmt(it) }
            val pos = "$joints,${fmt(pose.x)},${fmt(pose.y)},${fmt(pose.z)},${fmt(pose.rx)},${fmt(pose.ry)},${fmt(pose.rz)}"
            val ext1 = fmt(pose.ext1)
            val out = mutableListOf<TBarLine>()
            if (extAxis) out += TBarLine("ExtAxisMoveJ(1,$ext1,0.000,0.000,0.000,$speed,-1)")
            out += TBarLine(
                "MoveL($pos,$toolIndex,0,100,100,$speed,-1,0,$ext1,0.000,0.000,0.000,0,0,0,0,0,0,0,0,100,0)",
                point = reached,
            )
            return out
        }
        return ikMoveL(point.pose, speed, toolIndex, extAxis, reached)
    }

    private fun ikMoveL(
        pose: Pose,
        speed: Int,
        toolIndex: Int,
        extAxis: Boolean,
        reached: TBarPoint?,
    ): List<TBarLine> {
        val nx = fmt(pose.x)
        val ny = fmt(pose.y)
        val nz = fmt(pose.z)
        val rx = fmt(pose.rx)
        val ry = fmt(pose.ry)
        val rz = fmt(pose.rz)
        val ext1 = fmt(pose.ext1)
        val out = mutableListOf<TBarLine>()
        if (extAxis) out += TBarLine("ExtAxisMoveJ(1,$ext1,0.000,0.000,0.000,$speed,-1)")
        // 求逆解那行老项目不挂指令号
        out += TBarLine(
            "j1,j2,j3,j4,j5,j6=GetInverseKinExaxis(0,{$nx,$ny,$nz,$rx,$ry,$rz},{$ext1,0.000,0.000,0.000},$toolIndex,0)",
            withId = false,
        )
        out += TBarLine(
            "MoveL(j1,j2,j3,j4,j5,j6,$nx,$ny,$nz,$rx,$ry,$rz,$toolIndex,0,100,100,$speed,-1,0,$ext1,0.000,0.000,0.000,0,0,0,0,0,0,0,0,100,0)",
            point = reached,
        )
        return out
    }

    private fun fmt(value: Double): String = String.format(Locale.US, "%.3f", value)

    private fun fmtNum(value: Double): String =
        if (value == value.toLong().toDouble()) value.toLong().toString()
        else String.format(Locale.US, "%.1f", value)
}
