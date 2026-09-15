package com.gbndt.shijiaoqi.utils

import com.gbndt.shijiaoqi.data.models.Pose
import com.gbndt.shijiaoqi.data.models.WeldPath
import com.gbndt.shijiaoqi.data.models.WeldPoint
import com.gbndt.shijiaoqi.data.models.WeldPointType
import java.util.UUID
import kotlin.math.sqrt

object CornerWeldGenerator {

    private fun normalize(v: Point3D): Point3D {
        val mag = sqrt(v.x * v.x + v.y * v.y + v.z * v.z)
        if (mag < 1e-9) return Point3D(0.0, 0.0, 0.0)
        return Point3D(v.x / mag, v.y / mag, v.z / mag)
    }

    private fun crossProduct(v1: Point3D, v2: Point3D): Point3D {
        return Point3D(
            v1.y * v2.z - v1.z * v2.y,
            v1.z * v2.x - v1.x * v2.z,
            v1.x * v2.y - v1.y * v2.x
        )
    }

    /**
     * 计算3D空间中两条直线的最近交点 (或公垂线中点)
     * @param p1 第一条直线上的一点 (起点)
     * @param p2 第一条直线上另一点 (终点)
     * @param p3 第二条直线上的一点 (起点)
     * @param p4 第二条直线上另一点 (终点)
     * @return 交点坐标 Point3D，若两线平行则返回 null
     */
    fun calculate3DIntersection(p1: Point3D, p2: Point3D, p3: Point3D, p4: Point3D): Point3D? {
        // 直线 1 方向向量
        val u = Point3D(p2.x - p1.x, p2.y - p1.y, p2.z - p1.z)
        // 直线 2 方向向量
        val v = Point3D(p4.x - p3.x, p4.y - p3.y, p4.z - p3.z)
        // 两线起点连线
        val w = Point3D(p1.x - p3.x, p1.y - p3.y, p1.z - p3.z)

        val a = u.x * u.x + u.y * u.y + u.z * u.z
        val b = u.x * v.x + u.y * v.y + u.z * v.z
        val c = v.x * v.x + v.y * v.y + v.z * v.z
        val d = u.x * w.x + u.y * w.y + u.z * w.z
        val e = v.x * w.x + v.y * w.y + v.z * w.z

        val denominator = a * c - b * b
        if (denominator < 1e-8) {
            // 两条直线平行，无法求出交点
            return null
        }

        val sc = (b * e - c * d) / denominator
        val tc = (a * e - b * d) / denominator

        // 直线 1 上的最近点
        val cp1 = Point3D(p1.x + sc * u.x, p1.y + sc * u.y, p1.z + sc * u.z)
        // 直线 2 上的最近点
        val cp2 = Point3D(p3.x + tc * v.x, p3.y + tc * v.y, p3.z + tc * v.z)

        // 返回两点的中点作为交点
        return Point3D((cp1.x + cp2.x) / 2.0, (cp1.y + cp2.y) / 2.0, (cp1.z + cp2.z) / 2.0)
    }

