package com.gbndt.shijiaoqi.data.session

import com.gbndt.shijiaoqi.data.crypt.Wm2
import com.gbndt.shijiaoqi.data.pouch.AssetDep
import com.gbndt.shijiaoqi.data.pouch.CachedEnvelope
import com.gbndt.shijiaoqi.data.pouch.ClosureMemberPlain
import com.gbndt.shijiaoqi.data.pouch.ClosureSnapshotPlain
import com.gbndt.shijiaoqi.data.pouch.Digest
import com.gbndt.shijiaoqi.data.pouch.Pouch
import com.gbndt.shijiaoqi.data.pouch.PouchProcessSource
import com.gbndt.shijiaoqi.data.pouch.PouchRejected
import com.gbndt.shijiaoqi.data.pouch.TransitClosure
import com.gbndt.shijiaoqi.data.pouch.TransitMember
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.UUID
import com.gbndt.shijiaoqi.model.FactoryOffer

/** 登录会话：落库、恢复、时效与退出清钥。 */
class BagSessionTest {
    @Test
    fun loginWithoutSerialThenMatchArm() {
        val key = Wm2.randomKey()
        val person = UUID.randomUUID()
        val cid = UUID.randomUUID()
        val factory = FakeFactory(key, person, devices = listOf(PadDevice(cid.toString(), "焊机", "ARM-1", "C0008", key.copyOf())))
        val bag = BagSession(
            { "" },
            factory,
            MemoryIdentityStore(),
            MemoryEnvelopeStore(),
        )
        bag.login("http://f", UUID.randomUUID().toString(), "op", "p")
        assertTrue(bag.loggedIn)
        assertFalse(bag.armMatched)
        assertThrows(LoginRejected::class.java) { bag.matchArm("") }
        assertThrows(LoginRejected::class.java) { bag.matchArm("NOPE") }
        val issued = bag.issuePersonal(Pouch.KIND_PROJECT, "新", """[]""".toByteArray())
        assertEquals("GC-C0008-000001", issued.code)
        bag.flushDirty()
        assertEquals(issued.id, factory.padAssets[issued.id]?.id)
        bag.matchArm("ARM-1")
        assertTrue(bag.armMatched)
        assertEquals(cid.toString(), bag.savedClientId())
        assertEquals("GC-C0008-000001", bag.pouch.codeOf(issued.id))
        bag.matchArm(" arm-1 ")
        assertTrue(bag.armMatched)
        bag.matchArm("ARM-1-extra")
        assertTrue(bag.armMatched)
    }

    @Test
    fun multiDeviceStampsFirstShortAndUploadsOnLogin() {
        val factory = FakeFactory(
            devices = listOf(
                PadDevice(UUID.randomUUID().toString(), "焊1", "ARM-1", "C0008"),
                PadDevice(UUID.randomUUID().toString(), "焊2", "ARM-2", "C0009"),
            ),
        )
        val bag = BagSession({ "" }, factory, MemoryIdentityStore(), MemoryEnvelopeStore())
        bag.login("http://f", UUID.randomUUID().toString(), "op", "p")
        val issued = bag.issuePersonal(Pouch.KIND_PROCESS, "新工艺", """{"n":1}""".toByteArray())
        assertEquals("GY-C0008-000001", issued.code)
        bag.flushDirty()
        assertEquals(issued.id, factory.padAssets[issued.id]?.id)
    }

    @Test
    fun keepsSavedShortWhenManyDevices() {
        val identity = MemoryIdentityStore()
        identity.clientShortCode = "C0009"
        val factory = FakeFactory(
            devices = listOf(
                PadDevice(UUID.randomUUID().toString(), "焊1", "ARM-1", "C0008"),
                PadDevice(UUID.randomUUID().toString(), "焊2", "ARM-2", "C0009"),
            ),
        )
        val bag = BagSession({ "" }, factory, identity, MemoryEnvelopeStore())
        bag.login("http://f", UUID.randomUUID().toString(), "op", "p")
        val issued = bag.issuePersonal(Pouch.KIND_PROCESS, "新", """{"n":1}""".toByteArray())
        assertEquals("GY-C0009-000001", issued.code)
    }

