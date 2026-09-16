package com.gbndt.shijiaoqi.domain.multilayer

import com.gbndt.shijiaoqi.model.multilayer.MultiLayerWeldPath
import com.gbndt.shijiaoqi.model.Pose
import com.gbndt.shijiaoqi.model.multilayer.WeldPassOffset
import com.gbndt.shijiaoqi.model.single.WeldPath
import com.gbndt.shijiaoqi.model.WeldPoint
import com.gbndt.shijiaoqi.model.WeldPointType
import kotlin.math.abs
import kotlin.math.sqrt

/** 一层偏移后的点：笛卡尔保持原点，偏移另带；无参考点时已叠进笛卡尔。 */
data class OffsetPoint(
    val type: WeldPointType, // 点类型
    val pose: Pose, // 原点笛卡尔；无参考点时已含简单平移
    val joints: List<Double>, // 关节角
    val offsets: List<Double>? = null, // 六元组执行偏移；空列表表示无
) {
    /** 预览用实际笛卡尔：有层偏移就叠上。 */
    fun world(): Pose {
        val o = offsets
        if (o == null || o.size < 6 || o.all { abs(it) < 1e-5 }) return pose
        return pose.copy(
            x = pose.x + o[0],
            y = pose.y + o[1],
            z = pose.z + o[2],
            rx = pose.rx + o[3],
            ry = pose.ry + o[4],
            rz = pose.rz + o[5],
        )
    }
}

