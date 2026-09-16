package com.gbndt.shijiaoqi.data.repository

import com.gbndt.shijiaoqi.model.FactoryOffer
import com.gbndt.shijiaoqi.data.session.BagSession
import com.gbndt.shijiaoqi.data.session.DeviceSerialHolder
import com.gbndt.shijiaoqi.model.SessionState
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.withContext
import java.util.UUID
import javax.inject.Inject
import javax.inject.Singleton

/** 登录会话唯一入口：对外只有挂起函数与状态流，线程在这里收敛。 */
@Singleton
class SessionRepository @Inject constructor(
    private val session: BagSession,
    private val serials: DeviceSerialHolder,
    private val io: CoroutineDispatcher,
) {
    val state: StateFlow<SessionState> = session.state

    fun savedUrl(): String = session.savedUrl()
    fun savedFactoryId(): String = session.savedFactoryId()
    fun savedClientId(): String = session.savedClientId()

    /** 扫本网厂服务；阻塞的网络探测放到 IO。 */
    suspend fun findFactories(): List<FactoryOffer> = withContext(io) { session.findFactories() }

    /** 本厂账号登录，成功后按策略把密文拉进本机袋。 */
    suspend fun login(baseUrl: String, factoryId: String, loginName: String, password: String) =
        withContext(io) { session.login(baseUrl, factoryId, loginName, password) }

    /** 连上臂读到的识别号；只在内存里，进程死即失。 */
    fun setDeviceSerial(serial: String) = serials.set(serial)

    /** 读到机械臂识别号后再匹配本机；未登录时拒绝。 */
    suspend fun matchArm(serial: String) = withContext(io) { session.matchArm(serial) }

    fun logout() = session.logout()

    suspend fun activate(projectId: UUID) = withContext(io) { session.activate(projectId) }
}