    @Test
    fun syncPullsFactoryWhenCleanAndClientWinsWhenDirty() {
        val factory = FakeFactory(
            devices = listOf(PadDevice(UUID.randomUUID().toString(), "焊", "ARM-1", "C0008")),
        )
        val bag = BagSession({ "" }, factory, MemoryIdentityStore(), MemoryEnvelopeStore())
        bag.login("http://f", UUID.randomUUID().toString(), "op", "p")

        val cleanId = UUID.randomUUID()
        val factoryBody = """{"n":9}""".toByteArray()
        bag.pouch.putPlain(cleanId, Pouch.LEVEL_FACTORY, "厂改", 1, null, """{"n":1}""".toByteArray())
        factory.padClosures = listOf(ClosureRef(cleanId, 2, ByteArray(0), "厂改", Pouch.LEVEL_FACTORY))
        factory.padTransits = mapOf(
            cleanId to TransitClosure(
                wrap = ByteArray(0),
                members = listOf(
                    TransitMember(
                        id = cleanId,
                        level = Pouch.LEVEL_FACTORY,
                        name = "厂改",
                        revision = 2,
                        ownerId = null,
                        content = factoryBody,
                        kind = Pouch.KIND_PROCESS,
                        copyable = true,
                    ),
                ),
                assetId = cleanId,
                revision = 2,
                kind = Pouch.KIND_PROCESS,
            ),
        )
        bag.syncWithFactory()
        assertArrayEquals(factoryBody, bag.open(cleanId))

        val issued = bag.issuePersonal(Pouch.KIND_PROCESS, "我的", """{"n":1}""".toByteArray())
        bag.flushDirty()
        factory.padClosures = factory.padClosures + ClosureRef(issued.id, 3, ByteArray(0), "厂抢", Pouch.LEVEL_PERSONAL)
        factory.padTransits = factory.padTransits + (
            issued.id to TransitClosure(
                wrap = ByteArray(0),
                members = listOf(
                    TransitMember(
                        id = issued.id,
                        level = Pouch.LEVEL_PERSONAL,
                        name = "厂抢",
                        revision = 3,
                        ownerId = bag.pouch.personId(),
                        content = """{"n":9}""".toByteArray(),
                        kind = Pouch.KIND_PROCESS,
                        copyable = true,
                    ),
                ),
                assetId = issued.id,
                revision = 3,
                kind = Pouch.KIND_PROCESS,
            )
        )
        bag.rewritePlain(issued.id, """{"n":7}""".toByteArray())
        bag.syncWithFactory()
        assertArrayEquals("""{"n":7}""".toByteArray(), factory.padContents[issued.id])
        assertArrayEquals("""{"n":7}""".toByteArray(), bag.open(issued.id))
    }

    @Test
    fun dropPersonalStaysGoneAfterSyncEvenIfFactoryStillListsIt() {
        val factory = FakeFactory(
            devices = listOf(PadDevice(UUID.randomUUID().toString(), "焊", "ARM-1", "C0008")),
        )
        val bag = BagSession({ "" }, factory, MemoryIdentityStore(), MemoryEnvelopeStore())
        bag.login("http://f", UUID.randomUUID().toString(), "op", "p")
        val issued = bag.issuePersonal(Pouch.KIND_PROCESS, "删", """{"n":1}""".toByteArray())
        bag.flushDirty()
        factory.padClosures = listOf(ClosureRef(issued.id, 1, ByteArray(0), "删", Pouch.LEVEL_PERSONAL))
        factory.padTransits = mapOf(issued.id to processTransit(issued.id, "删", """{"n":1}""".toByteArray(), bag.pouch.personId()))
        bag.dropPersonal(issued.id)
        bag.syncWithFactory()
        assertFalse(bag.pouch.held(issued.id))
        assertFalse(factory.padAssets.containsKey(issued.id))
        bag.syncWithFactory()
        assertFalse(bag.pouch.held(issued.id))
        assertFalse(bag.pouch.isDeleted(issued.id))
    }

    @Test
    fun dropNeverUploadedClearsTombstoneOn404() {
        val factory = FakeFactory(
            devices = listOf(PadDevice(UUID.randomUUID().toString(), "焊", "ARM-1", "C0008")),
        )
        val bag = BagSession({ "" }, factory, MemoryIdentityStore(), MemoryEnvelopeStore())
        bag.login("http://f", UUID.randomUUID().toString(), "op", "p")
        val issued = bag.issuePersonal(Pouch.KIND_PROCESS, "未传", """{"n":1}""".toByteArray())
        bag.dropPersonal(issued.id)
        assertTrue(bag.pouch.isDeleted(issued.id))
        bag.syncWithFactory()
        assertFalse(bag.pouch.isDeleted(issued.id))
        assertTrue(factory.padAssets.isEmpty())
    }

    @Test
    fun dropReferencedByFactoryProjectComesBack() {
        val factory = FakeFactory(
            devices = listOf(PadDevice(UUID.randomUUID().toString(), "焊", "ARM-1", "C0008")),
        )
        val bag = BagSession({ "" }, factory, MemoryIdentityStore(), MemoryEnvelopeStore())
        bag.login("http://f", UUID.randomUUID().toString(), "op", "p")
        val issued = bag.issuePersonal(Pouch.KIND_PROCESS, "钉着", """{"n":1}""".toByteArray())
        bag.flushDirty()
        val body = """{"n":1}""".toByteArray()
        val proj = UUID.randomUUID()
        factory.padAssets[proj] = RemoteAsset(
            id = proj,
            kind = Pouch.KIND_PROJECT,
            level = Pouch.LEVEL_FACTORY,
            name = "厂工程",
            code = "GC-F01-000001",
            status = Pouch.STATUS_AVAILABLE,
            copyable = true,
            revision = 1,
            digest = Digest.sum("""[]""".toByteArray()),
            deps = listOf(AssetDep(issued.id, 1, Digest.sum(body))),
        )
        factory.padClosures = listOf(ClosureRef(issued.id, 1, ByteArray(0), "钉着", Pouch.LEVEL_PERSONAL))
        factory.padTransits = mapOf(issued.id to processTransit(issued.id, "钉着", body, bag.pouch.personId()))
        bag.dropPersonal(issued.id)
        bag.syncWithFactory()
        assertTrue(factory.padAssets.containsKey(issued.id))
        assertTrue(bag.pouch.held(issued.id))
        assertFalse(bag.pouch.isDeleted(issued.id))
    }

