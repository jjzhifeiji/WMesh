package com.gbndt.shijiaoqi.domain.weld

import com.gbndt.shijiaoqi.model.CornerGroupParams
import com.gbndt.shijiaoqi.model.Pose
import com.gbndt.shijiaoqi.model.WeldPath
import com.gbndt.shijiaoqi.model.WeldPoint
import com.gbndt.shijiaoqi.model.WeldProcess
import com.gbndt.shijiaoqi.domain.weld.CornerRun
import com.gbndt.shijiaoqi.domain.weld.CornerSpec
import java.util.UUID

/** 把包角层几何收成焊道；只写 processId。 */
object CornerWeldGenerator {

    fun calculate3DIntersection(p1: Point3D, p2: Point3D, p3: Point3D, p4: Point3D): Point3D? =
        CornerRun.intersection(p1, p2, p3, p4)

    fun generateCornerPaths(
        baseProcess: WeldProcess,
        processId: String,
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
