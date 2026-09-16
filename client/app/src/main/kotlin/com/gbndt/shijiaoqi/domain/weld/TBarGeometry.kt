package com.gbndt.shijiaoqi.domain.weld

import com.gbndt.shijiaoqi.model.Pose
import kotlin.math.abs
import kotlin.math.atan2
import kotlin.math.sqrt

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

    private fun midpoint(a: Pose, b: Pose): Point3D {
        return Point3D((a.x + b.x) / 2.0, (a.y + b.y) / 2.0, (a.z + b.z) / 2.0)
    }

    private fun pointOnLine(start: Pose, end: Pose, t: Double): Point3D {
        return Point3D(
            start.x + (end.x - start.x) * t,
            start.y + (end.y - start.y) * t,
            start.z + (end.z - start.z) * t
        )
    }

    private fun planeNormal(aLower: Pose, bLower: Pose, aUpper: Pose, bUpper: Pose): Point3D? {
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

    /** 位置用中点；姿态从安全点出发，只绕焊缝方向补旋转角，使焊枪垂直四点平面。 */
    private fun computeTorchPose(
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
