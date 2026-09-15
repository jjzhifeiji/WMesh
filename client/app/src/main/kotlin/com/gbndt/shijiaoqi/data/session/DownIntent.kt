package com.gbndt.shijiaoqi.data.session

import com.gbndt.shijiaoqi.data.pouch.Pouch
import org.bouncycastle.crypto.params.Ed25519PublicKeyParameters
import org.bouncycastle.crypto.signers.Ed25519Signer
import java.nio.ByteBuffer
import java.nio.ByteOrder
import java.util.UUID

data class DownIntent(
    val typ: String,
    val revision: Long,
    val assetId: String? = null,
    val digest: ByteArray? = null,
    val maxCachedProjects: Int = 0,
    val cacheScope: String = "",
    val persistUnwrapKey: Boolean = false,
    val keyTtlSeconds: Long = 0,
    val sig: ByteArray = ByteArray(0),
)

/** 厂→本机控制面：验签、修订只向前、正文不得出现。 */
object DownIntents {
    const val TYP_POLICY = "policy"
    const val TYP_CLOSURE = "closure"
    const val TYP_ACK = "ack"

    fun shouldApply(held: Long, incoming: Long): Boolean = incoming > held

    fun hasBody(raw: String): Boolean {
        if (raw.contains("\"content\"") || raw.contains("\"members\"") || raw.contains("\"wrap\"")) return true
        return raw.contains("WM2")
    }

    fun verify(pub: ByteArray, factoryId: UUID, clientId: UUID, intent: DownIntent): Boolean {
        if (pub.size != 32 || intent.sig.size != 64) return false
        val params = Ed25519PublicKeyParameters(pub, 0)
        val signer = Ed25519Signer()
        signer.init(false, params)
        val msg = message(factoryId, clientId, intent)
        signer.update(msg, 0, msg.size)
        return signer.verifySignature(intent.sig)
    }

    fun message(factoryId: UUID, clientId: UUID, intent: DownIntent): ByteArray {
        val asset = if (intent.assetId.isNullOrBlank()) ByteArray(16) else Pouch.uuidBytes(UUID.fromString(intent.assetId))
        val digest = intent.digest ?: ByteArray(0)
        val typ = intent.typ.toByteArray()
        val scope = intent.cacheScope.toByteArray()
        val persist: Byte = if (intent.persistUnwrapKey) 1 else 0
        val out = ByteArray(16 + 16 + typ.size + 8 + 16 + 8 + digest.size + 8 + scope.size + 1 + 8)
        var off = 0
        fun put(b: ByteArray) {
            System.arraycopy(b, 0, out, off, b.size)
            off += b.size
        }
        fun putU64(v: Long) {
            val buf = ByteBuffer.allocate(8).order(ByteOrder.BIG_ENDIAN).putLong(v).array()
            put(buf)
        }
        put(Pouch.uuidBytes(factoryId))
        put(Pouch.uuidBytes(clientId))
        put(typ)
        putU64(intent.revision)
        put(asset)
        putU64(digest.size.toLong())
        put(digest)
        putU64(intent.maxCachedProjects.toLong())
        put(scope)
        out[off] = persist
        off++
        putU64(intent.keyTtlSeconds)
        return out
    }

    fun ackJson(assetId: String?, revision: Long): String {
        val id = if (assetId.isNullOrBlank()) "" else ",\"assetId\":\"$assetId\""
        return """{"typ":"$TYP_ACK"$id,"revision":$revision}"""
    }
}
