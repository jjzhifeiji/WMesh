package com.gbndt.shijiaoqi.domain.tbar

import com.gbndt.shijiaoqi.geom.Rotation
import com.gbndt.shijiaoqi.geom.Vec3
import com.gbndt.shijiaoqi.model.Pose
import com.gbndt.shijiaoqi.model.single.WeldPath
import com.gbndt.shijiaoqi.model.single.WeldPathSurrogate
import com.gbndt.shijiaoqi.model.single.toSurrogate
import com.gbndt.shijiaoqi.model.single.toWeldPath
import com.gbndt.shijiaoqi.model.tbar.GapBand
import kotlin.math.abs
import kotlin.math.atan2
import kotlin.math.sqrt
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.decodeFromJsonElement
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive

/** 按间隙切出的一段打底或盖面。 */
data class TBarSeg(
    val tStart: Double,
    val tEnd: Double,
    val startPose: Pose,
    val endPose: Pose,
    val gapStart: Double,
    val gapEnd: Double,
    val processId: String,
    val band: GapBand,
)

/** T 排间隙带匹配与 21 点切段；不读标准库文件夹。 */
object TBarRun {
    const val SAMPLES = 21
    const val TEMPLATE_ID = "44444444-4444-4444-8444-444444444444"
    const val KIND = "tbar"

    fun bandLabel(band: GapBand): String {
        val minStr = trimNum(band.minGap)
        val maxStr = trimNum(band.maxGap)
        return "${band.layer}H-$minStr-$maxStr"
    }

    fun processId(band: GapBand, pass: TBarPass): String =
        if (pass == TBarPass.ROOT) band.rootProcessId else band.capProcessId

    /** 先同层 [min,max)，再闭区间，再取最近带。 */
    fun matchBand(gapMm: Double, bands: List<GapBand>, layer: Int = 1): GapBand? {
        val layerItems = bands.filter { it.layer == layer }.sortedBy { it.minGap }
        if (layerItems.isEmpty()) return null
        layerItems.firstOrNull { gapMm >= it.minGap && gapMm < it.maxGap }?.let { return it }
        layerItems.firstOrNull { gapMm >= it.minGap && gapMm <= it.maxGap }?.let { return it }
        return layerItems.minByOrNull { band ->
            when {
                gapMm < band.minGap -> band.minGap - gapMm
                gapMm > band.maxGap -> gapMm - band.maxGap
                else -> 0.0
            }
        }
    }

    fun buildSegments(
        aLower: Pose,
        bLower: Pose,
        aUpper: Pose,
        bUpper: Pose,
        startPose: Pose,
        endPose: Pose,
        bands: List<GapBand>,
        pass: TBarPass,
        layer: Int = 1,
    ): List<TBarSeg> {
        data class Sample(val t: Double, val gap: Double, val band: GapBand)
        val sampled = (0 until SAMPLES).map { i ->
            val t = i / (SAMPLES - 1).toDouble()
            val gap = TBarGeometry.gapAt(aLower, aUpper, bLower, bUpper, t)
            val matched = matchBand(gap, bands, layer)
                ?: throw IllegalStateException("间隙 ${"%.1f".format(gap)} mm 未匹配到 ${layer}H 间隙带")
            Sample(t, gap, matched)
        }
        val segments = mutableListOf<TBarSeg>()
        var i = 0
        while (i < sampled.size) {
            val current = sampled[i]
            var j = i
            while (j + 1 < sampled.size && sampled[j + 1].band == current.band) {
                j++
            }
            val t0 = sampled[i].t
            val t1 = sampled[j].t
            segments.add(
                TBarSeg(
                    tStart = t0,
                    tEnd = t1,
                    startPose = lerpPose(startPose, endPose, t0),
                    endPose = lerpPose(startPose, endPose, t1),
                    gapStart = sampled[i].gap,
                    gapEnd = sampled[j].gap,
                    processId = processId(current.band, pass),
                    band = current.band,
                )
            )
            i = j + 1
        }
        return segments
    }

    private fun trimNum(value: Double): String =
        if (value == value.toLong().toDouble()) value.toLong().toString() else value.toString()

