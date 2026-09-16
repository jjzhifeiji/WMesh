package com.gbndt.shijiaoqi.domain.weld

import com.gbndt.shijiaoqi.model.Pose
import kotlin.math.*

data class Point3D(val x: Double, val y: Double, val z: Double) {
    fun distanceTo(other: Point3D): Double {
        val dx = x - other.x
        val dy = y - other.y
        val dz = z - other.z
        return sqrt(dx * dx + dy * dy + dz * dz)
    }

    operator fun minus(other: Point3D): Point3D {
        return Point3D(x - other.x, y - other.y, z - other.z)
    }
}

object CoordinateUtils {

    fun Pose.toPoint3D() = Point3D(x, y, z)

    fun eulerToRotationMatrix(roll: Double, pitch: Double, yaw: Double): Array<DoubleArray> {
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

    fun rotationMatrixToEulerClosest(
        rotationMatrix: Array<DoubleArray>,
        refRx: Double,
        refRy: Double,
        refRz: Double
    ): Triple<Double, Double, Double> {
        val sy = sqrt(rotationMatrix[0][0] * rotationMatrix[0][0] + rotationMatrix[1][0] * rotationMatrix[1][0])
        val singular = sy < 1e-6

        val roll1 = if (!singular) atan2(rotationMatrix[2][1], rotationMatrix[2][2]) else atan2(-rotationMatrix[1][2], rotationMatrix[1][1])
        val pitch1 = atan2(-rotationMatrix[2][0], sy)
        val yaw1 = if (!singular) atan2(rotationMatrix[1][0], rotationMatrix[0][0]) else 0.0

        val pitch2 = Math.PI - pitch1
        val roll2: Double
        val yaw2: Double

        if (!singular) {
            roll2 = roll1 + Math.PI
            yaw2 = yaw1 + Math.PI
        } else {
            roll2 = roll1
            yaw2 = yaw1
        }

        val r1 = Math.toDegrees(roll1)
        val p1 = Math.toDegrees(pitch1)
        val y1 = Math.toDegrees(yaw1)

        val r2 = Math.toDegrees(roll2)
        val p2 = Math.toDegrees(pitch2)
        val y2 = Math.toDegrees(yaw2)

        fun closestAngle(target: Double, ref: Double): Double {
            val diff = target - ref
            var d = diff % 360.0
            if (d > 180) d -= 360
            if (d < -180) d += 360
            return ref + d
        }

        val optR1 = closestAngle(r1, refRx)
        val optP1 = closestAngle(p1, refRy)
        val optY1 = closestAngle(y1, refRz)
        val dist1 = (optR1 - refRx).pow(2) + (optP1 - refRy).pow(2) + (optY1 - refRz).pow(2)

        val optR2 = closestAngle(r2, refRx)
        val optP2 = closestAngle(p2, refRy)
        val optY2 = closestAngle(y2, refRz)
        val dist2 = (optR2 - refRx).pow(2) + (optP2 - refRy).pow(2) + (optY2 - refRz).pow(2)

        return if (dist1 < dist2) {
            Triple(optR1, optP1, optY1)
        } else {
            Triple(optR2, optP2, optY2)
        }
    }

    fun multiplyMatrices(a: Array<DoubleArray>, b: Array<DoubleArray>): Array<DoubleArray> {
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

    fun axisAngleToRotationMatrix(axis: Point3D, angleDegrees: Double): Array<DoubleArray> {
        val rad = Math.toRadians(angleDegrees)
        val c = cos(rad)
        val s = sin(rad)
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
}
