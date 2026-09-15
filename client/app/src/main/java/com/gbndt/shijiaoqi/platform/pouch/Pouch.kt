package com.gbndt.shijiaoqi.platform.pouch

import com.gbndt.shijiaoqi.platform.crypt.Wm2
import java.security.MessageDigest
import java.util.UUID

data class CachedEnvelope(
    val id: UUID,
    val level: String,
    val ownerId: UUID?,
    val name: String,
    val revision: Long,
    val blob: ByteArray,
)

data class TransitMember(
    val id: UUID,
    val level: String,
    val name: String,
    val revision: Long,
    val ownerId: UUID?,
    val content: ByteArray,
    val kind: String = "",
    val status: String = "",
    val digest: ByteArray = ByteArray(0),
    val deps: List<AssetDep> = emptyList(),
    val code: String = "",
)

data class TransitClosure(
    val wrap: ByteArray,
    val members: List<TransitMember>,
    val assetId: UUID,
    val revision: Long,
    val kind: String = "project",
    val level: String = "",
    val status: String = "",
    val digest: ByteArray = ByteArray(0),
    val targetClientId: UUID? = null,
)

/** 本机袋：磁盘只有信封，解封钥默认只在登录内存；激活恰好一份工程。 */
class Pouch {
    private var persist = false
    private var person: UUID? = null
    private var persistPerson: UUID? = null
    private var unwrap: ByteArray? = null
    private var material: ByteArray? = null
    private val items = LinkedHashMap<UUID, CachedEnvelope>()
    private val closures = LinkedHashMap<UUID, CachedClosure>()
    private var clientId: UUID? = null
    private var welding = false
    private var online = true
    private var maxCached = 2
    private var cacheScope = SCOPE_ALL
    private var activeId: UUID? = null
    private var activeRev = 0L
    private var origin: String? = null
    private val nextN = mutableMapOf(KIND_PROCESS to 1L, KIND_PROJECT to 1L)
    private val codes = LinkedHashMap<UUID, String>()
    private val byCode = LinkedHashMap<String, UUID>()
    private val facts = ArrayList<PendingFact>()
    private val uploads = ArrayList<HeldUpload>()
    private var roles: Set<String> = emptySet()

    fun login(unwrapKey: ByteArray, personId: UUID, persistUnwrapKey: Boolean) {
        require(unwrapKey.size == Wm2.KEY_SIZE)
        logout()
        unwrap = unwrapKey.copyOf()
        person = personId
        persist = persistUnwrapKey
        if (persistUnwrapKey) {
            material = unwrapKey.copyOf()
            persistPerson = personId
        } else {
            Wm2.zero(material)
            material = null
            persistPerson = null
        }
    }

    fun logout() {
        Wm2.zero(unwrap)
        unwrap = null
        person = null
        welding = false
        activeId = null
        activeRev = 0L
        if (!persist) {
            Wm2.zero(material)
            material = null
            persistPerson = null
        }
        roles = emptySet()
    }

    fun hasUnwrapKey(): Boolean = unwrap?.size == Wm2.KEY_SIZE

    fun bindClient(id: UUID) {
        clientId = id
    }

    fun setPolicy(maxCachedProjects: Int, scope: String) {
        maxCached = if (maxCachedProjects < 1) 2 else maxCachedProjects
        cacheScope = if (scope == SCOPE_CURRENT) SCOPE_CURRENT else SCOPE_ALL
        pruneToActive()
    }

    fun setOnline(value: Boolean) {
        online = value
    }

    fun setWelding(value: Boolean) {
        welding = value
    }

    fun hasClosure(id: UUID): Boolean = closures.containsKey(id)

    fun activeProject(): UUID? = activeId

    fun activeRevision(): Long = activeRev

    fun exportClosures(): List<CachedClosure> = closures.values.map { it.copy(members = it.members.toList()) }

