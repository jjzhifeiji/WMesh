package com.gbndt.shijiaoqi.utils

import com.gbndt.shijiaoqi.data.models.Pose
import com.gbndt.shijiaoqi.data.models.WeldProcess
import kotlin.math.abs
import kotlin.math.atan2
import kotlin.math.sqrt

data class TBarGapFolder(
    val name: String,
    val layer: Int,
    val minGap: Double,
    val maxGap: Double,
    val folderPath: String,
    val rootProcess: WeldProcess,
    val rootPath: String,
    val capProcess: WeldProcess,
    val capPath: String
) {
    fun displayLabel(): String {
        val minStr = if (minGap == minGap.toLong().toDouble()) minGap.toLong().toString() else minGap.toString()
        val maxStr = if (maxGap == maxGap.toLong().toDouble()) maxGap.toLong().toString() else maxGap.toString()
        return "$name ($minStr~$maxStr mm)"
    }
}

enum class TBarPass { ROOT, CAP }

data class TBarSegment(
    val tStart: Double,
    val tEnd: Double,
    val startPose: Pose,
    val endPose: Pose,
    val gapStart: Double,
    val gapEnd: Double,
    val process: WeldProcess,
    val folderName: String
)

object TBarGeometry {
    const val PROCESS_FOLDER_NAME = "6T1.2-T排立对接"
    const val PROCESS_FOLDER = "Standard/6T1.2-T排立对接"
    private const val NAME_REGEX = """^(\d+)H-(\d+(?:\.\d+)?)-(\d+(?:\.\d+)?)"""

    fun parseGapProcessName(fileName: String): Triple<Int, Double, Double>? {
        val match = Regex(NAME_REGEX).find(fileName) ?: return null
        val layer = match.groupValues[1].toInt()
        val minGap = match.groupValues[2].toDouble()
        val maxGap = match.groupValues[3].toDouble()
        return Triple(layer, minGap, maxGap)
    }

    fun matchFolder(gapMm: Double, folders: List<TBarGapFolder>, layer: Int = 1): TBarGapFolder? {
        val layerItems = folders.filter { it.layer == layer }.sortedBy { it.minGap }
        if (layerItems.isEmpty()) return null
        layerItems.firstOrNull { gapMm >= it.minGap && gapMm < it.maxGap }?.let { return it }
        layerItems.firstOrNull { gapMm >= it.minGap && gapMm <= it.maxGap }?.let { return it }
        return layerItems.minByOrNull { folder ->
            when {
                gapMm < folder.minGap -> folder.minGap - gapMm
                gapMm > folder.maxGap -> gapMm - folder.maxGap
                else -> 0.0
            }
        }
    }

    fun midpoint(a: Pose, b: Pose): Point3D {
        return Point3D((a.x + b.x) / 2.0, (a.y + b.y) / 2.0, (a.z + b.z) / 2.0)
    }

    fun pointOnLine(start: Pose, end: Pose, t: Double): Point3D {
        return Point3D(
            start.x + (end.x - start.x) * t,
            start.y + (end.y - start.y) * t,
            start.z + (end.z - start.z) * t
        )
    }

    fun gapAt(aLower: Pose, aUpper: Pose, bLower: Pose, bUpper: Pose, t: Double): Double {
        val a = pointOnLine(aLower, aUpper, t)
        val b = pointOnLine(bLower, bUpper, t)
        return a.distanceTo(b)
    }

