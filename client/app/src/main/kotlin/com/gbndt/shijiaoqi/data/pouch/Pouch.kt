package com.gbndt.shijiaoqi.data.pouch

import com.gbndt.shijiaoqi.data.crypt.Wm2
import java.security.MessageDigest
import java.util.UUID

/** 袋内信封：磁盘只有密文，解开只发生在内存。 */
data class CachedEnvelope(
    val id: UUID, // 资产稳定身份
    val level: String, // factory / personal / platform
    val ownerId: UUID?, // 个人级创建人；其余为空
    val name: String, // 显示名，不当身份
    val revision: Long, // 当前修订
    val blob: ByteArray, // 信封密文，工艺明文永不进这列
)

/** 厂端下发闭包里的一条成员，内容仍是密文。 */
data class TransitMember(
    val id: UUID, // 稳定身份
    val level: String, // platform / factory / personal
    val name: String, // 显示名
    val revision: Long, // 钉死修订
    val ownerId: UUID?, // 个人级创建人；其余为空
    val content: ByteArray, // 过路密文，不是工艺明文
    val kind: String = "", // process / project
    val status: String = "", // 送达时状态
    val digest: ByteArray = ByteArray(0), // 内容 SHA-256
    val deps: List<AssetDep> = emptyList(), // 工艺必须空
    val code: String = "", // 只读编号，跟身份走
    val copyable: Boolean = true, // 可否另存；否即保密
)

