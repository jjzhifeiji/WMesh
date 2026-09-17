package com.gbndt.shijiaoqi.data.session

import com.gbndt.shijiaoqi.data.pouch.CachedClosure
import com.gbndt.shijiaoqi.data.pouch.CachedEnvelope
import com.gbndt.shijiaoqi.data.pouch.ClosureCodec
import com.gbndt.shijiaoqi.data.pouch.Pouch
import java.util.UUID

/** 当前登录人元数据，不含密码。 */
data class PersonMeta(
    val id: UUID, // 人员稳定身份
    val loginName: String, // 厂内登录名
    val displayName: String, // 显示名
)

/** 厂端客户端策略；退出或登录到期清钥。 */
data class PolicyMeta(
    val revision: Long, // 策略修订
    val persistUnwrapKey: Boolean, // 解封钥可否落盘；退出或登录到期必清
    val keyTtlSeconds: Long, // 登录时效秒；0 表示直到退出，进程内不踢
    val encryptPouch: Boolean = true, // 本机库是否 SQLCipher
)

/** 库内可见的登录令牌与时效。 */
data class SessionMeta(
    val token: String, // 登录令牌
    val expiresAtMillis: Long, // 到期毫秒；0 表示直到退出
    val loggedInAtMillis: Long, // 本次登录时间
)

/** 信封密文与离线元数据落盘；工艺明文永不进库。 */
interface EnvelopeStore {
    /** 用解封钥开库；encrypt 决定 SQLCipher 还是明文。 */
    fun open(passphrase: ByteArray, encrypt: Boolean = true)
    /** 关掉连接，袋文件仍留盘。 */
    fun close()
    /** 列出全部信封密文。 */
    fun loadEnvelopes(): List<CachedEnvelope>
    /** 按身份取一条信封密文。 */
    fun loadEnvelope(id: UUID): CachedEnvelope?
    /** 写入或覆盖一条信封密文。 */
    fun upsert(env: CachedEnvelope)
    /** 删掉袋里已经没有的信封。 */
    fun delete(id: UUID)
    /** 记下当前登录人和角色。 */
    fun savePerson(meta: PersonMeta, roles: List<String> = emptyList())
    /** 读当前登录人；没有则空。 */
    fun loadPerson(): PersonMeta?
    /** 读厂端授予的角色。 */
    fun loadRoles(): List<String>
    /** 记下厂端客户端策略。 */
    fun savePolicy(meta: PolicyMeta)
    /** 读已落盘策略。 */
    fun loadPolicy(): PolicyMeta?
    /** 工艺表整表换成袋内当前信封。 */
    fun replaceProcesses(list: List<CachedEnvelope>)
    /** 整表写入可见设备，不含钥。 */
    fun saveDevices(list: List<PadDevice>)
    /** 读已落盘设备，不含钥。 */
    fun loadDevices(): List<PadDevice>
    /** 记下登录令牌与时效。 */
    fun saveSession(token: String, expiresAtMillis: Long, loggedInAtMillis: Long)
    /** 读库内登录令牌。 */
    fun loadSession(): SessionMeta?
    /** 整表写入工程元数据；blobs 是工程明文。 */
    fun saveClosures(list: List<CachedClosure>, blobs: List<CachedEnvelope> = emptyList())
    /** 读已缓存工程闭包元数据。 */
    fun loadClosures(): List<CachedClosure>
    /** 读工程明文正文，不是工艺信封。 */
    fun loadProjectPlains(): List<CachedEnvelope>
    /** 记下当前激活工程。 */
    fun saveActive(id: UUID?, revision: Long)
    /** 读当前激活工程；没有则空。 */
    fun loadActive(): Pair<UUID?, Long>
    /** 写入发号台账与待汇聚。 */
    fun saveLedger(raw: String)
    /** 把台账表拼回袋内格式。 */
    fun loadLedger(): String

    /** 退出登录时丢掉本机袋文件，不含厂址。 */
    fun wipe()
}

/** 测试用内存袋，不落文件。 */
class MemoryEnvelopeStore : EnvelopeStore {
    private var open = false
    private val items = LinkedHashMap<UUID, CachedEnvelope>()
    private var person: PersonMeta? = null
    private var roles: List<String> = emptyList()
    private var policy: PolicyMeta? = null
    private var devices: List<PadDevice> = emptyList()
    private var session: SessionMeta? = null
    private var closures = ""
    private val projectPlains = LinkedHashMap<UUID, CachedEnvelope>()
    private var active = ""
    private var ledger = ""

    override fun open(passphrase: ByteArray, encrypt: Boolean) {
        require(passphrase.size == 32)
        open = true
    }

    override fun close() {
        open = false
    }

    override fun loadEnvelopes(): List<CachedEnvelope> {
        check(open)
        return items.values.map { it.copy(blob = it.blob.copyOf()) }
    }