    private fun lerpPose(a: Pose, b: Pose, t: Double): Pose = Pose(
        x = a.x + (b.x - a.x) * t,
        y = a.y + (b.y - a.y) * t,
        z = a.z + (b.z - a.z) * t,
        rx = a.rx + shortestAngle(a.rx, b.rx) * t,
        ry = a.ry + shortestAngle(a.ry, b.ry) * t,
        rz = a.rz + shortestAngle(a.rz, b.rz) * t,
        ext1 = a.ext1 + (b.ext1 - a.ext1) * t,
    )

    private fun shortestAngle(from: Double, to: Double): Double {
        var d = (to - from) % 360.0
        if (d > 180) d -= 360
        if (d < -180) d += 360
        return d
    }
}
enum class TBarPass { ROOT, CAP }

object TBarGeometry {
    fun gapAt(aLower: Pose, aUpper: Pose, bLower: Pose, bUpper: Pose, t: Double): Double {
        val a = pointOnLine(aLower, aUpper, t)
        val b = pointOnLine(bLower, bUpper, t)
        return a.distanceTo(b)
    }

    fun buildWeldPoses(
        aLower: Pose,
        bLower: Pose,
        aUpper: Pose,
        bUpper: Pose,
        startSafe: Pose,
        endSafe: Pose
    ): Pair<Pose, Pose> {
        val startPos = midpoint(aLower, bLower)
        val endPos = midpoint(aUpper, bUpper)
        val fallback = Pose(
            startPos.x, startPos.y, startPos.z,
            startSafe.rx, startSafe.ry, startSafe.rz,
            startSafe.ext1
        ) to Pose(
            endPos.x, endPos.y, endPos.z,
            endSafe.rx, endSafe.ry, endSafe.rz,
            endSafe.ext1
        )
        val travel = endPos - startPos
        val normal = planeNormal(aLower, bLower, aUpper, bUpper) ?: return fallback
        val startPose = computeTorchPose(startPos, normal, travel, startSafe)
        val endPose = computeTorchPose(endPos, normal, travel, endSafe)
        return startPose to endPose
    }

    private fun midpoint(a: Pose, b: Pose): Vec3 {
        return Vec3((a.x + b.x) / 2.0, (a.y + b.y) / 2.0, (a.z + b.z) / 2.0)
    }

    private fun pointOnLine(start: Pose, end: Pose, t: Double): Vec3 {
        return Vec3(
            start.x + (end.x - start.x) * t,
            start.y + (end.y - start.y) * t,
            start.z + (end.z - start.z) * t
        )
    }

    private fun planeNormal(aLower: Pose, bLower: Pose, aUpper: Pose, bUpper: Pose): Vec3? {
        val aL = aLower.toP()
        val bL = bLower.toP()
        val aU = aUpper.toP()
        val bU = bUpper.toP()
        val gap = (bL - aL) + (bU - aU)
        val trav = (aU - aL) + (bU - bL)
        val n = cross(gap, trav)
        val mag = magnitude(n)
        if (mag < 1e-9) {
            val n1 = cross(bL - aL, aU - aL)
            val n2 = cross(bU - aL, aU - aL)
            val n3 = cross(bL - aL, bU - aL)
            val avg = Vec3(n1.x + n2.x + n3.x, n1.y + n2.y + n3.y, n1.z + n2.z + n3.z)
            val m = magnitude(avg)
            if (m < 1e-9) return null
            return scale(avg, 1.0 / m)
        }
        return scale(n, 1.0 / mag)
    }

