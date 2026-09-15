package com.gbndt.shijiaoqi.utils

import com.gbndt.shijiaoqi.data.models.Pose
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
    fun Point3D.toPose() = Pose(x, y, z, 0.0, 0.0, 0.0)

    class CoordinateSystem(val origin: Point3D, val xAxis: Point3D, val zAxis: Point3D) {
        private fun vector(p1: Point3D, p2: Point3D): Point3D = Point3D(p2.x - p1.x, p2.y - p1.y, p2.z - p1.z)
        private fun magnitude(v: Point3D): Double = sqrt(v.x * v.x + v.y * v.y + v.z * v.z)
        private fun normalize(v: Point3D): Point3D {
            val mag = magnitude(v)
            if (mag < 1e-9) return Point3D(0.0, 0.0, 0.0)
            return Point3D(v.x / mag, v.y / mag, v.z / mag)
        }
        private fun crossProduct(v1: Point3D, v2: Point3D): Point3D =
            Point3D(v1.y * v2.z - v1.z * v2.y, v1.z * v2.x - v1.x * v2.z, v1.x * v2.y - v1.y * v2.x)

        // 计算旋转矩阵 (Row-major)
        // X, Y, Z axes are the rows of the rotation matrix if we transform Global to Local?
        // Wait, let's follow the user's logic:
        // User logic:
        // ab = vector(origin, xAxis) -> X axis direction
        // ac = vector(origin, zAxis) -> Ref Z direction
        // axis = normalize(crossProduct(ab, ac)) -> Y axis (Rotation axis for rotateBAByAngle logic?)
        
        // Actually, let's build a standard basis:
        // X' = normalize(xAxis - origin)
        // Z_temp = normalize(zAxis - origin)
        // Y' = normalize(cross(X', Z_temp))
        // Z' = cross(X', Y') (To ensure orthogonality)
        
        val rotationMatrix: Array<DoubleArray>
        
        init {
            val xVec = normalize(vector(origin, xAxis))
            val zTemp = normalize(vector(origin, zAxis))
            // Y = Z cross X (Right Hand Rule: Z cross X = Y)
            // This preserves the Z direction (zReal approx zTemp)
            val yVec = normalize(crossProduct(zTemp, xVec)) 
            
            val zReal = normalize(crossProduct(xVec, yVec))
            
            // Rotation Matrix from Local to Global (Base): [X_axis, Y_axis, Z_axis] as columns
            // R = [Xx Yx Zx]
            //     [Xy Yy Zy]
            //     [Xz Yz Zz]
            rotationMatrix = arrayOf(
                doubleArrayOf(xVec.x, yVec.x, zReal.x),
                doubleArrayOf(xVec.y, yVec.y, zReal.y),
                doubleArrayOf(xVec.z, yVec.z, zReal.z)
            )
        }

        // Transform a local point (offset) to global coordinates
        // P_global = R * P_local + Origin
        fun transformToGlobal(localPoint: Point3D): Point3D {
            val x = rotationMatrix[0][0] * localPoint.x + rotationMatrix[0][1] * localPoint.y + rotationMatrix[0][2] * localPoint.z + origin.x
            val y = rotationMatrix[1][0] * localPoint.x + rotationMatrix[1][1] * localPoint.y + rotationMatrix[1][2] * localPoint.z + origin.y
            val z = rotationMatrix[2][0] * localPoint.x + rotationMatrix[2][1] * localPoint.y + rotationMatrix[2][2] * localPoint.z + origin.z
            return Point3D(x, y, z)
        }

        // Transform a global point to local coordinates
        // P_local = R^T * (P_global - Origin)
        fun transformToLocal(globalPoint: Point3D): Point3D {
            val dx = globalPoint.x - origin.x
            val dy = globalPoint.y - origin.y
            val dz = globalPoint.z - origin.z
            
            // Dot product with basis vectors (Columns of R)
            val lx = rotationMatrix[0][0] * dx + rotationMatrix[1][0] * dy + rotationMatrix[2][0] * dz
            val ly = rotationMatrix[0][1] * dx + rotationMatrix[1][1] * dy + rotationMatrix[2][1] * dz
            val lz = rotationMatrix[0][2] * dx + rotationMatrix[1][2] * dy + rotationMatrix[2][2] * dz
            
            return Point3D(lx, ly, lz)
        }
        
        // Get Euler Angles (Rx, Ry, Rz) from the Rotation Matrix
        // Assuming Z-Y-X or similar convention?
        // User code used:
        // x = atan2(-R[1][2], R[2][2])
        // y = atan2(sy, R[2][0])
        // z = atan2(R[0][1], R[0][0])
        fun getEulerAngles(): Triple<Double, Double, Double> {
            val R = rotationMatrix
            val sy = sqrt(R[0][0] * R[0][0] + R[1][0] * R[1][0])
            val singular = sy < 1e-6
    
            val x = atan2(R[2][1], R[2][2]) // Roll (Rx) - Note: standard is usually atan2(R21, R22)
            val y = atan2(-R[2][0], sy)     // Pitch (Ry)
            val z = atan2(R[1][0], R[0][0]) // Yaw (Rz)
            
            // Convert to degrees
            return Triple(Math.toDegrees(x), Math.toDegrees(y), Math.toDegrees(z))
        }
    }
    
    // Linear Interpolation for Points
    fun lerp(p1: Point3D, p2: Point3D, t: Double): Point3D {
        return Point3D(
            p1.x + (p2.x - p1.x) * t,
            p1.y + (p2.y - p1.y) * t,
            p1.z + (p2.z - p1.z) * t
        )
    }
    
    // Spherical Linear Interpolation (SLERP) for Vectors (simplified)
    // Or just Lerp + Normalize for basis vectors (Fast & Good enough for small angles)
    fun slerpBasis(v1: Point3D, v2: Point3D, t: Double): Point3D {
        // Simple Lerp + Normalize is often sufficient for weld paths
        val lx = v1.x + (v2.x - v1.x) * t
        val ly = v1.y + (v2.y - v1.y) * t
        val lz = v1.z + (v2.z - v1.z) * t
        val mag = sqrt(lx*lx + ly*ly + lz*lz)
        return Point3D(lx/mag, ly/mag, lz/mag)
    }

    // Interpolate Coordinate System
    // Returns the CoordinateSystem object
    fun interpolateCoordinateSystem(
        originStart: Point3D, xRefStart: Point3D, zRefStart: Point3D,
        originEnd: Point3D, xRefEnd: Point3D, zRefEnd: Point3D,
        t: Double
    ): CoordinateSystem {
        // 1. Interpolate Origin
        val currentOrigin = lerp(originStart, originEnd, t)
        
        // 2. Interpolate Basis Vectors (Reference vectors relative to origin)
        // Vector Start
        val vecXStart = Point3D(xRefStart.x - originStart.x, xRefStart.y - originStart.y, xRefStart.z - originStart.z)
        val vecZStart = Point3D(zRefStart.x - originStart.x, zRefStart.y - originStart.y, zRefStart.z - originStart.z)
        
        // Vector End
        val vecXEnd = Point3D(xRefEnd.x - originEnd.x, xRefEnd.y - originEnd.y, xRefEnd.z - originEnd.z)
        val vecZEnd = Point3D(zRefEnd.x - originEnd.x, zRefEnd.y - originEnd.y, zRefEnd.z - originEnd.z)
        
        // Interpolate Directions
        val currentVecX = slerpBasis(vecXStart, vecXEnd, t)
        val currentVecZ = slerpBasis(vecZStart, vecZEnd, t)
        
        // 3. Build Coordinate System at t
        return CoordinateSystem(
            currentOrigin, 
            Point3D(currentOrigin.x + currentVecX.x, currentOrigin.y + currentVecX.y, currentOrigin.z + currentVecX.z),
            Point3D(currentOrigin.x + currentVecZ.x, currentOrigin.y + currentVecZ.y, currentOrigin.z + currentVecZ.z)
        )
    }

    // Interpolate and Transform (Legacy wrapper)
    fun interpolateAndTransform(
        originStart: Point3D, xRefStart: Point3D, zRefStart: Point3D,
        originEnd: Point3D, xRefEnd: Point3D, zRefEnd: Point3D,
        t: Double,
        localOffset: Point3D
    ): Point3D {
        val cs = interpolateCoordinateSystem(originStart, xRefStart, zRefStart, originEnd, xRefEnd, zRefEnd, t)
        return cs.transformToGlobal(localOffset)
    }
    
    // --- Orientation Helpers (From jisuan.kt) ---

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

    fun rotationMatrixToEuler(rotationMatrix: Array<DoubleArray>): Triple<Double, Double, Double> {
        val sy = sqrt(rotationMatrix[0][0] * rotationMatrix[0][0] + rotationMatrix[1][0] * rotationMatrix[1][0])
        val singular = sy < 1e-6

        val roll = if (!singular) atan2(rotationMatrix[2][1], rotationMatrix[2][2]) else atan2(-rotationMatrix[1][2], rotationMatrix[1][1])
        val pitch = atan2(-rotationMatrix[2][0], sy)
        val yaw = if (!singular) atan2(rotationMatrix[1][0], rotationMatrix[0][0]) else 0.0

        return Triple(Math.toDegrees(roll), Math.toDegrees(pitch), Math.toDegrees(yaw))
    }

    // Calculate the Euler angles closest to a reference set to avoid flips
    fun rotationMatrixToEulerClosest(
        rotationMatrix: Array<DoubleArray>, 
        refRx: Double, 
        refRy: Double, 
        refRz: Double
    ): Triple<Double, Double, Double> {
        val sy = sqrt(rotationMatrix[0][0] * rotationMatrix[0][0] + rotationMatrix[1][0] * rotationMatrix[1][0])
        val singular = sy < 1e-6

        // Solution 1
        val roll1 = if (!singular) atan2(rotationMatrix[2][1], rotationMatrix[2][2]) else atan2(-rotationMatrix[1][2], rotationMatrix[1][1])
        val pitch1 = atan2(-rotationMatrix[2][0], sy)
        val yaw1 = if (!singular) atan2(rotationMatrix[1][0], rotationMatrix[0][0]) else 0.0
        
        // Solution 2 (Alternative Pitch)
        // Pitch2 = 180 - Pitch1 (or Pi - Pitch1 in radians)
        val pitch2 = Math.PI - pitch1
        val cosP2 = cos(pitch2)
        
        val roll2: Double
        val yaw2: Double
        
        if (!singular) {
            // If not singular, we can derive Roll2/Yaw2 from matrix elements divided by cosP2 (which is -cosP1)
            // But we must be careful with signs.
            // R21 = sin(r)*cos(p), R22 = cos(r)*cos(p)
            // tan(r) = R21/R22. 
            // If cos(p) flips sign, both R21/cos(p) and R22/cos(p) flip sign.
            // So tan(r) stays same, but r changes by 180 degrees (atan2 quadrant change).
            roll2 = roll1 + Math.PI
            yaw2 = yaw1 + Math.PI
        } else {
            // Singular case (Gimbal lock), infinite solutions usually, or coupled.
            // Just stick to Solution 1 for singular case to avoid complexity
            roll2 = roll1
            yaw2 = yaw1
        }
        
        // Convert to Degrees
        val r1 = Math.toDegrees(roll1)
        val p1 = Math.toDegrees(pitch1)
        val y1 = Math.toDegrees(yaw1)
        
        val r2 = Math.toDegrees(roll2)
        val p2 = Math.toDegrees(pitch2)
        val y2 = Math.toDegrees(yaw2)
        
        // Helper to find closest equivalent angle (handling 360 wrap)
        fun closestAngle(target: Double, ref: Double): Double {
            var a = target
            val diff = a - ref
            // Normalize diff to -180..180
            var d = diff % 360.0
            if (d > 180) d -= 360
            if (d < -180) d += 360
            return ref + d
        }
        
        // Optimize Solution 1
        val optR1 = closestAngle(r1, refRx)
        val optP1 = closestAngle(p1, refRy)
        val optY1 = closestAngle(y1, refRz)
        val dist1 = (optR1-refRx).pow(2) + (optP1-refRy).pow(2) + (optY1-refRz).pow(2)
        
        // Optimize Solution 2
        val optR2 = closestAngle(r2, refRx)
        val optP2 = closestAngle(p2, refRy)
        val optY2 = closestAngle(y2, refRz)
        val dist2 = (optR2-refRx).pow(2) + (optP2-refRy).pow(2) + (optY2-refRz).pow(2)
        
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

    // Axis-Angle Rotation Matrix (Rodrigues' formula)
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

    /**
     * Computes the target pose by applying offsets to the Base Pose, relative to the Coordinate System.
     * The position offset is applied along the CS axes.
     * The rotation offset is applied by rotating the Base Pose around the CS axes.
     */
    fun computeTargetPose(
        basePose: Pose,
        cs: CoordinateSystem?,
        offsetX: Double, offsetY: Double, offsetZ: Double,
        offsetRx: Double, offsetRy: Double, offsetRz: Double
    ): Pose {
        // If CS is null, apply offsets in Base Frame (World Frame)
        if (cs == null) {
            return Pose(
                x = basePose.x + offsetX,
                y = basePose.y + offsetY,
                z = basePose.z + offsetZ,
                rx = basePose.rx + offsetRx,
                ry = basePose.ry + offsetRy,
                rz = basePose.rz + offsetRz
            )
        }

        // 1. Position Calculation
        // Extract CS Axes (Columns of Rotation Matrix)
        // Col 0 = Local X Axis (in Global coords)
        // Col 1 = Local Y Axis (in Global coords)
        // Col 2 = Local Z Axis (in Global coords)
        val axisX = Point3D(cs.rotationMatrix[0][0], cs.rotationMatrix[1][0], cs.rotationMatrix[2][0])
        val axisY = Point3D(cs.rotationMatrix[0][1], cs.rotationMatrix[1][1], cs.rotationMatrix[2][1])
        val axisZ = Point3D(cs.rotationMatrix[0][2], cs.rotationMatrix[1][2], cs.rotationMatrix[2][2])
        
        // Transform Local Offsets to Global Offsets
        // Global = R * Local
        // GlobalX = R[0][0]*X + R[0][1]*Y + R[0][2]*Z = axisX.x*X + axisY.x*Y + axisZ.x*Z
        val globalOffsetX = axisX.x * offsetX + axisY.x * offsetY + axisZ.x * offsetZ
        val globalOffsetY = axisX.y * offsetX + axisY.y * offsetY + axisZ.y * offsetZ
        val globalOffsetZ = axisX.z * offsetX + axisY.z * offsetY + axisZ.z * offsetZ
        
        val newX = basePose.x + globalOffsetX
        val newY = basePose.y + globalOffsetY
        val newZ = basePose.z + globalOffsetZ
        
        // 2. Rotation Calculation
        // Apply rotations around the Local Axes (which are defined in Global coords as axisX, axisY, axisZ)
        // User Logic Update: Enable all 3 rotations (Rx, Ry, Rz) to allow user control.
        // Specifically, Ry will rotate around Local Y (Weld Seam or Transverse depending on teaching).
        
        var currentRot = eulerToRotationMatrix(basePose.rx, basePose.ry, basePose.rz)
        
        // Apply Rx around Local X
        if (abs(offsetRx) > 1e-6) {
            val rotMat = axisAngleToRotationMatrix(axisX, offsetRx)
            currentRot = multiplyMatrices(rotMat, currentRot)
        }
        
        // Apply Ry around Local Y
        if (abs(offsetRy) > 1e-6) {
            val rotMat = axisAngleToRotationMatrix(axisY, offsetRy)
            currentRot = multiplyMatrices(rotMat, currentRot)
        }
        
        // Apply Rz around Local Z
        if (abs(offsetRz) > 1e-6) {
            val rotMat = axisAngleToRotationMatrix(axisZ, offsetRz)
            currentRot = multiplyMatrices(rotMat, currentRot)
        }
        
        // Calculate closest Euler angles to avoid large value jumps
        val (newRx, newRy, newRz) = rotationMatrixToEulerClosest(currentRot, basePose.rx, basePose.ry, basePose.rz)
        
        return Pose(newX, newY, newZ, newRx, newRy, newRz)
    }

    fun computeMultiLayerPose(
        cs: CoordinateSystem,
        offsetX: Double, offsetY: Double, offsetZ: Double,
        offsetRx: Double, offsetRy: Double, offsetRz: Double
    ): Pose {
        // Deprecated or used for visualization only if no base pose
        val p3d = cs.transformToGlobal(Point3D(offsetX, offsetY, offsetZ))
        var accumulatedRot = cs.rotationMatrix
        val localX = Point3D(cs.rotationMatrix[0][0], cs.rotationMatrix[0][1], cs.rotationMatrix[0][2])
        val localY = Point3D(cs.rotationMatrix[1][0], cs.rotationMatrix[1][1], cs.rotationMatrix[1][2])
        val localZ = Point3D(cs.rotationMatrix[2][0], cs.rotationMatrix[2][1], cs.rotationMatrix[2][2])
        
        if (abs(offsetRx) > 1e-6) {
            val rxMat = axisAngleToRotationMatrix(localX, offsetRx)
            accumulatedRot = multiplyMatrices(rxMat, accumulatedRot)
        }
        if (abs(offsetRy) > 1e-6) {
            val ryMat = axisAngleToRotationMatrix(localY, offsetRy)
            accumulatedRot = multiplyMatrices(ryMat, accumulatedRot)
        }
        if (abs(offsetRz) > 1e-6) {
            val rzMat = axisAngleToRotationMatrix(localZ, offsetRz)
            accumulatedRot = multiplyMatrices(rzMat, accumulatedRot)
        }
        val (newRx, newRy, newRz) = rotationMatrixToEuler(accumulatedRot)
        return Pose(p3d.x, p3d.y, p3d.z, newRx, newRy, newRz)
    }

    // Helper to keep the old applyLocalRotation if needed, but computeMultiLayerPose is preferred for Multi-Layer
    fun applyLocalRotation(basePose: Pose, cs: CoordinateSystem, offsetRx: Double, offsetRy: Double, offsetRz: Double): Pose {
        // 1. Get current robot rotation matrix
        val currentRot = eulerToRotationMatrix(basePose.rx, basePose.ry, basePose.rz)
        
        // 2. Get Local Axes (Global vectors) from CS
        // cs.rotationMatrix columns are X, Y, Z axes of the local frame
        val localX = Point3D(cs.rotationMatrix[0][0], cs.rotationMatrix[1][0], cs.rotationMatrix[2][0])
        val localY = Point3D(cs.rotationMatrix[0][1], cs.rotationMatrix[1][1], cs.rotationMatrix[2][1])
        val localZ = Point3D(cs.rotationMatrix[0][2], cs.rotationMatrix[1][2], cs.rotationMatrix[2][2])
        
        // 3. Create Rotation Matrices for offsets around Local Axes
        // Order: Rx (around X), then Ry (around Y), then Rz (around Z) - or user preferred order?
        // Usually, welding offsets might be applied independently. Let's assume intrinsic composition or extrinsic sequential.
        // If we rotate around X, then around NEW Y, etc., it's intrinsic.
        // If we rotate around original Local X, original Local Y... (Extrinsic relative to Local Frame).
        
        // Let's accumulate rotation matrix
        var accumulatedRot = currentRot
        
        // Apply Rx offset (Rotation around Local X axis)
        if (abs(offsetRx) > 1e-6) {
            val rxMat = axisAngleToRotationMatrix(localX, offsetRx)
            accumulatedRot = multiplyMatrices(rxMat, accumulatedRot)
        }
        
        // Apply Ry offset (Rotation around Local Y axis)
        if (abs(offsetRy) > 1e-6) {
            val ryMat = axisAngleToRotationMatrix(localY, offsetRy)
            accumulatedRot = multiplyMatrices(ryMat, accumulatedRot)
        }
        
        // Apply Rz offset (Rotation around Local Z axis)
        if (abs(offsetRz) > 1e-6) {
            val rzMat = axisAngleToRotationMatrix(localZ, offsetRz)
            accumulatedRot = multiplyMatrices(rzMat, accumulatedRot)
        }
        
        // 4. Convert back to Euler
        val (newRx, newRy, newRz) = rotationMatrixToEuler(accumulatedRot)
        
        return basePose.copy(rx = newRx, ry = newRy, rz = newRz)
    }}
