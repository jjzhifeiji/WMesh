package com.gbndt.shijiaoqi.domain.script

import com.gbndt.shijiaoqi.domain.weld.MultiLayerPass
import com.gbndt.shijiaoqi.domain.weld.MultiLayerRun
import com.gbndt.shijiaoqi.model.MultiLayerWeldPath

/** 多层 Lua：层间交错后每条焊道走单层点序列。不管采点。 */
object MultiLayerLua {
    /** 交错后组 Lua；有未采点返回 null。 */
    fun job(
        paths: List<MultiLayerWeldPath>,
        welding: Boolean,
        simulating: Boolean,
        speedMode: String = "1倍",
        toolIndex: Int = 1,
        extAxis: Boolean = false,
        resume: StopResume? = null,
    ): List<LuaLine>? {
        val scripts = toScripts(paths) ?: return null
        if (scripts.isEmpty()) return emptyList()
        return SingleLayerLua.job(scripts, welding, simulating, speedMode, toolIndex, extAxis, resume)
    }

    /** 按层交错展开成可下发焊道；缺位姿返回 null。 */
    fun toScripts(paths: List<MultiLayerWeldPath>): List<ScriptPath>? {
        val counts = paths.map { it.passes.size }
        val steps = MultiLayerRun.interleave(counts) { i, layer ->
            val p = paths[i]
            if (layer == -1) p.basePath.isEnabled else p.passes[layer].isEnabled
        }
        val out = ArrayList<ScriptPath>(steps.size)
        for (step in steps) {
            val mp = paths[step.pathIndex]
            if (step.passIndex == -1) {
                val pts = ArrayList<ScriptPoint>(mp.basePath.points.size)
                for (p in mp.basePath.points) {
                    val pose = p.pose ?: return null
                    pts += ScriptPoint(
                        p.type,
                        pose,
                        p.jointAngles ?: List(6) { 0.0 },
                        offsets = emptyList(),
                    )
                }
                out += ScriptPath(
                    points = pts,
                    process = mp.basePath.process,
                    uiIndex = step.pathIndex,
                    passTag = -1,
                )
            } else {
                val pts = MultiLayerPass.generate(mp.basePath, mp.passes[step.passIndex], mp) ?: return null
                out += ScriptPath(
                    points = pts.map {
                        ScriptPoint(it.type, it.pose, it.joints, offsets = it.offsets ?: emptyList())
                    },
                    process = mp.passes[step.passIndex].process,
                    uiIndex = step.pathIndex,
                    passTag = step.passIndex,
                )
            }
        }
        return out
    }
}
