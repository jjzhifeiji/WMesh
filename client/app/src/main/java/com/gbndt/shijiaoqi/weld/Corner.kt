package com.gbndt.shijiaoqi.weld

import com.gbndt.shijiaoqi.data.models.Pose
import com.gbndt.shijiaoqi.data.models.WeldPointType
import com.gbndt.shijiaoqi.utils.Point3D
import kotlin.math.sqrt

/** 一层包角焊道上的一个点。 */
data class CornerPoint(
    val type: WeldPointType,
    val pose: Pose,
)

/** 一层包角：长度、起终点、固定四点。 */
data class CornerLayer(
    val index: Int,
    val length: Double,
    val start: Point3D,
    val end: Point3D,
    val points: List<CornerPoint>,
)

/** 包角生成入参；工艺身份不在这里。 */
data class CornerSpec(
    val cornerPose: Pose,
    val safeStartPose: Pose?,
    val safeEndPose: Pose?,
    val weldOrientation: Pose?,
    val vecA: Point3D,
    val vecB: Point3D,
    val initialLength: Double,
    val layerCount: Int,
    val upwardOffset: Double,
    val lengthReduction: Double,
)

/** 包角点序与现网三角锥分层；不写工艺路径。 */
object CornerRun {
    val POINT_ORDER = listOf(
        WeldPointType.START_SAFE,
        WeldPointType.START,
        WeldPointType.END,
        WeldPointType.END_SAFE,
    )

    fun layerLength(initial: Double, reduction: Double, index: Int): Double =
        initial - index * reduction

    /** 两条直线公垂线中点；平行则空。 */
    fun intersection(p1: Point3D, p2: Point3D, p3: Point3D, p4: Point3D): Point3D? {
        val u = Point3D(p2.x - p1.x, p2.y - p1.y, p2.z - p1.z)
        val v = Point3D(p4.x - p3.x, p4.y - p3.y, p4.z - p3.z)
        val w = Point3D(p1.x - p3.x, p1.y - p3.y, p1.z - p3.z)
        val a = u.x * u.x + u.y * u.y + u.z * u.z
        val b = u.x * v.x + u.y * v.y + u.z * v.z
        val c = v.x * v.x + v.y * v.y + v.z * v.z
        val d = u.x * w.x + u.y * w.y + u.z * w.z
        val e = v.x * w.x + v.y * w.y + v.z * w.z
        val denominator = a * c - b * b
        if (denominator < 1e-8) return null
        val sc = (b * e - c * d) / denominator
        val tc = (a * e - b * d) / denominator
        val cp1 = Point3D(p1.x + sc * u.x, p1.y + sc * u.y, p1.z + sc * u.z)
        val cp2 = Point3D(p3.x + tc * v.x, p3.y + tc * v.y, p3.z + tc * v.z)
        return Point3D((cp1.x + cp2.x) / 2.0, (cp1.y + cp2.y) / 2.0, (cp1.z + cp2.z) / 2.0)
    }

