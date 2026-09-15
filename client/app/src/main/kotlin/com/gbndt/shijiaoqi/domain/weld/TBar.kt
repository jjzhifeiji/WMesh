package com.gbndt.shijiaoqi.domain.weld

import com.gbndt.shijiaoqi.model.GapBand
import com.gbndt.shijiaoqi.model.Pose
import com.gbndt.shijiaoqi.domain.weld.TBarGeometry
import com.gbndt.shijiaoqi.domain.weld.TBarPass

/** 按间隙切出的一段打底或盖面。 */
data class TBarSeg(
    val tStart: Double,
    val tEnd: Double,
    val startPose: Pose,
    val endPose: Pose,
    val gapStart: Double,
    val gapEnd: Double,
    val processId: String,
    val band: GapBand,
)

/** T 排间隙带匹配与 21 点切段；不读标准库文件夹。 */
object TBarRun {
    const val SAMPLES = 21
    const val TEMPLATE_ID = "44444444-4444-4444-8444-444444444444"
    const val KIND = "tbar"

    fun bandLabel(band: GapBand): String {
        val minStr = trimNum(band.minGap)
        val maxStr = trimNum(band.maxGap)
        return "${band.layer}H-$minStr-$maxStr"
    }

    fun processId(band: GapBand, pass: TBarPass): String =
        if (pass == TBarPass.ROOT) band.rootProcessId else band.capProcessId

    /** 先同层 [min,max)，再闭区间，再取最近带。 */
    fun matchBand(gapMm: Double, bands: List<GapBand>, layer: Int = 1): GapBand? {
        val layerItems = bands.filter { it.layer == layer }.sortedBy { it.minGap }
        if (layerItems.isEmpty()) return null
        layerItems.firstOrNull { gapMm >= it.minGap && gapMm < it.maxGap }?.let { return it }
        layerItems.firstOrNull { gapMm >= it.minGap && gapMm <= it.maxGap }?.let { return it }
        return layerItems.minByOrNull { band ->
            when {
                gapMm < band.minGap -> band.minGap - gapMm
                gapMm > band.maxGap -> gapMm - band.maxGap
                else -> 0.0
            }
        }
    }

    fun buildSegments(
        aLower: Pose,
        bLower: Pose,
        aUpper: Pose,
        bUpper: Pose,
        startPose: Pose,
        endPose: Pose,
        bands: List<GapBand>,
        pass: TBarPass,
        layer: Int = 1,
    ): List<TBarSeg> {
        data class Sample(val t: Double, val gap: Double, val band: GapBand)
        val sampled = (0 until SAMPLES).map { i ->
            val t = i / (SAMPLES - 1).toDouble()
            val gap = TBarGeometry.gapAt(aLower, aUpper, bLower, bUpper, t)
            val matched = matchBand(gap, bands, layer)
                ?: throw IllegalStateException("间隙 ${"%.1f".format(gap)} mm 未匹配到 ${layer}H 间隙带")
            Sample(t, gap, matched)
        }
        val segments = mutableListOf<TBarSeg>()
        var i = 0
        while (i < sampled.size) {
            val current = sampled[i]
            var j = i
            while (j + 1 < sampled.size && sampled[j + 1].band == current.band) {
                j++
            }
            val t0 = sampled[i].t
            val t1 = sampled[j].t
            segments.add(
                TBarSeg(
                    tStart = t0,
                    tEnd = t1,
                    startPose = lerpPose(startPose, endPose, t0),
                    endPose = lerpPose(startPose, endPose, t1),
                    gapStart = sampled[i].gap,
                    gapEnd = sampled[j].gap,
                    processId = processId(current.band, pass),
                    band = current.band,
                )
            )
            i = j + 1
        }
        return segments
    }

    private fun trimNum(value: Double): String =
        if (value == value.toLong().toDouble()) value.toLong().toString() else value.toString()

    private fun lerpPose(a: Pose, b: Pose, t: Double): Pose = Pose(
        x = a.x + (b.x - a.x) * t,
        y = a.y + (b.y - a.y) * t,
        z = a.z + (b.z - a.z) * t,
        rx = a.rx + shortestAngle(a.rx, b.rx) * t,
        ry = a.ry + shortestAngle(a.ry, b.ry) * t,
        rz = a.rz + shortestAngle(a.rz, b.rz) * t,
        ext1 = a.ext1 + (b.ext1 - a.ext1) * t,
    )

    private fun shortestAngle(from: Double, to: Double): Double {
        var d = (to - from) % 360.0
        if (d > 180) d -= 360
        if (d < -180) d += 360
        return d
    }
}
