package com.gbndt.shijiaoqi.data.remote

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import com.gbndt.shijiaoqi.model.FactoryOffer

class LanScanTest {
    @Test
    fun parseFactoryUdpReply() {
        assertEquals(52081, LanScan.parseReply("WMESH-FACTORY/1 52081\n"))
        assertEquals(null, LanScan.parseReply("nope"))
        assertEquals(null, LanScan.parseReply("WMESH-FACTORY/1 0"))
    }

    @Test
    fun keepAllServicesForDropdown() {
        val mine = FactoryOffer("http://10.0.0.8:52081", "fac", "active", true, "cli", "焊机")
        val other = FactoryOffer("http://10.0.0.9:52081", "fac2", "active", false, "", "")
        val hits = LanScan.find("http://10.0.0.8:52081") { base ->
            if (base.contains("10.0.0.8")) listOf(mine, other) else emptyList()
        }
        assertEquals(2, hits.size)
        assertTrue(hits.any { it.belongs && it.clientId == "cli" })
        assertTrue(hits.any { it.factoryId == "fac2" })
        assertEquals("焊机 · 10.0.0.8:52081", mine.label())
    }
}