    /** 当前激活闭包里的工艺成员，列表不解开正文。 */
    fun listProcessMembers(): List<CachedMember> {
        val aid = activeId ?: return emptyList()
        val cl = closures[aid] ?: return emptyList()
        return cl.members.filter { it.kind == KIND_PROCESS }
    }

    fun restoreClosures(list: List<CachedClosure>) {
        closures.clear()
        list.forEach { closures[it.assetId] = it }
        list.forEach { c ->
            c.members.forEach { m ->
                if (m.code.isNotBlank()) bindCode(m.id, m.code)
            }
        }
    }

    fun restoreActive(id: UUID?, revision: Long) {
        if (id == null || !closures.containsKey(id)) {
            activeId = null
            activeRev = 0L
            return
        }
        activeId = id
        activeRev = revision
    }

    fun putPlain(id: UUID, level: String, name: String, revision: Long, ownerId: UUID?, plain: ByteArray) {
        val key = unwrap ?: throw SecurityException("unauthorized")
        val who = person ?: throw SecurityException("unauthorized")
        if (level == LEVEL_PERSONAL && ownerId != who) throw SecurityException("forbidden")
        val aad = Wm2.assetAad(uuidBytes(id), revision, "pouch")
        val blob = Wm2.seal(key, plain, aad)
        items[id] = CachedEnvelope(id, level, ownerId, name, revision, blob)
    }

    fun putPlainIfNewer(id: UUID, level: String, name: String, revision: Long, ownerId: UUID?, plain: ByteArray): Boolean {
        val held = items[id]
        if (held != null && held.revision >= revision) return false
        putPlain(id, level, name, revision, ownerId, plain)
        return true
    }

    fun revisionOf(id: UUID): Long = items[id]?.revision ?: 0L

    fun ingestTransit(factoryId: UUID, clientId: UUID, wrap: ByteArray, members: List<TransitMember>): Int {
        val key = unwrap ?: throw SecurityException("unauthorized")
        val fid = uuidBytes(factoryId)
        val cid = uuidBytes(clientId)
        val dek = Wm2.open(key, wrap, Wm2.clientTransitDekAad(fid, cid))
        try {
            var n = 0
            for (m in members) {
                val plain = Wm2.open(dek, m.content, Wm2.clientTransitAad(fid, cid, uuidBytes(m.id), m.revision))
                try {
                    if (putPlainIfNewer(m.id, m.level, m.name, m.revision, m.ownerId, plain)) n++
                } finally {
                    Wm2.zero(plain)
                }
            }
            return n
        } finally {
            Wm2.zero(dek)
        }
    }

    fun cacheTransit(factoryId: UUID, clientId: UUID, t: TransitClosure) {
        val key = unwrap ?: throw PouchRejected(ERR_UNAUTHORIZED)
        val fid = uuidBytes(factoryId)
        val cid = uuidBytes(clientId)
        val dek = Wm2.open(key, t.wrap, Wm2.clientTransitDekAad(fid, cid))
        val plains = ArrayList<ClosureMemberPlain>(t.members.size)
        try {
            for (m in t.members) {
                val plain = Wm2.open(dek, m.content, Wm2.clientTransitAad(fid, cid, uuidBytes(m.id), m.revision))
                val dig = if (m.digest.isNotEmpty()) m.digest else Digest.sum(plain)
                plains.add(
                    ClosureMemberPlain(
                        id = m.id,
                        kind = m.kind.ifBlank { if (m.id == t.assetId) KIND_PROJECT else KIND_PROCESS },
                        level = m.level,
                        name = m.name,
                        status = m.status.ifBlank { STATUS_AVAILABLE },
                        revision = m.revision,
                        content = plain,
                        digest = dig,
                        deps = m.deps,
                        ownerId = m.ownerId,
                        code = m.code,
                    ),
                )
            }
            val parts = plains.map { Digest.Member(it.id, it.revision, it.digest, it.content) }
            val pack = if (t.digest.isNotEmpty()) t.digest else Digest.closureSum(parts)
            cacheClosure(
                ClosureSnapshotPlain(
                    kind = t.kind.ifBlank { KIND_PROJECT },
                    assetId = t.assetId,
                    revision = t.revision,
                    level = t.level.ifBlank { plains.firstOrNull()?.level.orEmpty() },
                    status = t.status.ifBlank { STATUS_AVAILABLE },
                    digest = pack,
                    targetClientId = t.targetClientId ?: clientId,
                    members = plains,
                ),
            )
        } finally {
            Wm2.zero(dek)
            plains.forEach { Wm2.zero(it.content) }
        }
    }