    @Test
    fun factoryRemovedPersonalIsRecreatedFromPad() {
        val factory = FakeFactory(
            devices = listOf(PadDevice(UUID.randomUUID().toString(), "焊", "ARM-1", "C0008")),
        )
        val bag = BagSession({ "" }, factory, MemoryIdentityStore(), MemoryEnvelopeStore())
        bag.login("http://f", UUID.randomUUID().toString(), "op", "p")
        val issued = bag.issuePersonal(Pouch.KIND_PROCESS, "还在", """{"n":1}""".toByteArray())
        bag.flushDirty()
        factory.padAssets.remove(issued.id)
        factory.padContents.remove(issued.id)
        factory.padClosures = emptyList()
        factory.padTransits = emptyMap()
        bag.syncWithFactory()
        assertEquals(issued.id, factory.padAssets[issued.id]?.id)
        assertArrayEquals("""{"n":1}""".toByteArray(), factory.padContents[issued.id])
    }

    @Test
    fun missingFactoryOriginalIsNotRecreatedAsPersonal() {
        val factory = FakeFactory(
            devices = listOf(PadDevice(UUID.randomUUID().toString(), "焊", "ARM-1", "C0008")),
        )
        val bag = BagSession({ "" }, factory, MemoryIdentityStore(), MemoryEnvelopeStore())
        bag.login("http://f", UUID.randomUUID().toString(), "op", "p")
        val factoryId = UUID.randomUUID()
        bag.pouch.putPlain(factoryId, Pouch.LEVEL_FACTORY, "厂原件", 1, null, """{"n":1}""".toByteArray())
        bag.syncWithFactory()
        assertTrue(bag.pouch.held(factoryId))
        assertFalse(bag.pouch.isDirty(factoryId))
        assertTrue(factory.padAssets.isEmpty())
    }

    @Test
    fun dropPinnedProcessUnpinsThenDeletesOnFactory() {
        val factory = FakeFactory(
            devices = listOf(PadDevice(UUID.randomUUID().toString(), "焊", "ARM-1", "C0008")),
        )
        val bag = BagSession({ "" }, factory, MemoryIdentityStore(), MemoryEnvelopeStore())
        bag.login("http://f", UUID.randomUUID().toString(), "op", "p")
        val proc = bag.issuePersonal(Pouch.KIND_PROCESS, "焊", """{"n":1}""".toByteArray())
        val proj = bag.issuePersonal(Pouch.KIND_PROJECT, "工程", """[]""".toByteArray())
        bag.pouch.pinProcess(proj.id, proc.id)
        bag.flushDirty()
        assertTrue(factory.padAssets[proj.id]!!.deps.any { it.id == proc.id })
        bag.dropPersonal(proc.id)
        bag.syncWithFactory()
        assertTrue(factory.padAssets[proj.id]!!.deps.none { it.id == proc.id })
        assertFalse(factory.padAssets.containsKey(proc.id))
        assertFalse(bag.pouch.held(proc.id))
    }

    @Test
    fun findFactoriesShipsLogsWithoutLogin() {
        val factory = FakeFactory()
        factory.discoverHits = listOf(
            FactoryOffer("http://10.0.0.8:52081", "fac-1", "active", false, "", ""),
        )
        val shipped = mutableListOf<Pair<String, String>>()
        val bag = BagSession(
            { "" },
            factory,
            MemoryIdentityStore(),
            MemoryEnvelopeStore(),
            onFactoryNet = { base, factoryId -> shipped += base to factoryId },
        )
        val hits = bag.findFactories()
        assertEquals(1, hits.size)
        assertEquals(listOf("http://10.0.0.8:52081" to "fac-1"), shipped.distinct())
    }

    @Test
    fun matchArmShipsLogsWithFactory() {
        val cid = UUID.randomUUID()
        val factoryId = UUID.randomUUID().toString()
        val shipped = mutableListOf<Pair<String, String>>()
        val bag = BagSession(
            { "ARM-1" },
            FakeFactory(devices = listOf(PadDevice(cid.toString(), "焊机", "ARM-1", "C0008"))),
            MemoryIdentityStore(),
            MemoryEnvelopeStore(),
            onFactoryNet = { base, fid -> shipped += base to fid },
        )
        bag.login("http://10.0.0.8:52081", factoryId, "op", "p")
        assertTrue(shipped.any { it.first == "http://10.0.0.8:52081" && it.second == factoryId })
        bag.matchArm("ARM-1")
        assertEquals(cid.toString(), bag.savedClientId())
    }

