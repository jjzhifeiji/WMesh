package com.gbndt.shijiaoqi.domain.weld

import com.gbndt.shijiaoqi.model.Pose
import com.gbndt.shijiaoqi.model.WeldPoint
import com.gbndt.shijiaoqi.model.WeldPointType
import kotlin.math.abs
import kotlin.math.atan2
import kotlin.math.cos
import kotlin.math.sin
import kotlin.math.sqrt

/** 按点进度算焊长、断点圆弧中点；不管 Lua。 */
object WeldProgress {
    /** 笛卡尔直线距离，毫米；断点余长用。 */
    fun linearMm(from: Pose?, to: Pose?): Double = dist(from, to)

    /** 两点之间的段长，毫米；圆弧按两段弦对圆心角。 */
    fun segmentLength(points: List<WeldPoint>, fromIndex: Int, toIndex: Int): Double {
        if (fromIndex < 0 || toIndex < 0) return 0.0
        val pFrom = points.getOrNull(fromIndex) ?: return 0.0
        val pTo = points.getOrNull(toIndex) ?: return 0.0
        if (pTo.type == WeldPointType.ARC_MIDDLE) {
            val pEnd = points.getOrNull(toIndex + 1)
            if (pEnd != null) return arcPart(pFrom, pTo, pEnd, firstHalf = true)
        }
        if (pFrom.type == WeldPointType.ARC_MIDDLE) {
            val pStart = points.getOrNull(fromIndex - 1)
            if (pStart != null) return arcPart(pStart, pFrom, pTo, firstHalf = false)
        }
        return dist(pFrom.pose, pTo.pose)
    }

    /** 停在圆弧上时，用当前位置重算圆中，避免指令点报错。 */
    fun newArcMid(pStart: Pose, pMid: Pose, pEnd: Pose, pCurrent: Pose): Pose {
        val v1 = v(pStart)
        val v2 = v(pMid)
        val v3 = v(pEnd)
        val vC = v(pCurrent)
        val v12 = v2 - v1
        val v23 = v3 - v2
        var normal = v12.cross(v23)
        if (normal.length() < 1e-6) {
            val fallback = (vC + v3) * 0.5
            val vC3 = v3 - vC
            var arbitrary = Vec3(1.0, 0.0, 0.0)
            if (abs(vC3.normalize().x) > 0.9) arbitrary = Vec3(0.0, 1.0, 0.0)
            val tiny = vC3.cross(arbitrary).normalize() * 0.01
            return pCurrent.copy(x = fallback.x + tiny.x, y = fallback.y + tiny.y, z = fallback.z + tiny.z)
        }
        normal = normal.normalize()
        val m1 = (v1 + v2) * 0.5
        val m2 = (v2 + v3) * 0.5
        val d1 = v12.cross(normal).normalize()
        val d2 = v23.cross(normal).normalize()
        val det = d1.cross(d2).dot(normal)
        if (abs(det) < 1e-6) {
            val fallback = (vC + v3) * 0.5
            val tiny = normal * 0.01
            return pCurrent.copy(x = fallback.x + tiny.x, y = fallback.y + tiny.y, z = fallback.z + tiny.z)
        }
        val t = (m2 - m1).cross(d2).dot(normal) / det
        val center = m1 + d1 * t
        val radius = (v1 - center).length()
        val vCProj = vC - normal * (vC - center).dot(normal)
        val cp1 = (v1 - center).normalize()
        val cp2 = (v2 - center).normalize()
        val cp3 = (v3 - center).normalize()
        val cpC = (vCProj - center).normalize()
        val xAxis = cp1
        val yAxis = normal.cross(cp1).normalize()
        fun angle(vec: Vec3): Double {
            var a = atan2(vec.dot(yAxis), vec.dot(xAxis))
            if (a < 0) a += 2 * Math.PI
            return a
        }
        val angleM = angle(cp2)
        var angleE = angle(cp3)
        var aC = angle(cpC)
        if (angleE <= 1e-5) angleE = 2 * Math.PI
        if (aC > angleE) {
            aC = if (2 * Math.PI - aC < aC - angleE) 0.0 else angleE - 0.001
        }
        val newMid = if (aC < angleM) {
            v2
        } else {
            val midAngle = (aC + angleE) / 2.0
            center + (xAxis * cos(midAngle) + yAxis * sin(midAngle)) * radius
        }
        fun shortest(a1: Double, a2: Double): Double {
            var diff = (a2 - a1) % 360.0
            if (diff > 180.0) diff -= 360.0
            if (diff < -180.0) diff += 360.0
            return a1 + diff / 2.0
        }
        return pCurrent.copy(
            x = newMid.x,
            y = newMid.y,
            z = newMid.z,
            rx = shortest(pCurrent.rx, pEnd.rx),
            ry = shortest(pCurrent.ry, pEnd.ry),
            rz = shortest(pCurrent.rz, pEnd.rz),
        )
    }