    fun generateLayers(spec: CornerSpec): List<CornerLayer> {
        val isAVertical = Math.abs(spec.vecA.z) > Math.abs(spec.vecB.z)
        val vecVerticalRaw = if (isAVertical) spec.vecA else spec.vecB
        val vecHorizontalRaw = if (isAVertical) spec.vecB else spec.vecA
        val vecZ = normalize(vecVerticalRaw)
        val vecHNorm = normalize(vecHorizontalRaw)
        val dotHZ = vecHNorm.x * vecZ.x + vecHNorm.y * vecZ.y + vecHNorm.z * vecZ.z
        val vecFlat = normalize(
            Point3D(
                vecHNorm.x - dotHZ * vecZ.x,
                vecHNorm.y - dotHZ * vecZ.y,
                vecHNorm.z - dotHZ * vecZ.z,
            )
        )
        val rootP = Point3D(spec.cornerPose.x, spec.cornerPose.y, spec.cornerPose.z)
        var freeDir = vecFlat
        if (spec.safeStartPose != null) {
            val sx = spec.safeStartPose.x - rootP.x
            val sy = spec.safeStartPose.y - rootP.y
            val sz = spec.safeStartPose.z - rootP.z
            val dotSafeZ = sx * vecZ.x + sy * vecZ.y + sz * vecZ.z
            val safeProj = Point3D(
                sx - dotSafeZ * vecZ.x,
                sy - dotSafeZ * vecZ.y,
                sz - dotSafeZ * vecZ.z,
            )
            val mag = sqrt(safeProj.x * safeProj.x + safeProj.y * safeProj.y + safeProj.z * safeProj.z)
            if (mag > 1e-3) freeDir = normalize(safeProj)
        }
        val w1 = normalize(crossProduct(vecZ, vecFlat))
        val w2 = normalize(crossProduct(vecFlat, vecZ))
        val dotW1 = w1.x * freeDir.x + w1.y * freeDir.y + w1.z * freeDir.z
        val dotW2 = w2.x * freeDir.x + w2.y * freeDir.y + w2.z * freeDir.z
        val vecOther = if (dotW1 > dotW2) w1 else w2
        val crossTest = crossProduct(vecFlat, vecOther)
        val isFlatLeft = (crossTest.x * vecZ.x + crossTest.y * vecZ.y + crossTest.z * vecZ.z) > 0
        val vecLeftOut = if (isFlatLeft) vecFlat else vecOther
        val vecRightOut = if (isFlatLeft) vecOther else vecFlat
        val weldRx = spec.weldOrientation?.rx ?: spec.cornerPose.rx
        val weldRy = spec.weldOrientation?.ry ?: spec.cornerPose.ry
        val weldRz = spec.weldOrientation?.rz ?: spec.cornerPose.rz
        val out = mutableListOf<CornerLayer>()
        for (i in 0 until spec.layerCount) {
            val currentL = layerLength(spec.initialLength, spec.lengthReduction, i)
            if (currentL <= 0) break
            val distZ = i * spec.upwardOffset
            val w = currentL / sqrt(2.0)
            val startP = Point3D(
                rootP.x + distZ * vecZ.x + w * vecLeftOut.x,
                rootP.y + distZ * vecZ.y + w * vecLeftOut.y,
                rootP.z + distZ * vecZ.z + w * vecLeftOut.z,
            )
            val endP = Point3D(
                rootP.x + distZ * vecZ.x + w * vecRightOut.x,
                rootP.y + distZ * vecZ.y + w * vecRightOut.y,
                rootP.z + distZ * vecZ.z + w * vecRightOut.z,
            )
            val startSafe = spec.safeStartPose?.copy()
                ?: Pose(startP.x, startP.y, startP.z + 50.0, spec.cornerPose.rx, spec.cornerPose.ry, spec.cornerPose.rz)
            val endSafe = spec.safeEndPose?.copy()
                ?: Pose(endP.x, endP.y, endP.z + 50.0, spec.cornerPose.rx, spec.cornerPose.ry, spec.cornerPose.rz)
            out += CornerLayer(
                index = i,
                length = currentL,
                start = startP,
                end = endP,
                points = listOf(
                    CornerPoint(WeldPointType.START_SAFE, startSafe),
                    CornerPoint(WeldPointType.START, Pose(startP.x, startP.y, startP.z, weldRx, weldRy, weldRz)),
                    CornerPoint(WeldPointType.END, Pose(endP.x, endP.y, endP.z, weldRx, weldRy, weldRz)),
                    CornerPoint(WeldPointType.END_SAFE, endSafe),
                ),
            )
        }
        return out
    }

    private fun normalize(v: Point3D): Point3D {
        val mag = sqrt(v.x * v.x + v.y * v.y + v.z * v.z)
        if (mag < 1e-9) return Point3D(0.0, 0.0, 0.0)
        return Point3D(v.x / mag, v.y / mag, v.z / mag)
    }

    private fun crossProduct(v1: Point3D, v2: Point3D): Point3D =
        Point3D(
            v1.y * v2.z - v1.z * v2.y,
            v1.z * v2.x - v1.x * v2.z,
            v1.x * v2.y - v1.y * v2.x,
        )
}
