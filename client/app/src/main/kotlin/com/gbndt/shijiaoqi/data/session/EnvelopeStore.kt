package com.gbndt.shijiaoqi.data.session

import com.gbndt.shijiaoqi.data.pouch.CachedClosure
import com.gbndt.shijiaoqi.data.pouch.CachedEnvelope
import com.gbndt.shijiaoqi.data.pouch.ClosureCodec
import com.gbndt.shijiaoqi.data.pouch.Pouch
import java.util.UUID

data class PersonMeta(
    val id: UUID,
    val loginName: String,
    val displayName: String,
)

data class PolicyMeta(
    val revision: Long,
    val maxCachedProjects: Int,
    val cacheScope: String,
    val persistUnwrapKey: Boolean,
    val keyTtlSeconds: Long,
)

/** 信封与人员元数据落盘；不含解封钥原文。 */
interface EnvelopeStore {
    fun open(passphrase: ByteArray)
    fun close()
    fun loadEnvelopes(): List<CachedEnvelope>
    fun upsert(env: CachedEnvelope)
    fun delete(id: UUID)
    fun savePerson(meta: PersonMeta)
    fun loadPerson(): PersonMeta?
    fun savePolicy(meta: PolicyMeta)
    fun loadPolicy(): PolicyMeta?
    fun saveClosures(list: List<CachedClosure>)
    fun loadClosures(): List<CachedClosure>
    fun saveActive(id: UUID?, revision: Long)
    fun loadActive(): Pair<UUID?, Long>
    fun saveLedger(raw: String)
    fun loadLedger(): String
}

class MemoryEnvelopeStore : EnvelopeStore {
    private var open = false
    private val items = LinkedHashMap<UUID, CachedEnvelope>()
    private var person: PersonMeta? = null
    private var policy: PolicyMeta? = null
    private var closures = ""
    private var active = ""
    private var ledger = ""

    override fun open(passphrase: ByteArray) {
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

    override fun upsert(env: CachedEnvelope) {
        check(open)
        items[env.id] = env.copy(blob = env.blob.copyOf())
    }

    override fun delete(id: UUID) {
        check(open)
        items.remove(id)
    }

    override fun savePerson(meta: PersonMeta) {
        check(open)
        person = meta
    }

    override fun loadPerson(): PersonMeta? {
        check(open)
        return person
    }

    override fun savePolicy(meta: PolicyMeta) {
        check(open)
        policy = meta
    }

    override fun loadPolicy(): PolicyMeta? {
        check(open)
        return policy
    }

    override fun saveClosures(list: List<CachedClosure>) {
        check(open)
        closures = ClosureCodec.encode(list)
    }

    override fun loadClosures(): List<CachedClosure> {
        check(open)
        return ClosureCodec.decode(closures)
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
}

fun persistPouch(store: EnvelopeStore, pouch: Pouch) {
    val kept = pouch.diskSnapshot().associateBy { it.id }
    store.loadEnvelopes().forEach { env ->
        if (env.id !in kept) store.delete(env.id)
    }
    kept.values.forEach { store.upsert(it) }
    store.saveClosures(pouch.exportClosures())
    store.saveActive(pouch.activeProject(), pouch.activeRevision())
    store.saveLedger(pouch.exportLedger())
}

internal fun parseActive(raw: String): Pair<UUID?, Long> {
    if (raw.isBlank()) return null to 0L
    val p = raw.split('|')
    if (p.size < 2) return null to 0L
    return UUID.fromString(p[0]) to p[1].toLong()
}

interface IdentityStore {
    var factoryUrl: String
    var factoryId: String
    var clientId: String
    var clientShortCode: String
}

class MemoryIdentityStore : IdentityStore {
    override var factoryUrl: String = ""
    override var factoryId: String = ""
    override var clientId: String = ""
    override var clientShortCode: String = ""
}