    fun cacheClosure(snap: ClosureSnapshotPlain) {
        if (!loggedIn()) throw PouchRejected(ERR_UNAUTHORIZED)
        validateClosure(snap)
        if (snap.kind != KIND_PROJECT) throw PouchRejected(ERR_FORBIDDEN)
        val self = clientId ?: throw PouchRejected(ERR_FORBIDDEN)
        if (snap.targetClientId == null || snap.targetClientId != self) throw PouchRejected(ERR_FORBIDDEN)
        val held = closures[snap.assetId]
        if (held != null && held.revision >= snap.revision) return
        if (held == null && projectCount() >= maxCached) throw PouchRejected(ERR_CACHE_FULL)
        bindMemberCodes(snap.members)
        val old = held?.members.orEmpty()
        for (m in snap.members) {
            putPlain(m.id, m.level, m.name, m.revision, m.ownerId, m.content)
        }
        closures[snap.assetId] = metaFromSnap(snap)
        dropUnreferenced(old)
    }

    fun activate(projectId: UUID) {
        if (!loggedIn()) throw PouchRejected(ERR_UNAUTHORIZED)
        if (!closures.containsKey(projectId)) {
            if (cacheScope == SCOPE_CURRENT && !online) throw PouchRejected(ERR_NOT_FOUND)
            throw PouchRejected(ERR_NOT_FOUND)
        }
        if (welding && activeId != null && activeId != projectId) throw PouchRejected(ERR_FORBIDDEN)
        val snap = snapshotFromDisk(projectId)
        validateClosure(snap)
        val self = clientId ?: throw PouchRejected(ERR_FORBIDDEN)
        if (snap.targetClientId == null || snap.targetClientId != self) throw PouchRejected(ERR_FORBIDDEN)
        val root = snap.members.first()
        if (root.level == LEVEL_PERSONAL && root.ownerId != person) throw PouchRejected(ERR_FORBIDDEN)
        if (root.status != STATUS_AVAILABLE) throw PouchRejected(ERR_NOT_AVAILABLE)
        activeId = snap.assetId
        activeRev = snap.revision
        pruneToActive()
    }

    fun openProcess(processId: UUID): ByteArray {
        if (!loggedIn()) throw PouchRejected(ERR_UNAUTHORIZED)
        val aid = activeId ?: throw PouchRejected(ERR_NOT_FOUND)
        val cl = closures[aid] ?: throw PouchRejected(ERR_NOT_FOUND)
        val member = cl.members.firstOrNull { it.id == processId } ?: throw PouchRejected(ERR_NOT_FOUND)
        if (member.kind != KIND_PROCESS) throw PouchRejected(ERR_NOT_FOUND)
        return open(processId)
    }

    fun uncache(projectId: UUID) {
        if (!loggedIn()) throw PouchRejected(ERR_UNAUTHORIZED)
        if (welding) throw PouchRejected(ERR_FORBIDDEN)
        if (activeId == projectId) throw PouchRejected(ERR_FORBIDDEN)
        val old = closures.remove(projectId) ?: throw PouchRejected(ERR_NOT_FOUND)
        dropUnreferenced(old.members)
    }

