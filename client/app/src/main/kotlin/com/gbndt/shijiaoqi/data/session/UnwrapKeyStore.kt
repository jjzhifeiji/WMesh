package com.gbndt.shijiaoqi.data.session

import java.io.File
import java.nio.ByteBuffer
import java.util.UUID

/** 解封钥落独立二进制文件；退出或登录到期必删。 */
interface UnwrapKeyStore {
    /** 允许落盘时写入登录人袋钥。 */
    fun save(pouchKey: ByteArray, devices: List<PadDevice>)
    /** 读袋钥；没有或损坏则空。 */
    fun loadPouchKey(): ByteArray?
    /** 丢掉钥文件，内存不留。 */
    fun clear()
}

/** 测试用内存钥，不落文件。 */
class MemoryUnwrapKeyStore : UnwrapKeyStore {
    private var pouchKey: ByteArray? = null

    override fun save(pouchKey: ByteArray, devices: List<PadDevice>) {
        this.pouchKey = pouchKey.copyOf()
    }

    override fun loadPouchKey(): ByteArray? = pouchKey?.copyOf()

    override fun clear() {
        pouchKey?.fill(0)
        pouchKey = null
    }
}

/** 本机私有目录里的一份二进制钥文件，不进 SQLite。 */
class FileUnwrapKeyStore(private val file: File) : UnwrapKeyStore {
    override fun save(pouchKey: ByteArray, devices: List<PadDevice>) {
        require(pouchKey.size == 32)
        val keyed = devices.mapNotNull { d ->
            if (d.unwrapKey.size != 32) return@mapNotNull null
            val id = runCatching { UUID.fromString(d.id) }.getOrNull() ?: return@mapNotNull null
            id to d.unwrapKey
        }
        val buf = ByteBuffer.allocate(HEADER + 32 + 2 + keyed.size * DEVICE)
        buf.put(MAGIC)
        buf.put(VERSION)
        buf.put(pouchKey)
        buf.putShort(keyed.size.toShort())
        for ((id, key) in keyed) {
            buf.putLong(id.mostSignificantBits)
            buf.putLong(id.leastSignificantBits)
            buf.put(key)
        }
        val packed = buf.array().copyOf(buf.position())
        val tmp = File(file.path + ".tmp")
        tmp.writeBytes(packed)
        if (!tmp.renameTo(file)) {
            file.delete()
            tmp.renameTo(file)
        }
    }

    override fun loadPouchKey(): ByteArray? {
        if (!file.isFile || file.length() < HEADER + 32L) return null
        val raw = file.readBytes()
        if (raw.size < HEADER + 32) return null
        if (!raw.copyOfRange(0, 4).contentEquals(MAGIC)) return null
        if (raw[4] != VERSION) return null
        return raw.copyOfRange(HEADER, HEADER + 32)
    }

    override fun clear() {
        file.delete()
        File(file.path + ".tmp").delete()
    }

    private companion object {
        val MAGIC = byteArrayOf('W'.code.toByte(), 'M'.code.toByte(), 'U'.code.toByte(), 'K'.code.toByte()) // 文件头，防误读
        const val VERSION: Byte = 1 // 格式版本
        const val HEADER = 5
        const val DEVICE = 48
    }
}
