package com.gbndt.shijiaoqi.domain.multilayer

import com.gbndt.shijiaoqi.domain.shared.LuaLine
import com.gbndt.shijiaoqi.domain.shared.ScriptPath
import com.gbndt.shijiaoqi.domain.shared.ScriptPoint
import com.gbndt.shijiaoqi.domain.shared.StopResume
import com.gbndt.shijiaoqi.domain.single.SingleLayerLua
import com.gbndt.shijiaoqi.model.Pose
import com.gbndt.shijiaoqi.model.WeldPointType
import com.gbndt.shijiaoqi.model.multilayer.MultiLayerWeldPath
import com.gbndt.shijiaoqi.model.multilayer.MultiLayerWeldPathSurrogate
import com.gbndt.shijiaoqi.model.multilayer.toMultiLayerWeldPath
import com.gbndt.shijiaoqi.model.multilayer.toSurrogate
import kotlin.math.*
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.decodeFromJsonElement
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive

data class DcddPoint(val x: Double, val y: Double, val z: Double)

class DcddCoordinateSystem(val origin: DcddPoint, val xAxis: DcddPoint, val zAxis: DcddPoint) {
    private fun vector(p1: DcddPoint, p2: DcddPoint): DcddPoint = DcddPoint(p2.x - p1.x, p2.y - p1.y, p2.z - p1.z)
    private fun magnitude(v: DcddPoint): Double = sqrt(v.x * v.x + v.y * v.y + v.z * v.z)
    private fun normalize(v: DcddPoint): DcddPoint {
        val mag = magnitude(v)
        return DcddPoint(v.x / mag, v.y / mag, v.z / mag)
    }
    private fun crossProduct(v1: DcddPoint, v2: DcddPoint): DcddPoint =
        DcddPoint(v1.y * v2.z - v1.z * v2.y, v1.z * v2.x - v1.x * v2.z, v1.x * v2.y - v1.y * v2.x)

    private fun rotationMatrix(axis: DcddPoint, angle: Double): Array<DoubleArray> {
        val c = cos(angle)
        val s = sin(angle)
        val t = 1 - c
        val x = axis.x
        val y = axis.y
        val z = axis.z

        return arrayOf(
            doubleArrayOf(t * x * x + c, t * x * y - s * z, t * x * z + s * y),
            doubleArrayOf(t * x * y + s * z, t * y * y + c, t * y * z - s * x),
            doubleArrayOf(t * x * z - s * y, t * y * z + s * x, t * z * z + c)
        )
    }

    private fun applyRotation(v: DcddPoint, matrix: Array<DoubleArray>): DcddPoint =
        DcddPoint(matrix[0][0] * v.x + matrix[0][1] * v.y + matrix[0][2] * v.z,
              matrix[1][0] * v.x + matrix[1][1] * v.y + matrix[1][2] * v.z,
              matrix[2][0] * v.x + matrix[2][1] * v.y + matrix[2][2] * v.z)

    fun rotateBAByAngle(angleDegrees: Double): Pair<DcddPoint, Triple<Double, Double, Double>> {
        val angleRadians = Math.toRadians(angleDegrees)
        val ab = vector(origin, xAxis)
        val ac = vector(origin, zAxis)
        val axis = normalize(crossProduct(ab, ac))
        val rotMatrix = rotationMatrix(axis, angleRadians)
        val newAb = applyRotation(ab, rotMatrix)
        val newB = DcddPoint(newAb.x + origin.x, newAb.y + origin.y, newAb.z + origin.z)
        val eulerAngles = rotationMatrixToEulerAngles(rotMatrix)
        return Pair(newB, eulerAngles)
    }

    private fun rotationMatrixToEulerAngles(R: Array<DoubleArray>): Triple<Double, Double, Double> {
        val sy = sqrt(R[0][0] * R[0][0] + R[1][0] * R[1][0])
        val singular = sy < 1e-6

        val x = atan2(-R[1][2], R[2][2])
        val y = atan2(sy, R[2][0])
        val z = if (!singular) atan2(R[0][1], R[0][0]) else 0.0

        return Triple(x, y, z)
    }
}
/** 多层交错下发与无参考点时的层偏移；有参考点的几何在 MultiLayerPass。 */
object MultiLayerRun {
    data class Step(val pathIndex: Int, val passIndex: Int)

