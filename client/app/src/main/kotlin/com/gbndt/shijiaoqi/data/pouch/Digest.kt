package com.gbndt.shijiaoqi.data.pouch

import java.nio.ByteBuffer
import java.nio.ByteOrder
import java.security.MessageDigest
import java.util.UUID

/** SHA-256 与厂端 digest 同形，用于闭包完整性，不是签名。 */
object Digest {
    data class Member(
        val id: UUID,
        val revision: Long,
        val digest: ByteArray,
        val content: ByteArray,
    )

    fun sum(content: ByteArray): ByteArray = MessageDigest.getInstance("SHA-256").digest(content)

    fun match(content: ByteArray, digest: ByteArray): Boolean =
        MessageDigest.isEqual(sum(content), digest)

    fun closureSum(members: List<Member>): ByteArray {
        val md = MessageDigest.getInstance("SHA-256")
        val rev = ByteArray(8)
        for (m in members) {
            md.update(Pouch.uuidBytes(m.id))
            ByteBuffer.wrap(rev).order(ByteOrder.BIG_ENDIAN).putLong(m.revision)
            md.update(rev)
            md.update(m.digest)
            md.update(m.content)
        }
        return md.digest()
    }
}
