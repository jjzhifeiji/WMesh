package com.gbndt.shijiaoqi.data.pouch

import com.gbndt.shijiaoqi.data.crypt.Wm2
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.UUID

class PouchTest {
    @Test
    fun loginRequiredAndDefaultKeyNotPersisted() {
        val p = Pouch()
        val id = UUID.randomUUID()
        val who = UUID.randomUUID()
        val plain = """{"n":1}""".toByteArray()
        assertThrows(SecurityException::class.java) { p.putPlain(id, Pouch.LEVEL_FACTORY, "厂级", 1, null, plain) }
        val key = Wm2.randomKey()
        p.login(key, who, false)
        p.putPlain(id, Pouch.LEVEL_FACTORY, "厂级", 1, null, plain)
        assertArrayEquals(plain, p.open(id))
        p.logout()
        assertFalse(p.hasUnwrapKey())
        assertThrows(SecurityException::class.java) { p.open(id) }
        assertThrows(SecurityException::class.java) { p.relockFromMaterial() }
        p.diskSnapshot().forEach { env ->
            val s = String(env.blob, Charsets.ISO_8859_1)
            assertFalse(s.contains("""{"n":1}"""))
            assertTrue(Wm2.isEnvelope(env.blob))
        }
    }

    @Test
    fun personalFollowsPreviousOwner() {
        val p = Pouch()
        val key = Wm2.randomKey()
        val a = UUID.randomUUID()
        val b = UUID.randomUUID()
        val personal = UUID.randomUUID()
        val factory = UUID.randomUUID()
        p.login(key, a, false)
        p.putPlain(personal, Pouch.LEVEL_PERSONAL, "我的", 1, a, """{"p":1}""".toByteArray())
        p.putPlain(factory, Pouch.LEVEL_FACTORY, "厂级", 1, null, """{"f":1}""".toByteArray())
        p.logout()
        p.login(key, b, false)
        assertThrows(SecurityException::class.java) { p.open(personal) }
        assertArrayEquals("""{"f":1}""".toByteArray(), p.open(factory))
        assertTrue(p.held(factory))
        assertFalse(p.held(UUID.randomUUID()))
    }

    @Test
    fun transitIngestAndStaleRevision() {
        val unwrap = Wm2.randomKey()
        val dek = Wm2.randomKey()
        val who = UUID.randomUUID()
        val factoryId = UUID.randomUUID()
        val asset = UUID.randomUUID()
        val plain = """{"current":180}""".toByteArray()
        val wrap = Wm2.seal(unwrap, dek, Wm2.clientTransitDekAad(Pouch.uuidBytes(factoryId), Pouch.uuidBytes(who)))
        val blob = Wm2.seal(dek, plain, Wm2.clientTransitAad(Pouch.uuidBytes(factoryId), Pouch.uuidBytes(who), Pouch.uuidBytes(asset), 2))
        val p = Pouch()
        p.login(unwrap, who, false)
        val n = p.ingestTransit(
            factoryId,
            who,
            wrap,
            listOf(TransitMember(asset, Pouch.LEVEL_FACTORY, "工艺", 2, null, blob)),
        )
        assertTrue(n == 1)
        assertArrayEquals(plain, p.open(asset))
        assertFalse(p.putPlainIfNewer(asset, Pouch.LEVEL_FACTORY, "工艺", 1, null, """{"current":1}""".toByteArray()))
        assertArrayEquals(plain, p.open(asset))
    }

    @Test
    fun dirtySkipPullAndSecretStaysUncopyable() {
        val p = Pouch()
        p.login(Wm2.randomKey(), UUID.randomUUID(), false)
        val id = UUID.randomUUID()
        p.putPlain(id, Pouch.LEVEL_FACTORY, "厂", 1, null, """{"n":1}""".toByteArray())
        p.rewrite(id, """{"n":2}""".toByteArray())
        assertTrue(p.isDirty(id))
        assertFalse(p.putPlainIfNewer(id, Pouch.LEVEL_FACTORY, "厂", 9, null, """{"n":9}""".toByteArray()))
        assertArrayEquals("""{"n":2}""".toByteArray(), p.open(id))

        val secretId = UUID.randomUUID()
        val secret = """{"current":180}""".toByteArray()
        p.cacheTransit(
            UUID.randomUUID(),
            UUID.randomUUID(),
            TransitClosure(
                wrap = ByteArray(0),
                members = listOf(
                    TransitMember(
                        id = secretId,
                        level = Pouch.LEVEL_FACTORY,
                        name = "保密",
                        revision = 1,
                        ownerId = null,
                        content = secret,
                        kind = Pouch.KIND_PROCESS,
                        copyable = false,
                    ),
                ),
                assetId = secretId,
                revision = 1,
                kind = Pouch.KIND_PROCESS,
            ),
        )
        assertFalse(p.copyableOf(secretId))
        p.rewrite(secretId, """{"current":1}""".toByteArray())
        p.cacheTransit(
            UUID.randomUUID(),
            UUID.randomUUID(),
            TransitClosure(
                wrap = ByteArray(0),
                members = listOf(
                    TransitMember(
                        id = secretId,
                        level = Pouch.LEVEL_FACTORY,
                        name = "保密",
                        revision = 3,
                        ownerId = null,
                        content = """{"current":9}""".toByteArray(),
                        kind = Pouch.KIND_PROCESS,
                        copyable = false,
                    ),
                ),
                assetId = secretId,
                revision = 3,
                kind = Pouch.KIND_PROCESS,
            ),
        )
        assertArrayEquals("""{"current":1}""".toByteArray(), p.open(secretId))
        assertTrue(p.isDirty(secretId))
    }

    @Test
    fun platformCopyableTrueIsKeptOnSameRevisionPull() {
        val p = Pouch()
        p.login(Wm2.randomKey(), UUID.randomUUID(), false)
        val id = UUID.randomUUID()
        val body = """{"name":"平台测试工艺 1","offsetX":0,"current":200}""".toByteArray()
        p.putPlain(id, Pouch.LEVEL_PLATFORM, "平台测试工艺 1", 1, null, body)
        assertFalse(p.copyableOf(id))
        p.cacheTransit(
            UUID.randomUUID(),
            UUID.randomUUID(),
            TransitClosure(
                wrap = ByteArray(0),
                members = listOf(
                    TransitMember(
                        id = id,
                        level = Pouch.LEVEL_PLATFORM,
                        name = "平台测试工艺 1",
                        revision = 1,
                        ownerId = null,
                        content = body,
                        kind = Pouch.KIND_PROCESS,
                        copyable = true,
                    ),
                ),
                assetId = id,
                revision = 1,
                kind = Pouch.KIND_PROCESS,
            ),
        )
        assertTrue(p.copyableOf(id))
        assertTrue(p.listCachedProcesses().single { it.id == id }.copyable)
    }
}