    @Test
    fun activateWithoutArmThenWeldChecksLocalList() {
        val cid = UUID.randomUUID()
        var serial = ""
        val bag = BagSession(
            { serial },
            FakeFactory(devices = listOf(PadDevice(cid.toString(), "焊机", "ARM-1", "C0008"))),
            MemoryIdentityStore(),
            MemoryEnvelopeStore(),
        )
        bag.login("http://f", UUID.randomUUID().toString(), "op", "p")
        assertFalse(bag.armMatched)
        val missing = assertThrows(PouchRejected::class.java) { bag.activate(UUID.randomUUID()) }
        assertEquals(Pouch.ERR_NOT_FOUND, missing.code)
        bag.setWelding(true)
        bag.setWelding(false)
        serial = "ARM-1"
        bag.matchArm(serial)
        bag.setWelding(true)
        bag.setWelding(false)
    }

    @Test
    fun openSkipsStoreWhenAlreadyHeld() {
        val store = CountingEnvelopeStore()
        val cid = UUID.randomUUID()
        val bag = BagSession(
            { "ARM-1" },
            FakeFactory(devices = listOf(PadDevice(cid.toString(), "焊机", "ARM-1", "C0008"))),
            MemoryIdentityStore(),
            store,
        )
        bag.login("http://f", UUID.randomUUID().toString(), "op", "p")
        val proc = processMember("工艺", """{"a":1}""".toByteArray())
        val snap = projectSnap(cid, "工程", proc)
        bag.cacheClosure(snap)
        store.envelopeLoads = 0
        bag.activate(snap.assetId)
        assertArrayEquals("""{"items":[]}""".toByteArray(), bag.open(snap.assetId))
        assertEquals(0, store.envelopeLoads)
    }

    @Test
    fun restoreUsesPersistedDevicesOffline() {
        val key = Wm2.randomKey()
        val person = UUID.randomUUID()
        val cid = UUID.randomUUID()
        val vault = MemorySessionVault()
        val store = MemoryEnvelopeStore()
        val keys = MemoryUnwrapKeyStore()
        val identity = MemoryIdentityStore()
        val factory = FakeFactory(
            key,
            person,
            persistUnwrapKey = true,
            devices = listOf(PadDevice(cid.toString(), "焊机", "ARM-1", "C0008")),
        )
        val bag = BagSession({ "" }, factory, identity, store, vault = vault, keys = keys)
        bag.login("http://f", UUID.randomUUID().toString(), "op", "p")
        assertEquals("ARM-1", store.loadDevices()[0].deviceSerial)
        val again = BagSession({ "arm-1" }, factory, identity, store, vault = vault, keys = keys)
        assertTrue(again.loggedIn)
        assertFalse(again.armMatched)
        again.matchArm("arm-1")
        assertTrue(again.armMatched)
        assertEquals(cid.toString(), again.savedClientId())
    }

    @Test
    fun loginOpensAndLogoutClears() {
        val key = Wm2.randomKey()
        val person = UUID.randomUUID()
        val store = MemoryEnvelopeStore()
        val bag = BagSession({ "ARM-1" }, FakeFactory(key, person), MemoryIdentityStore(), store)
        bag.login("http://f", UUID.randomUUID().toString(), "op", "p")
        assertTrue(bag.loggedIn)
        val aid = UUID.randomUUID()
        bag.cachePlain(aid, Pouch.LEVEL_FACTORY, "厂级", 1, null, """{"n":1}""".toByteArray())
        assertArrayEquals("""{"n":1}""".toByteArray(), bag.open(aid))
        bag.logout()
        assertFalse(bag.loggedIn)
        assertFalse(bag.armMatched)
        assertFalse(bag.pouch.hasUnwrapKey())
        assertThrows(Exception::class.java) { bag.open(aid) }
    }

    @Test
    fun nextPersonCannotOpenPreviousPersonal() {
        val key = Wm2.randomKey()
        val a = UUID.randomUUID()
        val b = UUID.randomUUID()
        val store = MemoryEnvelopeStore()
        val factory = FakeFactory(key, a)
        val bag = BagSession({ "ARM-1" }, factory, MemoryIdentityStore(), store)
        bag.login("http://f", UUID.randomUUID().toString(), "a", "p")
        val pid = UUID.randomUUID()
        bag.cachePlain(pid, Pouch.LEVEL_PERSONAL, "我的", 1, a, """{"p":1}""".toByteArray())
        bag.logout()
        factory.personId = b
        bag.login("http://f", UUID.randomUUID().toString(), "b", "p")
        assertThrows(Exception::class.java) { bag.open(pid) }
    }

