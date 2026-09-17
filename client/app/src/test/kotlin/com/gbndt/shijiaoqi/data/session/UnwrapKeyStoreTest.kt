package com.gbndt.shijiaoqi.data.session

import com.gbndt.shijiaoqi.data.crypt.Wm2
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Test
import java.io.File
import java.util.UUID

/** 解封钥二进制文件：落盘、读回、退出清掉。 */
class UnwrapKeyStoreTest {
    @Test
    fun fileRoundtripThenClear() {
        val file = File.createTempFile("unwrap", ".bin")
        val store = FileUnwrapKeyStore(file)
        val pouch = Wm2.randomKey()
        val cid = UUID.randomUUID()
        val deviceKey = Wm2.randomKey()
        store.save(pouch, listOf(PadDevice(cid.toString(), "焊机", "ARM-1", "C0008", deviceKey)))
        assertArrayEquals(pouch, store.loadPouchKey())
        store.clear()
        assertNull(store.loadPouchKey())
        assertFalse(file.exists())
    }

    @Test
    fun garbageFileYieldsNoKey() {
        val file = File.createTempFile("unwrap", ".bin")
        file.writeBytes(ByteArray(8) { 1 })
        assertNull(FileUnwrapKeyStore(file).loadPouchKey())
        file.delete()
    }
}