    /** 断点关节与终点关节取中点，给续圆弧当种子。 */
    fun midJoints(current: List<Double>, end: List<Double>): List<Double> {
        if (current.size < 6 || end.size < 6) return current
        return List(6) { i -> (current[i] + end[i]) / 2.0 }
    }

    private fun arcPart(pStart: WeldPoint, pMid: WeldPoint, pEnd: WeldPoint, firstHalf: Boolean): Double {
        val v1 = pStart.pose?.let { v(it) } ?: return 0.0
        val v2 = pMid.pose?.let { v(it) } ?: return 0.0
        val v3 = pEnd.pose?.let { v(it) } ?: return 0.0
        val v12 = v2 - v1
        val v23 = v3 - v2
        if (v12.cross(v23).length() < 1e-3) {
            return if (firstHalf) v12.length() else v23.length()
        }
        val normal = v12.cross(v23).normalize()
        val m1 = (v1 + v2) * 0.5
        val m2 = (v2 + v3) * 0.5
        val d1 = v12.cross(normal).normalize()
        val d2 = v23.cross(normal).normalize()
        val det = d1.cross(d2).dot(normal)
        if (abs(det) < 1e-3) return if (firstHalf) v12.length() else v23.length()
        val t = (m2 - m1).cross(d2).dot(normal) / det
        val center = m1 + d1 * t
        val r = (v1 - center).length()
        val ang12 = angleBetween(v1 - center, v2 - center, normal)
        val ang23 = angleBetween(v2 - center, v3 - center, normal)
        return if (firstHalf) r * ang12 else r * ang23
    }

    private fun angleBetween(a: Vec3, b: Vec3, normal: Vec3): Double {
        var angle = atan2(a.cross(b).dot(normal), a.dot(b))
        if (angle < 0) angle += 2 * Math.PI
        return angle
    }

    private fun dist(a: Pose?, b: Pose?): Double {
        if (a == null || b == null) return 0.0
        val d = v(b) - v(a)
        return d.length()
    }

    private fun v(p: Pose) = Vec3(p.x, p.y, p.z)

    private data class Vec3(val x: Double, val y: Double, val z: Double) {
        operator fun plus(o: Vec3) = Vec3(x + o.x, y + o.y, z + o.z)
        operator fun minus(o: Vec3) = Vec3(x - o.x, y - o.y, z - o.z)
        operator fun times(s: Double) = Vec3(x * s, y * s, z * s)
        fun length() = sqrt(x * x + y * y + z * z)
        fun normalize(): Vec3 {
            val len = length()
            return if (len > 0) Vec3(x / len, y / len, z / len) else Vec3(0.0, 0.0, 0.0)
        }
        fun cross(o: Vec3) = Vec3(y * o.z - z * o.y, z * o.x - x * o.z, x * o.y - y * o.x)
        fun dot(o: Vec3) = x * o.x + y * o.y + z * o.z
    }
}
