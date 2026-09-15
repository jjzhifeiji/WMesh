package com.gbndt.shijiaoqi.platform.session

import android.content.Context
import androidx.room.Dao
import androidx.room.Database
import androidx.room.Entity
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.PrimaryKey
import androidx.room.Query
import androidx.room.Room
import androidx.room.RoomDatabase
import com.gbndt.shijiaoqi.platform.pouch.CachedClosure
import com.gbndt.shijiaoqi.platform.pouch.CachedEnvelope
import com.gbndt.shijiaoqi.platform.pouch.ClosureCodec
import net.zetetic.database.sqlcipher.SupportOpenHelperFactory
import java.util.UUID

@Entity(tableName = "envelopes")
data class EnvelopeRow(
    @PrimaryKey val id: String,
    val level: String,
    val ownerId: String?,
    val name: String,
    val revision: Long,
    val blob: ByteArray,
)

@Entity(tableName = "kv")
data class KvRow(
    @PrimaryKey val k: String,
    val v: String,
)

@Dao
interface PouchDao {
    @Query("SELECT * FROM envelopes")
    fun allEnvelopes(): List<EnvelopeRow>

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    fun upsertEnvelope(row: EnvelopeRow)

    @Query("DELETE FROM envelopes WHERE id = :id")
    fun deleteEnvelope(id: String)

    @Query("SELECT * FROM kv WHERE k = :k")
    fun getKv(k: String): KvRow?

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    fun putKv(row: KvRow)
}

@Database(entities = [EnvelopeRow::class, KvRow::class], version = 1, exportSchema = false)
abstract class PouchDatabase : RoomDatabase() {
    abstract fun dao(): PouchDao
}

/** SQLCipher 整库加密；口令是登录领到的解封钥，关闭后盘上只剩信封。 */
class RoomEnvelopeStore(private val context: Context) : EnvelopeStore {
    private var db: PouchDatabase? = null

    override fun open(passphrase: ByteArray) {
        close()
        System.loadLibrary("sqlcipher")
        val factory = SupportOpenHelperFactory(passphrase.copyOf())
        db = Room.databaseBuilder(context.applicationContext, PouchDatabase::class.java, "wmesh-pouch.db")
            .openHelperFactory(factory)
            .fallbackToDestructiveMigration()
            .build()
    }

    override fun close() {
        db?.close()
        db = null
    }

    override fun loadEnvelopes(): List<CachedEnvelope> {
        return dao().allEnvelopes().map {
            CachedEnvelope(
                id = UUID.fromString(it.id),
                level = it.level,
                ownerId = it.ownerId?.let(UUID::fromString),
                name = it.name,
                revision = it.revision,
                blob = it.blob,
            )
        }
    }

    override fun upsert(env: CachedEnvelope) {
        dao().upsertEnvelope(
            EnvelopeRow(
                id = env.id.toString(),
                level = env.level,
                ownerId = env.ownerId?.toString(),
                name = env.name,
                revision = env.revision,
                blob = env.blob.copyOf(),
            ),
        )
    }

    override fun delete(id: UUID) {
        dao().deleteEnvelope(id.toString())
    }

    override fun savePerson(meta: PersonMeta) {
        dao().putKv(KvRow("person", "${meta.id}|${meta.loginName}|${meta.displayName}"))
    }

    override fun loadPerson(): PersonMeta? {
        val raw = dao().getKv("person")?.v ?: return null
        val p = raw.split('|')
        if (p.size < 3) return null
        return PersonMeta(UUID.fromString(p[0]), p[1], p[2])
    }

    override fun savePolicy(meta: PolicyMeta) {
        dao().putKv(KvRow("policy", "${meta.revision}|${meta.maxCachedProjects}|${meta.cacheScope}|${meta.persistUnwrapKey}|${meta.keyTtlSeconds}"))
    }

    override fun loadPolicy(): PolicyMeta? {
        val raw = dao().getKv("policy")?.v ?: return null
        val p = raw.split('|')
        if (p.size < 5) return null
        return PolicyMeta(p[0].toLong(), p[1].toInt(), p[2], p[3].toBoolean(), p[4].toLong())
    }

    override fun saveClosures(list: List<CachedClosure>) {
        dao().putKv(KvRow("closures", ClosureCodec.encode(list)))
    }

    override fun loadClosures(): List<CachedClosure> {
        return ClosureCodec.decode(dao().getKv("closures")?.v.orEmpty())
    }

    override fun saveActive(id: UUID?, revision: Long) {
        dao().putKv(KvRow("active", if (id == null) "" else "$id|$revision"))
    }

    override fun loadActive(): Pair<UUID?, Long> {
        return parseActive(dao().getKv("active")?.v.orEmpty())
    }

    private fun dao(): PouchDao = db?.dao() ?: error("unauthorized")
}
