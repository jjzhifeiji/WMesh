package com.gbndt.shijiaoqi.ui.navigation

import androidx.compose.runtime.staticCompositionLocalOf
import com.gbndt.shijiaoqi.ui.teach.TeachSession
import com.gbndt.shijiaoqi.ui.welding.WeldPad

/** 当前在屏的焊接手柄口；采集/开焊在 Activity 收键后往这里转。 */
val LocalWeldInput = staticCompositionLocalOf<(WeldPad?) -> Unit> { {} }

/** 示教会话：点动与工具，三个模式共用一份。 */
val LocalTeach = staticCompositionLocalOf<TeachSession> {
    error("TeachSession not provided")
}
