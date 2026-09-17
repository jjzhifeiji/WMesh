package com.gbndt.shijiaoqi.ui.navigation

import androidx.navigation3.runtime.NavKey
import kotlinx.serialization.Serializable

/** 开屏。 */
@Serializable
data object SplashKey : NavKey

/** 选单层 / 多层 / T 排 / 指令测试。 */
@Serializable
data object ModeKey : NavKey

/** 单层焊道。 */
@Serializable
data object SingleWeldKey : NavKey

/** 多层焊缝。 */
@Serializable
data object MultiLayerKey : NavKey

/** T 排对接。 */
@Serializable
data object TBarKey : NavKey

/** 机械臂指令测试。 */
@Serializable
data object RobotTestKey : NavKey

/** 个人中心。 */
@Serializable
data object AccountKey : NavKey

/** 修改密码。 */
@Serializable
data object AccountPasswordKey : NavKey
