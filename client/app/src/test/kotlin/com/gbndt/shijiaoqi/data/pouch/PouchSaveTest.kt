package com.gbndt.shijiaoqi.data.pouch

import com.gbndt.shijiaoqi.data.session.BagSession
import com.gbndt.shijiaoqi.data.session.FakeFactory
import com.gbndt.shijiaoqi.data.session.MemoryEnvelopeStore
import com.gbndt.shijiaoqi.data.session.MemoryIdentityStore
import com.gbndt.shijiaoqi.data.session.PadDevice
import com.gbndt.shijiaoqi.domain.shared.ProcessJson
import com.gbndt.shijiaoqi.model.WeldProcess
import com.gbndt.shijiaoqi.model.single.WeldPath
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.UUID

/** 焊道保存另存可复制工艺，不改厂级/保密原件。 */
class PouchSaveTest {
    @Test
    fun copyableFactoryForksPersonalSecretKeepsId() {
        val cid = UUID.randomUUID()
        val factory = FakeFactory(devices = listOf(PadDevice(cid.toString(), "焊机", "ARM-1", "C0008")))
        val bag = BagSession(
            { "ARM-1" },
            factory,
            MemoryIdentityStore(),
            MemoryEnvelopeStore(),
        )
        bag.login("http://f", UUID.randomUUID().toString(), "op", "p")

        val factoryId = UUID.randomUUID()
        val factoryBody = ProcessJson.encode(WeldProcess(name = "厂", current = 180.0))
        bag.cachePlain(factoryId, Pouch.LEVEL_FACTORY, "厂", 1, null, factoryBody)

        val path = WeldPath(
            id = "w1",
            name = "道1",
            points = mutableListOf(),
            process = WeldProcess(name = "厂", current = 200.0),
            processId = factoryId.toString(),
        )
        PouchSave.weldPath(bag, path)
        assertNotEquals(factoryId.toString(), path.processId)
        assertArrayEquals(factoryBody, bag.open(factoryId))
        val personal = UUID.fromString(path.processId)
        assertEquals(Pouch.LEVEL_PERSONAL, bag.pouch.levelOf(personal))
        assertTrue(bag.pouch.isDirty(personal))
        assertFalse(bag.pouch.isDirty(factoryId))

        bag.flushDirty()
        assertFalse(bag.pouch.isDirty(personal))
        val uploaded = factory.padAssets[personal]!!
        assertEquals(Pouch.STATUS_AVAILABLE, uploaded.status)
        assertEquals(Pouch.LEVEL_PERSONAL, uploaded.level)
        assertEquals(personal, uploaded.id)

        val secretId = UUID.randomUUID()
        val secret = ProcessJson.encode(WeldProcess(name = "密", current = 180.0))
        bag.pouch.cacheTransit(
            UUID.randomUUID(),
            UUID.randomUUID(),
            TransitClosure(
                wrap = ByteArray(0),
                members = listOf(
                    TransitMember(
                        id = secretId,
                        level = Pouch.LEVEL_PLATFORM,
                        name = "密",
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
        val secretPath = WeldPath(
            id = "w2",
            name = "道2",
            points = mutableListOf(),
            process = WeldProcess(name = "密", current = 1.0),
            processId = secretId.toString(),
        )
        PouchSave.weldPath(bag, secretPath)
        assertEquals(secretId.toString(), secretPath.processId)
        assertArrayEquals(secret, bag.open(secretId))
        val e = assertThrows(PouchRejected::class.java) {
            bag.ensurePersonalProcess(secretId, "密", ProcessJson.encode(WeldProcess(name = "密", current = 1.0)))
        }
        assertEquals(Pouch.ERR_NOT_COPYABLE, e.code)
    }
}
