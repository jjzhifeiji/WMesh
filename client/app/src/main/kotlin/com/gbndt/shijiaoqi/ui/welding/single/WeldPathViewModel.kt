@file:Suppress("DEPRECATION")

package com.gbndt.shijiaoqi.ui.welding.single

import android.app.Application
import com.gbndt.shijiaoqi.data.prefs.DeviceSettingsStore
import com.gbndt.shijiaoqi.data.repository.PouchRepository
import com.gbndt.shijiaoqi.data.repository.UpdateRepository
import com.gbndt.shijiaoqi.data.repository.RobotRepository
import com.gbndt.shijiaoqi.data.repository.SessionRepository
import com.gbndt.shijiaoqi.ui.teach.TeachSession
import com.gbndt.shijiaoqi.ui.welding.WeldingViewModel
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject

/** 对照用，新单层屏不要再用这个类。 */
@Deprecated("对照用：新焊接屏不要再用。算法往 domain/示教搬。")
@HiltViewModel
class WeldPathViewModel @Inject constructor(
    application: Application,
    session: SessionRepository,
    socketManager: RobotRepository,
    pouch: PouchRepository,
    updateManager: UpdateRepository,
    deviceSettings: DeviceSettingsStore,
    teach: TeachSession,
) : WeldingViewModel(application, session, socketManager, pouch, updateManager, deviceSettings, teach)
