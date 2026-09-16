package com.gbndt.shijiaoqi.domain.script

import com.gbndt.shijiaoqi.model.Pose

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
