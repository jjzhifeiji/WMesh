package com.gbndt.shijiaoqi.domain.weld

import kotlin.math.*

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
