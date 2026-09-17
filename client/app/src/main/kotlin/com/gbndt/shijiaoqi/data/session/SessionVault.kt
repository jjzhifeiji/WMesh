package com.gbndt.shijiaoqi.data.session

/** 落盘的人员会话：令牌必有；解封钥不进这里，只进独立二进制文件。 */
data class SavedSession(
    val token: String, // 登录令牌
    val expiresAtMillis: Long, // 到期毫秒；0 表示直到退出
    val personId: String, // 人员稳定身份
    val personName: String, // 显示名
    val loginName: String, // 厂内登录名
    val roles: List<String>, // 厂端授予角色
    val persistUnwrapKey: Boolean, // 解封钥可否落盘
    val keyTtlSeconds: Long, // 登录时效秒
    val loggedInAtMillis: Long, // 本次登录时间
    val devices: List<PadDevice>, // 可见设备，不含钥
    val encryptPouch: Boolean = true, // 本机库是否 SQLCipher
)

/** 登录令牌与人员会话；不含解封钥、不含工艺明文。 */
interface SessionVault {
    fun load(): SavedSession?
    fun save(session: SavedSession)
    fun clear()
}

/** 测试用内存仓。 */
class MemorySessionVault : SessionVault {
    private var saved: SavedSession? = null

    override fun load(): SavedSession? = saved?.copy(
        devices = saved?.devices.orEmpty().map { it.copy(unwrapKey = it.unwrapKey.copyOf()) },
    )

    override fun save(session: SavedSession) {
        saved = session.copy(
            devices = session.devices.map { it.copy(unwrapKey = it.unwrapKey.copyOf()) },
        )
    }

    override fun clear() {
        saved = null
    }
}
