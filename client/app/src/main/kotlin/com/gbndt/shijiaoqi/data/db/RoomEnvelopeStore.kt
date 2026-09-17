package com.gbndt.shijiaoqi.data.db

import android.content.Context
import androidx.room.Room
import com.gbndt.shijiaoqi.config.AppConfig
import com.gbndt.shijiaoqi.data.pouch.CachedClosure
import com.gbndt.shijiaoqi.data.pouch.CachedEnvelope
import com.gbndt.shijiaoqi.data.pouch.ClosureCodec
import net.zetetic.database.sqlcipher.SupportOpenHelperFactory
import java.io.File
import java.util.UUID
import com.gbndt.shijiaoqi.data.session.EnvelopeStore
import com.gbndt.shijiaoqi.data.session.PadDevice
import com.gbndt.shijiaoqi.data.session.PersonMeta
import com.gbndt.shijiaoqi.data.session.PolicyMeta
import com.gbndt.shijiaoqi.data.session.SessionMeta

/** 本机袋：四张表；口令是解封钥，密钥本身不进库。 */
class RoomEnvelopeStore(private val context: Context) : EnvelopeStore {
    private var db: PouchDatabase? = null

    override fun open(passphrase: ByteArray, encrypt: Boolean) {
        close()
        val file = context.applicationContext.getDatabasePath(AppConfig.POUCH_DB_NAME)
        // 加密和明文文件头不同，对不上就先删，避免 SQLCipher 把旧袋打成「不是数据库」。
        if (file.exists() && pouchLooksPlainSqlite(file) != !encrypt) {
            wipe()
        }
        if (encrypt) System.loadLibrary("sqlcipher")
        try {
            db = connect(passphrase, encrypt)
            dao().getConfig()
            return
        } catch (e: Exception) {
            close()
            if (encrypt && !sqlcipherWrongKey(e)) throw e
            if (!encrypt && !sqliteOpenFailed(e)) throw e
        }
        wipe()
        if (encrypt) System.loadLibrary("sqlcipher")
        db = connect(passphrase, encrypt)
        dao().getConfig()
    }

    override fun wipe() {
        close()
        deleteFiles()
    }

    private fun deleteFiles() {
        val ctx = context.applicationContext
        val file = ctx.getDatabasePath(AppConfig.POUCH_DB_NAME)
        ctx.deleteDatabase(AppConfig.POUCH_DB_NAME)
        file.delete()
        File(file.path + "-wal").delete()
        File(file.path + "-shm").delete()
        File(file.path + "-journal").delete()
    }

    private fun connect(passphrase: ByteArray, encrypt: Boolean): PouchDatabase {
        val builder = Room.databaseBuilder(context.applicationContext, PouchDatabase::class.java, AppConfig.POUCH_DB_NAME)
            .fallbackToDestructiveMigration(true)
        if (encrypt) {
            builder.openHelperFactory(SupportOpenHelperFactory(passphrase.copyOf()))
        }
        return builder.build()
    }

    override fun close() {
        db?.close()
        db = null
    }

    override fun loadEnvelopes(): List<CachedEnvelope> {
        val out = ArrayList<CachedEnvelope>()
        dao().allProcesses().forEach { out.add(it.toCached()) }
        return out
    }

    override fun loadEnvelope(id: UUID): CachedEnvelope? {
        dao().getProcess(id.toString())?.let { return it.toCached() }
        return null
    }

    override fun replaceProcesses(list: List<CachedEnvelope>) {
        dao().replaceProcesses(list.map { it.toRow() })
    }

    override fun upsert(env: CachedEnvelope) {
        dao().upsertProcess(env.toRow())
    }

    override fun delete(id: UUID) {
        val key = id.toString()
        dao().deleteProcess(key)
        dao().deleteProject(key)
    }

    override fun savePerson(meta: PersonMeta, roles: List<String>) {
        patchConfig {
            it.copy(personId = meta.id.toString(), loginName = meta.loginName, displayName = meta.displayName, roles = roles.joinToString(","))
        }
    }

    override fun loadPerson(): PersonMeta? {
        val row = dao().getConfig() ?: return null
        if (row.personId.isBlank()) return null
        return PersonMeta(UUID.fromString(row.personId), row.loginName, row.displayName)
    }

    override fun loadRoles(): List<String> {
        return dao().getConfig()?.roles.orEmpty().split(',').map { it.trim() }.filter { it.isNotEmpty() }
    }

    override fun savePolicy(meta: PolicyMeta) {
        patchConfig {
            it.copy(
                revision = meta.revision,
                persistUnwrapKey = meta.persistUnwrapKey,
                keyTtlSeconds = meta.keyTtlSeconds,
                encryptPouch = meta.encryptPouch,
            )
        }
    }