    @Test
    fun shortCodeAndPendingSurviveLogout() {
        val key = Wm2.randomKey()
        val person = UUID.randomUUID()
        val store = MemoryEnvelopeStore()
        val factory = FakeFactory(key, person, clientShortCode = "C0008", roles = listOf(Pouch.ROLE_OPERATOR))
        val bag = BagSession({ "ARM-1" }, factory, MemoryIdentityStore(), store)
        val fid = UUID.randomUUID().toString()
        bag.login("http://f", fid, "op", "p")
        bag.matchArm("ARM-1")
        val issued = bag.issuePersonal(Pouch.KIND_PROCESS, "焊", """{"n":1}""".toByteArray())
        assertEquals("GY-C0008-000001", issued.code)
        assertEquals(Pouch.LEVEL_PERSONAL, issued.level)
        bag.enqueueUpload(Pouch.KIND_POINT_CLOUD, "pcd".toByteArray())
        bag.logout()
        assertFalse(bag.loggedIn)
        assertEquals(0, bag.pendingUploads().size)
        assertThrows(Exception::class.java) { bag.open(issued.id) }
    }

    @Test
    fun loginPersistsOfflineTables() {
        val key = Wm2.randomKey()
        val person = UUID.randomUUID()
        val cid = UUID.randomUUID().toString()
        val store = MemoryEnvelopeStore()
        val keys = MemoryUnwrapKeyStore()
        val bag = BagSession(
            { "" },
            FakeFactory(
                key,
                person,
                persistUnwrapKey = true,
                roles = listOf(Pouch.ROLE_OPERATOR),
                devices = listOf(PadDevice(cid, "焊机", "ARM-1", "C0008")),
            ),
            MemoryIdentityStore(),
            store,
            keys = keys,
        )
        bag.login("http://f", UUID.randomUUID().toString(), "op", "p")
        assertEquals("op", store.loadPerson()?.loginName)
        assertEquals(listOf(Pouch.ROLE_OPERATOR), store.loadRoles())
        assertEquals(true, store.loadPolicy()?.persistUnwrapKey)
        assertEquals(1, store.loadDevices().size)
        assertEquals("ARM-1", store.loadDevices()[0].deviceSerial)
        assertEquals(0, store.loadDevices()[0].unwrapKey.size)
        assertEquals(32, keys.loadPouchKey()?.size)
        assertEquals("tok", store.loadSession()?.token)
    }

    @Test
    fun loginOmitsUnwrapKeyWhenNotPersisted() {
        val store = MemoryEnvelopeStore()
        val keys = MemoryUnwrapKeyStore()
        BagSession({ "" }, FakeFactory(persistUnwrapKey = false), MemoryIdentityStore(), store, keys = keys)
            .login("http://f", UUID.randomUUID().toString(), "op", "p")
        assertEquals(0, store.loadDevices()[0].unwrapKey.size)
        assertEquals(null, keys.loadPouchKey())
        assertEquals("tok", store.loadSession()?.token)
    }

    @Test
    fun persistKeyRestoresSessionAndPouch() {
        val key = Wm2.randomKey()
        val person = UUID.randomUUID()
        val vault = MemorySessionVault()
        val store = MemoryEnvelopeStore()
        val keys = MemoryUnwrapKeyStore()
        val identity = MemoryIdentityStore()
        val factory = FakeFactory(key, person, persistUnwrapKey = true)
        val bag = BagSession({ "" }, factory, identity, store, vault = vault, keys = keys)
        val fid = UUID.randomUUID().toString()
        bag.login("http://f", fid, "op", "p")
        val aid = UUID.randomUUID()
        bag.cachePlain(aid, Pouch.LEVEL_FACTORY, "厂级", 1, null, """{"n":1}""".toByteArray())
        assertEquals("tok", vault.load()?.token)
        assertEquals(32, keys.loadPouchKey()?.size)
        val again = BagSession({ "" }, factory, identity, store, vault = vault, keys = keys)
        assertTrue(again.loggedIn)
        assertArrayEquals("""{"n":1}""".toByteArray(), again.open(aid))
    }

    @Test
    fun tokenPersistsButDefaultPolicyNeedsLoginAgain() {
        val vault = MemorySessionVault()
        val factory = FakeFactory(persistUnwrapKey = false)
        val bag = BagSession({ "" }, factory, MemoryIdentityStore(), MemoryEnvelopeStore(), vault = vault)
        bag.login("http://f", UUID.randomUUID().toString(), "op", "p")
        assertEquals("tok", vault.load()?.token)
        val again = BagSession({ "" }, factory, MemoryIdentityStore(), MemoryEnvelopeStore(), vault = vault)
        assertFalse(again.loggedIn)
    }