    /**
     * 生成包角焊道
     *
     * @param baseProcess 使用的焊接工艺
     * @param baseProcessPath 焊接工艺路径
     * @param cornerPose 角落原点的姿态（提供基准的XYZ和Rx,Ry,Rz）
     * @param weldOrientation 包角起点/终点姿态（Rx,Ry,Rz）；为空则沿用角落基准点姿态
     * @param vecA 参考向量A (底面走向，需归一化前或后)
     * @param vecB 参考向量B (垂直面走向，需归一化前或后)
     * @param initialLength 第一层的长度 (L)
     * @param initialDistance 第一层距离角点的距离 (D0)
     * @param layerCount 层数
     * @param inwardOffset 向里偏移量 (ΔD)
     * @param upwardOffset 向上偏移量 (ΔZ)
     * @param lengthReduction 长度递减量 (ΔL)
     * @return 生成的 WeldPath 列表
     */
    fun generateCornerPaths(
        baseProcess: com.gbndt.shijiaoqi.data.models.WeldProcess,
        baseProcessPath: String,
        cornerPose: Pose,
        safeStartPose: Pose?,
        safeEndPose: Pose?,
        weldOrientation: Pose? = null,
        vecAIn: Point3D,
        vecBIn: Point3D,
        initialLength: Double,
        layerCount: Int,
        upwardOffset: Double,
        lengthReduction: Double,
        refPathAId: String,
        refPathBId: String
    ): List<WeldPath> {
        // 1. 自动识别立焊和平焊方向
        // 立焊方向在 Z 轴上的投影绝对值通常远大于平焊
        val isAVertical = Math.abs(vecAIn.z) > Math.abs(vecBIn.z)
        
        val vecVerticalRaw = if (isAVertical) vecAIn else vecBIn
        val vecHorizontalRaw = if (isAVertical) vecBIn else vecAIn

        val vecZ = normalize(vecVerticalRaw) // 立焊方向 (指向远离角落，通常为向上)
        val vecHNorm = normalize(vecHorizontalRaw) // 平焊原始方向

        // 2. 空间投影校正：确保平焊方向严格垂直于立焊方向
        val dotHZ = vecHNorm.x * vecZ.x + vecHNorm.y * vecZ.y + vecHNorm.z * vecZ.z
        val vecFlat = normalize(Point3D(
            vecHNorm.x - dotHZ * vecZ.x,
            vecHNorm.y - dotHZ * vecZ.y,
            vecHNorm.z - dotHZ * vecZ.z
        ))

        // 3. 确定安全空间(Free Space)方向，以确保生成的墙面向量不会指向工件内部(避免撞枪)
        // 默认使用 Z 轴和水平向外的组合作为安全方向，如果有 safeStartPose 则优先使用它的方向
        val rootP = Point3D(cornerPose.x, cornerPose.y, cornerPose.z)
        var freeDir = vecFlat // 默认安全方向至少与平焊同向
        if (safeStartPose != null) {
            val sx = safeStartPose.x - rootP.x
            val sy = safeStartPose.y - rootP.y
            val sz = safeStartPose.z - rootP.z
            // 投影到垂直于 vecZ 的平面上
            val dotSafeZ = sx * vecZ.x + sy * vecZ.y + sz * vecZ.z
            val safeProj = Point3D(
                sx - dotSafeZ * vecZ.x,
                sy - dotSafeZ * vecZ.y,
                sz - dotSafeZ * vecZ.z
            )
            val mag = Math.sqrt(safeProj.x * safeProj.x + safeProj.y * safeProj.y + safeProj.z * safeProj.z)
            if (mag > 1e-3) {
                freeDir = normalize(safeProj)
            }
        }

        // 4. 计算另一侧墙面的向量
        // 另一侧墙面必定垂直于 vecZ 且垂直于 vecFlat
        val w1 = normalize(crossProduct(vecZ, vecFlat))
        val w2 = normalize(crossProduct(vecFlat, vecZ))
        
        // 选择与 safeDir 点乘为正的方向（即指向安全空间、远离工件内部的方向）
        val dotW1 = w1.x * freeDir.x + w1.y * freeDir.y + w1.z * freeDir.z
        val dotW2 = w2.x * freeDir.x + w2.y * freeDir.y + w2.z * freeDir.z
        val vecOther = if (dotW1 > dotW2) w1 else w2

        // 5. 判断左右墙面
        // 叉乘 Left x Right = vecZ (右手坐标系中，从左向右握拳，大拇指向上)
        val crossTest = crossProduct(vecFlat, vecOther)
        val isFlatLeft = (crossTest.x * vecZ.x + crossTest.y * vecZ.y + crossTest.z * vecZ.z) > 0
        
        val vecLeftOut = if (isFlatLeft) vecFlat else vecOther
        val vecRightOut = if (isFlatLeft) vecOther else vecFlat

        val weldRx = weldOrientation?.rx ?: cornerPose.rx
        val weldRy = weldOrientation?.ry ?: cornerPose.ry
        val weldRz = weldOrientation?.rz ?: cornerPose.rz

        val generatedPaths = mutableListOf<WeldPath>()
        val groupId = UUID.randomUUID().toString()

        // 6. 逐层生成三角锥形态的焊道 (从下往上)
        for (i in 0 until layerCount) {
            // 当前层长度 (随着层数增加，即高度上升，横跨的焊道越来越短)
            val currentL = initialLength - i * lengthReduction
            if (currentL <= 0) break

            // 当前层高度 (沿立焊方向向上)
            val distZ = i * upwardOffset

            // 假设等腰直角三角形，w = L / sqrt(2)
            // w 是起点和终点距离角落(沿各自墙面)的水平距离
            val w = currentL / Math.sqrt(2.0)

            // 起点 (落在左侧墙面上，沿着 vecLeftOut 向外) -> 对应“左边开始”
            val startP = Point3D(
                rootP.x + distZ * vecZ.x + w * vecLeftOut.x,
                rootP.y + distZ * vecZ.y + w * vecLeftOut.y,
                rootP.z + distZ * vecZ.z + w * vecLeftOut.z
            )

            // 终点 (落在右侧墙面上，沿着平焊方向 vecRightOut 向外) -> 对应“右边结束”
            val endP = Point3D(
                rootP.x + distZ * vecZ.x + w * vecRightOut.x,
                rootP.y + distZ * vecZ.y + w * vecRightOut.y,
                rootP.z + distZ * vecZ.z + w * vecRightOut.z
            )

            val groupParams = com.gbndt.shijiaoqi.data.models.CornerGroupParams(
                groupId = groupId,
                refPathAId = refPathAId,
                refPathBId = refPathBId,
                layerCount = layerCount,
                initialLength = initialLength,
                upwardOffset = upwardOffset,
                lengthReduction = lengthReduction,
                isMaster = (i == 0), // The first layer is the master for updating
                torchRx = weldOrientation?.rx,
                torchRy = weldOrientation?.ry,
                torchRz = weldOrientation?.rz,
                processPath = baseProcessPath
            )

            // 创建 WeldPath
            val newPath = WeldPath(
                id = UUID.randomUUID().toString(),
                name = "包角焊道 ${i + 1}",
                points = androidx.compose.runtime.mutableStateListOf(),
                process = baseProcess,
                processPath = baseProcessPath,
                selectedPointIndex = 0,
                isEnabled = true,
                cornerGroupParams = groupParams
            )

            // 添加安全起点 (复用传入的立焊安全起点，若未提供则默认用第一层点垂直抬高)
            if (safeStartPose != null) {
                newPath.points.add(
                    WeldPoint(
                        id = UUID.randomUUID().toString(),
                        type = WeldPointType.START_SAFE,
                        pose = safeStartPose.copy()
                    )
                )
            } else {
                newPath.points.add(
                    WeldPoint(
                        id = UUID.randomUUID().toString(),
                        type = WeldPointType.START_SAFE,
                        pose = Pose(startP.x, startP.y, startP.z + 50.0, cornerPose.rx, cornerPose.ry, cornerPose.rz)
                    )
                )
            }

            // 添加起点，姿态使用采集的焊枪姿态（未采集则沿用角落基准点）
            newPath.points.add(
                WeldPoint(
                    id = UUID.randomUUID().toString(),
                    type = WeldPointType.START,
                    pose = Pose(startP.x, startP.y, startP.z, weldRx, weldRy, weldRz)
                )
            )

            // 添加终点，姿态与起点相同
            newPath.points.add(
                WeldPoint(
                    id = UUID.randomUUID().toString(),
                    type = WeldPointType.END,
                    pose = Pose(endP.x, endP.y, endP.z, weldRx, weldRy, weldRz)
                )
            )

            // 添加安全终点 (复用传入的立焊安全终点，若未提供则默认用当前层点垂直抬高)
            if (safeEndPose != null) {
                newPath.points.add(
                    WeldPoint(
                        id = UUID.randomUUID().toString(),
                        type = WeldPointType.END_SAFE,
                        pose = safeEndPose.copy()
                    )
                )
            } else {
                newPath.points.add(
                    WeldPoint(
                        id = UUID.randomUUID().toString(),
                        type = WeldPointType.END_SAFE,
                        pose = Pose(endP.x, endP.y, endP.z + 50.0, cornerPose.rx, cornerPose.ry, cornerPose.rz)
                    )
                )
            }

            generatedPaths.add(newPath)
        }

        return generatedPaths
    }
}
