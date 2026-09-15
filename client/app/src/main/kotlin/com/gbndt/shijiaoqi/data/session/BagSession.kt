package com.gbndt.shijiaoqi.data.session

import com.gbndt.shijiaoqi.data.crypt.Wm2
import com.gbndt.shijiaoqi.model.SessionState
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import com.gbndt.shijiaoqi.data.pouch.Pouch
import com.gbndt.shijiaoqi.data.pouch.PouchRejected
import java.util.UUID
import com.gbndt.shijiaoqi.data.remote.DownChannel
import com.gbndt.shijiaoqi.data.remote.FactoryOffer
import com.gbndt.shijiaoqi.data.remote.LanScan
import com.gbndt.shijiaoqi.data.remote.NoopDownChannel

/** 本机登录会话：解封钥只在内存，信封进袋，默认不把钥落盘。 */
class BagSession(
    private val serials: DeviceSerialReader,
    private val factory: FactoryGateway,
    private val identity: IdentityStore,
    private val store: EnvelopeStore,
    private val down: DownChannel = NoopDownChannel(),
) {
    val pouch = Pouch()

    private val _state = MutableStateFlow(SessionState())

    /** 唯一可信源：会话状态只在这里改，调用方只读这一个流。 */
    val state: StateFlow<SessionState> = _state.asStateFlow()

    val loggedIn: Boolean get() = _state.value.loggedIn
    val armMatched: Boolean get() = _state.value.armMatched
    val personName: String get() = _state.value.personName
    val error: String? get() = _state.value.error
    val busy: Boolean get() = _state.value.busy

    private fun edit(block: (SessionState) -> SessionState) = _state.update(block)
    private var token: String? = null
    private var devices: List<PadDevice> = emptyList()

    fun savedUrl(): String = identity.factoryUrl
    fun savedFactoryId(): String = identity.factoryId
    fun savedClientId(): String = identity.clientId

    fun findFactories(): List<FactoryOffer> {
        edit { it.copy(error = null, busy = true) }
        try {
            val hits = LanScan.find(identity.factoryUrl) { base ->
                factory.discover(base, "")
            }
            if (hits.isEmpty()) {
                edit { it.copy(error = "本网没有发现厂服务") }
            }
            return hits
        } catch (e: LoginRejected) {
            edit { it.copy(error = translate(e.code)) }
            throw e
        } catch (e: Exception) {
            edit { it.copy(error = e.message ?: "scan failed") }
            throw e
        } finally {
            edit { it.copy(busy = false) }
        }
    }

    fun login(baseUrl: String, factoryId: String, loginName: String, password: String) {
        edit { it.copy(error = null, busy = true) }
        try {
            identity.factoryUrl = baseUrl.trim()
            identity.factoryId = factoryId.trim()
            identity.clientId = ""
            identity.clientShortCode = ""
            val sess = factory.loginPad(
                identity.factoryUrl, identity.factoryId, loginName.trim(), password,
            )
            logoutMemory()
            val pouchKey = sess.devices.filter { it.unwrapKey.size == 32 }.minByOrNull { it.id }?.unwrapKey?.copyOf()
                ?: Wm2.randomKey()
            pouch.login(pouchKey, sess.person.id, sess.policy.persistUnwrapKey)
            store.open(pouchKey)
            Wm2.zero(pouchKey)
            runCatching {
                pouch.restoreEnvelopes(store.loadEnvelopes())
                pouch.restoreLedger(store.loadLedger())
                pouch.restoreClosures(store.loadClosures())
            }
            pouch.setOnline(true)
            pouch.setPolicy(sess.policy.maxCachedProjects, sess.policy.cacheScope)
            pouch.setRoles(sess.roles)
            token = sess.token
            pullAllDevices(sess)
            sess.devices.forEach { Wm2.zero(it.unwrapKey) }
            devices = sess.devices.map { it.copy(unwrapKey = ByteArray(0)) }
            persistPouch(store, pouch)
            store.savePerson(sess.person)
            store.savePolicy(sess.policy)
            edit {
                it.copy(
                    personName = sess.person.displayName.ifBlank { sess.person.loginName },
                    loggedIn = true,
                    armMatched = false,
                )
            }
            val serial = serials.read().trim()
            if (serial.isNotEmpty()) {
                runCatching { matchArm(serial) }
            }
        } catch (e: LoginRejected) {
            logoutMemory()
            edit { it.copy(error = translate(e.code)) }
            throw e
        } catch (e: Exception) {
            logoutMemory()
            edit { it.copy(error = e.message ?: "login failed") }
            throw e
        } finally {
            edit { it.copy(busy = false) }
        }
    }

    fun matchArm(serial: String) {
        val got = serial.trim()
        if (!loggedIn) throw LoginRejected("unauthorized")
        if (got.isEmpty()) {
            edit { it.copy(error = "读不到设备号", armMatched = false) }
            throw LoginRejected("device serial is required")
        }
        val hit = devices.firstOrNull { it.deviceSerial == got }
            ?: run {
                edit { it.copy(error = "设备号未在本厂登记", armMatched = false) }
                pouch.clearClient()
                identity.clientId = ""
                identity.clientShortCode = ""
                throw LoginRejected("device serial does not match")
            }
        identity.clientId = hit.id
        identity.clientShortCode = hit.shortCode
        pouch.bindClient(UUID.fromString(hit.id))
        if (hit.shortCode.isNotBlank()) pouch.setOrigin(hit.shortCode)
        edit { it.copy(armMatched = true, error = null) }
    }

    fun cachePlain(id: UUID, level: String, name: String, revision: Long, ownerId: UUID?, plain: ByteArray) {
        pouch.putPlain(id, level, name, revision, ownerId, plain)
        persistPouch(store, pouch)
    }

    /** 覆盖袋内已有信封，盘上仍是密文。 */
    fun rewritePlain(id: UUID, plain: ByteArray) {
        val env = pouch.envelope(id) ?: throw PouchRejected(Pouch.ERR_NOT_FOUND)
        cachePlain(id, env.level, env.name, env.revision, env.ownerId, plain)
    }

    fun cacheClosure(snap: com.gbndt.shijiaoqi.data.pouch.ClosureSnapshotPlain) {
        pouch.cacheClosure(snap)
        persistPouch(store, pouch)
    }

    fun activate(projectId: UUID) {
        if (!armMatched) throw LoginRejected("device serial does not match")
        pouch.activate(projectId)
        persistPouch(store, pouch)
    }

    fun openProcess(processId: UUID): ByteArray = pouch.openProcess(processId)

    fun setWelding(value: Boolean) {
        pouch.setWelding(value)
    }

    fun open(id: UUID): ByteArray = pouch.open(id)

    fun issuePersonal(kind: String, name: String, content: ByteArray) =
        pouch.issuePersonal(kind, name, content).also { persistPouch(store, pouch) }

    fun enqueueFact() = pouch.enqueueFact().also { persistPouch(store, pouch) }

    fun enqueueUpload(kind: String, content: ByteArray) =
        pouch.enqueueUpload(kind, content).also { persistPouch(store, pouch) }

    fun pendingFacts() = pouch.pendingFacts()

    fun pendingUploads() = pouch.pendingUploads()

    fun openPendingUpload(id: UUID): ByteArray = pouch.openPendingUpload(id)

    fun logout() {
        logoutMemory()
        edit { it.copy(error = null) }
    }

    private fun pullAllDevices(sess: PadLoginResult) {
        if (sess.policy.cacheScope == "current") return
        val tok = sess.token
        val fid = identity.factoryId
        val base = identity.factoryUrl
        val factoryId = UUID.fromString(fid)
        val keyed = sess.devices.filter { it.id.isNotBlank() && it.unwrapKey.size == 32 }
        if (keyed.isEmpty()) return
        val boxes = keyed.map { d -> d to factory.padInbox(base, fid, d.id, tok) }
        val n = boxes.sumOf { it.second.closures.size }
        pouch.setPolicy(maxOf(sess.policy.maxCachedProjects, n.coerceAtLeast(1)), "all")
        for ((d, box) in boxes) {
            val cid = UUID.fromString(d.id)
            pouch.bindClient(cid)
            for (ref in box.closures) {
                val t = factory.padPullClosure(base, fid, d.id, ref.assetId.toString(), tok)
                try {
                    pouch.cacheTransit(factoryId, cid, t, d.unwrapKey)
                } catch (_: PouchRejected) {
                }
            }
        }
        pouch.clearClient()
        pouch.setPolicy(sess.policy.maxCachedProjects, sess.policy.cacheScope)
    }

    private fun logoutMemory() {
        down.stop()
        pouch.logout()
        pouch.clearClient()
        store.close()
        token = null
        devices = emptyList()
        edit { it.copy(loggedIn = false, armMatched = false, personName = "") }
    }

    private fun translate(code: String): String = when (code) {
        "device serial is required" -> "读不到设备号"
        "device serial already bound" -> "该设备号已绑其他机"
        "device serial does not match" -> "设备号未在本厂登记"
        "invalid credentials" -> "登录名或密码不对"
        "client binding is void" -> "绑定已作废"
        "not found" -> "未绑定本厂"
        "scan failed" -> "扫描厂服务失败"
        else -> code
    }
}