    @Test
    fun logoutClearsTokenAndProcessData() {
        val vault = MemorySessionVault()
        val store = MemoryEnvelopeStore()
        val keys = MemoryUnwrapKeyStore()
        val factory = FakeFactory(persistUnwrapKey = true)
        val bag = BagSession({ "" }, factory, MemoryIdentityStore(), store, vault = vault, keys = keys)
        bag.login("http://f", UUID.randomUUID().toString(), "op", "p")
        val aid = UUID.randomUUID()
        bag.cachePlain(aid, Pouch.LEVEL_FACTORY, "厂级", 1, null, """{"n":1}""".toByteArray())
        bag.logout()
        assertEquals(null, vault.load())
        assertEquals(null, keys.loadPouchKey())
        assertFalse(bag.pouch.hasUnwrapKey())
        val again = BagSession({ "" }, factory, MemoryIdentityStore(), store, vault = vault, keys = keys)
        assertFalse(again.loggedIn)
        assertThrows(Exception::class.java) { bag.open(aid) }
    }

    @Test
    fun expiredLoginTtlClearsPersistedKey() {
        var now = 1_000L
        val vault = MemorySessionVault()
        val store = MemoryEnvelopeStore()
        val keys = MemoryUnwrapKeyStore()
        val factory = FakeFactory(persistUnwrapKey = true, keyTtlSeconds = 60)
        val bag = BagSession({ "" }, factory, MemoryIdentityStore(), store, vault = vault, keys = keys, clock = { now })
        bag.login("http://f", UUID.randomUUID().toString(), "op", "p")
        val aid = UUID.randomUUID()
        bag.cachePlain(aid, Pouch.LEVEL_FACTORY, "厂级", 1, null, """{"n":1}""".toByteArray())
        assertTrue(bag.loggedIn)
        now += 61_000
        val again = BagSession({ "" }, factory, MemoryIdentityStore(), store, vault = vault, keys = keys, clock = { now })
        assertFalse(again.loggedIn)
        assertEquals(null, vault.load())
        assertEquals(null, keys.loadPouchKey())
        assertFalse(again.pouch.hasUnwrapKey())
        assertThrows(Exception::class.java) { again.open(aid) }
    }

    @Test
    fun zeroLoginTtlDoesNotKickOffline() {
        var now = 1_000L
        val vault = MemorySessionVault()
        val store = MemoryEnvelopeStore()
        val keys = MemoryUnwrapKeyStore()
        val factory = FakeFactory(persistUnwrapKey = true, keyTtlSeconds = 0)
        val bag = BagSession({ "" }, factory, MemoryIdentityStore(), store, vault = vault, keys = keys, clock = { now })
        bag.login("http://f", UUID.randomUUID().toString(), "op", "p")
        now += 365L * 24 * 60 * 60 * 1000
        val again = BagSession({ "" }, factory, MemoryIdentityStore(), store, vault = vault, keys = keys, clock = { now })
        assertTrue(again.loggedIn)
    }

    @Test
    fun changePasswordUsesSessionToken() {
        val factory = FakeFactory()
        val bag = BagSession({ "" }, factory, MemoryIdentityStore(), MemoryEnvelopeStore())
        assertThrows(LoginRejected::class.java) { bag.changePassword("n") }
        bag.login("http://f", UUID.randomUUID().toString(), "op", "p")
        bag.changePassword("new-pass")
        assertEquals("new-pass", factory.lastPassword)
        assertThrows(LoginRejected::class.java) { bag.changePassword("  ") }
    }

    @Test
    fun loginCachesInboxClosures() {
        val key = Wm2.randomKey()
        val person = UUID.randomUUID()
        val cid = UUID.randomUUID()
        val factoryId = UUID.randomUUID()
        val procBody = """{"current":180}""".toByteArray()
        val proc = processMember("工艺", procBody)
        val snap = projectSnap(cid, "工程", proc)
        val root = snap.members.first()
        val projectTransit = TransitClosure(
            wrap = ByteArray(0),
            members = listOf(
                TransitMember(
                    id = root.id,
                    level = root.level,
                    name = root.name,
                    revision = root.revision,
                    ownerId = root.ownerId,
                    content = root.content.copyOf(),
                    kind = root.kind,
                    status = root.status,
                    digest = root.digest,
                    deps = root.deps,
                    code = root.code,
                ),
            ),
            assetId = snap.assetId,
            revision = snap.revision,
            kind = snap.kind,
            level = snap.level,
            status = snap.status,
            digest = Digest.closureSum(listOf(Digest.Member(root.id, root.revision, root.digest, root.content))),
            targetClientId = null,
        )
        val procSnap = ClosureSnapshotPlain(
            kind = Pouch.KIND_PROCESS,
            assetId = proc.id,
            revision = proc.revision,
            level = proc.level,
            status = proc.status,
            digest = Digest.closureSum(listOf(Digest.Member(proc.id, proc.revision, proc.digest, proc.content))),
            targetClientId = cid,
            members = listOf(proc),
        )
        val processTransit = sealTransit(key, factoryId, person, procSnap)
        val store = MemoryEnvelopeStore()
        val factory = FakeFactory(
            key,
            person,
            devices = emptyList(),
        )
        factory.padClosures = listOf(
            ClosureRef(snap.assetId, snap.revision, snap.digest, snap.members.first().name, Pouch.LEVEL_FACTORY),
            ClosureRef(proc.id, proc.revision, proc.digest, proc.name, Pouch.LEVEL_FACTORY),
        )
        factory.padTransits = mapOf(snap.assetId to projectTransit, proc.id to processTransit)
        val bag = BagSession({ "" }, factory, MemoryIdentityStore(), store)
        bag.login("http://f", factoryId.toString(), "op", "p")
        assertTrue(bag.loggedIn)
        assertEquals(1, store.loadClosures().size)
        assertTrue(store.loadEnvelopes().any { it.id == proc.id })
        assertTrue(store.loadProjectPlains().any { it.id == snap.assetId })
        assertFalse(store.loadEnvelopes().any { it.id == snap.assetId })
        val env = store.loadEnvelopes().first { it.id == proc.id }
        assertTrue(Wm2.isEnvelope(env.blob))
        val src = PouchProcessSource(
            openBytes = { id ->
                runCatching { bag.openProcess(id) }.getOrNull()
                    ?: runCatching { bag.open(id) }.getOrNull()
            },
        )
        val got = src.open(proc.id)!!
        assertEquals(180.0, got.current, 0.0)
        assertTrue(Wm2.isEnvelope(store.loadEnvelopes().first { it.id == proc.id }.blob))
    }

