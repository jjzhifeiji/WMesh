package com.gbndt.shijiaoqi.domain.script

import com.gbndt.shijiaoqi.data.robot.protocol.FrPacket
import com.gbndt.shijiaoqi.data.robot.protocol.RobotCommands

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