    fun interleave(
        pathPassCounts: List<Int>,
        enabled: (pathIndex: Int, passIndex: Int) -> Boolean,
    ): List<Step> {
        if (pathPassCounts.isEmpty()) return emptyList()
        val maxPasses = pathPassCounts.maxOrNull() ?: 0
        val out = mutableListOf<Step>()
        for (layer in -1 until maxPasses) {
            pathPassCounts.indices.forEach { mIndex ->
                if (layer == -1) {
                    if (enabled(mIndex, -1)) out += Step(mIndex, -1)
                } else if (layer < pathPassCounts[mIndex] && enabled(mIndex, layer)) {
                    out += Step(mIndex, layer)
                }
            }
        }
        return out
    }

    fun simpleOffset(
        pose: Pose,
        type: WeldPointType,
        atStart: Boolean,
        atEnd: Boolean,
        valX: Double,
        valYLeft: Double,
        valYRight: Double,
        valZ: Double,
    ): Pose {
        if (type == WeldPointType.START_SAFE || type == WeldPointType.END_SAFE) return pose
        val y = when {
            atStart -> valYLeft
            atEnd -> valYRight
            else -> 0.0
        }
        return pose.copy(x = pose.x + valX, y = pose.y + y, z = pose.z + valZ)
    }
}
/** 从闭包工程正文解多层焊缝；只认 processId。 */
object MultiLayerProject {
    private val json = Json {
        ignoreUnknownKeys = true
        isLenient = true
        coerceInputValues = true
        allowSpecialFloatingPointValues = true
    }

    fun parse(bytes: ByteArray): List<MultiLayerWeldPath> {
        val text = bytes.decodeToString()
        if (text.isBlank() || text.trim() == "[]") return emptyList()
        val element = json.parseToJsonElement(text)
        val arr = element as? JsonArray ?: return emptyList()
        return arr.mapNotNull { item ->
            val obj = item.jsonObject
            val kind = obj["kind"]?.jsonPrimitive?.contentOrNull
            if (!obj.containsKey("basePath") && kind != "multi") {
                null
            } else {
                try {
                    json.decodeFromJsonElement<MultiLayerWeldPathSurrogate>(item).toMultiLayerWeldPath()
                } catch (_: Exception) {
                    null
                }
            }
        }
    }

    fun encode(paths: List<MultiLayerWeldPath>): ByteArray =
        json.encodeToString(paths.map { it.toSurrogate() }).encodeToByteArray()
}
/** 多层 Lua：层间交错后每条焊道走单层点序列。不管采点。 */
object MultiLayerLua {
    /** 交错后组 Lua；有未采点返回 null。 */
    fun job(
        paths: List<MultiLayerWeldPath>,
        welding: Boolean,
        simulating: Boolean,
        speedMode: String = "1倍",
        toolIndex: Int = 1,
        extAxis: Boolean = false,
        resume: StopResume? = null,
    ): List<LuaLine>? {
        val scripts = toScripts(paths) ?: return null
        if (scripts.isEmpty()) return emptyList()
        return SingleLayerLua.job(scripts, welding, simulating, speedMode, toolIndex, extAxis, resume)
    }

    /** 按层交错展开成可下发焊道；缺位姿返回 null。 */
    fun toScripts(paths: List<MultiLayerWeldPath>): List<ScriptPath>? {
        val counts = paths.map { it.passes.size }
        val steps = MultiLayerRun.interleave(counts) { i, layer ->
            val p = paths[i]
            if (layer == -1) p.basePath.isEnabled else p.passes[layer].isEnabled
        }
        val out = ArrayList<ScriptPath>(steps.size)
        for (step in steps) {
            val mp = paths[step.pathIndex]
            if (step.passIndex == -1) {
                val pts = ArrayList<ScriptPoint>(mp.basePath.points.size)
                for (p in mp.basePath.points) {
                    val pose = p.pose ?: return null
                    pts += ScriptPoint(
                        p.type,
                        pose,
                        p.jointAngles ?: List(6) { 0.0 },
                        offsets = emptyList(),
                    )
                }
                out += ScriptPath(
                    points = pts,
                    process = mp.basePath.process,
                    uiIndex = step.pathIndex,
                    passTag = -1,
                )
            } else {
                val pts = MultiLayerPass.generate(mp.basePath, mp.passes[step.passIndex], mp) ?: return null
                out += ScriptPath(
                    points = pts.map {
                        ScriptPoint(it.type, it.pose, it.joints, offsets = it.offsets ?: emptyList())
                    },
                    process = mp.passes[step.passIndex].process,
                    uiIndex = step.pathIndex,
                    passTag = step.passIndex,
                )
            }
        }
        return out
    }
}