    override fun loadEnvelope(id: UUID): CachedEnvelope? {
        check(open)
        val env = items[id] ?: return null
        return env.copy(blob = env.blob.copyOf())
    }

    override fun upsert(env: CachedEnvelope) {
        check(open)
        items[env.id] = env.copy(blob = env.blob.copyOf())
    }

    override fun replaceProcesses(list: List<CachedEnvelope>) {
        check(open)
        items.clear()
        list.forEach { items[it.id] = it.copy(blob = it.blob.copyOf()) }
    }

    override fun delete(id: UUID) {
        check(open)
        items.remove(id)
    }

    override fun savePerson(meta: PersonMeta, roles: List<String>) {
        check(open)
        person = meta
        this.roles = roles.toList()
    }

    override fun loadPerson(): PersonMeta? {
        check(open)
        return person
    }

    override fun loadRoles(): List<String> {
        check(open)
        return roles
    }

    override fun savePolicy(meta: PolicyMeta) {
        check(open)
        policy = meta
    }

    override fun loadPolicy(): PolicyMeta? {
        check(open)
        return policy
    }

    override fun saveDevices(list: List<PadDevice>) {
        check(open)
        devices = list.map { it.copy(unwrapKey = ByteArray(0)) }
    }

    override fun loadDevices(): List<PadDevice> {
        check(open)
        return devices.map { it.copy(unwrapKey = it.unwrapKey.copyOf()) }
    }

    override fun saveSession(token: String, expiresAtMillis: Long, loggedInAtMillis: Long) {
        check(open)
        session = SessionMeta(token, expiresAtMillis, loggedInAtMillis)
    }

    override fun loadSession(): SessionMeta? {
        check(open)
        return session
    }

    override fun saveClosures(list: List<CachedClosure>, blobs: List<CachedEnvelope>) {
        check(open)
        closures = ClosureCodec.encode(list)
        projectPlains.clear()
        blobs.forEach { projectPlains[it.id] = it.copy(blob = it.blob.copyOf()) }
    }

    override fun loadClosures(): List<CachedClosure> {
        check(open)
        return ClosureCodec.decode(closures)
    }

    override fun loadProjectPlains(): List<CachedEnvelope> {
        check(open)
        return projectPlains.values.map { it.copy(blob = it.blob.copyOf()) }
    }

    override fun saveActive(id: UUID?, revision: Long) {
        check(open)
        active = if (id == null) "" else "$id|$revision"
    }

    override fun loadActive(): Pair<UUID?, Long> {
        check(open)
        return parseActive(active)
    }

    override fun saveLedger(raw: String) {
        check(open)
        ledger = raw
    }

    override fun loadLedger(): String {
        check(open)
        return ledger
    }

    override fun wipe() {
        items.clear()
        projectPlains.clear()
        person = null
        roles = emptyList()
        policy = null
        devices = emptyList()
        session = null
        closures = ""
        active = ""
        ledger = ""
        open = false
    }
}

/** 把袋内工艺、工程、台账写进库；密钥不进库。 */
fun persistPouch(store: EnvelopeStore, pouch: Pouch) {
    val closures = pouch.exportClosures()
    val projectIds = closures.map { it.assetId }.toSet()
    val snap = pouch.diskSnapshot()
    store.replaceProcesses(snap.filter { it.id !in projectIds })
    store.saveClosures(closures, pouch.exportProjectPlains())
    store.saveActive(pouch.activeProject(), pouch.activeRevision())
    store.saveLedger(pouch.exportLedger())
}

/** 把激活工程从旧串解析出来。 */
internal fun parseActive(raw: String): Pair<UUID?, Long> {
    if (raw.isBlank()) return null to 0L
    val p = raw.split('|')
    if (p.size < 2) return null to 0L
    return UUID.fromString(p[0]) to p[1].toLong()
}

/** 旧袋只有五段时按加密处理。 */
internal fun parsePolicyKv(raw: String): PolicyMeta? {
    val p = raw.split('|')
    if (p.size < 5) return null
    val encrypt = if (p.size >= 6) p[5].toBoolean() else true
    return PolicyMeta(p[0].toLong(), p[3].toBoolean(), p[4].toLong(), encrypt)
}

/** 厂址与当前匹配的 Client，不含工艺。 */
interface IdentityStore {
    var factoryUrl: String // 上次扫到的厂服地址
    var factoryId: String // 本厂身份
    var clientId: String // 当前匹配的 Client
    var clientShortCode: String // 本机短号
}

/** 测试用身份，不落盘。 */
class MemoryIdentityStore : IdentityStore {
    override var factoryUrl: String = ""
    override var factoryId: String = ""
    override var clientId: String = ""
    override var clientShortCode: String = ""
}
