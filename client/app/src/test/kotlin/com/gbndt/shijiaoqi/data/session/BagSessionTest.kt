package com.gbndt.shijiaoqi.data.session

import com.gbndt.shijiaoqi.data.crypt.Wm2
import com.gbndt.shijiaoqi.data.pouch.Pouch
import com.gbndt.shijiaoqi.data.pouch.TransitClosure
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.UUID
import com.gbndt.shijiaoqi.data.remote.FactoryOffer

class BagSessionTest {
    @Test
    fun loginWithoutSerialThenMatchArm() {
        val key = Wm2.randomKey()
        val person = UUID.randomUUID()
        val cid = UUID.randomUUID()
        val bag = BagSession(
            { "" },
            FakeFactory(key, person, devices = listOf(PadDevice(cid.toString(), "焊机", "ARM-1", "C0008", key.copyOf()))),
            MemoryIdentityStore(),
            MemoryEnvelopeStore(),
        )
        bag.login("http://f", UUID.randomUUID().toString(), "op", "p")
        assertTrue(bag.loggedIn)
        assertFalse(bag.armMatched)
        assertThrows(LoginRejected::class.java) { bag.matchArm("") }
        assertThrows(LoginRejected::class.java) { bag.matchArm("NOPE") }
        bag.matchArm("ARM-1")
        assertTrue(bag.armMatched)
        assertEquals(cid.toString(), bag.savedClientId())
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
        assertThrows(SecurityException::class.java) { bag.open(pid) }
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
        val cloud = "pcd".toByteArray()
        val up = bag.enqueueUpload(Pouch.KIND_POINT_CLOUD, cloud)
        bag.logout()
        bag.login("http://f", fid, "op", "p")
        bag.matchArm("ARM-1")
        assertEquals(issued.code, bag.pouch.codeOf(issued.id))
        assertEquals(1, bag.pendingUploads().size)
        assertArrayEquals(cloud, bag.openPendingUpload(up.id))
        val other = BagSession({ "ARM-2" }, FakeFactory(Wm2.randomKey(), UUID.randomUUID(), "C0001"), MemoryIdentityStore(), MemoryEnvelopeStore())
        other.login("http://f", UUID.randomUUID().toString(), "op", "p")
        other.matchArm("ARM-1")
        assertEquals(0, other.pendingUploads().size)
    }
}

internal class FakeFactory(
    private val key: ByteArray = Wm2.randomKey(),
    var personId: UUID = UUID.randomUUID(),
    var clientShortCode: String = "",
    var roles: List<String> = emptyList(),
    var devices: List<PadDevice> = listOf(
        PadDevice(UUID.randomUUID().toString(), "焊机", "ARM-1", clientShortCode, key.copyOf()),
    ),
) : FactoryGateway {
    override fun registerDevice(baseUrl: String, factoryId: String, clientId: String, serial: String) {}

    override fun loginOnClient(
        baseUrl: String,
        factoryId: String,
        clientId: String,
        serial: String,
        loginName: String,
        password: String,
    ) = ClientLoginResult(
        token = "tok",
        unwrapKey = key.copyOf(),
        person = PersonMeta(personId, loginName, loginName),
        policy = PolicyMeta(0, 2, "all", persistUnwrapKey = false, keyTtlSeconds = 0),
        clientShortCode = clientShortCode,
        roles = roles,
    )

    override fun loginPad(
        baseUrl: String,
        factoryId: String,
        loginName: String,
        password: String,
    ) = PadLoginResult(
        token = "tok",
        person = PersonMeta(personId, loginName, loginName),
        policy = PolicyMeta(0, 2, "all", persistUnwrapKey = false, keyTtlSeconds = 0),
        roles = roles,
        devices = devices.map { it.copy(unwrapKey = it.unwrapKey.copyOf()) },
    )

    override fun inbox(baseUrl: String, factoryId: String, clientId: String, token: String) =
        ClientInbox(PolicyMeta(0, 2, "all", persistUnwrapKey = false, keyTtlSeconds = 0), emptyList(), ByteArray(0))

    override fun padInbox(baseUrl: String, factoryId: String, clientId: String, token: String) =
        inbox(baseUrl, factoryId, clientId, token)

    override fun pullClosure(baseUrl: String, factoryId: String, clientId: String, projectId: String, token: String): TransitClosure {
        throw LoginRejected("not found")
    }

    override fun padPullClosure(baseUrl: String, factoryId: String, clientId: String, projectId: String, token: String): TransitClosure {
        throw LoginRejected("not found")
    }

    override fun discover(baseUrl: String, serial: String): List<FactoryOffer> = emptyList()
}
