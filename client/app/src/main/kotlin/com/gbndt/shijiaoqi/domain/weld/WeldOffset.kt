package com.gbndt.shijiaoqi.domain.weld

import com.gbndt.shijiaoqi.model.Pose
import com.gbndt.shijiaoqi.model.WeldPointType
import com.gbndt.shijiaoqi.model.WeldProcess
import kotlin.math.sqrt

/** 算工艺偏移用的点：类型、笛卡尔、X 向参考。 */
data class OffsetSite(
    val type: WeldPointType, // 点类型
    val pose: Pose?, // 笛卡尔；未采集为空
    val refX: Pose? = null, // 焊缝 X 向参考，可空
)

/** 焊缝坐标系工艺偏移；安全点为 0。不管 Lua。 */
object WeldOffset {
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


    /** 当前点工艺偏移；安全点为 0。七轴时 X 取反。 */
    fun at(
        index: Int,
        sites: List<OffsetSite>,
        process: WeldProcess,
        flipX: Boolean,
    ): Triple<Double, Double, Double> {
        val current = sites[index]
        val type = current.type
        if (type == WeldPointType.START_SAFE || type == WeldPointType.END_SAFE) {
            return Triple(0.0, 0.0, 0.0)
        }

        val offXRaw = process.offsetX.toDoubleOrNull() ?: 0.0
        val offX = if (flipX) -offXRaw else offXRaw
        val offY = process.offsetY.toDoubleOrNull() ?: 0.0
        val offZ = process.offsetZ.toDoubleOrNull() ?: 0.0

        if (offX == 0.0 && offY == 0.0 && offZ == 0.0) {
            return Triple(0.0, 0.0, 0.0)
        }

        val pCurr = current.pose?.let { Vector3(it.x, it.y, it.z) } ?: return Triple(0.0, 0.0, 0.0)
        val globalZ = Vector3(0.0, 0.0, 1.0)

        // Find Prev Point
        var vPrev: Vector3? = null
        var prevIndex = -1
        for (i in index - 1 downTo 0) {
            val pt = sites[i]
            if (pt.type != WeldPointType.START_SAFE && pt.type != WeldPointType.END_SAFE) {
                pt.pose?.let { vPrev = Vector3(it.x, it.y, it.z) }
                prevIndex = i
                break
            }
        }

        // Find Next Point
        var vNext: Vector3? = null
        var nextPt: OffsetSite? = null
        var nextIndex = -1
        for (i in index + 1 until sites.size) {
            val pt = sites[i]
            if (pt.type != WeldPointType.START_SAFE && pt.type != WeldPointType.END_SAFE) {
                pt.pose?.let { vNext = Vector3(it.x, it.y, it.z) }
                nextPt = pt
                nextIndex = i
                break
            }
        }

        // Helper to get Weld Frame (N=Horizontal, B=Perp)
        fun getFrame(p1: Vector3, p2: Vector3, refX: Vector3? = null): Pair<Vector3, Vector3> {
            val t = (p2 - p1).normalize()
            
            var n: Vector3
            if (refX != null) {
                // Use Ref Point logic:
                // X direction is perpendicular to T, in the plane defined by T and RefPoint.
                // Actually user said: "这个点和起点连线不一定垂直于焊缝，只是给出一个x方向，真正的x方向还需要起点和终点连线后，然后垂直于这条连线的才是x方向"
                // So, define plane using T and (Ref - P1). Then N = vector in that plane, perp to T.
                // Or simply: N_raw = Ref - P1. Then N = Project N_raw onto plane perp to T.
                // N = (N_raw - (N_raw.dot(T)) * T).normalize()
                
                // Let's use the Ref point relative to p1
                val vRef = (refX - p1)
                
                // Project vRef onto plane perpendicular to T to get X direction (N)
                // N = vRef - (vRef . T) * T
                val projection = vRef - t * vRef.dot(t)
                
                if (projection.length() > 1e-3) {
                    n = projection.normalize()
                } else {
                    // Ref point is collinear with T, fallback to global Z logic
                    n = t.cross(globalZ).normalize()
                    if (n.length() < 1e-3) n = Vector3(1.0, 0.0, 0.0)
                }
            } else {
                // Default logic: N = T x Z (Horizontal)
                n = t.cross(globalZ).normalize()
                if (n.length() < 1e-3) n = Vector3(1.0, 0.0, 0.0) 
            }
            
            // B = N x T (Z offset dir) - User said: "z方向就是垂直于x，y方向" => B = N x T
            val b = n.cross(t).normalize() 
            return Pair(n, b)
        }
        
        // Helper to solve Line-Circle Intersection for Line->Arc Transition
        fun solveLineArcIntersect(
            lineStart: Vector3, lineEnd: Vector3,
            arcStart: Vector3, arcMid: Vector3, arcEnd: Vector3,
            refX: Vector3? = null
        ): Vector3? {
            // Line Params
            val tLine = (lineEnd - lineStart).normalize()
            val (nLine, bLine) = getFrame(lineStart, lineEnd, refX)
            val lineOffsetOrigin = lineEnd + nLine * offX + tLine * offY + bLine * offZ // End of offset line
            
            // Arc Params
            val (center, normal, radius) = getCircleParams(arcStart, arcMid, arcEnd) ?: return null
            val offsetRadius = radius + offX // Radial offset
            val offsetCenter = center + normal * offZ
            
            // Solve |(lineOffsetOrigin + t*tLine) - offsetCenter|^2 = offsetRadius^2
            // Let V = lineOffsetOrigin - offsetCenter
            val V = lineOffsetOrigin - offsetCenter
            // t^2 + 2(V.tLine)t + (V.V - R^2) = 0
            val a = 1.0
            val b = 2 * V.dot(tLine)
            val c = V.dot(V) - offsetRadius * offsetRadius
            
            val delta = b * b - 4 * a * c
            if (delta < 0) return null // No intersection
            
            // Two solutions, pick closest to 0 (closest to original transition point)
            val t1 = (-b - sqrt(delta)) / (2 * a)
            val t2 = (-b + sqrt(delta)) / (2 * a)
            
            val t = if (kotlin.math.abs(t1) < kotlin.math.abs(t2)) t1 else t2
            return lineOffsetOrigin + tLine * t
        }

        val offsetVec: Vector3 = when {
            // Case 1: Line -> Arc Transition
            // Current is Line End. Next is Arc Middle.
            vPrev != null && vNext != null && nextPt?.type == WeldPointType.ARC_MIDDLE -> {
                // We need Arc End (NextNext)
                var vNextNext: Vector3? = null
                for (i in nextIndex + 1 until sites.size) {
                    val pt = sites[i]
                    if (pt.type != WeldPointType.START_SAFE && pt.type != WeldPointType.END_SAFE) {
                        pt.pose?.let { vNextNext = Vector3(it.x, it.y, it.z) }
                        break
                    }
                }
                
                // Use RefX from Prev Point (Start of the line segment)
                // Actually, current is the END of the line segment.
                // The RefPoint should be associated with the segment.
                // Typically RefPoint is stored on the START point of a segment.
                // For segment Prev -> Curr, check Prev.refPointX
                val refX = sites[prevIndex].refX?.let { Vector3(it.x, it.y, it.z) }
                
                if (vNextNext != null) {
                    val intersect = solveLineArcIntersect(vPrev!!, pCurr, pCurr, vNext!!, vNextNext!!, refX)
                    if (intersect != null) {
                         intersect - pCurr
                    } else {
                        // Fallback to Miter if no intersection
                        // Standard Miter Logic...
                         val tIn = (pCurr - vPrev!!).normalize()
                         val tOut = (vNext!! - pCurr).normalize()
                         val (nIn, bIn) = getFrame(vPrev!!, pCurr, refX)
                         // For Arc Start (pCurr -> vNext), we might need another frame or same?
                         // Arc frame is usually Frenet or based on Plane.
                         // But Miter requires two frames.
                         // Let's assume standard Arc frame logic for the outgoing part if no ref there.
                         val (nOut, bOut) = getFrame(pCurr, vNext!!) // TODO: Arc frame might need Ref too?
                         
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
                     Vector3(0.0,0.0,0.0)
                }
            }
            
            // Case 2: Middle Point: Miter (Bisector) - Has both Prev and Next (Standard Line-Line)
            vPrev != null && vNext != null -> {
                val tIn = (pCurr - vPrev!!).normalize()
                val tOut = (vNext!! - pCurr).normalize()
                
                // RefX for In segment (Prev -> Curr)
                val refXIn = sites[prevIndex].refX?.let { Vector3(it.x, it.y, it.z) }
                // RefX for Out segment (Curr -> Next)
                val refXOut = current.refX?.let { Vector3(it.x, it.y, it.z) }
                
                val (nIn, bIn) = getFrame(vPrev!!, pCurr, refXIn)
                val (nOut, bOut) = getFrame(pCurr, vNext!!, refXOut)
                
                // Miter for N (X offset)
                val nAvg = (nIn + nOut).normalize()
                val dotN = nIn.dot(nOut)
                val kX = if (dotN > -0.99) 1.0 / sqrt((1 + dotN) / 2) else 1.0
                
                // Miter for B (Z offset)
                val bAvg = (bIn + bOut).normalize()
                val dotB = bIn.dot(bOut)
                val kZ = if (dotB > -0.99) 1.0 / sqrt((1 + dotB) / 2) else 1.0
                
                val tAvg = (tIn + tOut).normalize()
                 
                nAvg * (offX * kX) + bAvg * (offZ * kZ) + tAvg * offY
            }
            // Start Point (or First Point) - No Prev, Has Next
            vNext != null -> {
                val t = (vNext!! - pCurr).normalize()
                // RefX for this segment (Curr -> Next)
                val refX = current.refX?.let { Vector3(it.x, it.y, it.z) }
                val (n, b) = getFrame(pCurr, vNext!!, refX)
                n * offX + t * offY + b * offZ
            }
            // End Point (or Last Point) - Has Prev, No Next
            vPrev != null -> {
                val t = (pCurr - vPrev!!).normalize()
                // RefX for incoming segment (Prev -> Curr)
                val refX = sites[prevIndex].refX?.let { Vector3(it.x, it.y, it.z) }
                val (n, b) = getFrame(vPrev!!, pCurr, refX)
                n * offX + t * offY + b * offZ
            }
            else -> Vector3(0.0, 0.0, 0.0)
        }

        return Triple(offsetVec.x, offsetVec.y, offsetVec.z)
    }

    private fun getCircleParams(p1: Vector3, p2: Vector3, p3: Vector3): Triple<Vector3, Vector3, Double>? {
        val v1 = p2 - p1
        val v2 = p3 - p2
        var normal = v1.cross(v2)
        if (normal.length() < 1e-3) return null // Collinear
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

    /** 圆中与终点工艺偏移。七轴时 X 取反。 */
    fun arc(
        midIndex: Int,
        sites: List<OffsetSite>,
        process: WeldProcess,
        flipX: Boolean,
    ): Pair<Triple<Double, Double, Double>, Triple<Double, Double, Double>> {
        val midPoint = sites[midIndex]
        val endPoint = sites[midIndex + 1]
        val offXRaw = process.offsetX.toDoubleOrNull() ?: 0.0
        val offX = if (flipX) -offXRaw else offXRaw
        val offY = process.offsetY.toDoubleOrNull() ?: 0.0
        val offZ = process.offsetZ.toDoubleOrNull() ?: 0.0

        if (offX == 0.0 && offY == 0.0 && offZ == 0.0) {
            return Pair(Triple(0.0, 0.0, 0.0), Triple(0.0, 0.0, 0.0))
        }

        var pStart: Vector3? = null
        for (i in midIndex - 1 downTo 0) {
             val pt = sites[i]
             if (pt.type != WeldPointType.START_SAFE && pt.type != WeldPointType.END_SAFE) {
                 pt.pose?.let { pStart = Vector3(it.x, it.y, it.z) }
                 break
             }
        }
        
        val pMid = midPoint.pose?.let { Vector3(it.x, it.y, it.z) }
        val pEnd = endPoint.pose?.let { Vector3(it.x, it.y, it.z) }

        if (pStart == null || pMid == null || pEnd == null) {
            return Pair(Triple(0.0, 0.0, 0.0), Triple(0.0, 0.0, 0.0))
        }

        val params = getCircleParams(pStart!!, pMid, pEnd)
        val (center, normal, radius) = params ?: return Pair(Triple(0.0,0.0,0.0), Triple(0.0,0.0,0.0))

        // Check Next Point for Arc->Line Intersection
        var vNext: Vector3? = null
        // Search after endPoint (which is midIndex + 1)
        for (i in midIndex + 2 until sites.size) {
            val pt = sites[i]
            if (pt.type != WeldPointType.START_SAFE && pt.type != WeldPointType.END_SAFE) {
                pt.pose?.let { vNext = Vector3(it.x, it.y, it.z) }
                break
            }
        }

        fun getOffset(p: Vector3, isEndPoint: Boolean): Triple<Double, Double, Double> {
            // Arc -> Line Intersection Logic for End Point
            if (isEndPoint && vNext != null) {
                // Line: pEnd -> vNext
                val tLine = (vNext!! - pEnd).normalize()
                val globalZ = Vector3(0.0, 0.0, 1.0)
                var nLine = tLine.cross(globalZ).normalize()
                if (nLine.length() < 1e-3) nLine = Vector3(1.0, 0.0, 0.0)
                val bLine = nLine.cross(tLine).normalize()
                
                val lineOffsetOrigin = pEnd + nLine * offX + tLine * offY + bLine * offZ
                
                val offsetRadius = radius + offX
                val offsetCenter = center + normal * offZ
                
                val V = lineOffsetOrigin - offsetCenter
                val a = 1.0
                val b = 2 * V.dot(tLine)
                val c = V.dot(V) - offsetRadius * offsetRadius
                val delta = b * b - 4 * a * c
                
                if (delta >= 0) {
                    val t1 = (-b - sqrt(delta)) / (2 * a)
                    val t2 = (-b + sqrt(delta)) / (2 * a)
                    // Pick closest to 0
                    val t = if (kotlin.math.abs(t1) < kotlin.math.abs(t2)) t1 else t2
                    val intersect = lineOffsetOrigin + tLine * t
                    val vec = intersect - pEnd
                    return Triple(vec.x, vec.y, vec.z)
                }
            }
            
            // Standard Arc Offset
            val radial = if ((p - center).length() < 1e-3) Vector3(1.0, 0.0, 0.0) else (p - center).normalize()
            val tangent = normal.cross(radial)
            val vec = radial * offX + tangent * offY + normal * offZ
            return Triple(vec.x, vec.y, vec.z)
        }
        
        return Pair(getOffset(pMid, false), getOffset(pEnd, true))
    }
}