    fun open(id: UUID): ByteArray {
        val key = unwrap ?: throw SecurityException("unauthorized")
        val who = person ?: throw SecurityException("unauthorized")
        val env = items[id] ?: throw NoSuchElementException("not found")
        if (env.level == LEVEL_PERSONAL && env.ownerId != who) throw SecurityException("forbidden")
        return Wm2.open(key, env.blob, Wm2.assetAad(uuidBytes(env.id), env.revision, "pouch"))
    }

    fun diskSnapshot(): List<CachedEnvelope> = items.values.map {
        it.copy(blob = it.blob.copyOf())
    }

    fun restoreEnvelopes(list: List<CachedEnvelope>) {
        items.clear()
        list.forEach { items[it.id] = it.copy(blob = it.blob.copyOf()) }
    }

    fun setOrigin(code: String) {
        val t = code.trim()
        if (!AssetCode.validClientOrigin(t)) throw PouchRejected(ERR_CODE_MISSING)
        origin = t
    }

    fun origin(): String? = origin

    fun setRoles(values: Collection<String>) {
        roles = values.toSet()
    }

    fun seedSeq(kind: String, next: Long) {
        if (kind != KIND_PROCESS && kind != KIND_PROJECT) throw PouchRejected(ERR_CODE_CONFLICT)
        nextN[kind] = next
    }

    fun issuePersonal(kind: String, name: String, content: ByteArray): IssuedAsset {
        if (!loggedIn()) throw PouchRejected(ERR_UNAUTHORIZED)
        val short = origin ?: throw PouchRejected(ERR_CODE_MISSING)
        val n = nextN[kind] ?: 1L
        val code = AssetCode.format(kind, short, n)
        val id = UUID.randomUUID()
        bindCode(id, code)
        nextN[kind] = n + 1
        putPlain(id, LEVEL_PERSONAL, name, 1, person, content)
        return IssuedAsset(id, kind, code, LEVEL_PERSONAL)
    }

    fun bindCode(id: UUID, code: String) {
        if (!AssetCode.valid(code)) throw PouchRejected(ERR_CODE_CONFLICT)
        val have = codes[id]
        if (have != null) {
            if (have != code) throw PouchRejected(ERR_CODE_CONFLICT)
            return
        }
        val owner = byCode[code]
        if (owner != null && owner != id) throw PouchRejected(ERR_CODE_CONFLICT)
        codes[id] = code
        byCode[code] = id
    }

    fun codeOf(id: UUID): String? = codes[id]

    fun enqueueFact(): PendingFact {
        if (!loggedIn()) throw PouchRejected(ERR_UNAUTHORIZED)
        if (!canEnqueue()) throw PouchRejected(ERR_FORBIDDEN)
        val item = PendingFact(UUID.randomUUID(), person!!)
        facts.add(item)
        return item
    }

    fun enqueueUpload(kind: String, content: ByteArray): PendingUploadRecord {
        if (!loggedIn()) throw PouchRejected(ERR_UNAUTHORIZED)
        if (kind != KIND_POINT_CLOUD && kind != KIND_IMAGE) throw PouchRejected(ERR_FORBIDDEN)
        if (!canEnqueue()) throw PouchRejected(ERR_FORBIDDEN)
        val cid = clientId ?: throw PouchRejected(ERR_FORBIDDEN)
        val body = content.copyOf()
        val rec = PendingUploadRecord(UUID.randomUUID(), kind, Digest.sum(body), person!!, cid)
        uploads.add(HeldUpload(rec, body))
        return rec
    }

    fun pendingFacts(): List<PendingFact> = facts.toList()

    fun pendingUploads(): List<PendingUploadRecord> = uploads.map { it.rec }

    fun openPendingUpload(id: UUID): ByteArray {
        if (!loggedIn()) throw PouchRejected(ERR_UNAUTHORIZED)
        val held = uploads.firstOrNull { it.rec.id == id } ?: throw PouchRejected(ERR_NOT_FOUND)
        return held.content.copyOf()
    }