    fun planeNormal(aLower: Pose, bLower: Pose, aUpper: Pose, bUpper: Pose): Point3D? {
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
            val avg = Point3D(n1.x + n2.x + n3.x, n1.y + n2.y + n3.y, n1.z + n2.z + n3.z)
            val m = magnitude(avg)
            if (m < 1e-9) return null
            return scale(avg, 1.0 / m)
        }
        return scale(n, 1.0 / mag)
    }

    /**
     * 位置用中点；姿态从安全点出发，只绕焊缝方向补旋转角，使焊枪垂直四点平面。
     * 前倾角、摆动 X 都沿用安全点，欧拉贴近原姿态，避免整套重建工具轴造成歧义位姿。
     */
    fun computeTorchPose(
        position: Point3D,
        planeNormal: Point3D,
        travel: Point3D,
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

        val currentRot = CoordinateUtils.eulerToRotationMatrix(safePose.rx, safePose.ry, safePose.rz)
        val newRot = if (abs(angleDeg) > 1e-6) {
            CoordinateUtils.multiplyMatrices(
                CoordinateUtils.axisAngleToRotationMatrix(t, angleDeg),
                currentRot
            )
        } else {
            currentRot
        }
        val (rx, ry, rz) = try {
            CoordinateUtils.rotationMatrixToEulerClosest(newRot, safePose.rx, safePose.ry, safePose.rz)
        } catch (_: Throwable) {
            return fallback
        }
        if (!rx.isFinite() || !ry.isFinite() || !rz.isFinite()) return fallback
        return Pose(position.x, position.y, position.z, rx, ry, rz, safePose.ext1)
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

    fun buildSegments(
        aLower: Pose,
        bLower: Pose,
        aUpper: Pose,
        bUpper: Pose,
        startPose: Pose,
        endPose: Pose,
        folders: List<TBarGapFolder>,
        pass: TBarPass,
        layer: Int = 1
    ): List<TBarSegment> {
        val samples = 21
        data class Sample(val t: Double, val gap: Double, val folder: TBarGapFolder)
        val sampled = (0 until samples).map { i ->
            val t = i / (samples - 1).toDouble()
            val gap = gapAt(aLower, aUpper, bLower, bUpper, t)
            val matched = matchFolder(gap, folders, layer)
                ?: throw IllegalStateException("间隙 ${String.format("%.1f", gap)} mm 未匹配到 ${layer}H 工艺文件夹")
            Sample(t, gap, matched)
        }
        val segments = mutableListOf<TBarSegment>()
        var i = 0
        while (i < sampled.size) {
            val current = sampled[i]
            var j = i
            while (j + 1 < sampled.size && sampled[j + 1].folder.folderPath == current.folder.folderPath) {
                j++
            }
            val process = if (pass == TBarPass.ROOT) current.folder.rootProcess else current.folder.capProcess
            val t0 = sampled[i].t
            val t1 = sampled[j].t
            segments.add(
                TBarSegment(
                    tStart = t0,
                    tEnd = t1,
                    startPose = lerpPose(startPose, endPose, t0),
                    endPose = lerpPose(startPose, endPose, t1),
                    gapStart = sampled[i].gap,
                    gapEnd = sampled[j].gap,
                    process = process,
                    folderName = current.folder.name
                )
            )
            i = j + 1
        }
        return segments
    }

    private fun lerpPose(a: Pose, b: Pose, t: Double): Pose {
        return Pose(
            x = a.x + (b.x - a.x) * t,
            y = a.y + (b.y - a.y) * t,
            z = a.z + (b.z - a.z) * t,
            rx = a.rx + shortestAngle(a.rx, b.rx) * t,
            ry = a.ry + shortestAngle(a.ry, b.ry) * t,
            rz = a.rz + shortestAngle(a.rz, b.rz) * t,
            ext1 = a.ext1 + (b.ext1 - a.ext1) * t
        )
    }

    private fun shortestAngle(from: Double, to: Double): Double {
        var d = (to - from) % 360.0
        if (d > 180) d -= 360
        if (d < -180) d += 360
        return d
    }

    private fun Pose.toP() = Point3D(x, y, z)

    private fun toolAxis(pose: Pose, col: Int): Point3D {
        val r = CoordinateUtils.eulerToRotationMatrix(pose.rx, pose.ry, pose.rz)
        return normalize(Point3D(r[0][col], r[1][col], r[2][col]))
    }

    private operator fun Point3D.minus(other: Point3D) = Point3D(x - other.x, y - other.y, z - other.z)
    private operator fun Point3D.plus(other: Point3D) = Point3D(x + other.x, y + other.y, z + other.z)
    private fun dot(a: Point3D, b: Point3D) = a.x * b.x + a.y * b.y + a.z * b.z
    private fun cross(a: Point3D, b: Point3D) = Point3D(
        a.y * b.z - a.z * b.y,
        a.z * b.x - a.x * b.z,
        a.x * b.y - a.y * b.x
    )
    private fun magnitude(v: Point3D) = sqrt(v.x * v.x + v.y * v.y + v.z * v.z)
    private fun scale(v: Point3D, s: Double) = Point3D(v.x * s, v.y * s, v.z * s)
    private fun normalize(v: Point3D): Point3D {
        val mag = magnitude(v)
        if (mag < 1e-9) return Point3D(0.0, 0.0, 0.0)
        return scale(v, 1.0 / mag)
    }
}
