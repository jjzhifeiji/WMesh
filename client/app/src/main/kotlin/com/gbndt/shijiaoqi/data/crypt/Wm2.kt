package com.gbndt.shijiaoqi.data.crypt

import java.security.MessageDigest
import java.security.SecureRandom
import javax.crypto.Cipher
import javax.crypto.spec.GCMParameterSpec
import javax.crypto.spec.SecretKeySpec

/** WM2 信封：与厂端 contentcrypt 同形，KEK 只包 DEK。 */
object Wm2 {
    const val KEY_SIZE = 32
    private const val MAGIC = "WM2"
    private const val VERSION = 1
    private const val NONCE = 12
    private const val HINT = 8
    private const val WRAP = KEY_SIZE + 16
    private const val MIN = 3 + 1 + HINT + NONCE + WRAP + NONCE + 16

    fun randomKey(): ByteArray {
        val out = ByteArray(KEY_SIZE)
        SecureRandom().nextBytes(out)
        return out
    }

    fun zero(buf: ByteArray?) {
        if (buf == null) return
        buf.fill(0)
    }

    fun isEnvelope(blob: ByteArray): Boolean {
        if (blob.size < MIN) return false
        return blob[0] == 'W'.code.toByte() && blob[1] == 'M'.code.toByte() && blob[2] == '2'.code.toByte() && blob[3] == VERSION.toByte()
    }

    fun seal(kek: ByteArray, plaintext: ByteArray, aad: ByteArray): ByteArray {
        require(kek.size == KEY_SIZE)
        val dek = randomKey()
        try {
            val wrapAad = extraAad(aad, "dek")
            val bodyAad = extraAad(aad, "body")
            val (nW, wrap) = gcmSeal(kek, dek, wrapAad)
            val (nC, body) = gcmSeal(dek, plaintext, bodyAad)
            return MAGIC.toByteArray() + byteArrayOf(VERSION.toByte()) + kekHint(kek) + nW + wrap + nC + body
        } finally {
            zero(dek)
        }
    }

    fun open(kek: ByteArray, blob: ByteArray, aad: ByteArray): ByteArray {
        if (!isEnvelope(blob)) return blob.copyOf()
        require(kek.size == KEY_SIZE && blob.size >= MIN && blob[3] == VERSION.toByte())
        var off = 4 + HINT
        val nW = blob.copyOfRange(off, off + NONCE)
        off += NONCE
        val wrap = blob.copyOfRange(off, off + WRAP)
        off += WRAP
        val nC = blob.copyOfRange(off, off + NONCE)
        off += NONCE
        val body = blob.copyOfRange(off, blob.size)
        val dek = gcmOpen(kek, nW, wrap, extraAad(aad, "dek"))
        try {
            return gcmOpen(dek, nC, body, extraAad(aad, "body"))
        } finally {
            zero(dek)
        }
    }

    fun assetAad(id: ByteArray, rev: Long, table: String): ByteArray {
        return id + revBytes(rev) + table.toByteArray()
    }

    fun clientTransitAad(factoryId: ByteArray, boundId: ByteArray, assetId: ByteArray, rev: Long): ByteArray {
        return factoryId + boundId + assetId + revBytes(rev) + "client-transit".toByteArray()
    }

    fun clientTransitDekAad(factoryId: ByteArray, boundId: ByteArray): ByteArray {
        return factoryId + boundId + "client-transit-dek".toByteArray()
    }

    private fun revBytes(rev: Long): ByteArray {
        val revb = ByteArray(8)
        var v = rev
        for (i in 7 downTo 0) {
            revb[i] = (v and 0xff).toByte()
            v = v ushr 8
        }
        return revb
    }

    private fun kekHint(kek: ByteArray): ByteArray {
        val md = MessageDigest.getInstance("SHA-256")
        return md.digest(kek).copyOf(HINT)
    }

    private fun extraAad(aad: ByteArray, part: String): ByteArray = aad + part.toByteArray()

    private fun gcmSeal(key: ByteArray, plain: ByteArray, aad: ByteArray): Pair<ByteArray, ByteArray> {
        val nonce = ByteArray(NONCE)
        SecureRandom().nextBytes(nonce)
        val c = Cipher.getInstance("AES/GCM/NoPadding")
        c.init(Cipher.ENCRYPT_MODE, SecretKeySpec(key, "AES"), GCMParameterSpec(128, nonce))
        c.updateAAD(aad)
        return nonce to c.doFinal(plain)
    }

    private fun gcmOpen(key: ByteArray, nonce: ByteArray, ct: ByteArray, aad: ByteArray): ByteArray {
        val c = Cipher.getInstance("AES/GCM/NoPadding")
        c.init(Cipher.DECRYPT_MODE, SecretKeySpec(key, "AES"), GCMParameterSpec(128, nonce))
        c.updateAAD(aad)
        return c.doFinal(ct)
    }
}