    fun exportLedger(): String = LedgerCodec.encode(
        origin = origin.orEmpty(),
        nextProcess = nextN[KIND_PROCESS] ?: 1L,
        nextProject = nextN[KIND_PROJECT] ?: 1L,
        codes = codes,
        facts = facts,
        uploads = uploads,
    )

    fun restoreLedger(raw: String) {
        val state = LedgerCodec.decode(raw)
        origin = state.origin.ifBlank { null }
        nextN[KIND_PROCESS] = state.nextProcess
        nextN[KIND_PROJECT] = state.nextProject
        codes.clear()
        byCode.clear()
        state.codes.forEach { (id, code) ->
            codes[id] = code
            byCode[code] = id
        }
        facts.clear()
        facts.addAll(state.facts)
        uploads.clear()
        uploads.addAll(state.uploads)
    }

    fun currentPerson(): UUID? = person

    fun relockFromMaterial() {
        val mat = material
        if (!persist || mat == null || mat.size != Wm2.KEY_SIZE) throw SecurityException("unauthorized")
        unwrap = mat.copyOf()
        person = persistPerson
    }

    private fun loggedIn(): Boolean = hasUnwrapKey() && person != null

    private fun canEnqueue(): Boolean =
        ROLE_OPERATOR in roles || ROLE_PROCESS_ENGINEER in roles

    private fun bindMemberCodes(members: List<ClosureMemberPlain>) {
        val seen = HashMap<String, UUID>()
        for (m in members) {
            if (m.code.isBlank()) continue
            if (!AssetCode.valid(m.code)) throw PouchRejected(ERR_CODE_CONFLICT)
            val have = codes[m.id]
            if (have != null && have != m.code) throw PouchRejected(ERR_CODE_CONFLICT)
            val owner = byCode[m.code]
            if (owner != null && owner != m.id) throw PouchRejected(ERR_CODE_CONFLICT)
            val dup = seen[m.code]
            if (dup != null && dup != m.id) throw PouchRejected(ERR_CODE_CONFLICT)
            seen[m.code] = m.id
        }
        for (m in members) {
            if (m.code.isNotBlank()) bindCode(m.id, m.code)
        }
    }

    private fun projectCount(): Int = closures.values.count { it.kind == KIND_PROJECT }

    private fun pruneToActive() {
        if (cacheScope != SCOPE_CURRENT) return
        val keep = activeId ?: return
        val dropped = closures.entries.filter { it.key != keep }.map { it.value }
        closures.keys.retainAll(setOf(keep))
        dropped.forEach { dropUnreferenced(it.members) }
    }

    private fun dropUnreferenced(old: List<CachedMember>) {
        val ref = referencedIds()
        old.forEach { m ->
            if (m.id !in ref) items.remove(m.id)
        }
    }

    private fun referencedIds(): Set<UUID> {
        val out = HashSet<UUID>()
        closures.values.forEach { c -> c.members.forEach { out.add(it.id) } }
        return out
    }

    private fun snapshotFromDisk(id: UUID): ClosureSnapshotPlain {
        val meta = closures[id] ?: throw PouchRejected(ERR_NOT_FOUND)
        val members = meta.members.map { m ->
            val plain = try {
                open(m.id)
            } catch (e: NoSuchElementException) {
                throw PouchRejected(ERR_INCOMPLETE)
            } catch (e: SecurityException) {
                throw PouchRejected(e.message ?: ERR_FORBIDDEN)
            }
            ClosureMemberPlain(
                id = m.id, kind = m.kind, level = m.level, name = m.name, status = m.status,
                revision = m.revision, content = plain, digest = m.digest, deps = m.deps, ownerId = m.ownerId, code = m.code,
            )
        }
        return ClosureSnapshotPlain(
            kind = meta.kind, assetId = meta.assetId, revision = meta.revision, level = meta.level,
            status = meta.status, digest = meta.digest, targetClientId = meta.targetClientId, members = members,
        )
    }