/** 多层层偏移：有参考点走现网几何，没有就平移。不管 Lua。 */
object MultiLayerPass {
    /** 一层上的点；缺位姿返回 null。有参考点偏移另带，没有就平移进笛卡尔。 */
    fun generate(basePath: WeldPath, pass: WeldPassOffset, multiPath: MultiLayerWeldPath): List<OffsetPoint>? {
        val startIdx = basePath.points.indexOfFirst { it.type == WeldPointType.START }
        val endIdx = basePath.points.indexOfLast { it.type == WeldPointType.END }
        val middleIdx = basePath.points.indexOfFirst { it.type == WeldPointType.ARC_MIDDLE }

        var totalLen = 0.0
        var lenToMiddle = 0.0
        if (startIdx != -1 && endIdx != -1) {
            for (i in startIdx until endIdx) {
                val p1 = basePath.points[i].pose
                val p2 = basePath.points[i + 1].pose
                if (p1 != null && p2 != null) {
                    val d = dist(p1, p2)
                    totalLen += d
                    if (middleIdx != -1 && i < middleIdx) lenToMiddle += d
                }
            }
        }

        val hasMiddleRef = multiPath.refPointXMiddle != null && multiPath.refPointZMiddle != null &&
            middleIdx != -1 && middleIdx > startIdx && middleIdx < endIdx

        val out = ArrayList<OffsetPoint>(basePath.points.size)
        for ((index, point) in basePath.points.withIndex()) {
            val basePose = point.pose ?: return null
            if (point.type == WeldPointType.START_SAFE || point.type == WeldPointType.END_SAFE) {
                out += OffsetPoint(point.type, basePose, jointsOf(point), emptyList())
                continue
            }
            val currentIdx = index
            val hasRefs = multiPath.refPointX1 != null && multiPath.refPointZ1 != null &&
                multiPath.refPointXEnd != null && multiPath.refPointZEnd != null &&
                startIdx != -1 && endIdx != -1 && currentIdx >= startIdx && currentIdx <= endIdx
            if (!hasRefs) {
                val simple = MultiLayerRun.simpleOffset(
                    basePose,
                    point.type,
                    currentIdx == startIdx,
                    currentIdx == endIdx,
                    pass.valX,
                    pass.valYLeft,
                    pass.valYRight,
                    pass.valZ,
                )
                out += OffsetPoint(point.type, simple, jointsOf(point), emptyList())
                continue
            }

            var currentLen = 0.0
            for (i in startIdx until currentIdx) {
                val p1 = basePath.points[i].pose
                val p2 = basePath.points[i + 1].pose
                if (p1 != null && p2 != null) currentLen += dist(p1, p2)
            }
            val interpolatedY = when (currentIdx) {
                startIdx -> pass.valYLeft
                endIdx -> pass.valYRight
                else -> 0.0
            }
            val originStart = basePath.points[startIdx].pose!!
            val originEnd = basePath.points[endIdx].pose!!
            val originA = basePose

            val (refX, refZ) = if (hasMiddleRef) {
                if (currentLen <= lenToMiddle) {
                    val segmentT = if (lenToMiddle > 1e-6) currentLen / lenToMiddle else 0.0
                    val vecXStart = subtractPoses(multiPath.refPointX1!!.pose, originStart)
                    val vecXMid = subtractPoses(multiPath.refPointXMiddle!!.pose, basePath.points[middleIdx].pose!!)
                    val vecXCurrent = lerpPose(vecXStart, vecXMid, segmentT)
                    val b = originA.copy(
                        x = originA.x + vecXCurrent.x,
                        y = originA.y + vecXCurrent.y,
                        z = originA.z + vecXCurrent.z,
                    )
                    val vecZStart = subtractPoses(multiPath.refPointZ1!!.pose, originStart)
                    val vecZMid = subtractPoses(multiPath.refPointZMiddle!!.pose, basePath.points[middleIdx].pose!!)
                    val vecZCurrent = lerpPose(vecZStart, vecZMid, segmentT)
                    val c = originA.copy(
                        x = originA.x + vecZCurrent.x,
                        y = originA.y + vecZCurrent.y,
                        z = originA.z + vecZCurrent.z,
                    )
                    Pair(b, c)
                } else {
                    val segmentLen = totalLen - lenToMiddle
                    val distFromMiddle = currentLen - lenToMiddle
                    val segmentT = if (segmentLen > 1e-6) distFromMiddle / segmentLen else 0.0
                    val vecXMid = subtractPoses(multiPath.refPointXMiddle!!.pose, basePath.points[middleIdx].pose!!)
                    val vecXEnd = subtractPoses(multiPath.refPointXEnd!!.pose, originEnd)
                    val vecXCurrent = lerpPose(vecXMid, vecXEnd, segmentT)
                    val b = originA.copy(
                        x = originA.x + vecXCurrent.x,
                        y = originA.y + vecXCurrent.y,
                        z = originA.z + vecXCurrent.z,
                    )
                    val vecZMid = subtractPoses(multiPath.refPointZMiddle!!.pose, basePath.points[middleIdx].pose!!)
                    val vecZEnd = subtractPoses(multiPath.refPointZEnd!!.pose, originEnd)
                    val vecZCurrent = lerpPose(vecZMid, vecZEnd, segmentT)
                    val c = originA.copy(
                        x = originA.x + vecZCurrent.x,
                        y = originA.y + vecZCurrent.y,
                        z = originA.z + vecZCurrent.z,
                    )
                    Pair(b, c)
                }
            } else {
                val vecXStart = subtractPoses(multiPath.refPointX1!!.pose, originStart)
                val vecZStart = subtractPoses(multiPath.refPointZ1!!.pose, originStart)
                var vx = Vector3(vecXStart.x, vecXStart.y, vecXStart.z)
                var vz = Vector3(vecZStart.x, vecZStart.y, vecZStart.z)
                // Bishop 把起点参考系沿折线转到当前点，拐角处才跟得上。
                for (idx in startIdx until currentIdx - 1) {
                    val p0 = basePath.points[idx].pose ?: continue
                    val p1 = basePath.points[idx + 1].pose ?: continue
                    var p2: Pose? = null
                    for (j in idx + 2 until basePath.points.size) {
                        val pt = basePath.points[j]
                        if (pt.type != WeldPointType.START_SAFE && pt.type != WeldPointType.END_SAFE) {
                            p2 = pt.pose
                            break
                        }
                    }
                    if (p2 != null) {
                        val t0 = Vector3(p1.x - p0.x, p1.y - p0.y, p1.z - p0.z).normalize()
                        val t1 = Vector3(p2.x - p1.x, p2.y - p1.y, p2.z - p1.z).normalize()
                        val crossT = t0.cross(t1)
                        val sinT = crossT.length()
                        if (sinT > 1e-6) {
                            val axis = crossT.normalize()
                            val cosT = t0.dot(t1)
                            vx = vx * cosT + axis.cross(vx) * sinT + axis * (axis.dot(vx)) * (1.0 - cosT)
                            vz = vz * cosT + axis.cross(vz) * sinT + axis * (axis.dot(vz)) * (1.0 - cosT)
                        }
                    }
                }
                val b = originA.copy(x = originA.x + vx.x, y = originA.y + vx.y, z = originA.z + vx.z)
                val c = originA.copy(x = originA.x + vz.x, y = originA.y + vz.y, z = originA.z + vz.z)
                Pair(b, c)
            }

            val refXVec = Vector3(refX.x - originA.x, refX.y - originA.y, refX.z - originA.z)
            val refZVec = Vector3(refZ.x - originA.x, refZ.y - originA.y, refZ.z - originA.z)
            val offsetVec = getOffsetForPoint(
                currentIdx = currentIdx,
                allPoints = basePath.points,
                refXVec = refXVec,
                refZVec = refZVec,
                offX = pass.valX,
                offY = interpolatedY,
                offZ = pass.valZ,
            )
            val dcddA = DcddPoint(originA.x, originA.y, originA.z)
            val dcddB = DcddPoint(refX.x, refX.y, refX.z)
            val dcddC = DcddPoint(refZ.x, refZ.y, refZ.z)
            val coordSys = DcddCoordinateSystem(dcddA, dcddB, dcddC)
            val (_, euler) = coordSys.rotateBAByAngle(pass.valR)
            val offsets = listOf(
                offsetVec.x,
                offsetVec.y,
                offsetVec.z,
                Math.toDegrees(euler.first),
                Math.toDegrees(euler.second) - 90,
                Math.toDegrees(euler.third),
            )
            out += OffsetPoint(point.type, originA, jointsOf(point), offsets)
        }
        return out
    }
}

