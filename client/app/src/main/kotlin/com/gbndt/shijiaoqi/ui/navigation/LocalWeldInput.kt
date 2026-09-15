package com.gbndt.shijiaoqi.ui.navigation

import androidx.compose.runtime.staticCompositionLocalOf
import com.gbndt.shijiaoqi.ui.welding.WeldViewModelInterface

/** 当前在屏的焊接 ViewModel 注册口；手柄按键在 Activity 收，往这里转。 */
val LocalWeldInput = staticCompositionLocalOf<(WeldViewModelInterface?) -> Unit> { {} }
