package com.gbndt.shijiaoqi.domain.weld

import com.gbndt.shijiaoqi.model.Pose
import com.gbndt.shijiaoqi.model.WeldPointType

/** 多层交错下发与无参考点时的层偏移；有参考点的几何仍走现网 generatePassPath。 */
object MultiLayerRun {
    data class Step(val pathIndex: Int, val passIndex: Int)

    fun interleave(
        pathPassCounts: List<Int>,
        enabled: (pathIndex: Int, passIndex: Int) -> Boolean,
    ): List<Step> {
        if (pathPassCounts.isEmpty()) return emptyList()
        val maxPasses = pathPassCounts.maxOrNull() ?: 0
        val out = mutableListOf<Step>()
        for (layer in -1 until maxPasses) {
            pathPassCounts.indices.forEach { mIndex ->
                if (layer == -1) {
                    if (enabled(mIndex, -1)) out += Step(mIndex, -1)
                } else if (layer < pathPassCounts[mIndex] && enabled(mIndex, layer)) {
                    out += Step(mIndex, layer)
                }
            }
        }
        return out
    }

    fun simpleOffset(
        pose: Pose,
        type: WeldPointType,
        atStart: Boolean,
        atEnd: Boolean,
        valX: Double,
        valYLeft: Double,
        valYRight: Double,
        valZ: Double,
    ): Pose {
        if (type == WeldPointType.START_SAFE || type == WeldPointType.END_SAFE) return pose
        val y = when {
            atStart -> valYLeft
            atEnd -> valYRight
            else -> 0.0
        }
        return pose.copy(x = pose.x + valX, y = pose.y + y, z = pose.z + valZ)
    }
}
