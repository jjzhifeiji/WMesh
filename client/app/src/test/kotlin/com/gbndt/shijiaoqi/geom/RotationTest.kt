package com.gbndt.shijiaoqi.geom

import org.junit.Assert.assertEquals
import org.junit.Test
import kotlin.math.abs

/** 旋转矩阵与欧拉互转；不管焊道。 */
class RotationTest {
    @Test
    fun zeroEulerIsIdentity() {
        val m = Rotation.eulerToMatrix(0.0, 0.0, 0.0)
        assertEquals(1.0, m[0][0], 1e-9)
        assertEquals(1.0, m[1][1], 1e-9)
        assertEquals(1.0, m[2][2], 1e-9)
        assertEquals(0.0, m[0][1], 1e-9)
        assertEquals(0.0, m[1][0], 1e-9)
    }

    @Test
    fun eulerRoundtripStaysNearReference() {
        val rx = 10.0
        val ry = 20.0
        val rz = 30.0
        val m = Rotation.eulerToMatrix(rx, ry, rz)
        val (outRx, outRy, outRz) = Rotation.matrixToEulerClosest(m, rx, ry, rz)
        assertEquals(rx, outRx, 1e-6)
        assertEquals(ry, outRy, 1e-6)
        assertEquals(rz, outRz, 1e-6)
    }

    @Test
    fun multiplyByIdentityLeavesMatrix() {
        val m = Rotation.eulerToMatrix(15.0, -8.0, 40.0)
        val i = Rotation.eulerToMatrix(0.0, 0.0, 0.0)
        val out = Rotation.multiply(m, i)
        for (r in 0..2) for (c in 0..2) {
            assertEquals(m[r][c], out[r][c], 1e-9)
        }
    }

    @Test
    fun zAxis90TurnsXTowardY() {
        val m = Rotation.axisAngleToMatrix(Vec3(0.0, 0.0, 1.0), 90.0)
        assertEquals(0.0, m[0][0], 1e-9)
        assertEquals(-1.0, m[0][1], 1e-9)
        assertEquals(1.0, m[1][0], 1e-9)
        assertEquals(0.0, m[1][1], 1e-9)
        assertEquals(1.0, m[2][2], 1e-9)
        assertTrueAbsZero(m[0][2])
        assertTrueAbsZero(m[1][2])
        assertTrueAbsZero(m[2][0])
        assertTrueAbsZero(m[2][1])
    }

    private fun assertTrueAbsZero(v: Double) {
        assertEquals(0.0, abs(v), 1e-9)
    }
}