private fun jointsOf(point: WeldPoint): List<Double> =
    point.jointAngles?.takeIf { it.size >= 6 } ?: List(6) { 0.0 }

private fun dist(a: Pose, b: Pose): Double {
    val dx = b.x - a.x
    val dy = b.y - a.y
    val dz = b.z - a.z
    return sqrt(dx * dx + dy * dy + dz * dz)
}

private data class Vector3(val x: Double, val y: Double, val z: Double) {
    operator fun plus(other: Vector3) = Vector3(x + other.x, y + other.y, z + other.z)
    operator fun minus(other: Vector3) = Vector3(x - other.x, y - other.y, z - other.z)
    operator fun times(scalar: Double) = Vector3(x * scalar, y * scalar, z * scalar)
    fun length() = sqrt(x * x + y * y + z * z)
    fun normalize(): Vector3 {
        val len = length()
        return if (len > 0) Vector3(x / len, y / len, z / len) else Vector3(0.0, 0.0, 0.0)
    }
    fun cross(other: Vector3) = Vector3(
        y * other.z - z * other.y,
        z * other.x - x * other.z,
        x * other.y - y * other.x
    )
    fun dot(other: Vector3) = x * other.x + y * other.y + z * other.z
}

private fun subtractPoses(p1: Pose, p2: Pose): Pose {
    return Pose(
        x = p1.x - p2.x,
        y = p1.y - p2.y,
        z = p1.z - p2.z,
        rx = p1.rx - p2.rx,
        ry = p1.ry - p2.ry,
        rz = p1.rz - p2.rz
    )
}

private fun lerpPose(p1: Pose, p2: Pose, t: Double): Pose {
    return Pose(
        x = p1.x + (p2.x - p1.x) * t,
        y = p1.y + (p2.y - p1.y) * t,
        z = p1.z + (p2.z - p1.z) * t,
        rx = p1.rx + (p2.rx - p1.rx) * t,
        ry = p1.ry + (p2.ry - p1.ry) * t,
        rz = p1.rz + (p2.rz - p1.rz) * t
    )
}

