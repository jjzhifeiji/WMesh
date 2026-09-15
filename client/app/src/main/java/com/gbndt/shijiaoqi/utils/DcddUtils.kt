package com.gbndt.shijiaoqi.utils

import kotlin.math.*

// Data classes from usbhid
data class DcddPoint(val x: Double, val y: Double, val z: Double)
data class DcddPose(val position: DcddPoint, val orientation: Triple<Double, Double, Double>)

// From jisuan.kt
class DcddCoordinateTransformer(
    private val A1: DcddPoint,
    private val B1: DcddPoint,
    private val C1: DcddPoint,
    private val toolToBaseOffset: DcddPose
) {
    private val toolToBaseRotationMatrix: Array<DoubleArray>
    private val toolToBaseTranslationVector: DoubleArray

    init {
        toolToBaseRotationMatrix = eulerToRotationMatrix(toolToBaseOffset.orientation.first, toolToBaseOffset.orientation.second, toolToBaseOffset.orientation.third)
        toolToBaseTranslationVector = doubleArrayOf(toolToBaseOffset.position.x, toolToBaseOffset.position.y, toolToBaseOffset.position.z)
    }
    private val rotationMatrix: Array<DoubleArray>
    private val translationVector: DoubleArray

    init {
        // 计算X轴方向向量并单位化
        val X = normalize(B1.x - A1.x, B1.y - A1.y, B1.z - A1.z)

        // 计算Z轴方向向量并单位化
        val Z = normalize(C1.x - A1.x, C1.y - A1.y, C1.z - A1.z)

        // 计算Y轴方向向量
        val Y = crossProduct(X, Z).let { normalize(it[0], it[1], it[2]) }

        rotationMatrix = arrayOf(X, Y, Z)
        translationVector = doubleArrayOf(A1.x, A1.y, A1.z)
    }

    fun transform(point: DcddPoint): DcddPoint {
        val homogeneousPoint = doubleArrayOf(point.x, point.y, point.z, 1.0)
        val transformed = DoubleArray(4)
        for (i in 0..2) {
            for (j in 0..2) {
                transformed[i] += rotationMatrix[j][i] * homogeneousPoint[j]
            }
            transformed[i] += translationVector[i]
        }
        return DcddPoint(transformed[0], transformed[1], transformed[2])
    }

    private fun normalize(x: Double, y: Double, z: Double): DoubleArray {
        val length = sqrt(x * x + y * y + z * z)
        return doubleArrayOf(x / length, y / length, z / length)
    }

    private fun crossProduct(v1: DoubleArray, v2: DoubleArray): DoubleArray {
        return doubleArrayOf(
            v1[1] * v2[2] - v1[2] * v2[1],
            v1[2] * v2[0] - v1[0] * v2[2],
            v1[0] * v2[1] - v1[1] * v2[0]
        )
    }

    fun getOffsetFromA1(point: DcddPoint): DcddPoint {
        val pointInTool = transform(point)
        return DcddPoint(
            pointInTool.x - A1.x,
            pointInTool.y - A1.y,
            pointInTool.z - A1.z
        )
    }
    fun transformToBaseCoordinates(pose: DcddPose): DcddPose {
        val positionInBase = transformPositionToBase(pose.position)
        val orientationInBase = transformOrientationToBase(pose.orientation)
        return DcddPose(positionInBase, orientationInBase)
    }

    private fun transformPositionToBase(point: DcddPoint): DcddPoint {
        val homogeneousPoint = doubleArrayOf(point.x, point.y, point.z, 1.0)
        val transformed = DoubleArray(4)
        for (i in 0..2) {
            for (j in 0..2) {
                transformed[i] += toolToBaseRotationMatrix[j][i] * homogeneousPoint[j]
            }
            transformed[i] += toolToBaseTranslationVector[i]
        }
        return DcddPoint(transformed[0], transformed[1], transformed[2])
    }

    private fun transformOrientationToBase(orientation: Triple<Double, Double, Double>): Triple<Double, Double, Double> {
        val rotationMatrix = eulerToRotationMatrix(orientation.first, orientation.second, orientation.third)
        val baseRotationMatrix = multiplyMatrices(toolToBaseRotationMatrix, rotationMatrix)
        return rotationMatrixToEuler(baseRotationMatrix)
    }

    private fun eulerToRotationMatrix(roll: Double, pitch: Double, yaw: Double): Array<DoubleArray> {
        val cy = cos(Math.toRadians(yaw))
        val sy = sin(Math.toRadians(yaw))
        val cp = cos(Math.toRadians(pitch))
        val sp = sin(Math.toRadians(pitch))
        val cr = cos(Math.toRadians(roll))
        val sr = sin(Math.toRadians(roll))

        return arrayOf(
            doubleArrayOf(cy * cp, cy * sp * sr - sy * cr, cy * sp * cr + sy * sr),
            doubleArrayOf(sy * cp, sy * sp * sr + cy * cr, sy * sp * cr - cy * sr),
            doubleArrayOf(-sp, cp * sr, cp * cr)
        )
    }

    private fun multiplyMatrices(a: Array<DoubleArray>, b: Array<DoubleArray>): Array<DoubleArray> {
        val result = Array(3) { DoubleArray(3) }
        for (i in 0..2) {
            for (j in 0..2) {
                for (k in 0..2) {
                    result[i][j] += a[i][k] * b[k][j]
                }
            }
        }
        return result
    }

    private fun rotationMatrixToEuler(rotationMatrix: Array<DoubleArray>): Triple<Double, Double, Double> {
        val sy = sqrt(rotationMatrix[0][0] * rotationMatrix[0][0] + rotationMatrix[1][0] * rotationMatrix[1][0])
        val singular = sy < 1e-6

        val roll = if (!singular) atan2(rotationMatrix[2][1], rotationMatrix[2][2]) else atan2(-rotationMatrix[1][2], rotationMatrix[1][1])
        val pitch = atan2(-rotationMatrix[2][0], sy)
        val yaw = if (!singular) atan2(rotationMatrix[1][0], rotationMatrix[0][0]) else 0.0

        return Triple(Math.toDegrees(roll), Math.toDegrees(pitch), Math.toDegrees(yaw))
    }

    fun getOrientationOffset(initialPose: DcddPose, rotatedPose: DcddPose): Triple<Double, Double, Double> {
        val initialPoseBase = transformToBaseCoordinates(initialPose)
        val rotatedPoseBase = transformToBaseCoordinates(rotatedPose)

        return Triple(
            rotatedPoseBase.orientation.first - initialPoseBase.orientation.first,
            rotatedPoseBase.orientation.second - initialPoseBase.orientation.second,
            rotatedPoseBase.orientation.third - initialPoseBase.orientation.third
        )
    }
}