    @Test
    fun loginSurvivesOneBadPull() {
        val key = Wm2.randomKey()
        val cid = UUID.randomUUID()
        val factory = FakeFactory(
            key,
            devices = listOf(PadDevice(cid.toString(), "焊机", "ARM-1", "")),
        )
        factory.padClosures = listOf(ClosureRef(UUID.randomUUID(), 1, ByteArray(0), "坏包", Pouch.LEVEL_FACTORY))
        val bag = BagSession({ "" }, factory, MemoryIdentityStore(), MemoryEnvelopeStore())
        bag.login("http://f", UUID.randomUUID().toString(), "op", "p")
        assertTrue(bag.loggedIn)
    }
}

private class CountingEnvelopeStore(
    private val inner: MemoryEnvelopeStore = MemoryEnvelopeStore(),
) : EnvelopeStore by inner {
    var envelopeLoads = 0
    override fun loadEnvelope(id: UUID): CachedEnvelope? {
        envelopeLoads++
        return inner.loadEnvelope(id)
    }
}

internal class FakeFactory(
    private val key: ByteArray = Wm2.randomKey(),
    var personId: UUID = UUID.randomUUID(),
    var clientShortCode: String = "",
    var roles: List<String> = emptyList(),
    var devices: List<PadDevice> = listOf(
        PadDevice(UUID.randomUUID().toString(), "焊机", "ARM-1", clientShortCode),
    ),
    var persistUnwrapKey: Boolean = false,
    var keyTtlSeconds: Long = 0,
) : FactoryGateway {
    var padClosures: List<ClosureRef> = emptyList()
    var padTransits: Map<UUID, TransitClosure> = emptyMap()
    val padAssets = LinkedHashMap<UUID, RemoteAsset>()
    val padContents = LinkedHashMap<UUID, ByteArray>()
    private fun policy() = PolicyMeta(0, persistUnwrapKey, keyTtlSeconds)

    override fun loginPad(
        baseUrl: String,
        factoryId: String,
        loginName: String,
        password: String,
    ) = PadLoginResult(
        token = "tok",
        unwrapKey = key.copyOf(),
        person = PersonMeta(personId, loginName, loginName),
        policy = policy(),
        roles = roles,
        devices = devices.map { it.copy(unwrapKey = it.unwrapKey.copyOf()) },
    )

    override fun padInbox(baseUrl: String, factoryId: String, token: String) =
        ClientInbox(policy(), padClosures)

    override fun padPullClosure(baseUrl: String, factoryId: String, assetId: String, token: String): TransitClosure {
        return padTransits[UUID.fromString(assetId)] ?: throw LoginRejected("not found")
    }

    var discoverHits: List<FactoryOffer> = emptyList()
    override fun discover(baseUrl: String): List<FactoryOffer> = discoverHits

    var lastPassword: String? = null
    override fun changePassword(baseUrl: String, factoryId: String, token: String, password: String) {
        lastPassword = password
    }

    override fun getAsset(baseUrl: String, factoryId: String, token: String, assetId: String): RemoteAsset? =
        padAssets[UUID.fromString(assetId)]

    override fun createPadAsset(
        baseUrl: String,
        factoryId: String,
        token: String,
        kind: String,
        name: String,
        content: String,
        id: String,
        code: String,
        deps: List<AssetDep>,
    ): RemoteAsset {
        val body = content.toByteArray()
        val assetId = if (id.isNotBlank()) UUID.fromString(id) else UUID.randomUUID()
        val row = RemoteAsset(
            id = assetId,
            kind = kind,
            level = Pouch.LEVEL_PERSONAL,
            name = name,
            code = code,
            status = Pouch.STATUS_AVAILABLE,
            copyable = true,
            revision = 1,
            digest = Digest.sum(body),
            deps = deps,
        )
        padAssets[assetId] = row
        padContents[assetId] = body
        return row
    }

    override fun updateAssetContent(
        baseUrl: String,
        factoryId: String,
        token: String,
        assetId: String,
        expected: Long,
        content: String,
    ): RemoteAsset {
        val id = UUID.fromString(assetId)
        val cur = padAssets[id] ?: throw LoginRejected("not found")
        if (cur.revision != expected) throw LoginRejected("revision does not match")
        val body = content.toByteArray()
        val next = cur.copy(revision = cur.revision + 1, digest = Digest.sum(body))
        padAssets[id] = next
        padContents[id] = body
        return next
    }

    override fun setAssetDeps(
        baseUrl: String,
        factoryId: String,
        token: String,
        assetId: String,
        expected: Long,
        deps: List<AssetDep>,
    ): RemoteAsset {
        val id = UUID.fromString(assetId)
        val cur = padAssets[id] ?: throw LoginRejected("not found")
        if (cur.revision != expected) throw LoginRejected("revision does not match")
        val next = cur.copy(revision = cur.revision + 1, deps = deps)
        padAssets[id] = next
        return next
    }

    override fun deleteAsset(baseUrl: String, factoryId: String, token: String, assetId: String) {
        val id = UUID.fromString(assetId)
        if (id !in padAssets) return
        val used = padAssets.any { (other, row) -> other != id && row.deps.any { it.id == id } }
        if (used) throw LoginRejected("still referenced")
        padAssets.remove(id)
        padContents.remove(id)
        padClosures = padClosures.filter { it.assetId != id }
        padTransits = padTransits - id
    }
}

