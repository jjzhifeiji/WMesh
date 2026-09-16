package com.gbndt.shijiaoqi.domain.script

import com.gbndt.shijiaoqi.domain.weld.OffsetSite
import com.gbndt.shijiaoqi.domain.weld.WeldOffset
import com.gbndt.shijiaoqi.domain.weld.WeldProgress
import com.gbndt.shijiaoqi.model.Pose
import com.gbndt.shijiaoqi.model.WeldPointType
import com.gbndt.shijiaoqi.model.WeldProcess
import java.util.Locale
import kotlin.math.abs

/** 一条单层焊道上的点：类型、位姿、关节；offsets 空则按工艺现算。 */
data class ScriptPoint(
    val type: WeldPointType,
    val pose: Pose,
    val joints: List<Double> = listOf(0.0, 0.0, 0.0, 0.0, 0.0, 0.0),
    val refX: Pose? = null, // X 向参考
    val offsets: List<Double>? = null, // 预计算六元组；null 按工艺算，空列表表示无
)

/** 一条焊道：主工艺加附加工艺，几何共用。 */
data class ScriptPath(
    val points: List<ScriptPoint>,
    val process: WeldProcess,
    val extras: List<WeldProcess> = emptyList(),
    val enabled: Boolean = true,
    val uiIndex: Int = -1, // 界面焊道下标；-1 用遍历下标
    val passTag: Int = -1, // 多层道次；底道 -1
)

/** 单层 Lua：全局速度、工艺参数、点运动后再起弧；模拟不加起收弧。 */
object SingleLayerLua {
    const val GLOBAL_SPEED = "SetSpeed(10)"

    /** 组 Lua 正文。七轴插导轨指令，有偏移走 flag=3 或逆解。 */
    fun job(
        paths: List<ScriptPath>,
        welding: Boolean,
        simulating: Boolean,
        speedMode: String = "1倍",
        toolIndex: Int = 1,
        extAxis: Boolean = false,
        resume: StopResume? = null,
    ): List<LuaLine> {
        val out = mutableListOf(LuaLine(GLOBAL_SPEED))
        var found = resume == null
        for ((pathIndex, path) in paths.withIndex()) {
            val ui = if (path.uiIndex >= 0) path.uiIndex else pathIndex
            if (!path.enabled && path.extras.isEmpty()) continue
            val items = ArrayList<Triple<Int, WeldProcess, StopResume?>>()
            if (path.enabled) items += Triple(path.passTag, path.process, null)
            for ((extraIdx, extra) in path.extras.withIndex()) items += Triple(extraIdx, extra, null)
            for ((tag, process, _) in items) {
                val hit = resume != null && ui == resume.pathIndex && tag == resume.extraIndex
                if (!found) {
                    if (!hit) continue
                    found = true
                }
                out += pathLines(
                    path.points, process, welding && !simulating, simulating,
                    speedMode, toolIndex, extAxis, ui, tag, resume.takeIf { hit },
                )
            }
        }
        return out
    }

