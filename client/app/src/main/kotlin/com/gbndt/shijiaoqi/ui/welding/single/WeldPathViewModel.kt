package com.gbndt.shijiaoqi.ui.welding.single

import android.app.Application
import com.gbndt.shijiaoqi.data.repository.RobotRepository
import com.gbndt.shijiaoqi.data.repository.SessionRepository
import com.gbndt.shijiaoqi.ui.welding.WeldingViewModel
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject

/** 单层单道：公共实现即是它的行为，这里不另加东西。 */
@HiltViewModel
class WeldPathViewModel @Inject constructor(
    application: Application,
    session: SessionRepository,
    socketManager: RobotRepository,
) : WeldingViewModel(application, session, socketManager)