    private fun metaFromSnap(snap: ClosureSnapshotPlain) = CachedClosure(
        assetId = snap.assetId,
        revision = snap.revision,
        kind = snap.kind,
        level = snap.level,
        status = snap.status,
        name = snap.members.firstOrNull()?.name.orEmpty(),
        digest = snap.digest.copyOf(),
        targetClientId = snap.targetClientId,
        members = snap.members.map {
            CachedMember(
                id = it.id, kind = it.kind, level = it.level, name = it.name, status = it.status,
                revision = it.revision, digest = it.digest.copyOf(), deps = it.deps, ownerId = it.ownerId, code = it.code,
            )
        },
    )

    private fun validateClosure(snap: ClosureSnapshotPlain) {
        if (snap.members.isEmpty()) throw PouchRejected(ERR_INCOMPLETE)
        for (m in snap.members) {
            if (!Digest.match(m.content, m.digest)) throw PouchRejected(ERR_INTEGRITY)
        }
        val parts = snap.members.map { Digest.Member(it.id, it.revision, it.digest, it.content) }
        if (!MessageDigest.isEqual(Digest.closureSum(parts), snap.digest)) throw PouchRejected(ERR_INTEGRITY)
        val root = snap.members.first()
        if (snap.assetId != root.id || snap.revision != root.revision || snap.kind != root.kind) {
            throw PouchRejected(ERR_MISMATCH)
        }
        if (snap.kind == KIND_PROCESS) {
            if (snap.members.size != 1) throw PouchRejected(ERR_MISMATCH)
            return
        }
        if (snap.kind != KIND_PROJECT || root.kind != KIND_PROJECT) throw PouchRejected(ERR_MISMATCH)
        val expect = 1 + root.deps.size
        if (snap.members.size != expect) {
            if (snap.members.size < expect) throw PouchRejected(ERR_INCOMPLETE)
            throw PouchRejected(ERR_MISMATCH)
        }
        root.deps.forEachIndexed { i, d ->
            val m = snap.members[i + 1]
            if (m.id != d.id || m.revision != d.revision) throw PouchRejected(ERR_MISMATCH)
            if (!MessageDigest.isEqual(m.digest, d.digest)) throw PouchRejected(ERR_MISMATCH)
        }
    }

    companion object {
        const val LEVEL_FACTORY = "factory"
        const val LEVEL_PLATFORM = "platform"
        const val LEVEL_PERSONAL = "personal"
        const val KIND_PROCESS = "process"
        const val KIND_PROJECT = "project"
        const val SCOPE_ALL = "all"
        const val SCOPE_CURRENT = "current"
        const val STATUS_AVAILABLE = "available"
        const val ERR_UNAUTHORIZED = "unauthorized"
        const val ERR_FORBIDDEN = "forbidden"
        const val ERR_NOT_FOUND = "not found"
        const val ERR_CACHE_FULL = "client cache is full"
        const val ERR_INCOMPLETE = "closure is incomplete"
        const val ERR_MISMATCH = "closure revision mismatch"
        const val ERR_INTEGRITY = "asset integrity check failed"
        const val ERR_NOT_AVAILABLE = "asset is not available"
        const val ERR_CODE_MISSING = AssetCode.ERR_MISSING
        const val ERR_CODE_CONFLICT = AssetCode.ERR_CONFLICT
        const val ERR_CODE_EXHAUSTED = AssetCode.ERR_EXHAUSTED
        const val KIND_POINT_CLOUD = "point_cloud"
        const val KIND_IMAGE = "image"
        const val ROLE_OPERATOR = "operator"
        const val ROLE_PROCESS_ENGINEER = "process_engineer"

        fun uuidBytes(id: UUID): ByteArray {
            val buf = ByteArray(16)
            val hi = id.mostSignificantBits
            val lo = id.leastSignificantBits
            for (i in 0 until 8) buf[i] = (hi ushr (56 - 8 * i)).toByte()
            for (i in 0 until 8) buf[8 + i] = (lo ushr (56 - 8 * i)).toByte()
            return buf
        }
    }
}