private fun getOffsetForPoint(
    currentIdx: Int,
    allPoints: List<WeldPoint>,
    refXVec: Vector3,
    refZVec: Vector3,
    offX: Double,
    offY: Double,
    offZ: Double
): Vector3 {
    val currentPt = allPoints[currentIdx]
    val pCurr = currentPt.pose!!.let { Vector3(it.x, it.y, it.z) }

    var prevPt: WeldPoint? = null
    var vPrev: Vector3? = null
    var prevIndex = -1
    for (i in currentIdx - 1 downTo 0) {
        val pt = allPoints[i]
        if (pt.type != WeldPointType.START_SAFE && pt.type != WeldPointType.END_SAFE) {
            prevPt = pt
            vPrev = pt.pose?.let { Vector3(it.x, it.y, it.z) }
            prevIndex = i
            break
        }
    }

    var nextPt: WeldPoint? = null
    var vNext: Vector3? = null
    var nextIndex = -1
    for (i in currentIdx + 1 until allPoints.size) {
        val pt = allPoints[i]
        if (pt.type != WeldPointType.START_SAFE && pt.type != WeldPointType.END_SAFE) {
            nextPt = pt
            vNext = pt.pose?.let { Vector3(it.x, it.y, it.z) }
            nextIndex = i
            break
        }
    }

    fun getFrame(p1: Vector3, p2: Vector3, rx: Vector3, rz: Vector3): Pair<Vector3, Vector3> {
        val t = (p2 - p1).normalize()
        val projX = rx - t * rx.dot(t)
        var n = if (projX.length() > 1e-3) projX.normalize() else t.cross(Vector3(0.0, 0.0, 1.0)).normalize()
        if (n.length() < 1e-3) n = Vector3(1.0, 0.0, 0.0)
        
        val projZ = rz - t * rz.dot(t)
        val b = if (projZ.length() > 1e-3) projZ.normalize() else n.cross(t).normalize()
        return Pair(n, b)
    }
    
    fun getCircleParams(p1: Vector3, p2: Vector3, p3: Vector3): Triple<Vector3, Vector3, Double>? {
        val v1 = p2 - p1
        val v2 = p3 - p2
        var normal = v1.cross(v2)
        if (normal.length() < 1e-3) return null
        normal = normal.normalize()
        val m1 = (p1 + p2) * 0.5
        val m2 = (p2 + p3) * 0.5
        val d1 = v1.cross(normal).normalize()
        val d2 = v2.cross(normal).normalize()
        val det = d1.cross(d2).dot(normal)
        if (kotlin.math.abs(det) < 1e-3) return null
        val t = (m2 - m1).cross(d2).dot(normal) / det
        val center = m1 + d1 * t
        val radius = (p1 - center).length()
        return Triple(center, normal, radius)
    }
    
    fun solveLineArcIntersect(
        lineStart: Vector3, lineEnd: Vector3,
        arcStart: Vector3, arcMid: Vector3, arcEnd: Vector3,
        rx: Vector3, rz: Vector3,
        isArcFirst: Boolean
    ): Vector3? {
        val tLine = (lineEnd - lineStart).normalize()
        val (nLine, bLine) = getFrame(lineStart, lineEnd, rx, rz)
        
        val pTransition = if (isArcFirst) lineStart else lineEnd
        val lineOffsetOrigin = pTransition + nLine * offX + bLine * offZ
        
        val params = getCircleParams(arcStart, arcMid, arcEnd) ?: return null
        val (center, arcNormal, radius) = params
        
        val radial_mid = (arcMid - center).normalize()
        var tMid = arcNormal.cross(radial_mid).normalize()
        if ((arcEnd - arcStart).dot(tMid) < 0) tMid = tMid * -1.0
        
        val (nMid, bMid) = getFrame(arcMid, arcMid + tMid, rx, rz)
        val vOffMid = nMid * offX + bMid * offZ
        val delta_R = vOffMid.dot(radial_mid)
        val shift = vOffMid - radial_mid * delta_R
        
        val offsetRadius = radius + delta_R
        val offsetCenter = center + shift
        
        val V = lineOffsetOrigin - offsetCenter
        val a = 1.0
        val b = 2 * V.dot(tLine)
        val c = V.dot(V) - offsetRadius * offsetRadius
        val delta = b * b - 4 * a * c
        if (delta < 0) return lineOffsetOrigin
        
        val t1 = (-b - sqrt(delta)) / (2 * a)
        val t2 = (-b + sqrt(delta)) / (2 * a)
        
        val p1 = lineOffsetOrigin + tLine * t1
        val p2 = lineOffsetOrigin + tLine * t2
        val dist1 = kotlin.math.abs(t1)
        val dist2 = kotlin.math.abs(t2)
        
        return if (dist1 < dist2) p1 else p2
    }

    val offsetVec = when {
        // 1. ARC MIDDLE
        currentPt.type == WeldPointType.ARC_MIDDLE -> {
            if (vPrev != null && vNext != null) {
                val params = getCircleParams(vPrev, pCurr, vNext)
                if (params != null) {
                    val (center, arcNormal, _) = params
                    val radial_mid = (pCurr - center).normalize()
                    var tMid = arcNormal.cross(radial_mid).normalize()
                    if ((vNext - vPrev).dot(tMid) < 0) tMid = tMid * -1.0
                    
                    val (nMid, bMid) = getFrame(pCurr, pCurr + tMid, refXVec, refZVec)
                    val vOffMid = nMid * offX + bMid * offZ
                    val delta_R = vOffMid.dot(radial_mid)
                    val shift = vOffMid - radial_mid * delta_R
                    
                    radial_mid * delta_R + shift + tMid * offY
                } else {
                    // Collinear fallback
                    val tIn = (pCurr - vPrev).normalize()
                    val tOut = (vNext - pCurr).normalize()
                    val (nIn, bIn) = getFrame(vPrev, pCurr, refXVec, refZVec)
                    val (nOut, bOut) = getFrame(pCurr, vNext, refXVec, refZVec)
                    val nAvg = (nIn + nOut).normalize()
                    val dotN = nIn.dot(nOut)
                    val kX = if (dotN > -0.99) 1.0 / sqrt((1 + dotN) / 2) else 1.0
                    val bAvg = (bIn + bOut).normalize()
                    val dotB = bIn.dot(bOut)
                    val kZ = if (dotB > -0.99) 1.0 / sqrt((1 + dotB) / 2) else 1.0
                    val tAvg = (tIn + tOut).normalize()
                    nAvg * (offX * kX) + bAvg * (offZ * kZ) + tAvg * offY
                }
            } else {
                Vector3(0.0, 0.0, 0.0)
            }
        }

        // 2. LINE -> ARC Transition (Arc Start)
        vPrev != null && vNext != null && nextPt?.type == WeldPointType.ARC_MIDDLE -> {
            var vNextNext: Vector3? = null
            for (i in nextIndex + 1 until allPoints.size) {
                val pt = allPoints[i]
                if (pt.type != WeldPointType.START_SAFE && pt.type != WeldPointType.END_SAFE) {
                    pt.pose?.let { vNextNext = Vector3(it.x, it.y, it.z) }
                    break
                }
            }
            val localVNextNext = vNextNext
            if (localVNextNext != null) {
                val intersect = solveLineArcIntersect(
                    lineStart = vPrev, lineEnd = pCurr,
                    arcStart = pCurr, arcMid = vNext, arcEnd = localVNextNext,
                    rx = refXVec, rz = refZVec,
                    isArcFirst = false
                )
                if (intersect != null) {
                    intersect - pCurr
                } else {
                    val tIn = (pCurr - vPrev).normalize()
                    val tOut = (vNext - pCurr).normalize()
                    val (nIn, bIn) = getFrame(vPrev, pCurr, refXVec, refZVec)
                    val (nOut, bOut) = getFrame(pCurr, vNext, refXVec, refZVec)
                    val nAvg = (nIn + nOut).normalize()
                    val kX = if (nIn.dot(nOut) > -0.99) 1.0 / sqrt((1 + nIn.dot(nOut)) / 2) else 1.0
                    val bAvg = (bIn + bOut).normalize()
                    val kZ = if (bIn.dot(bOut) > -0.99) 1.0 / sqrt((1 + bIn.dot(bOut)) / 2) else 1.0
                    nAvg * (offX * kX) + bAvg * (offZ * kZ) + ((tIn + tOut).normalize()) * offY
                }
            } else {
                Vector3(0.0, 0.0, 0.0)
            }
        }

        // 3. ARC -> LINE Transition (Arc End)
        vPrev != null && vNext != null && prevPt?.type == WeldPointType.ARC_MIDDLE -> {
            var vPrevPrev: Vector3? = null
            for (i in prevIndex - 1 downTo 0) {
                val pt = allPoints[i]
                if (pt.type != WeldPointType.START_SAFE && pt.type != WeldPointType.END_SAFE) {
                    pt.pose?.let { vPrevPrev = Vector3(it.x, it.y, it.z) }
                    break
                }
            }
            val localVPrevPrev = vPrevPrev
            if (localVPrevPrev != null) {
                val intersect = solveLineArcIntersect(
                    lineStart = pCurr, lineEnd = vNext,
                    arcStart = localVPrevPrev, arcMid = vPrev, arcEnd = pCurr,
                    rx = refXVec, rz = refZVec,
                    isArcFirst = true
                )
                if (intersect != null) {
                    intersect - pCurr
                } else {
                    val tIn = (pCurr - vPrev).normalize()
                    val tOut = (vNext - pCurr).normalize()
                    val (nIn, bIn) = getFrame(vPrev, pCurr, refXVec, refZVec)
                    val (nOut, bOut) = getFrame(pCurr, vNext, refXVec, refZVec)
                    val nAvg = (nIn + nOut).normalize()
                    val kX = if (nIn.dot(nOut) > -0.99) 1.0 / sqrt((1 + nIn.dot(nOut)) / 2) else 1.0
                    val bAvg = (bIn + bOut).normalize()
                    val kZ = if (bIn.dot(bOut) > -0.99) 1.0 / sqrt((1 + bIn.dot(bOut)) / 2) else 1.0
                    nAvg * (offX * kX) + bAvg * (offZ * kZ) + ((tIn + tOut).normalize()) * offY
                }
            } else {
                Vector3(0.0, 0.0, 0.0)
            }
        }

        // 4. LINE -> LINE Transition (Miter Joint)
        vPrev != null && vNext != null -> {
            val tIn = (pCurr - vPrev).normalize()
            val tOut = (vNext - pCurr).normalize()
            
            val crossT = tIn.cross(tOut)
            val sinT = crossT.length()
            val refXOut = if (sinT > 1e-6) {
                val axis = crossT.normalize()
                val cosT = tIn.dot(tOut)
                refXVec * cosT + axis.cross(refXVec) * sinT + axis * (axis.dot(refXVec)) * (1.0 - cosT)
            } else refXVec
            
            val refZOut = if (sinT > 1e-6) {
                val axis = crossT.normalize()
                val cosT = tIn.dot(tOut)
                refZVec * cosT + axis.cross(refZVec) * sinT + axis * (axis.dot(refZVec)) * (1.0 - cosT)
            } else refZVec
            
            val (nIn, bIn) = getFrame(vPrev, pCurr, refXVec, refZVec)
            val (nOut, bOut) = getFrame(pCurr, vNext, refXOut, refZOut)
            val nAvg = (nIn + nOut).normalize()
            val dotN = nIn.dot(nOut)
            val kX = if (dotN > -0.99) 1.0 / sqrt((1 + dotN) / 2) else 1.0
            val bAvg = (bIn + bOut).normalize()
            val dotB = bIn.dot(bOut)
            val kZ = if (dotB > -0.99) 1.0 / sqrt((1 + dotB) / 2) else 1.0
            val tAvg = (tIn + tOut).normalize()
            nAvg * (offX * kX) + bAvg * (offZ * kZ) + tAvg * offY
        }

        // 5. START POINT
        vNext != null -> {
            if (nextPt?.type == WeldPointType.ARC_MIDDLE) {
                var vNextNext: Vector3? = null
                for (i in nextIndex + 1 until allPoints.size) {
                    val pt = allPoints[i]
                    if (pt.type != WeldPointType.START_SAFE && pt.type != WeldPointType.END_SAFE) {
                        pt.pose?.let { vNextNext = Vector3(it.x, it.y, it.z) }
                        break
                    }
                }
                val localVNextNext = vNextNext
                if (localVNextNext != null) {
                    val params = getCircleParams(pCurr, vNext, localVNextNext)
                    if (params != null) {
                        val (center, normal, _) = params
                        val radial = (pCurr - center).normalize()
                        var tStart = normal.cross(radial).normalize()
                        if ((vNext - pCurr).dot(tStart) < 0) tStart = tStart * -1.0
                        val (n, b) = getFrame(pCurr, pCurr + tStart, refXVec, refZVec)
                        return n * offX + tStart * offY + b * offZ
                    }
                }
            }
            val t = (vNext - pCurr).normalize()
            val (n, b) = getFrame(pCurr, vNext, refXVec, refZVec)
            n * offX + t * offY + b * offZ
        }

        // 6. END POINT
        vPrev != null -> {
            if (prevPt?.type == WeldPointType.ARC_MIDDLE) {
                var vPrevPrev: Vector3? = null
                for (i in prevIndex - 1 downTo 0) {
                    val pt = allPoints[i]
                    if (pt.type != WeldPointType.START_SAFE && pt.type != WeldPointType.END_SAFE) {
                        pt.pose?.let { vPrevPrev = Vector3(it.x, it.y, it.z) }
                        break
                    }
                }
                val localVPrevPrev = vPrevPrev
                if (localVPrevPrev != null) {
                    val params = getCircleParams(localVPrevPrev, vPrev, pCurr)
                    if (params != null) {
                        val (center, normal, _) = params
                        val radial = (pCurr - center).normalize()
                        var tEnd = normal.cross(radial).normalize()
                        if ((pCurr - vPrev).dot(tEnd) < 0) tEnd = tEnd * -1.0
                        val (n, b) = getFrame(pCurr, pCurr + tEnd, refXVec, refZVec)
                        return n * offX + tEnd * offY + b * offZ
                    }
                }
            }
            val t = (pCurr - vPrev).normalize()
            val (n, b) = getFrame(vPrev, pCurr, refXVec, refZVec)
            n * offX + t * offY + b * offZ
        }

        else -> Vector3(0.0, 0.0, 0.0)
    }
    return offsetVec
}