/** 厂端过站包；工程明文 Wrap 为空，工艺成员仍是过站信封。 */
data class TransitClosure(
    val wrap: ByteArray, // 过站 DEK；工程明文为空
    val members: List<TransitMember>, // 工程仅根；工艺单独成包
    val assetId: UUID, // 根身份
    val revision: Long, // 根修订
    val kind: String = "project", // process / project
    val level: String = "", // 与源相同
    val status: String = "", // 与源相同
    val digest: ByteArray = ByteArray(0), // 本包摘要
    val targetClientId: UUID? = null, // 历史字段，工程不再发给某台设备
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
    private val projectBodies = LinkedHashMap<UUID, ByteArray>()
    private var clientId: UUID? = null
    private var welding = false
    private var activeId: UUID? = null
    private var activeRev = 0L
    private var origin: String? = null
    private val nextN = mutableMapOf(KIND_PROCESS to 1L, KIND_PROJECT to 1L)
    private val codes = LinkedHashMap<UUID, String>()
    private val byCode = LinkedHashMap<String, UUID>()
    private val facts = ArrayList<PendingFact>()
    private val uploads = ArrayList<HeldUpload>()
    private var roles: Set<String> = emptySet()
    private val copyableById = LinkedHashMap<UUID, Boolean>()
    private val dirty = LinkedHashSet<UUID>()
    private val deleted = LinkedHashSet<UUID>()
    private val synced = LinkedHashMap<UUID, ByteArray>()

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
        clientId = null
        origin = null
    }

    /** 退出登录：内存钥、信封、闭包一起丢掉。 */
    fun wipeContents() {
        persist = false
        logout()
        Wm2.zero(material)
        material = null
        persistPerson = null
        items.clear()
        closures.clear()
        projectBodies.values.forEach { Wm2.zero(it) }
        projectBodies.clear()
        facts.clear()
        uploads.clear()
        codes.clear()
        byCode.clear()
        copyableById.clear()
        dirty.clear()
        deleted.clear()
        synced.clear()
        origin = null
        nextN[KIND_PROCESS] = 1L
        nextN[KIND_PROJECT] = 1L
    }

    fun hasUnwrapKey(): Boolean = unwrap?.size == Wm2.KEY_SIZE

    fun bindClient(id: UUID) {
        clientId = id
    }

    fun clearClient() {
        clientId = null
    }

    fun boundClient(): UUID? = clientId

    fun personId(): UUID? = person

    fun setWelding(value: Boolean) {
        welding = value
    }

    fun hasClosure(id: UUID): Boolean = closures.containsKey(id)

    fun activeProject(): UUID? = activeId

    fun activeRevision(): Long = activeRev

    /** 导出已缓存工程元数据，不含正文。 */
    fun exportClosures(): List<CachedClosure> = closures.values.map { it.copy(members = it.members.toList()) }

    /** 当前工程引用的工艺；明文工程只带 Id，列表不解开正文。 */
    fun listProcessMembers(): List<CachedMember> {
        val aid = activeId ?: return emptyList()
        val cl = closures[aid] ?: return emptyList()
        val packed = cl.members.filter { it.kind == KIND_PROCESS }
        if (packed.isNotEmpty()) return packed
        val root = cl.members.firstOrNull() ?: return emptyList()
        return root.deps.map { d ->
            val env = items[d.id]
            CachedMember(
                id = d.id,
                kind = KIND_PROCESS,
                level = env?.level.orEmpty(),
                name = env?.name.orEmpty(),
                status = "",
                revision = d.revision,
                digest = d.digest.copyOf(),
                ownerId = env?.ownerId,
                copyable = copyableOf(d.id, env?.level.orEmpty()),
            )
        }
    }

    /** 袋内全部工艺信封，与厂端工艺列表对齐，不解开正文。 */
    fun listCachedProcesses(): List<CachedMember> {
        val projectIds = closures.keys
        val who = person
        return items.values.filter { it.id !in projectIds }.mapNotNull {
            if (it.level == LEVEL_PERSONAL && it.ownerId != who) return@mapNotNull null
            CachedMember(
                id = it.id,
                kind = KIND_PROCESS,
                level = it.level,
                name = it.name,
                status = "",
                revision = it.revision,
                digest = ByteArray(0),
                ownerId = it.ownerId,
                copyable = copyableOf(it.id, it.level),
            )
        }
    }

    /** 从库恢复工程元数据；编号跟人走。 */
    fun restoreClosures(list: List<CachedClosure>) {
        closures.clear()
        list.forEach { closures[it.assetId] = it }
        list.forEach { c ->
            c.members.forEach { m ->
                if (m.code.isNotBlank()) bindCode(m.id, m.code)
                rememberCopyable(m.id, m.copyable)
            }
        }
    }

    /** 从库恢复工程明文；工艺仍走信封。 */
    fun restoreProjectBodies(list: List<CachedEnvelope>) {
        projectBodies.values.forEach { Wm2.zero(it) }
        projectBodies.clear()
        list.forEach { projectBodies[it.id] = it.blob.copyOf() }
    }

    /** 工程明文副本，供落盘；不是工艺信封。 */
    fun exportProjectPlains(): List<CachedEnvelope> =
        closures.values.filter { it.kind == KIND_PROJECT }.mapNotNull { c ->
            val body = projectBodies[c.assetId] ?: return@mapNotNull null
            val owner = c.members.firstOrNull()?.ownerId
            CachedEnvelope(c.assetId, c.level, owner, c.name, c.revision, body.copyOf())
        }

    /** 恢复激活工程；库里没有这份则清空。 */
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
        val aad = Wm2.assetAad(uuidBytes(id), revision, "pouch")
        val blob = Wm2.seal(key, plain, aad)
        items[id] = CachedEnvelope(id, level, ownerId, name, revision, blob)
    }

    fun putPlainIfNewer(id: UUID, level: String, name: String, revision: Long, ownerId: UUID?, plain: ByteArray): Boolean {
        if (isDirty(id)) return false
        val held = items[id]
        if (held != null && held.revision >= revision) return false
        putPlain(id, level, name, revision, ownerId, plain)
        markSynced(id, Digest.sum(plain))
        return true
    }

    fun rewrite(id: UUID, plain: ByteArray) {
        items[id]?.let {
            putPlain(id, it.level, it.name, it.revision, it.ownerId, plain)
            markDirty(id)
            return
        }
        closures[id]?.let { c ->
            val m = c.members.first()
            putProject(
                ClosureMemberPlain(
                    id = m.id, kind = m.kind, level = m.level, name = m.name,
                    status = m.status, revision = m.revision, content = plain,
                    digest = Digest.sum(plain), deps = m.deps, ownerId = m.ownerId, code = m.code,
                    copyable = m.copyable,
                ),
            )
            markDirty(id)
            return
        }
        throw PouchRejected(ERR_NOT_FOUND)
    }

    fun revisionOf(id: UUID): Long = closures[id]?.revision ?: items[id]?.revision ?: 0L

    /** 工程明文进袋；只引用工艺 Id，不封信封。 */
    fun putProject(m: ClosureMemberPlain) {
        if (!loggedIn()) throw PouchRejected(ERR_UNAUTHORIZED)
        if (m.kind != KIND_PROJECT) throw PouchRejected(ERR_FORBIDDEN)
        val held = closures[m.id]
        if (held != null && held.revision > m.revision) return
        items.remove(m.id)
        Wm2.zero(projectBodies.remove(m.id))
        projectBodies[m.id] = m.content.copyOf()
        val contentDigest = if (m.digest.isNotEmpty()) m.digest.copyOf() else Digest.sum(m.content)
        val packDigest = Digest.closureSum(listOf(Digest.Member(m.id, m.revision, contentDigest, m.content)))
        closures[m.id] = CachedClosure(
            assetId = m.id,
            revision = m.revision,
            kind = KIND_PROJECT,
            level = m.level,
            status = m.status.ifBlank { STATUS_AVAILABLE },
            name = m.name,
            digest = packDigest,
            targetClientId = null,
            members = listOf(
                CachedMember(
                    id = m.id, kind = KIND_PROJECT, level = m.level, name = m.name,
                    status = m.status.ifBlank { STATUS_AVAILABLE }, revision = m.revision,
                    digest = contentDigest,
                    deps = m.deps, ownerId = m.ownerId, code = m.code,
                    copyable = m.copyable,
                ),
            ),
        )
        if (m.code.isNotBlank()) bindCode(m.id, m.code)
        rememberCopyable(m.id, m.copyable)
    }

    fun ingestTransit(factoryId: UUID, personId: UUID, wrap: ByteArray, members: List<TransitMember>): Int {
        val key = unwrap ?: throw SecurityException("unauthorized")
        val fid = uuidBytes(factoryId)
        val pid = uuidBytes(personId)
        val dek = Wm2.open(key, wrap, Wm2.clientTransitDekAad(fid, pid))
        try {
            var n = 0
            for (m in members) {
                val plain = Wm2.open(dek, m.content, Wm2.clientTransitAad(fid, pid, uuidBytes(m.id), m.revision))
                try {
                    if (m.id in deleted || isDirty(m.id)) continue
                    rememberCopyable(m.id, m.copyable)
                    if (putPlainIfNewer(m.id, m.level, m.name, m.revision, m.ownerId, plain)) {
                        n++
                    }
                } finally {
                    Wm2.zero(plain)
                }
            }
            return n
        } finally {
            Wm2.zero(dek)
        }
    }

    /** 解开厂端过路包后写入本机袋；工程明文不封，工艺密文用时再解。 */
    fun cacheTransit(factoryId: UUID, personId: UUID, t: TransitClosure, wrapKey: ByteArray? = null) {
        if (t.assetId in deleted) return
        val plains = ArrayList<ClosureMemberPlain>(t.members.size)
        try {
            if (t.wrap.isEmpty()) {
                for (m in t.members) {
                    val plain = m.content.copyOf()
                    plains.add(
                        ClosureMemberPlain(
                            id = m.id,
                            kind = m.kind.ifBlank { if (m.id == t.assetId) KIND_PROJECT else KIND_PROCESS },
                            level = m.level,
                            name = m.name,
                            status = m.status.ifBlank { STATUS_AVAILABLE },
                            revision = m.revision,
                            content = plain,
                            digest = if (m.digest.isNotEmpty()) m.digest else Digest.sum(plain),
                            deps = m.deps,
                            ownerId = m.ownerId,
                            code = m.code,
                            copyable = m.copyable,
                        ),
                    )
                }
            } else {
                val key = wrapKey ?: unwrap ?: throw PouchRejected(ERR_UNAUTHORIZED)
                val fid = uuidBytes(factoryId)
                val pid = uuidBytes(personId)
                val dek = Wm2.open(key, t.wrap, Wm2.clientTransitDekAad(fid, pid))
                try {
                    for (m in t.members) {
                        val plain = Wm2.open(dek, m.content, Wm2.clientTransitAad(fid, pid, uuidBytes(m.id), m.revision))
                        plains.add(
                            ClosureMemberPlain(
                                id = m.id,
                                kind = m.kind.ifBlank { if (m.id == t.assetId) KIND_PROJECT else KIND_PROCESS },
                                level = m.level,
                                name = m.name,
                                status = m.status.ifBlank { STATUS_AVAILABLE },
                                revision = m.revision,
                                content = plain,
                                digest = if (m.digest.isNotEmpty()) m.digest else Digest.sum(plain),
                                deps = m.deps,
                                ownerId = m.ownerId,
                                code = m.code,
                                copyable = m.copyable,
                            ),
                        )
                    }
                } finally {
                    Wm2.zero(dek)
                }
            }
            val packKind = t.kind.ifBlank { plains.firstOrNull()?.kind.orEmpty() }
            if (packKind == KIND_PROCESS) {
                for (m in plains) {
                    if (m.id !in deleted) applyRemotePlain(m)
                }
                return
            }
            val root = plains.firstOrNull { it.id == t.assetId } ?: plains.firstOrNull() ?: throw PouchRejected(ERR_INCOMPLETE)
            if (root.id !in deleted && !isDirty(root.id)) {
                putProject(root)
                markSynced(root.id, root.digest)
            }
            for (m in plains) {
                if (m.kind == KIND_PROCESS && m.id !in deleted) applyRemotePlain(m)
            }
        } finally {
            plains.forEach { Wm2.zero(it.content) }
        }
    }

    /** 缓存一份工程；明文根即可，不要求发给哪台设备。 */
    fun cacheClosure(snap: ClosureSnapshotPlain) {
        if (!loggedIn()) throw PouchRejected(ERR_UNAUTHORIZED)
        validateClosure(snap)
        if (snap.kind != KIND_PROJECT) throw PouchRejected(ERR_FORBIDDEN)
        val held = closures[snap.assetId]
        if (held != null && held.revision >= snap.revision) return
        if (isDirty(snap.assetId)) return
        bindMemberCodes(snap.members)
        val old = held?.members.orEmpty()
        for (m in snap.members) {
            if (m.kind == KIND_PROJECT) {
                putProject(m)
            } else if (!isDirty(m.id)) {
                putPlain(m.id, m.level, m.name, m.revision, m.ownerId, m.content)
                rememberCopyable(m.id, m.copyable)
            }
        }
        closures[snap.assetId] = metaFromSnap(snap.copy(targetClientId = null))
        dropUnreferenced(old)
    }

    /** 激活恰好一份工程；焊接中不允许换。 */
    fun activate(projectId: UUID) {
        if (!loggedIn()) throw PouchRejected(ERR_UNAUTHORIZED)
        if (!closures.containsKey(projectId)) {
            throw PouchRejected(ERR_NOT_FOUND)
        }
        if (welding && activeId != null && activeId != projectId) throw PouchRejected(ERR_FORBIDDEN)
        val snap = snapshotFromDisk(projectId)
        validateClosure(snap)
        val root = snap.members.first()
        if (root.level == LEVEL_PERSONAL && root.ownerId != person) throw PouchRejected(ERR_FORBIDDEN)
        if (root.status != STATUS_AVAILABLE) throw PouchRejected(ERR_NOT_AVAILABLE)
        activeId = snap.assetId
        activeRev = snap.revision
    }

    fun openProcess(processId: UUID): ByteArray {
        if (!loggedIn()) throw PouchRejected(ERR_UNAUTHORIZED)
        val aid = activeId ?: throw PouchRejected(ERR_NOT_FOUND)
        val cl = closures[aid] ?: throw PouchRejected(ERR_NOT_FOUND)
        val packed = cl.members.firstOrNull { it.id == processId }
        if (packed != null) {
            if (packed.kind != KIND_PROCESS) throw PouchRejected(ERR_NOT_FOUND)
            return open(processId)
        }
        val root = cl.members.firstOrNull() ?: throw PouchRejected(ERR_NOT_FOUND)
        if (root.deps.none { it.id == processId }) throw PouchRejected(ERR_NOT_FOUND)
        return open(processId)
    }

    fun uncache(projectId: UUID) {
        if (!loggedIn()) throw PouchRejected(ERR_UNAUTHORIZED)
        if (welding) throw PouchRejected(ERR_FORBIDDEN)
        if (activeId == projectId) throw PouchRejected(ERR_FORBIDDEN)
        val old = closures.remove(projectId) ?: throw PouchRejected(ERR_NOT_FOUND)
        dropUnreferenced(old.members)
    }

    /** 只删本机个人级；厂级/平台级原件不动。 */
    fun dropPersonal(id: UUID) {
        if (!loggedIn()) throw PouchRejected(ERR_UNAUTHORIZED)
        if (welding) throw PouchRejected(ERR_FORBIDDEN)
        closures[id]?.let { cl ->
            if (cl.level != LEVEL_PERSONAL) throw PouchRejected(ERR_FORBIDDEN)
            val owner = cl.members.firstOrNull()?.ownerId
            if (owner != person) throw PouchRejected(ERR_FORBIDDEN)
            uncache(id)
            rememberDeleted(id)
            forgetAsset(id)
            return
        }
        val env = items[id] ?: throw PouchRejected(ERR_NOT_FOUND)
        if (env.level != LEVEL_PERSONAL || env.ownerId != person) throw PouchRejected(ERR_FORBIDDEN)
        unpinPersonal(id)
        items.remove(id)
        rememberDeleted(id)
        forgetAsset(id)
    }

    fun open(id: UUID): ByteArray {
        if (!loggedIn()) throw SecurityException("unauthorized")
        val who = person ?: throw SecurityException("unauthorized")
        projectBodies[id]?.let { body ->
            val meta = closures[id]?.members?.firstOrNull()
            if (meta?.level == LEVEL_PERSONAL && meta.ownerId != who) throw SecurityException("forbidden")
            return body.copyOf()
        }
        val key = unwrap ?: throw SecurityException("unauthorized")
        val env = items[id] ?: throw NoSuchElementException("not found")
        if (env.level == LEVEL_PERSONAL && env.ownerId != who) throw SecurityException("forbidden")
        return Wm2.open(key, env.blob, Wm2.assetAad(uuidBytes(env.id), env.revision, "pouch"))
    }

    fun diskSnapshot(): List<CachedEnvelope> = items.values.map {
        it.copy(blob = it.blob.copyOf())
    }

    /** 袋内信封元数据；blob 仍是密文。 */
    fun envelope(id: UUID): CachedEnvelope? = items[id]

    /** 内存已有明文或信封，不必再读库。 */
    fun held(id: UUID): Boolean = projectBodies.containsKey(id) || items.containsKey(id)

    fun restoreEnvelopes(list: List<CachedEnvelope>) {
        items.clear()
        list.forEach { rememberCipher(it) }
    }

    /** 把库里的信封密文放进内存，解开时再查。 */
    fun rememberCipher(env: CachedEnvelope) {
        items[env.id] = env.copy(blob = env.blob.copyOf())
    }

    fun setOrigin(code: String) {
        val t = code.trim()
        if (!AssetCode.validClientOrigin(t)) throw PouchRejected(ERR_CODE_MISSING)
        origin = t
        stampPendingCodes()
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
        val id = UUID.randomUUID()
        val code = takeNextCode(kind)
        if (kind == KIND_PROJECT) {
            putProject(
                ClosureMemberPlain(
                    id = id, kind = KIND_PROJECT, level = LEVEL_PERSONAL, name = name,
                    status = STATUS_AVAILABLE, revision = 1, content = content,
                    digest = Digest.sum(content), ownerId = person, code = code, copyable = true,
                ),
            )
        } else {
            putPlain(id, LEVEL_PERSONAL, name, 1, person, content)
            if (code.isNotBlank()) bindCode(id, code)
        }
        rememberCopyable(id, true)
        markDirty(id)
        return IssuedAsset(id, kind, code, LEVEL_PERSONAL)
    }

    /** 还没连臂就先建，短号空着；连上再发编号。 */
    private fun takeNextCode(kind: String): String {
        val short = origin ?: return ""
        val n = nextN[kind] ?: 1L
        val code = AssetCode.format(kind, short, n)
        nextN[kind] = n + 1
        return code
    }

    /** 连臂拿到短号后，给尚未编号的个人级补号。 */
    private fun stampPendingCodes() {
        val short = origin ?: return
        stampPending(KIND_PROCESS, short)
        stampPending(KIND_PROJECT, short)
    }

    private fun stampPending(kind: String, short: String) {
        for (id in uncodedPersonal(kind)) {
            val n = nextN[kind] ?: 1L
            val code = AssetCode.format(kind, short, n)
            nextN[kind] = n + 1
            bindCode(id, code)
            closures[id]?.let { c ->
                closures[id] = c.copy(members = c.members.map { m ->
                    if (m.id == id) m.copy(code = code) else m
                })
            }
        }
    }

    private fun uncodedPersonal(kind: String): List<UUID> {
        val ids = when (kind) {
            KIND_PROCESS -> items.keys.filter { kindOf(it) == KIND_PROCESS && levelOf(it) == LEVEL_PERSONAL }
            KIND_PROJECT -> closures.filter { it.value.level == LEVEL_PERSONAL }.keys
            else -> emptyList()
        }
        return ids.filter { codes[it].isNullOrBlank() }.sortedBy { it.toString() }
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
        dirty = dirty.toList(),
        deleted = deleted.toList(),
        copyable = copyableById.toMap(),
        synced = synced.mapValues { it.value.copyOf() },
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
        dirty.clear()
        dirty.addAll(state.dirty)
        deleted.clear()
        deleted.addAll(state.deleted)
        copyableById.clear()
        copyableById.putAll(state.copyable)
        synced.clear()
        state.synced.forEach { (id, hash) -> synced[id] = hash.copyOf() }
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

    private fun dropUnreferenced(old: List<CachedMember>) {
        val ref = referencedIds()
        val candidates = HashSet<UUID>()
        old.forEach { m ->
            candidates.add(m.id)
            m.deps.forEach { candidates.add(it.id) }
        }
        candidates.forEach { id ->
            if (id !in ref) {
                items.remove(id)
                Wm2.zero(projectBodies.remove(id))
                forgetAsset(id)
            }
        }
    }

    private fun unpinPersonal(processId: UUID) {
        for (pid in closures.keys.toList()) {
            val cl = closures[pid] ?: continue
            if (cl.level != LEVEL_PERSONAL) continue
            val root = cl.members.firstOrNull() ?: continue
            if (root.deps.none { it.id == processId }) continue
            val next = root.deps.filter { it.id != processId }
            closures[pid] = cl.copy(members = listOf(root.copy(deps = next)) + cl.members.drop(1))
            markDirty(pid)
        }
    }

    private fun forgetAsset(id: UUID) {
        codes.remove(id)?.let { byCode.remove(it) }
        dirty.remove(id)
        synced.remove(id)
        copyableById.remove(id)
    }

    private fun referencedIds(): Set<UUID> {
        val out = HashSet<UUID>()
        closures.values.forEach { c ->
            c.members.forEach { m ->
                out.add(m.id)
                m.deps.forEach { out.add(it.id) }
            }
        }
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
                copyable = m.copyable,
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
                copyable = it.copyable,
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
        // 工程明文只带根；工艺用 Id 引用，不要求成员工艺正文。
        if (snap.members.size == 1) return
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

    fun isDirty(id: UUID): Boolean = id in dirty

    fun dirtyIds(): List<UUID> = dirty.toList()

    fun isDeleted(id: UUID): Boolean = id in deleted

    fun deletedIds(): List<UUID> = deleted.toList()

    fun forgetDeleted(id: UUID) {
        deleted.remove(id)
    }

    /** 厂端没有这份个人级时标脏，回连再上传。 */
    fun noteUnsynced(id: UUID) {
        if (id in deleted) return
        if (!items.containsKey(id) && !closures.containsKey(id)) return
        markDirty(id)
    }

    fun copyableOf(id: UUID, level: String = levelOf(id)): Boolean =
        copyableById[id] ?: (level != LEVEL_PLATFORM)

    fun levelOf(id: UUID): String = closures[id]?.level ?: items[id]?.level.orEmpty()

    fun kindOf(id: UUID): String = closures[id]?.kind ?: if (items.containsKey(id)) KIND_PROCESS else ""

    fun nameOf(id: UUID): String = closures[id]?.name ?: items[id]?.name.orEmpty()

    fun depsOf(id: UUID): List<AssetDep> = closures[id]?.members?.firstOrNull()?.deps.orEmpty()

    /** 工程钉上这条工艺的当前修订和摘要。 */
    fun pinProcess(projectId: UUID, processId: UUID) {
        val cl = closures[projectId] ?: return
        val root = cl.members.firstOrNull() ?: return
        val env = items[processId] ?: return
        val digest = try {
            Digest.sum(open(processId))
        } catch (_: Exception) {
            return
        }
        val dep = AssetDep(processId, env.revision, digest)
        val next = ArrayList<AssetDep>(root.deps.size + 1)
        var found = false
        for (d in root.deps) {
            if (d.id == processId) {
                next.add(dep)
                found = true
            } else {
                next.add(d)
            }
        }
        if (!found) next.add(dep)
        closures[projectId] = cl.copy(members = listOf(root.copy(deps = next)) + cl.members.drop(1))
        markDirty(projectId)
    }

    /** 上传成功后对齐厂端修订，并清掉脏标记。 */
    fun acceptRemote(id: UUID, revision: Long, digest: ByteArray) {
        val plain = open(id)
        try {
            items[id]?.let { env ->
                putPlain(id, env.level, env.name, revision, env.ownerId, plain)
                rememberCopyable(id, copyableOf(id, env.level))
                markSynced(id, if (digest.size == 32) digest else Digest.sum(plain))
                return
            }
            closures[id]?.let { c ->
                val m = c.members.first()
                putProject(
                    ClosureMemberPlain(
                        id = m.id, kind = m.kind, level = m.level, name = m.name,
                        status = m.status, revision = revision, content = plain,
                        digest = if (digest.size == 32) digest.copyOf() else Digest.sum(plain),
                        deps = m.deps, ownerId = m.ownerId, code = m.code, copyable = m.copyable,
                    ),
                )
                markSynced(id, if (digest.size == 32) digest else Digest.sum(plain))
            }
        } finally {
            Wm2.zero(plain)
        }
    }

    private fun rememberDeleted(id: UUID) {
        deleted.add(id)
        dirty.remove(id)
    }

    private fun applyRemotePlain(m: ClosureMemberPlain) {
        if (m.id in deleted || isDirty(m.id)) return
        putPlain(m.id, m.level, m.name, m.revision, m.ownerId, m.content)
        rememberCopyable(m.id, m.copyable)
        markSynced(m.id, m.digest)
    }

    private fun rememberCopyable(id: UUID, copyable: Boolean) {
        copyableById[id] = copyable
    }

    private fun markDirty(id: UUID) {
        dirty.add(id)
        synced.remove(id)
    }

    private fun markSynced(id: UUID, digest: ByteArray) {
        dirty.remove(id)
        synced[id] = digest.copyOf()
    }

    companion object {
        const val LEVEL_FACTORY = "factory" // 本厂厂级
        const val LEVEL_PLATFORM = "platform" // 已下发平台级
        const val LEVEL_PERSONAL = "personal" // 本厂个人级
        const val KIND_PROCESS = "process" // 可复用工艺
        const val KIND_PROJECT = "project" // 一次作业工程
        const val STATUS_AVAILABLE = "available" // 可用
        const val ERR_UNAUTHORIZED = "unauthorized" // 未登录或无钥
        const val ERR_FORBIDDEN = "forbidden" // 默认拒绝
        const val ERR_NOT_FOUND = "not found" // 袋里没有这份
        const val ERR_INCOMPLETE = "closure is incomplete" // 工程成员不齐
        const val ERR_MISMATCH = "closure revision mismatch" // 修订或摘要对不上
        const val ERR_INTEGRITY = "asset integrity check failed" // 摘要核验失败
        const val ERR_NOT_AVAILABLE = "asset is not available" // 工程不可用
        const val ERR_NOT_COPYABLE = "asset is not copyable" // 保密工艺不得另存
        const val ERR_CODE_MISSING = AssetCode.ERR_MISSING // 缺本机短号
        const val ERR_CODE_CONFLICT = AssetCode.ERR_CONFLICT // 编号冲突
        const val ERR_CODE_EXHAUSTED = AssetCode.ERR_EXHAUSTED // 序号用尽
        const val KIND_POINT_CLOUD = "point_cloud" // 待发点云
        const val KIND_IMAGE = "image" // 待发图片
        const val ROLE_OPERATOR = "operator" // 操作工
        const val ROLE_PROCESS_ENGINEER = "process_engineer" // 工艺工程师

        /** 把 UUID 收成 16 字节，供信封 AAD。 */
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
