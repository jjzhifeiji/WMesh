package com.gbndt.shijiaoqi.domain.script

import com.gbndt.shijiaoqi.model.WeldPointType

/** 跟 8083 行号：点亮焊点、开停焊长累计。不管组 Lua。 */
class WeldLineFollow {
    var lastProcessedLine = 0
    var lineToId: Map<Int, Int> = emptyMap()
    var idToPoint: Map<Int, Triple<Int, Int, Int>> = emptyMap()
    var lastPath = -1
    var lastPoint = -1
    var statsActive = false

    fun reset() {
        lastProcessedLine = 0
        lastPath = -1
        lastPoint = -1
        statsActive = false
    }

    fun load(numbered: WeldRun.NumberedLua) {
        lineToId = numbered.lineToId
        idToPoint = numbered.idToPoint
        lastProcessedLine = 0
    }

    /** 一次行号推进里要处理的点。 */
    data class Hit(
        val pathIndex: Int, // 界面焊道
        val pointIndex: Int, // 点亮的点
        val extraIndex: Int, // 附加或道次
        val startStats: Boolean, // 起点：开累计
        val stopStats: Boolean, // 终点：关累计
        val addFrom: Int, // 同焊道上一段点；-1 不加长
        val countLength: Boolean, // 这段计入焊长
    )

    /** 处理 8083 当前行；空闲时清进度。 */
    fun onProgLine(
        line: Int,
        busy: Boolean,
        typeOf: (path: Int, point: Int) -> WeldPointType?,
    ): List<Hit> {
        if (!busy) {
            lastProcessedLine = 0
            return emptyList()
        }
        if (line <= 0) return emptyList()
        if (line < lastProcessedLine) lastProcessedLine = 0
        if (line <= lastProcessedLine) return emptyList()
        val hits = ArrayList<Hit>()
        for (l in (lastProcessedLine + 1)..line) {
            val id = lineToId[l] ?: continue
            val (path, point, extra) = idToPoint[id] ?: continue
            val type = typeOf(path, point)
            val start = type == WeldPointType.START
            val end = type == WeldPointType.END
            val addFrom = if (lastPath == path && lastPoint >= 0 && !start) lastPoint else -1
            val countLength = statsActive && !start && addFrom >= 0
            hits += Hit(path, point, extra, start, end, addFrom, countLength)
            lastPath = path
            lastPoint = point
            if (start) statsActive = true
            if (end) statsActive = false
        }
        lastProcessedLine = line
        return hits
    }
}
