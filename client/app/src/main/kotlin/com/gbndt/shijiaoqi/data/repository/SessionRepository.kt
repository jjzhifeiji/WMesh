package com.gbndt.shijiaoqi.data.repository

import com.gbndt.shijiaoqi.model.FactoryOffer
import com.gbndt.shijiaoqi.data.prefs.DeviceSettingsStore
import com.gbndt.shijiaoqi.data.session.BagSession
import com.gbndt.shijiaoqi.data.session.DeviceSerialHolder
import com.gbndt.shijiaoqi.data.session.SessionGate
import com.gbndt.shijiaoqi.di.ApplicationScope
import com.gbndt.shijiaoqi.di.IoDispatcher
import com.gbndt.shijiaoqi.model.SessionState
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.util.UUID
import javax.inject.Inject
import javax.inject.Singleton

/** 登录会话唯一入口：对外只有挂起函数与状态流，线程在这里收敛。 */
@Singleton
class SessionRepository @Inject constructor(
    private val session: BagSession,
    private val pouch: PouchRepository,
    private val serials: DeviceSerialHolder,
    private val settings: DeviceSettingsStore,
    private val gate: SessionGate,
    @param:IoDispatcher private val io: CoroutineDispatcher,
    @param:ApplicationScope private val appScope: CoroutineScope,
) {
    val state: StateFlow<SessionState> = session.state

    private val _bootstrapped = MutableStateFlow(false)

    /** 开机恢复跑完才为真；未完成前不要盖登录框。 */
    val bootstrapped: StateFlow<Boolean> = _bootstrapped.asStateFlow()

    init {
        appScope.launch { restore() }
    }

    fun savedUrl(): String = session.savedUrl()
    fun savedFactoryId(): String = session.savedFactoryId()
    fun savedClientId(): String = session.savedClientId()

    /** 扫本网厂服务；阻塞的网络探测放到 IO。 */
    suspend fun findFactories(): List<FactoryOffer> = withContext(io) {
        gate.withLock { session.findFactories() }
    }

    /** 本厂账号登录，成功后按策略把密文拉进本机袋。 */
    suspend fun login(baseUrl: String, factoryId: String, loginName: String, password: String) {
        try {
            withContext(io) {
                gate.withLock { session.login(baseUrl, factoryId, loginName, password) }
            }
        } finally {
            pouch.refresh()
        }
    }

    /** 连上臂读到的识别号；只在内存里，进程死即失。 */
    fun setDeviceSerial(serial: String) = serials.set(serial)

    /** 读到机械臂识别号后按本厂已落盘名录本地比对。 */
    suspend fun matchArm(serial: String) = withContext(io) {
        gate.withLock { session.matchArm(serial) }
    }

    suspend fun logout() {
        try {
            withContext(io) {
                gate.withLock { session.logout() }
            }
        } finally {
            pouch.refresh()
        }
    }

    /** 改自己的日常密码；勾选过记住则一并更新落盘。 */
    suspend fun changePassword(password: String) = withContext(io) {
        gate.withLock {
            session.changePassword(password)
            val remembered = settings.loadRememberedLogin()
            if (remembered != null) settings.saveRememberedLogin(true, remembered.first, password)
        }
    }

    suspend fun activate(projectId: UUID) {
        pouch.activate(projectId)
    }

    private suspend fun restore() {
        try {
            withContext(io) {
                gate.withLock { session.restore() }
            }
        } finally {
            pouch.refresh()
            _bootstrapped.value = true
        }
    }
}
