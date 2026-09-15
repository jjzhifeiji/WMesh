package com.gbndt.shijiaoqi

import android.app.Application
import com.gbndt.shijiaoqi.data.repository.SessionRepository
import dagger.hilt.android.HiltAndroidApp
import javax.inject.Inject

/** 进程壳：只负责装配，业务一律走 repository。 */
@HiltAndroidApp
class ShiJiaoQiApp : Application() {
    @Inject
    lateinit var session: SessionRepository

    override fun onTerminate() {
        session.logout()
        super.onTerminate()
    }
}