// From xuanzhuan2.kt
class DcddCoordinateSystem(val origin: DcddPoint, val xAxis: DcddPoint, val zAxis: DcddPoint) {
    private fun vector(p1: DcddPoint, p2: DcddPoint): DcddPoint = DcddPoint(p2.x - p1.x, p2.y - p1.y, p2.z - p1.z)
    private fun magnitude(v: DcddPoint): Double = sqrt(v.x * v.x + v.y * v.y + v.z * v.z)
    private fun normalize(v: DcddPoint): DcddPoint {
        val mag = magnitude(v)
        return DcddPoint(v.x / mag, v.y / mag, v.z / mag)
    }
    private fun crossProduct(v1: DcddPoint, v2: DcddPoint): DcddPoint =
        DcddPoint(v1.y * v2.z - v1.z * v2.y, v1.z * v2.x - v1.x * v2.z, v1.x * v2.y - v1.y * v2.x)

    // 计算旋转矩阵
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

    // 应用旋转矩阵到向量上
    private fun applyRotation(v: DcddPoint, matrix: Array<DoubleArray>): DcddPoint =
        DcddPoint(matrix[0][0] * v.x + matrix[0][1] * v.y + matrix[0][2] * v.z,
              matrix[1][0] * v.x + matrix[1][1] * v.y + matrix[1][2] * v.z,
              matrix[2][0] * v.x + matrix[2][1] * v.y + matrix[2][2] * v.z)

    // 主要方法：旋转线段BA
    fun rotateBAByAngle(angleDegrees: Double): Pair<DcddPoint, Triple<Double, Double, Double>> {
        val angleRadians = Math.toRadians(angleDegrees)

        // 获取AB向量
        val ab = vector(origin, xAxis)

        // 计算AB和AC向量的叉乘得到旋转轴
        val ac = vector(origin, zAxis)
        val axis = normalize(crossProduct(ab, ac))

        // 创建旋转矩阵
        val rotMatrix = rotationMatrix(axis, angleRadians)

        // 应用旋转矩阵到AB向量上
        val newAb = applyRotation(ab, rotMatrix)

        // 将新的AB向量转换回世界坐标系中的点
        val newB = DcddPoint(newAb.x + origin.x, newAb.y + origin.y, newAb.z + origin.z)

        // 计算旋转矩阵对应的欧拉角
        val eulerAngles = rotationMatrixToEulerAngles(rotMatrix)

        return Pair(newB, eulerAngles)
    }

    // 从旋转矩阵中提取欧拉角
    private fun rotationMatrixToEulerAngles(R: Array<DoubleArray>): Triple<Double, Double, Double> {
        val sy = sqrt(R[0][0] * R[0][0] + R[1][0] * R[1][0])
        val singular = sy < 1e-6

        val x = atan2(-R[1][2], R[2][2])
        val y = atan2(sy, R[2][0])
        val z = if (!singular) atan2(R[0][1], R[0][0]) else 0.0

        return Triple(x, y, z)
    }
}
