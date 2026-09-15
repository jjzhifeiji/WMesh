package com.gbndt.shijiaoqi.platform.session

import com.gbndt.shijiaoqi.platform.crypt.Wm2
import com.gbndt.shijiaoqi.platform.pouch.Pouch
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.UUID

class ChannelSyncTest {
    @Test
    fun bodyNotAcceptedAndLowerRevisionIgnored() {
        assertTrue(DownIntents.hasBody("""{"typ":"closure","content":"x"}"""))
        assertTrue(DownIntents.hasBody("""{"typ":"closure","members":[]}"""))
        assertFalse(DownIntents.hasBody("""{"typ":"policy","revision":2}"""))
        assertTrue(DownIntents.shouldApply(1, 2))
        assertFalse(DownIntents.shouldApply(2, 2))
        assertFalse(DownIntents.shouldApply(3, 2))

        val key = Wm2.randomKey()
        val store = MemoryEnvelopeStore()
        store.open(key)
        store.savePolicy(PolicyMeta(3, 2, "all", persistUnwrapKey = false, keyTtlSeconds = 0))
        val pouch = Pouch()
        pouch.login(key, UUID.randomUUID(), false)
        val live = LiveChannel(
            baseUrl = "http://f",
            factoryId = UUID.randomUUID(),
            clientId = UUID.randomUUID(),
            token = "tok",
            mqttUrl = "",
            signingPub = ByteArray(0),
            pouch = pouch,
            store = store,
        )
        ChannelSync.onDown(FakeFactory(key), live, """{"typ":"policy","revision":1,"maxCachedProjects":9,"cacheScope":"current"}""") {}
        assertEquals(3, store.loadPolicy()!!.revision)
        ChannelSync.onDown(FakeFactory(key), live, """{"typ":"policy","revision":4,"maxCachedProjects":3,"cacheScope":"all"}""") {}
        assertEquals(4, store.loadPolicy()!!.revision)
        ChannelSync.onDown(FakeFactory(key), live, """{"typ":"closure","content":"WM2secret"}""") {}
        assertEquals(0, pouch.diskSnapshot().size)
    }
}
