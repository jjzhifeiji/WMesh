package com.gbndt.shijiaoqi.domain.single

import com.gbndt.shijiaoqi.geom.Vec3
import com.gbndt.shijiaoqi.model.Pose
import com.gbndt.shijiaoqi.model.WeldPoint
import com.gbndt.shijiaoqi.model.WeldPointType
import com.gbndt.shijiaoqi.model.WeldProcess
import com.gbndt.shijiaoqi.model.single.CornerGroupParams
import com.gbndt.shijiaoqi.model.single.WeldPath
import java.util.UUID
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
    val start: Vec3,
    val end: Vec3,
    val points: List<CornerPoint>,
)

/** 包角生成入参；工艺身份不在这里。 */
data class CornerSpec(
    val cornerPose: Pose,
    val safeStartPose: Pose?,
    val safeEndPose: Pose?,
    val weldOrientation: Pose?,
    val vecA: Vec3,
    val vecB: Vec3,
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
    fun intersection(p1: Vec3, p2: Vec3, p3: Vec3, p4: Vec3): Vec3? {
        val u = Vec3(p2.x - p1.x, p2.y - p1.y, p2.z - p1.z)
        val v = Vec3(p4.x - p3.x, p4.y - p3.y, p4.z - p3.z)
        val w = Vec3(p1.x - p3.x, p1.y - p3.y, p1.z - p3.z)
        val a = u.x * u.x + u.y * u.y + u.z * u.z
        val b = u.x * v.x + u.y * v.y + u.z * v.z
        val c = v.x * v.x + v.y * v.y + v.z * v.z
        val d = u.x * w.x + u.y * w.y + u.z * w.z
        val e = v.x * w.x + v.y * w.y + v.z * w.z
        val denominator = a * c - b * b
        if (denominator < 1e-8) return null
        val sc = (b * e - c * d) / denominator
        val tc = (a * e - b * d) / denominator
        val cp1 = Vec3(p1.x + sc * u.x, p1.y + sc * u.y, p1.z + sc * u.z)
        val cp2 = Vec3(p3.x + tc * v.x, p3.y + tc * v.y, p3.z + tc * v.z)
        return Vec3((cp1.x + cp2.x) / 2.0, (cp1.y + cp2.y) / 2.0, (cp1.z + cp2.z) / 2.0)
    }

    fun generateLayers(spec: CornerSpec): List<CornerLayer> {
        val isAVertical = Math.abs(spec.vecA.z) > Math.abs(spec.vecB.z)
        val vecVerticalRaw = if (isAVertical) spec.vecA else spec.vecB
        val vecHorizontalRaw = if (isAVertical) spec.vecB else spec.vecA
        val vecZ = normalize(vecVerticalRaw)
        val vecHNorm = normalize(vecHorizontalRaw)
        val dotHZ = vecHNorm.x * vecZ.x + vecHNorm.y * vecZ.y + vecHNorm.z * vecZ.z
        val vecFlat = normalize(
            Vec3(
                vecHNorm.x - dotHZ * vecZ.x,
                vecHNorm.y - dotHZ * vecZ.y,
                vecHNorm.z - dotHZ * vecZ.z,
            )
        )
        val rootP = Vec3(spec.cornerPose.x, spec.cornerPose.y, spec.cornerPose.z)
        var freeDir = vecFlat
        if (spec.safeStartPose != null) {
            val sx = spec.safeStartPose.x - rootP.x
            val sy = spec.safeStartPose.y - rootP.y
            val sz = spec.safeStartPose.z - rootP.z
            val dotSafeZ = sx * vecZ.x + sy * vecZ.y + sz * vecZ.z
            val safeProj = Vec3(
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
            val startP = Vec3(
                rootP.x + distZ * vecZ.x + w * vecLeftOut.x,
                rootP.y + distZ * vecZ.y + w * vecLeftOut.y,
                rootP.z + distZ * vecZ.z + w * vecLeftOut.z,
            )
            val endP = Vec3(
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

    private fun normalize(v: Vec3): Vec3 {
        val mag = sqrt(v.x * v.x + v.y * v.y + v.z * v.z)
        if (mag < 1e-9) return Vec3(0.0, 0.0, 0.0)
        return Vec3(v.x / mag, v.y / mag, v.z / mag)
    }

    private fun crossProduct(v1: Vec3, v2: Vec3): Vec3 =
        Vec3(
            v1.y * v2.z - v1.z * v2.y,
            v1.z * v2.x - v1.x * v2.z,
            v1.x * v2.y - v1.y * v2.x,
        )
}
/** 把包角层几何收成焊道；只写 processId。 */
object CornerWeldGenerator {

    fun calculate3DIntersection(p1: Vec3, p2: Vec3, p3: Vec3, p4: Vec3): Vec3? =
        CornerRun.intersection(p1, p2, p3, p4)

    fun generateCornerPaths(
        baseProcess: WeldProcess,
        processId: String,
        cornerPose: Pose,
        safeStartPose: Pose?,
        safeEndPose: Pose?,
        weldOrientation: Pose? = null,
        vecAIn: Vec3,
        vecBIn: Vec3,
        initialLength: Double,
        layerCount: Int,
        upwardOffset: Double,
        lengthReduction: Double,
        refPathAId: String,
        refPathBId: String,
    ): List<WeldPath> {
        val layers = CornerRun.generateLayers(
            CornerSpec(
                cornerPose = cornerPose,
                safeStartPose = safeStartPose,
                safeEndPose = safeEndPose,
                weldOrientation = weldOrientation,
                vecA = vecAIn,
                vecB = vecBIn,
                initialLength = initialLength,
                layerCount = layerCount,
                upwardOffset = upwardOffset,
                lengthReduction = lengthReduction,
            )
        )
        val groupId = UUID.randomUUID().toString()
        return layers.map { layer ->
            val groupParams = CornerGroupParams(
                groupId = groupId,
                refPathAId = refPathAId,
                refPathBId = refPathBId,
                layerCount = layerCount,
                initialLength = initialLength,
                upwardOffset = upwardOffset,
                lengthReduction = lengthReduction,
                isMaster = layer.index == 0,
                torchRx = weldOrientation?.rx,
                torchRy = weldOrientation?.ry,
                torchRz = weldOrientation?.rz,
                processId = processId,
            )
            val points = mutableListOf<WeldPoint>()
            layer.points.forEach { pt ->
                points.add(
                    WeldPoint(
                        id = UUID.randomUUID().toString(),
                        type = pt.type,
                        pose = pt.pose,
                    )
                )
            }
            WeldPath(
                id = UUID.randomUUID().toString(),
                name = "包角焊道 ${layer.index + 1}",
                points = points,
                process = baseProcess,
                processId = processId,
                selectedPointIndex = 0,
                isEnabled = true,
                cornerGroupParams = groupParams,
            )
        }
    }
}
