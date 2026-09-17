package com.gbndt.shijiaoqi.data.session

import com.gbndt.shijiaoqi.data.crypt.Wm2
import com.gbndt.shijiaoqi.data.log.PadLog
import com.gbndt.shijiaoqi.model.SessionState
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import com.gbndt.shijiaoqi.data.pouch.Digest
import com.gbndt.shijiaoqi.data.pouch.Pouch
import com.gbndt.shijiaoqi.data.pouch.PouchRejected
import java.util.UUID
import com.gbndt.shijiaoqi.model.FactoryOffer
import com.gbndt.shijiaoqi.data.remote.LanScan
import com.gbndt.shijiaoqi.data.remote.LanMulticast

/** 本机登录会话：令牌落盘；解封钥是否落盘跟厂策；退出或登录到期清钥。 */
class BagSession(
    private val serials: DeviceSerialReader,
    private val factory: FactoryGateway,
    private val identity: IdentityStore,
    private val store: EnvelopeStore,
    private val multicast: LanMulticast = LanMulticast.None,
    private val vault: SessionVault = MemorySessionVault(),
    private val keys: UnwrapKeyStore = MemoryUnwrapKeyStore(),
    private val clock: () -> Long = { System.currentTimeMillis() },
    private val onFactoryNet: (httpBase: String, factoryId: String) -> Unit = { _, _ -> },
    restoreOnStart: Boolean = true,
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

    init {
        if (restoreOnStart) restore()
    }

    fun savedUrl(): String = identity.factoryUrl
    fun savedFactoryId(): String = identity.factoryId
    fun savedClientId(): String = identity.clientId

    fun findFactories(): List<FactoryOffer> {
        edit { it.copy(error = null, busy = true) }
        PadLog.info("BagSession", "scan start")
        try {
            val hits = LanScan.find(identity.factoryUrl, factory::discover, multicast)
            if (hits.isEmpty()) {
                edit { it.copy(error = "本网没有发现厂服务") }
                PadLog.warn("BagSession", "scan empty")
            } else {
                PadLog.info("BagSession", "scan ok n=${hits.size}")
                // 扫到厂网即可直推日志，不登录。
                hits.distinctBy { it.httpBase.trimEnd('/') }.forEach { o ->
                    runCatching { onFactoryNet(o.httpBase, o.factoryId) }
                }
            }
            return hits
        } catch (e: LoginRejected) {
            PadLog.warn("BagSession", "scan rejected ${e.code}")
            edit { it.copy(error = translate(e.code)) }
            throw e
        } catch (e: Exception) {
            PadLog.error("BagSession", "scan failed", e)
            edit { it.copy(error = e.message ?: "scan failed") }
            throw e
        } finally {
            edit { it.copy(busy = false) }
        }
    }

    fun login(baseUrl: String, factoryId: String, loginName: String, password: String) {
        edit { it.copy(error = null, busy = true) }
        PadLog.info("BagSession", "login loading factory=$factoryId user=${loginName.trim()}")
        try {
            identity.factoryUrl = baseUrl.trim()
            identity.factoryId = factoryId.trim()
            identity.clientId = ""
            identity.clientShortCode = ""
            val sess = factory.loginPad(
                identity.factoryUrl, identity.factoryId, loginName.trim(), password,
            )
            logoutMemory()
            if (sess.unwrapKey.size != 32) throw LoginRejected("invalid unwrap key")
            val pouchKey = sess.unwrapKey.copyOf()
            pouch.login(pouchKey, sess.person.id, sess.policy.persistUnwrapKey)
            store.open(pouchKey, sess.policy.encryptPouch)
            runCatching {
                pouch.restoreEnvelopes(store.loadEnvelopes())
                pouch.restoreLedger(store.loadLedger())
                pouch.restoreClosures(store.loadClosures())
                pouch.restoreProjectBodies(store.loadProjectPlains())
                val active = store.loadActive()
                pouch.restoreActive(active.first, active.second)
            }
            pouch.setRoles(sess.roles)
            token = sess.token
            // 登录后立刻拉全量并落库，不依赖是否已有设备。
            pullCatalog(sess)
            persistPouch(store, pouch)
            runCatching { flushDirty() }
            store.savePerson(sess.person, sess.roles)
            store.savePolicy(sess.policy)
            store.saveDevices(sess.devices)
            val now = clock()
            val ttlMs = if (sess.policy.keyTtlSeconds > 0) sess.policy.keyTtlSeconds * 1000L else 0L
            store.saveSession(sess.token, if (ttlMs > 0) now + ttlMs else 0L, now)
            if (sess.policy.persistUnwrapKey) keys.save(pouchKey, sess.devices) else keys.clear()
            devices = sess.devices.map { it.copy(unwrapKey = ByteArray(0)) }
            saveVault(sess)
            sess.devices.forEach { Wm2.zero(it.unwrapKey) }
            Wm2.zero(sess.unwrapKey)
            Wm2.zero(pouchKey)
            edit {
                it.copy(
                    personName = sess.person.displayName.ifBlank { sess.person.loginName },
                    loginName = sess.person.loginName,
                    roles = sess.roles,
                    loggedIn = true,
                    armMatched = false,
                )
            }
            val serial = serials.read().trim()
            if (serial.isNotEmpty()) {
                runCatching { matchArm(serial) }
            }
            runCatching { onFactoryNet(identity.factoryUrl, identity.factoryId) }
            PadLog.info("BagSession", "login ok factory=${identity.factoryId}")
        } catch (e: LoginRejected) {
            logoutMemory()
            PadLog.warn("BagSession", "login rejected ${e.code} factory=$factoryId")
            edit { it.copy(error = translate(e.code)) }
            throw e
        } catch (e: Exception) {
            logoutMemory()
            PadLog.error("BagSession", "login failed factory=$factoryId", e)
            edit { it.copy(error = e.message ?: "login failed") }
            throw e
        } finally {
            edit { it.copy(busy = false) }
        }
    }

    fun matchArm(serial: String) {
        ensureFactoryArm(serial)
    }

    /** 作业前本地核：读到的号在登录落下的本厂设备名录里即可。 */
    fun ensureFactoryArm(serial: String = serials.read()) {
        val got = serial.trim()
        if (!loggedIn) throw LoginRejected("unauthorized")
        if (got.isEmpty()) {
            edit { it.copy(error = "读不到设备号", armMatched = false) }
            PadLog.warn("BagSession", "arm serial empty factory=${identity.factoryId}")
            throw LoginRejected("device serial is required")
        }
        val hit = findLocalDevice(got)
            ?: run {
                edit { it.copy(error = "设备号未在本厂登记", armMatched = false) }
                pouch.clearClient()
                identity.clientId = ""
                identity.clientShortCode = ""
                PadLog.warn("BagSession", "arm not listed factory=${identity.factoryId}")
                throw LoginRejected("device serial does not match")
            }
        identity.clientId = hit.id
        identity.clientShortCode = hit.shortCode
        pouch.bindClient(UUID.fromString(hit.id))
        if (hit.shortCode.isNotBlank()) pouch.setOrigin(hit.shortCode)
        edit { it.copy(armMatched = true, error = null) }
        PadLog.info("BagSession", "arm matched client=${hit.id}")
        runCatching { onFactoryNet(identity.factoryUrl, identity.factoryId) }
    }

    fun cachePlain(id: UUID, level: String, name: String, revision: Long, ownerId: UUID?, plain: ByteArray) {
        pouch.putPlain(id, level, name, revision, ownerId, plain)
        persistPouch(store, pouch)
    }

    /** 覆盖袋内已有正文，工艺仍封信封，工程仍明文。 */
    fun rewritePlain(id: UUID, plain: ByteArray) {
        pouch.rewrite(id, plain)
        persistPouch(store, pouch)
    }

    fun cacheClosure(snap: com.gbndt.shijiaoqi.data.pouch.ClosureSnapshotPlain) {
        pouch.cacheClosure(snap)
        persistPouch(store, pouch)
    }

    fun activate(projectId: UUID) {
        PadLog.info("BagSession", "activate project=$projectId")
        pouch.activate(projectId)
        persistPouch(store, pouch)
    }

    /** 出库解开工艺信封；明文只给调用方，袋里仍是密文。 */
    fun openProcess(processId: UUID): ByteArray {
        hydrateCipher(processId)
        return pouch.openProcess(processId)
    }

    fun setWelding(value: Boolean) {
        PadLog.info("BagSession", "welding=$value")
        if (value) ensureFactoryArm()
        pouch.setWelding(value)
    }

    fun open(id: UUID): ByteArray {
        hydrateCipher(id)
        return pouch.open(id)
    }

    /** 用的时候从库取信封密文，解开只发生在内存。 */
    private fun hydrateCipher(id: UUID) {
        if (pouch.held(id)) return
        store.loadEnvelope(id)?.let { pouch.rememberCipher(it) }
    }

    fun issuePersonal(kind: String, name: String, content: ByteArray) =
        pouch.issuePersonal(kind, name, content).also { persistPouch(store, pouch) }

    /** 可复制非个人级另存个人级；保密拒绝。 */
    fun ensurePersonalProcess(srcId: UUID, name: String, body: ByteArray): UUID {
        if (!pouch.copyableOf(srcId)) throw PouchRejected(Pouch.ERR_NOT_COPYABLE)
        if (pouch.levelOf(srcId) == Pouch.LEVEL_PERSONAL) {
            rewritePlain(srcId, body)
            pouch.activeProject()?.let { pouch.pinProcess(it, srcId) }
            persistPouch(store, pouch)
            return srcId
        }
        val issued = issuePersonal(Pouch.KIND_PROCESS, name.ifBlank { pouch.nameOf(srcId) }, body)
        pouch.activeProject()?.let { pouch.pinProcess(it, issued.id) }
        persistPouch(store, pouch)
        return issued.id
    }

    /** 把本机已改正文按内容摘要对齐到厂端；冲突则用厂端当前修订再写一次。 */
    fun flushDirty() {
        if (!loggedIn) return
        val tok = token ?: return
        val base = identity.factoryUrl
        val fid = identity.factoryId
        if (base.isBlank() || fid.isBlank()) return
        val processes = pouch.dirtyIds().filter { pouch.kindOf(it) == Pouch.KIND_PROCESS }
        val projects = pouch.dirtyIds().filter { pouch.kindOf(it) == Pouch.KIND_PROJECT }
        for (id in processes) runCatching { flushOne(base, fid, tok, id) }
        for (id in projects) {
            runCatching { flushDeps(base, fid, tok, id) }
            runCatching { flushOne(base, fid, tok, id) }
        }
        persistPouch(store, pouch)
    }

    private fun flushOne(base: String, fid: String, tok: String, id: UUID) {
        val plain = open(id)
        try {
            val text = String(plain, Charsets.UTF_8)
            val remote = factory.getAsset(base, fid, tok, id.toString())
            if (remote == null) {
                val row = factory.createPadAsset(
                    base, fid, tok, pouch.kindOf(id), pouch.nameOf(id), text,
                    id.toString(), pouch.codeOf(id).orEmpty(), pouch.depsOf(id),
                )
                pouch.acceptRemote(id, row.revision, row.digest)
                return
            }
            if (Digest.match(plain, remote.digest)) {
                pouch.acceptRemote(id, remote.revision, remote.digest)
                return
            }
            val row = try {
                factory.updateAssetContent(base, fid, tok, id.toString(), remote.revision, text)
            } catch (e: LoginRejected) {
                if (e.code != "revision does not match") throw e
                val again = factory.getAsset(base, fid, tok, id.toString()) ?: throw e
                factory.updateAssetContent(base, fid, tok, id.toString(), again.revision, text)
            }
            pouch.acceptRemote(id, row.revision, row.digest)
        } finally {
            Wm2.zero(plain)
        }
    }

    private fun flushDeps(base: String, fid: String, tok: String, id: UUID) {
        val local = pouch.depsOf(id)
        val remote = factory.getAsset(base, fid, tok, id.toString()) ?: return
        if (depsMatch(local, remote.deps)) return
        try {
            factory.setAssetDeps(base, fid, tok, id.toString(), remote.revision, local)
        } catch (e: LoginRejected) {
            if (e.code != "revision does not match") throw e
            val again = factory.getAsset(base, fid, tok, id.toString()) ?: throw e
            factory.setAssetDeps(base, fid, tok, id.toString(), again.revision, local)
        }
    }

    private fun depsMatch(a: List<com.gbndt.shijiaoqi.data.pouch.AssetDep>, b: List<com.gbndt.shijiaoqi.data.pouch.AssetDep>): Boolean {
        if (a.size != b.size) return false
        val byId = b.associateBy { it.id }
        return a.all { d ->
            val o = byId[d.id] ?: return false
            o.revision == d.revision && java.security.MessageDigest.isEqual(o.digest, d.digest)
        }
    }

    fun enqueueFact() = pouch.enqueueFact().also { persistPouch(store, pouch) }

    fun enqueueUpload(kind: String, content: ByteArray) =
        pouch.enqueueUpload(kind, content).also { persistPouch(store, pouch) }

    fun pendingFacts() = pouch.pendingFacts()

    fun pendingUploads() = pouch.pendingUploads()

    fun openPendingUpload(id: UUID): ByteArray = pouch.openPendingUpload(id)

    fun logout() {
        PadLog.info("BagSession", "logout")
        dropPersistedSession()
        logoutMemory()
        edit { it.copy(error = null) }
    }

    fun changePassword(password: String) {
        if (!loggedIn) throw LoginRejected("unauthorized")
        val next = password.trim()
        if (next.isEmpty()) throw LoginRejected("empty password")
        val tok = token ?: throw LoginRejected("unauthorized")
        factory.changePassword(identity.factoryUrl, identity.factoryId, tok, next)
        PadLog.info("BagSession", "password changed")
    }

    /** 按厂端同一份可见资产拉全量；工程明文，工艺用人钥解过站包。 */
    private fun pullCatalog(sess: PadLoginResult) {
        val tok = sess.token
        val fid = identity.factoryId
        val base = identity.factoryUrl
        val factoryId = UUID.fromString(fid)
        if (sess.unwrapKey.size != 32) return
        val personId = sess.person.id
        PadLog.info("BagSession", "catalog loading")
        val box = try {
            factory.padInbox(base, fid, tok)
        } catch (_: LoginRejected) {
            PadLog.warn("BagSession", "catalog inbox rejected")
            return
        }
        var ok = 0
        var failed = 0
        for (ref in box.closures) {
            val t = try {
                factory.padPullClosure(base, fid, ref.assetId.toString(), tok)
            } catch (_: LoginRejected) {
                failed++
                continue
            }
            try {
                pouch.cacheTransit(factoryId, personId, t, sess.unwrapKey)
                ok++
            } catch (_: PouchRejected) {
                failed++
            } catch (_: Exception) {
                failed++
            }
        }
        PadLog.info("BagSession", "catalog done listed=${box.closures.size} ok=$ok fail=$failed")
    }

    private fun saveVault(sess: PadLoginResult) {
        val now = clock()
        val ttlMs = if (sess.policy.keyTtlSeconds > 0) sess.policy.keyTtlSeconds * 1000L else 0L
        vault.save(
            SavedSession(
                token = sess.token,
                expiresAtMillis = if (ttlMs > 0) now + ttlMs else 0L,
                personId = sess.person.id.toString(),
                personName = sess.person.displayName.ifBlank { sess.person.loginName },
                loginName = sess.person.loginName,
                roles = sess.roles,
                persistUnwrapKey = sess.policy.persistUnwrapKey,
                keyTtlSeconds = sess.policy.keyTtlSeconds,
                loggedInAtMillis = now,
                encryptPouch = sess.policy.encryptPouch,
                devices = sess.devices.map { it.copy(unwrapKey = ByteArray(0)) },
            ),
        )
    }

    /** 开机恢复：登录未到期且钥文件还在才进主页；到期或退出过则清钥。 */
    fun restore() {
        val saved = vault.load() ?: return
        if (saved.token.isBlank() || saved.personId.isBlank()) {
            dropPersistedSession()
            return
        }
        val now = clock()
        if (saved.expiresAtMillis > 0 && now >= saved.expiresAtMillis) {
            dropPersistedSession()
            return
        }
        if (!saved.persistUnwrapKey) return
        val key = keys.loadPouchKey()
        if (key == null || key.size != 32) {
            dropPersistedSession()
            return
        }
        val personId = runCatching { UUID.fromString(saved.personId) }.getOrNull() ?: return
        try {
            pouch.login(key, personId, true)
            store.open(key, saved.encryptPouch)
            pouch.restoreEnvelopes(store.loadEnvelopes())
            pouch.restoreLedger(store.loadLedger())
            pouch.restoreClosures(store.loadClosures())
            pouch.restoreProjectBodies(store.loadProjectPlains())
            val active = store.loadActive()
            pouch.restoreActive(active.first, active.second)
            pouch.setRoles(saved.roles)
            token = saved.token
            devices = store.loadDevices().ifEmpty { saved.devices }
            val person = store.loadPerson()
            edit {
                it.copy(
                    personName = saved.personName.ifBlank { person?.displayName.orEmpty() },
                    loginName = saved.loginName.ifBlank { person?.loginName.orEmpty() },
                    roles = saved.roles,
                    loggedIn = true,
                    armMatched = false,
                    error = null,
                )
            }
            val serial = serials.read().trim()
            if (serial.isNotEmpty()) {
                runCatching { ensureFactoryArm(serial) }
            }
            PadLog.info("BagSession", "restore ok")
        } catch (_: Exception) {
            PadLog.warn("BagSession", "restore failed")
            dropPersistedSession()
            logoutMemory()
        }
    }

    /** 去空白、忽略大小写；号对上或互相包含就算本厂设备。 */
    private fun findLocalDevice(serial: String): PadDevice? {
        val want = foldSerial(serial)
        if (want.isEmpty()) return null
        return devices.firstOrNull { foldSerial(it.deviceSerial) == want }
            ?: devices.firstOrNull {
                val have = foldSerial(it.deviceSerial)
                have.isNotEmpty() && (have.contains(want) || want.contains(have))
            }
    }

    private fun foldSerial(raw: String): String =
        buildString(raw.length) {
            for (c in raw) if (!c.isWhitespace()) append(c.uppercaseChar())
        }

    /** 退出或登录到期：令牌、解封钥、本机袋一起丢掉。 */
    private fun dropPersistedSession() {
        vault.clear()
        keys.clear()
        pouch.wipeContents()
        runCatching { store.wipe() }
        store.close()
        token = null
        devices = emptyList()
    }

    private fun logoutMemory() {
        pouch.logout()
        pouch.clearClient()
        store.close()
        token = null
        devices = emptyList()
        edit { it.copy(loggedIn = false, armMatched = false, personName = "", loginName = "", roles = emptyList()) }
    }

    private fun translate(code: String): String = when (code) {
        "device serial is required" -> "读不到设备号"
        "device serial already bound" -> "该设备号已绑其他机"
        "device serial does not match" -> "设备号未在本厂登记"
        "invalid credentials" -> "登录名或密码不对"
        "empty password" -> "新密码不能为空"
        "client binding is void" -> "绑定已作废"
        "not found" -> "未绑定本厂"
        "scan failed" -> "扫描厂服务失败"
        "invalid unwrap key" -> "领不到解封钥"
        else -> code
    }
}