    /** 一条焊道的点序列。附加工艺各自重算偏移。 */
    fun pathLines(
        points: List<ScriptPoint>,
        process: WeldProcess,
        isWelding: Boolean,
        simulating: Boolean,
        speedMode: String,
        toolIndex: Int,
        extAxis: Boolean = false,
        pathIndex: Int = -1,
        extraIndex: Int = -1,
        resume: StopResume? = null,
    ): List<LuaLine> {
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
        val sites = points.map { OffsetSite(it.type, it.pose, it.refX) }
        val out = mutableListOf<LuaLine>()
        fun tag(line: LuaLine) = line.copy(pathIndex = pathIndex, extraIndex = extraIndex)
        fun add(text: String, point: Int = -1, withId: Boolean = true) {
            out += tag(LuaLine(text, withId, pathIndex, point, extraIndex))
        }
        add("WeldingSetProcessParam(2,${process.startArcCurrent},${process.startArcVoltage},${process.startArcTime},${process.current},${process.voltage},${process.endArcCurrent},${process.endArcVoltage},${process.endArcTime})")
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
            add("WeaveSetPara(3,$typeCode,${o.frequency},$waitTimeCode,${o.amplitude},${o.leftSideLength},${o.rightSideLength},${o.zeroTime},${o.leftStopTime},${o.rightStopTime},${o.callbackRatio},$posWaitCode,${o.azimuth},${o.inclination})")
        }
        var hasStartedArc = false
        var i = 0
        if (resume != null && resume.joints.size >= 6) {
            val stopLines = resumeHead(
                points, process, sites, resume, weldSpeed, toolIndex, extAxis, isWelding, oscType,
            )
            stopLines.forEach { out += tag(it.copy(pathIndex = pathIndex, extraIndex = extraIndex)) }
            i = stopLines.consumeUntil
            hasStartedArc = stopLines.arcOn
        }
        while (i < points.size) {
            val point = points[i]
            if (point.type == WeldPointType.ARC_MIDDLE && i + 1 < points.size) {
                val next = points[i + 1]
                moveC(i, points, sites, process, weldSpeed, toolIndex, extAxis).forEach { out += tag(it) }
                if (!hasStartedArc && oscType != "无摆动") add("WeaveStart(3)")
                if (next.type == WeldPointType.END) {
                    if (isWelding) add("ARCEnd(0,2,10000)")
                    if (oscType != "无摆动") add("WeaveEnd(0)")
                }
                i += 2
                continue
            }
            val moveSpeed = if (
                point.type == WeldPointType.START_SAFE ||
                point.type == WeldPointType.END_SAFE ||
                point.type == WeldPointType.START
            ) 100 else weldSpeed
            moveL(i, points, sites, process, moveSpeed, toolIndex, extAxis).forEach { out += tag(it) }
            if (point.type == WeldPointType.START && !hasStartedArc) {
                if (isWelding) add("ARCStart(0,2,10000)")
                if (oscType != "无摆动") add("WeaveStart(3)")
                hasStartedArc = true
            } else if (point.type == WeldPointType.END) {
                if (isWelding) add("ARCEnd(0,2,10000)")
                if (oscType != "无摆动") add("WeaveEnd(0)")
            }
            i++
        }
        return out
    }

    private class ResumeHead(val lines: List<LuaLine>, val consumeUntil: Int, val arcOn: Boolean) {
        inline fun forEach(block: (LuaLine) -> Unit) = lines.forEach(block)
    }

    private fun resumeHead(
        points: List<ScriptPoint>,
        process: WeldProcess,
        sites: List<OffsetSite>,
        resume: StopResume,
        weldSpeed: Int,
        toolIndex: Int,
        extAxis: Boolean,
        isWelding: Boolean,
        oscType: String,
        pathIndex: Int = -1,
        extraIndex: Int = -1,
    ): ResumeHead {
        val out = mutableListOf<LuaLine>()
        val pose = resume.pose
        val joints = resume.joints
        val ext1 = fmt(pose.ext1)
        val pos2 = posStr(pose, joints)
        if (extAxis) {
            out += LuaLine("ExtAxisMoveJ(1,$ext1,0.000,0.000,0.000,100,-1)")
        }
        out += LuaLine(
            "MoveL($pos2,$toolIndex,0,100,100,100,-1,0,$ext1,0.000,0.000,0.000,0,0,0,0,0,0,0,0,100,0)",
            pointIndex = resume.pointIndex.coerceAtLeast(0),
        )
        val startIdx = points.indexOfFirst { it.type == WeldPointType.START }
        var arcOn = false
        if (resume.pointIndex > startIdx && startIdx != -1) {
            if (isWelding) out += LuaLine("ARCStart(0,2,10000)")
            if (oscType != "无摆动") out += LuaLine("WeaveStart(3)")
            arcOn = true
        }
        var pointIndex = resume.pointIndex.coerceAtLeast(0)
        val safeIndex = pointIndex
        var isArcResume = false
        var arcStartIndex = -1
        when {
            safeIndex > 0 && safeIndex < points.size &&
                points[safeIndex].type == WeldPointType.END &&
                points[safeIndex - 1].type == WeldPointType.ARC_MIDDLE -> {
                arcStartIndex = safeIndex - 2
                isArcResume = true
            }
            safeIndex > 0 && safeIndex < points.size &&
                points[safeIndex].type == WeldPointType.ARC_MIDDLE &&
                points[safeIndex - 1].type == WeldPointType.START -> {
                arcStartIndex = safeIndex - 1
                isArcResume = true
            }
            safeIndex < points.size - 2 &&
                points[safeIndex].type == WeldPointType.START &&
                points[safeIndex + 1].type == WeldPointType.ARC_MIDDLE &&
                points[safeIndex + 2].type == WeldPointType.END -> {
                isArcResume = false
            }
            else -> {
                if (safeIndex < points.size && points[safeIndex].type == WeldPointType.ARC_MIDDLE &&
                    safeIndex > 0 && points[safeIndex - 1].type == WeldPointType.START
                ) {
                    arcStartIndex = safeIndex - 1
                    isArcResume = true
                } else if (safeIndex < points.size && points[safeIndex].type == WeldPointType.END &&
                    safeIndex > 1 && points[safeIndex - 1].type == WeldPointType.ARC_MIDDLE
                ) {
                    arcStartIndex = safeIndex - 2
                    isArcResume = true
                }
            }
        }
        if (isArcResume && arcStartIndex >= 0 && arcStartIndex + 2 < points.size) {
            val pStart = points[arcStartIndex]
            val pMid = points[arcStartIndex + 1]
            val pEnd = points[arcStartIndex + 2]
            val (midOff, endOff) = WeldOffset.arc(arcStartIndex + 1, sites, process, flipX = extAxis)
            val physStartOff = WeldOffset.at(arcStartIndex, sites, process, flipX = extAxis)
            val physStart = pStart.pose.copy(
                x = pStart.pose.x + physStartOff.first,
                y = pStart.pose.y + physStartOff.second,
                z = pStart.pose.z + physStartOff.third,
            )
            val physMid = pMid.pose.copy(
                x = pMid.pose.x + midOff.first,
                y = pMid.pose.y + midOff.second,
                z = pMid.pose.z + midOff.third,
            )
            val physEnd = pEnd.pose.copy(
                x = pEnd.pose.x + endOff.first,
                y = pEnd.pose.y + endOff.second,
                z = pEnd.pose.z + endOff.third,
            )
            val newMid = WeldProgress.newArcMid(physStart, physMid, physEnd, pose)
            val newJoints = WeldProgress.midJoints(joints, pEnd.joints)
            val midPosStr = posStr(newMid, newJoints)
            val endPosStr = posStr(pEnd.pose, pEnd.joints)
            val midOffsetStr = "1,0.000,0.000,0.000,0.000,0.000,0.000"
            val endOffsetStr = if (endOff.first == 0.0 && endOff.second == 0.0 && endOff.third == 0.0) {
                "0,0,0,0,0,0,0"
            } else {
                "3,${fmt(endOff.first)},${fmt(endOff.second)},${fmt(endOff.third)},0,0,0"
            }
            if (extAxis) {
                out += LuaLine("ExtAxisMoveJ(1,${fmt(pEnd.pose.ext1)},0.000,0.000,0.000,$weldSpeed,-1)")
            }
            out += LuaLine(
                "MoveC($midPosStr,$toolIndex,0,100,100,0,0,0,0,$midOffsetStr,$endPosStr,$toolIndex,0,100,100,0,0,0,0,$endOffsetStr,$weldSpeed,-1)",
                pointIndex = arcStartIndex + 2,
            )
            if (pEnd.type == WeldPointType.END) {
                if (isWelding) out += LuaLine("ARCEnd(0,2,10000)")
                if (oscType != "无摆动") out += LuaLine("WeaveEnd(0)")
            }
            return ResumeHead(out, arcStartIndex + 3, arcOn)
        }
        return ResumeHead(out, pointIndex, arcOn)
    }

    private fun six(
        i: Int,
        points: List<ScriptPoint>,
        sites: List<OffsetSite>,
        process: WeldProcess,
        extAxis: Boolean,
    ): List<Double> {
        val p = points[i]
        if (p.offsets != null) {
            return if (p.offsets.size >= 6) p.offsets.take(6) else List(6) { 0.0 }
        }
        val (x, y, z) = WeldOffset.at(i, sites, process, flipX = extAxis)
        return listOf(x, y, z, 0.0, 0.0, 0.0)
    }

    private fun nonzero(off: List<Double>): Boolean = off.any { abs(it) >= 1e-5 }

    private fun userParams(off: List<Double>): String {
        if (!nonzero(off)) return "0,0,0,0,0,0,0"
        return "3,${fmt(off[0])},${fmt(off[1])},${fmt(off[2])},${fmt(off[3])},${fmt(off[4])},${fmt(off[5])}"
    }

    private fun posStr(pose: Pose, joints: List<Double>): String {
        val j = joints6(joints).joinToString(",") { fmt(it) }
        val c = listOf(pose.x, pose.y, pose.z, pose.rx, pose.ry, pose.rz).joinToString(",") { fmt(it) }
        return "$j,$c"
    }

    private fun bake(pose: Pose, off: List<Double>, rotate: Boolean): Pose {
        val rx = if (rotate && off.size >= 6) pose.rx + off[3] else pose.rx
        val ry = if (rotate && off.size >= 6) pose.ry + off[4] else pose.ry
        val rz = if (rotate && off.size >= 6) pose.rz + off[5] else pose.rz
        return pose.copy(
            x = pose.x + off.getOrElse(0) { 0.0 },
            y = pose.y + off.getOrElse(1) { 0.0 },
            z = pose.z + off.getOrElse(2) { 0.0 },
            rx = rx,
            ry = ry,
            rz = rz,
        )
    }

    private fun moveL(
        i: Int,
        points: List<ScriptPoint>,
        sites: List<OffsetSite>,
        process: WeldProcess,
        speed: Int,
        toolIndex: Int,
        extAxis: Boolean,
    ): List<LuaLine> {
        val point = points[i]
        val pose = point.pose
        val off = six(i, points, sites, process, extAxis)
        val hasOffset = nonzero(off)
        val joints = joints6(point.joints)
        val ext1 = fmt(pose.ext1)
        val out = mutableListOf<LuaLine>()
        if (extAxis) {
            out += LuaLine("ExtAxisMoveJ(1,$ext1,0.000,0.000,0.000,$speed,-1)")
        }
        // 七轴加偏移走逆解，避免控制器 offset_flag≠0 报 112。
        if (hasOffset && extAxis && joints.size >= 6) {
            val baked = bake(pose, off, rotate = point.offsets != null)
            val nx = fmt(baked.x)
            val ny = fmt(baked.y)
            val nz = fmt(baked.z)
            val rx = fmt(baked.rx)
            val ry = fmt(baked.ry)
            val rz = fmt(baked.rz)
            out += LuaLine(
                "j1,j2,j3,j4,j5,j6=GetInverseKinExaxis(0,{$nx,$ny,$nz,$rx,$ry,$rz},{$ext1,0.000,0.000,0.000},$toolIndex,0)",
                withId = false,
            )
            out += LuaLine(
                "MoveL(j1,j2,j3,j4,j5,j6,$nx,$ny,$nz,$rx,$ry,$rz,$toolIndex,0,100,100,$speed,-1,0,$ext1,0.000,0.000,0.000,0,0,0,0,0,0,0,0,100,0)",
                pointIndex = i,
            )
            return out
        }
        val pos2 = posStr(pose, joints)
        out += LuaLine(
            "MoveL($pos2,$toolIndex,0,100,100,$speed,-1,0,$ext1,0.000,0.000,0.000,0,${userParams(off)},100,0)",
            pointIndex = i,
        )
        return out
    }

    private fun moveC(
        i: Int,
        points: List<ScriptPoint>,
        sites: List<OffsetSite>,
        process: WeldProcess,
        speed: Int,
        toolIndex: Int,
        extAxis: Boolean,
    ): List<LuaLine> {
        val mid = points[i]
        val end = points[i + 1]
        val midOff: List<Double>
        val endOff: List<Double>
        if (mid.offsets != null || end.offsets != null) {
            midOff = six(i, points, sites, process, extAxis)
            endOff = six(i + 1, points, sites, process, extAxis)
        } else {
            val (m, e) = WeldOffset.arc(i, sites, process, flipX = extAxis)
            midOff = listOf(m.first, m.second, m.third, 0.0, 0.0, 0.0)
            endOff = listOf(e.first, e.second, e.third, 0.0, 0.0, 0.0)
        }
        return moveCLines(mid, end, midOff, endOff, speed, toolIndex, extAxis, i + 1)
    }

    private fun moveCLines(
        mid: ScriptPoint,
        end: ScriptPoint,
        midOff: List<Double>,
        endOff: List<Double>,
        speed: Int,
        toolIndex: Int,
        extAxis: Boolean,
        endIndex: Int,
    ): List<LuaLine> {
        val ext1 = fmt(end.pose.ext1)
        val out = mutableListOf<LuaLine>()
        if (extAxis) {
            out += LuaLine("ExtAxisMoveJ(1,$ext1,0.000,0.000,0.000,$speed,-1)")
        }
        val exec = mid.offsets != null || end.offsets != null
        if (extAxis && exec && (nonzero(midOff) || nonzero(endOff))) {
            val bakedMid = bake(mid.pose, midOff, rotate = true)
            val bakedEnd = bake(end.pose, endOff, rotate = true)
            val mx = fmt(bakedMid.x)
            val my = fmt(bakedMid.y)
            val mz = fmt(bakedMid.z)
            val mrx = fmt(bakedMid.rx)
            val mry = fmt(bakedMid.ry)
            val mrz = fmt(bakedMid.rz)
            val ex = fmt(bakedEnd.x)
            val ey = fmt(bakedEnd.y)
            val ez = fmt(bakedEnd.z)
            val erx = fmt(bakedEnd.rx)
            val ery = fmt(bakedEnd.ry)
            val erz = fmt(bakedEnd.rz)
            val extMid = fmt(bakedMid.ext1)
            val extEnd = fmt(bakedEnd.ext1)
            out += LuaLine(
                "m1,m2,m3,m4,m5,m6=GetInverseKinExaxis(0,{$mx,$my,$mz,$mrx,$mry,$mrz},{$extMid,0.000,0.000,0.000},$toolIndex,0)",
                withId = false,
            )
            out += LuaLine(
                "e1,e2,e3,e4,e5,e6=GetInverseKinExaxis(0,{$ex,$ey,$ez,$erx,$ery,$erz},{$extEnd,0.000,0.000,0.000},$toolIndex,0)",
                withId = false,
            )
            out += LuaLine(
                "MoveC(m1,m2,m3,m4,m5,m6,$mx,$my,$mz,$mrx,$mry,$mrz,$toolIndex,0,100,100,0,0,0,0,0,0,0,0,0,0,0,e1,e2,e3,e4,e5,e6,$ex,$ey,$ez,$erx,$ery,$erz,$toolIndex,0,100,100,0,0,0,0,0,0,0,0,0,0,0,$speed,-1)",
                pointIndex = endIndex,
            )
            return out
        }
        val midPos = posStr(mid.pose, mid.joints)
        val endPos = posStr(end.pose, end.joints)
        out += LuaLine(
            "MoveC($midPos,$toolIndex,0,100,100,0,0,0,0,${userParams(midOff)},$endPos,$toolIndex,0,100,100,0,0,0,0,${userParams(endOff)},$speed,-1)",
            pointIndex = endIndex,
        )
        return out
    }

    private fun joints6(raw: List<Double>): List<Double> =
        if (raw.size >= 6) raw.take(6) else List(6) { raw.getOrElse(it) { 0.0 } }

    private fun fmt(v: Double): String = String.format(Locale.US, "%.3f", v)
}