    override fun loadPolicy(): PolicyMeta? {
        val row = dao().getConfig() ?: return null
        if (row.personId.isBlank() && row.token.isBlank() && row.revision == 0L) return null
        return PolicyMeta(
            row.revision,
            row.persistUnwrapKey,
            row.keyTtlSeconds,
            row.encryptPouch,
        )
    }

    override fun saveDevices(list: List<PadDevice>) {
        dao().replaceDevices(
            list.map { d -> DeviceRow(d.id, d.name, d.deviceSerial, d.shortCode) },
        )
    }

    override fun loadDevices(): List<PadDevice> {
        return dao().allDevices().map { d -> PadDevice(d.id, d.name, d.deviceSerial, d.shortCode) }
    }

    override fun saveSession(token: String, expiresAtMillis: Long, loggedInAtMillis: Long) {
        patchConfig { it.copy(token = token, expiresAtMillis = expiresAtMillis, loggedInAtMillis = loggedInAtMillis) }
    }

    override fun loadSession(): SessionMeta? {
        val row = dao().getConfig() ?: return null
        if (row.token.isBlank()) return null
        return SessionMeta(row.token, row.expiresAtMillis, row.loggedInAtMillis)
    }

    override fun saveClosures(list: List<CachedClosure>, blobs: List<CachedEnvelope>) {
        val byId = blobs.associateBy { it.id }
        dao().replaceProjects(
            list.map { c ->
                val blob = byId[c.assetId]?.blob?.copyOf() ?: ByteArray(0)
                ProjectRow(
                    id = c.assetId.toString(),
                    revision = c.revision,
                    kind = c.kind,
                    level = c.level,
                    status = c.status,
                    name = c.name,
                    digest = c.digest.copyOf(),
                    members = ClosureCodec.encode(listOf(c)),
                    blob = blob,
                )
            },
        )
    }

    override fun loadClosures(): List<CachedClosure> {
        return dao().allProjects().flatMap { ClosureCodec.decode(it.members) }
    }

    override fun loadProjectPlains(): List<CachedEnvelope> {
        return dao().allProjects().map { it.toPlain() }
    }

    override fun saveActive(id: UUID?, revision: Long) {
        patchConfig { it.copy(activeProjectId = id?.toString(), activeRevision = revision) }
    }

    override fun loadActive(): Pair<UUID?, Long> {
        val row = dao().getConfig() ?: return null to 0L
        val id = row.activeProjectId?.let { runCatching { UUID.fromString(it) }.getOrNull() }
        return id to row.activeRevision
    }

    override fun saveLedger(raw: String) {
        patchConfig { it.copy(ledger = raw) }
    }

    override fun loadLedger(): String = dao().getConfig()?.ledger.orEmpty()

    private fun patchConfig(block: (ConfigRow) -> ConfigRow) {
        dao().upsertConfig(block(dao().getConfig() ?: ConfigRow()))
    }

    private fun dao(): PouchDao = db?.dao() ?: error("unauthorized")

    private fun CachedEnvelope.toRow() = ProcessRow(
        id = id.toString(),
        level = level,
        ownerId = ownerId?.toString(),
        name = name,
        revision = revision,
        blob = blob.copyOf(),
    )

    private fun ProcessRow.toCached() = CachedEnvelope(
        id = UUID.fromString(id),
        level = level,
        ownerId = ownerId?.let(UUID::fromString),
        name = name,
        revision = revision,
        blob = blob.copyOf(),
    )

    private fun ProjectRow.toPlain() = CachedEnvelope(
        id = UUID.fromString(id),
        level = level,
        ownerId = null,
        name = name,
        revision = revision,
        blob = blob.copyOf(),
    )
}

/** SQLCipher 口令不对时文件看起来不像库。 */
internal fun sqlcipherWrongKey(e: Throwable): Boolean {
    var cur: Throwable? = e
    var n = 0
    while (cur != null && n++ < 8) {
        val name = cur.javaClass.simpleName
        val msg = cur.message.orEmpty().lowercase()
        if (name.contains("SQLite", ignoreCase = true)) return true
        if (msg.contains("not a database") || msg.contains("hmac") || msg.contains("sqlcipher") || msg.contains("code 26")) return true
        cur = cur.cause
    }
    return false
}

/** 明文 SQLite 打不开时同样整文件丢掉重建。 */
internal fun sqliteOpenFailed(e: Throwable): Boolean = sqlcipherWrongKey(e)

/** SQLite 明文库头是 16 字节 SQLite format 3。 */
internal fun pouchLooksPlainSqlite(file: File): Boolean {
    if (!file.isFile || file.length() < 16) return false
    val hdr = ByteArray(16)
    val n = file.inputStream().use { it.read(hdr) }
    if (n < 16) return false
    return hdr.contentEquals("SQLite format 3\u0000".toByteArray(Charsets.ISO_8859_1))
}