    /** 位置用中点；姿态从安全点出发，只绕焊缝方向补旋转角，使焊枪垂直四点平面。 */
    private fun computeTorchPose(
        position: Vec3,
        planeNormal: Vec3,
        travel: Vec3,
        safePose: Pose
    ): Pose {
        val fallback = Pose(position.x, position.y, position.z, safePose.rx, safePose.ry, safePose.rz, safePose.ext1)
        val toolZ = toolAxis(safePose, 2)
        val nMag = magnitude(planeNormal)
        val tMag = magnitude(travel)
        if (nMag < 1e-9 || tMag < 1e-9) return fallback
        var n = scale(planeNormal, 1.0 / nMag)
        val t = scale(travel, 1.0 / tMag)
        if (dot(toolZ, n) < 0) n = scale(n, -1.0)

        val zPerp = toolZ - scale(t, dot(toolZ, t))
        val nPerp = n - scale(t, dot(n, t))
        if (magnitude(zPerp) < 1e-6 || magnitude(nPerp) < 1e-6) return fallback
        val zP = normalize(zPerp)
        val nP = normalize(nPerp)
        val angleDeg = Math.toDegrees(atan2(dot(t, cross(zP, nP)), dot(zP, nP)))
        if (!angleDeg.isFinite()) return fallback

        val currentRot = Rotation.eulerToMatrix(safePose.rx, safePose.ry, safePose.rz)
        val newRot = if (abs(angleDeg) > 1e-6) {
            Rotation.multiply(
                Rotation.axisAngleToMatrix(t, angleDeg),
                currentRot
            )
        } else {
            currentRot
        }
        val (rx, ry, rz) = try {
            Rotation.matrixToEulerClosest(newRot, safePose.rx, safePose.ry, safePose.rz)
        } catch (_: Throwable) {
            return fallback
        }
        if (!rx.isFinite() || !ry.isFinite() || !rz.isFinite()) return fallback
        return Pose(position.x, position.y, position.z, rx, ry, rz, safePose.ext1)
    }

    private fun Pose.toP() = Vec3(x, y, z)

    private fun toolAxis(pose: Pose, col: Int): Vec3 {
        val r = Rotation.eulerToMatrix(pose.rx, pose.ry, pose.rz)
        return normalize(Vec3(r[0][col], r[1][col], r[2][col]))
    }

    private operator fun Vec3.plus(other: Vec3) = Vec3(x + other.x, y + other.y, z + other.z)
    private fun dot(a: Vec3, b: Vec3) = a.x * b.x + a.y * b.y + a.z * b.z
    private fun cross(a: Vec3, b: Vec3) = Vec3(
        a.y * b.z - a.z * b.y,
        a.z * b.x - a.x * b.z,
        a.x * b.y - a.y * b.x
    )
    private fun magnitude(v: Vec3) = sqrt(v.x * v.x + v.y * v.y + v.z * v.z)
    private fun scale(v: Vec3, s: Double) = Vec3(v.x * s, v.y * s, v.z * s)
    private fun normalize(v: Vec3): Vec3 {
        val mag = magnitude(v)
        if (mag < 1e-9) return Vec3(0.0, 0.0, 0.0)
        return scale(v, 1.0 / mag)
    }
}
/** 从闭包工程正文解 T 排；只认 T 排模版身份，不把单层 gapBands 当 T 排。 */
object TBarProject {
    private val json = Json {
        ignoreUnknownKeys = true
        isLenient = true
        coerceInputValues = true
        allowSpecialFloatingPointValues = true
    }

    fun parse(bytes: ByteArray): List<WeldPath> {
        val text = bytes.decodeToString()
        if (text.isBlank() || text.trim() == "[]") return emptyList()
        val element = json.parseToJsonElement(text)
        val arr = element as? JsonArray ?: return emptyList()
        return arr.mapNotNull { item ->
            val obj = item.jsonObject
            if (!isTBar(obj)) {
                null
            } else {
                try {
                    json.decodeFromJsonElement<WeldPathSurrogate>(item).toWeldPath()
                } catch (_: Exception) {
                    null
                }
            }
        }
    }

    private fun isTBar(obj: kotlinx.serialization.json.JsonObject): Boolean {
        return try {
            val kind = obj["kind"]?.jsonPrimitive?.contentOrNull
            val templateId = obj["templateId"]?.jsonPrimitive?.contentOrNull
            kind == TBarRun.KIND || templateId == TBarRun.TEMPLATE_ID
        } catch (_: Exception) {
            false
        }
    }

    fun encode(paths: List<WeldPath>): ByteArray =
        json.encodeToString(
            paths.map {
                it.toSurrogate().copy(kind = TBarRun.KIND, templateId = TBarRun.TEMPLATE_ID)
            },
        ).encodeToByteArray()
}
