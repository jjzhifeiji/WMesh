package com.gbndt.shijiaoqi.platform.session

import com.gbndt.shijiaoqi.platform.crypt.Wm2
import com.gbndt.shijiaoqi.platform.pouch.Pouch
import com.gbndt.shijiaoqi.platform.pouch.TransitClosure
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.UUID

class BagSessionTest {
    @Test
    fun emptySerialRefuses() {
        val bag = BagSession({ "" }, FakeFactory(), MemoryIdentityStore(), MemoryEnvelopeStore())
        assertThrows(LoginRejected::class.java) {
            bag.login("http://f", UUID.randomUUID().toString(), UUID.randomUUID().toString(), "op", "p")
        }
        assertFalse(bag.loggedIn)
        assertFalse(bag.pouch.hasUnwrapKey())
    }

    @Test
    fun loginOpensAndLogoutClears() {
        val key = Wm2.randomKey()
        val person = UUID.randomUUID()
        val store = MemoryEnvelopeStore()
        val bag = BagSession({ "ARM-1" }, FakeFactory(key, person), MemoryIdentityStore(), store)
        bag.login("http://f", UUID.randomUUID().toString(), UUID.randomUUID().toString(), "op", "p")
        assertTrue(bag.loggedIn)
        val aid = UUID.randomUUID()
        bag.cachePlain(aid, Pouch.LEVEL_FACTORY, "厂级", 1, null, """{"n":1}""".toByteArray())
        assertArrayEquals("""{"n":1}""".toByteArray(), bag.open(aid))
        bag.logout()
        assertFalse(bag.loggedIn)
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
        bag.login("http://f", UUID.randomUUID().toString(), UUID.randomUUID().toString(), "a", "p")
        val pid = UUID.randomUUID()
        bag.cachePlain(pid, Pouch.LEVEL_PERSONAL, "我的", 1, a, """{"p":1}""".toByteArray())
        bag.logout()
        factory.personId = b
        bag.login("http://f", UUID.randomUUID().toString(), UUID.randomUUID().toString(), "b", "p")
        assertThrows(SecurityException::class.java) { bag.open(pid) }
    }
}

internal class FakeFactory(
    private val key: ByteArray = Wm2.randomKey(),
    var personId: UUID = UUID.randomUUID(),
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
    )

    override fun inbox(baseUrl: String, factoryId: String, clientId: String, token: String) =
        ClientInbox(PolicyMeta(0, 2, "all", persistUnwrapKey = false, keyTtlSeconds = 0), emptyList(), ByteArray(0))

    override fun pullClosure(baseUrl: String, factoryId: String, clientId: String, projectId: String, token: String): TransitClosure {
        throw LoginRejected("not found")
    }
}
