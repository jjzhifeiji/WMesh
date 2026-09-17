package com.gbndt.shijiaoqi

import android.app.Application
import dagger.hilt.android.HiltAndroidApp

/** 进程壳：只负责装配，业务一律走 repository。 */
@HiltAndroidApp
class ShiJiaoQiApp : Application() {
    /** 本进程已占用过开屏；热启动进程还在就不再播。 */
    var splashShown = false
}
