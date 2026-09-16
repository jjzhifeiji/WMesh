package com.gbndt.shijiaoqi.domain.shared

import com.gbndt.shijiaoqi.data.robot.protocol.FrPacket
import com.gbndt.shijiaoqi.data.robot.protocol.RobotCommands
import com.gbndt.shijiaoqi.model.Pose
import com.gbndt.shijiaoqi.model.WeldPointType
import com.gbndt.shijiaoqi.model.WeldProcess

/** 一行 Lua：运动行才挂点号；求逆解不挂号，与旧机一致。 */
data class LuaLine(
    val text: String, // 指令正文
    val withId: Boolean = true, // 是否占用程序行号反馈
    val pathIndex: Int = -1, // 焊道下标
    val pointIndex: Int = -1, // 到达的点下标；-1 表示不点亮
    val extraIndex: Int = -1, // 附加工艺下标；主工艺为 -1
)

/** 从 STOP 记下的断点，用来续编 Lua。 */
data class StopResume(
    val pose: Pose, // 停下时的笛卡尔
    val joints: List<Double>, // 停下时的关节
    val pathIndex: Int, // 当时焊道
    val pointIndex: Int, // 当时点
    val extraIndex: Int = -1, // 当时附加工艺
)

/** 只取指令正文，单测比对用。 */
fun List<LuaLine>.texts(): List<String> = map { it.text }
/** 一条焊道上的点：类型、位姿、关节；offsets 空则按工艺现算。 */
data class ScriptPoint(
    val type: WeldPointType,
    val pose: Pose,
    val joints: List<Double> = listOf(0.0, 0.0, 0.0, 0.0, 0.0, 0.0),
    val refX: Pose? = null, // X 向参考
    val offsets: List<Double>? = null, // 预计算六元组；null 按工艺算，空列表表示无
)

/** 一条焊道：主工艺加附加工艺，几何共用。 */
data class ScriptPath(
    val points: List<ScriptPoint>,
    val process: WeldProcess,
    val extras: List<WeldProcess> = emptyList(),
    val enabled: Boolean = true,
    val uiIndex: Int = -1, // 界面焊道下标；-1 用遍历下标
    val passTag: Int = -1, // 多层道次；底道 -1
)
/** 单层批量与暂停继续的现网报文；不生成 Lua 正文。 */
object WeldRun {
    const val LUA_NAME = "/fruser/111.lua"
    const val AFTER_FILENAME_MS = 50L
    const val AFTER_BODY_ACK_MS = 500L
    const val AFTER_MODE_MS = 100L
    const val AFTER_PAUSE_MS = 1000L
    const val TYPE_FILENAME = 105
    const val TYPE_BODY = 106
    const val TYPE_START = 101
    const val TYPE_STOP = 102
    const val TYPE_PAUSE = 103
    const val TYPE_RESUME = 104

    data class BatchPlan(
        val fileName: String,
        val body: String,
        val modeAuto: String,
        val start: String,
    )

    /** 行号 → 指令号，以及会点亮焊点的指令号。 */
    data class NumberedLua(
        val body: String, // 带 \\r\\n 的 Lua 正文
        val lineToId: Map<Int, Int>, // 程序行号到指令号
        val idToPoint: Map<Int, Triple<Int, Int, Int>>, // 指令号到焊道/点/附加
    )

    /** 旧机正文用 CRLF，末行也带换行。 */
    fun joinLua(lines: List<String>): String =
        if (lines.isEmpty()) "" else lines.joinToString("\r\n") + "\r\n"

    /** 给带点号的行挂指令号，求逆解行不挂。 */
    fun number(lines: List<LuaLine>, nextId: () -> Int): NumberedLua {
        val lineToId = LinkedHashMap<Int, Int>()
        val idToPoint = LinkedHashMap<Int, Triple<Int, Int, Int>>()
        val texts = ArrayList<String>(lines.size)
        for (line in lines) {
            texts += line.text
            if (!line.withId) continue
            val id = nextId()
            lineToId[texts.size] = id
            if (line.pointIndex >= 0) {
                idToPoint[id] = Triple(line.pathIndex, line.pointIndex, line.extraIndex)
            }
        }
        return NumberedLua(joinLua(texts), lineToId, idToPoint)
    }

    fun batch(id105: Int, id106: Int, lua: String): BatchPlan = BatchPlan(
        fileName = FrPacket.encode(id105, TYPE_FILENAME, LUA_NAME),
        body = FrPacket.encode(id106, TYPE_BODY, lua),
        modeAuto = RobotCommands.modeAuto(),
        start = FrPacket.encode(77, TYPE_START, "Start"),
    )

    fun pause(): String = FrPacket.encode(77, TYPE_PAUSE, "Pause")

    fun resume(): String = FrPacket.encode(77, TYPE_RESUME, "RESUME")

    fun stop(): String = FrPacket.encode(7, TYPE_STOP, "STOP")

    fun reWeldAfterBreak(): String =
        FrPacket.encode(4, 806, "WeldingStartReWeldAfterBreakOff()")

    fun abortAfterBreak(): String =
        FrPacket.encode(4, 807, "WeldingAbortWeldAfterBreakOff()")

    /** 暂停：Pause → 切手动。 */
    fun pauseSeq(): List<String> = listOf(pause(), RobotCommands.modeManual())

    /** 继续：先自动；中断则再焊再 RESUME。 */
    fun resumeSeq(weldingBreakOff: Boolean): List<String> {
        val out = mutableListOf(RobotCommands.modeAuto())
        if (weldingBreakOff) out.add(reWeldAfterBreak())
        out.add(resume())
        return out
    }

    /** 停止：STOP → 切手动；中断再中止。 */
    fun stopSeq(weldingBreakOff: Boolean): List<String> {
        val out = mutableListOf(stop(), RobotCommands.modeManual())
        if (weldingBreakOff) out.add(abortAfterBreak())
        return out
    }
}
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