private fun processTransit(
    id: UUID,
    name: String,
    body: ByteArray,
    ownerId: UUID?,
    revision: Long = 1,
): TransitClosure = TransitClosure(
    wrap = ByteArray(0),
    members = listOf(
        TransitMember(
            id = id,
            level = Pouch.LEVEL_PERSONAL,
            name = name,
            revision = revision,
            ownerId = ownerId,
            content = body,
            kind = Pouch.KIND_PROCESS,
            copyable = true,
        ),
    ),
    assetId = id,
    revision = revision,
    kind = Pouch.KIND_PROCESS,
)

private fun processMember(name: String, body: ByteArray): ClosureMemberPlain {
    return ClosureMemberPlain(
        id = UUID.randomUUID(),
        kind = Pouch.KIND_PROCESS,
        level = Pouch.LEVEL_FACTORY,
        name = name,
        status = Pouch.STATUS_AVAILABLE,
        revision = 1,
        content = body,
        digest = Digest.sum(body),
    )
}

private fun projectSnap(client: UUID, name: String, vararg procs: ClosureMemberPlain): ClosureSnapshotPlain {
    val deps = procs.map { AssetDep(it.id, it.revision, it.digest) }
    val body = """{"items":[]}""".toByteArray()
    val rootId = UUID.randomUUID()
    val root = ClosureMemberPlain(
        id = rootId,
        kind = Pouch.KIND_PROJECT,
        level = Pouch.LEVEL_FACTORY,
        name = name,
        status = Pouch.STATUS_AVAILABLE,
        revision = 1,
        content = body,
        digest = Digest.sum(body),
        deps = deps,
    )
    val members = listOf(root) + procs
    val pack = Digest.closureSum(members.map { Digest.Member(it.id, it.revision, it.digest, it.content) })
    return ClosureSnapshotPlain(
        kind = Pouch.KIND_PROJECT,
        assetId = rootId,
        revision = 1,
        level = Pouch.LEVEL_FACTORY,
        status = Pouch.STATUS_AVAILABLE,
        digest = pack,
        targetClientId = client,
        members = members,
    )
}

private fun sealTransit(unwrap: ByteArray, factoryId: UUID, client: UUID, snap: ClosureSnapshotPlain): TransitClosure {
    val dek = Wm2.randomKey()
    val fid = Pouch.uuidBytes(factoryId)
    val cid = Pouch.uuidBytes(client)
    val sealed = snap.members.map { m ->
        TransitMember(
            id = m.id,
            level = m.level,
            name = m.name,
            revision = m.revision,
            ownerId = m.ownerId,
            content = Wm2.seal(dek, m.content, Wm2.clientTransitAad(fid, cid, Pouch.uuidBytes(m.id), m.revision)),
            kind = m.kind,
            status = m.status,
            digest = m.digest,
            deps = m.deps,
        )
    }
    return TransitClosure(
        wrap = Wm2.seal(unwrap, dek, Wm2.clientTransitDekAad(fid, cid)),
        members = sealed,
        assetId = snap.assetId,
        revision = snap.revision,
        kind = snap.kind,
        level = snap.level,
        status = snap.status,
        digest = snap.digest,
        targetClientId = client,
    )
}
