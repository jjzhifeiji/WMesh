package com.gbndt.shijiaoqi.platform.session

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import com.gbndt.shijiaoqi.platform.crypt.Wm2
import com.gbndt.shijiaoqi.platform.pouch.Pouch
import java.util.UUID

/** 本机登录会话：解封钥只在内存，信封进袋，默认不把钥落盘。 */
class BagSession(
    private val serials: DeviceSerialReader,
    private val factory: FactoryGateway,
    private val identity: IdentityStore,
    private val store: EnvelopeStore,
    private val down: DownChannel = NoopDownChannel(),
) {
    val pouch = Pouch()
    var loggedIn by mutableStateOf(false)
        private set
    var personName by mutableStateOf("")
        private set
    var error by mutableStateOf<String?>(null)
        private set
    var busy by mutableStateOf(false)
        private set
    private var token: String? = null

    fun savedUrl(): String = identity.factoryUrl
    fun savedFactoryId(): String = identity.factoryId
    fun savedClientId(): String = identity.clientId

    fun login(baseUrl: String, factoryId: String, clientId: String, loginName: String, password: String) {
        error = null
        val serial = serials.read().trim()
        if (serial.isEmpty()) {
            error = "读不到设备号"
            throw LoginRejected("device serial is required")
        }
        busy = true
        try {
            identity.factoryUrl = baseUrl.trim()
            identity.factoryId = factoryId.trim()
            identity.clientId = clientId.trim()
            factory.registerDevice(identity.factoryUrl, identity.factoryId, identity.clientId, serial)
            val sess = factory.loginOnClient(
                identity.factoryUrl, identity.factoryId, identity.clientId, serial, loginName.trim(), password,
            )
            logoutMemory()
            pouch.login(sess.unwrapKey, sess.person.id, sess.policy.persistUnwrapKey)
            store.open(sess.unwrapKey)
            pouch.restoreEnvelopes(store.loadEnvelopes())
            pouch.restoreLedger(store.loadLedger())
            pouch.restoreClosures(store.loadClosures())
            pouch.bindClient(UUID.fromString(identity.clientId))
            pouch.setOnline(true)
            val saved = store.loadActive()
            pouch.restoreActive(saved.first, saved.second)
            pouch.setPolicy(sess.policy.maxCachedProjects, sess.policy.cacheScope)
            if (sess.clientShortCode.isNotBlank()) identity.clientShortCode = sess.clientShortCode.trim()
            if (identity.clientShortCode.isNotBlank()) pouch.setOrigin(identity.clientShortCode)
            pouch.setRoles(sess.roles)
            persistPouch(store, pouch)
            store.savePerson(sess.person)
            store.savePolicy(sess.policy)
            token = sess.token
            personName = sess.person.displayName.ifBlank { sess.person.loginName }
            loggedIn = true
            Wm2.zero(sess.unwrapKey)
            down.start(
                LiveChannel(
                    baseUrl = identity.factoryUrl,
                    factoryId = UUID.fromString(identity.factoryId),
                    clientId = UUID.fromString(identity.clientId),
                    token = sess.token,
                    mqttUrl = sess.mqttUrl,
                    signingPub = sess.signingPublicKey,
                    pouch = pouch,
                    store = store,
                ),
            )
        } catch (e: LoginRejected) {
            logoutMemory()
            error = translate(e.code)
            throw e
        } catch (e: Exception) {
            logoutMemory()
            error = e.message ?: "login failed"
            throw e
        } finally {
            busy = false
        }
    }

    fun cachePlain(id: UUID, level: String, name: String, revision: Long, ownerId: UUID?, plain: ByteArray) {
        pouch.putPlain(id, level, name, revision, ownerId, plain)
        persistPouch(store, pouch)
    }

    fun cacheClosure(snap: com.gbndt.shijiaoqi.platform.pouch.ClosureSnapshotPlain) {
        pouch.cacheClosure(snap)
        persistPouch(store, pouch)
    }

    fun activate(projectId: UUID) {
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
        error = null
    }

    private fun logoutMemory() {
        down.stop()
        pouch.logout()
        store.close()
        token = null
        loggedIn = false
        personName = ""
    }

    private fun translate(code: String): String = when (code) {
        "device serial is required" -> "读不到设备号"
        "device serial already bound" -> "该设备号已绑其他机"
        "device serial does not match" -> "设备号与本机登记不一致"
        "invalid credentials" -> "登录名或密码不对"
        "client binding is void" -> "绑定已作废"
        "not found" -> "未绑定本厂"
        else -> code
    }
}
