package com.gbndt.shijiaoqi.data.remote

import com.gbndt.shijiaoqi.config.AppConfig
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import com.gbndt.shijiaoqi.model.FactoryOffer
import java.net.Inet4Address
import java.net.InetAddress
import java.util.concurrent.CopyOnWriteArrayList

class LanScanTest {
    @Test
    fun seedPutsPresetsBeforeSaved() {
        val saved = "http://10.0.0.8:52081"
        assertTrue(AppConfig.Factory.PRESET_HOSTS.isNotEmpty())
        assertEquals(AppConfig.Factory.HTTP_PORT, 52081)
        assertEquals(intArrayOf(52081).toList(), AppConfig.Factory.HTTP_PORTS.toList())
        assertEquals(AppConfig.Factory.PRESET_BASES + saved, LanScan.seedBases(saved))
    }

    @Test
    fun keepAllServicesForDropdown() {
        val mine = FactoryOffer("http://10.0.0.8:52081", "fac", "active", true, "cli", "焊机")
        val other = FactoryOffer("http://10.0.0.9:52081", "fac2", "active", false, "", "")
        val hits = LanScan.find("http://10.0.0.8:52081", probe = { base ->
            if (base.contains("10.0.0.8")) listOf(mine, other) else emptyList()
        })
        assertEquals(2, hits.size)
        assertTrue(hits.any { it.belongs && it.clientId == "cli" })
        assertTrue(hits.any { it.factoryId == "fac2" })
        assertEquals("焊机 · 10.0.0.8:52081", mine.label())
    }

    @Test
    fun presetsFinishBeforeSubnetSweep() {
        val addr = InetAddress.getByName("10.20.30.50") as Inet4Address
        val lan = object : LanMulticast {
            override fun links() = listOf(LanLink(addr, 24))
        }
        val seen = CopyOnWriteArrayList<String>()
        val seeds = LanScan.seedBases("")
        LanScan.find("", probe = { base ->
            seen.add(base)
            emptyList()
        }, multicast = lan)
        assertEquals(seeds.toSet(), seen.take(seeds.size).toSet())
        val rest = seen.drop(seeds.size)
        assertTrue(rest.any { it.startsWith("http://10.20.30.") && it.endsWith(":${AppConfig.Factory.HTTP_PORT}") })
        assertTrue(rest.none { it in seeds })
    }

    @Test
    fun subnetHostsSkipNetworkAndBroadcast() {
        val addr = InetAddress.getByName("192.168.1.50") as Inet4Address
        val hosts = LanScan.hostsInSubnet(addr, 24)
        assertEquals(254, hosts.size)
        assertTrue(hosts.any { it.hostAddress == "192.168.1.50" })
        assertTrue(hosts.none { it.hostAddress == "192.168.1.0" })
        assertTrue(hosts.none { it.hostAddress == "192.168.1.255" })
        assertTrue(LanScan.hostsInSubnet(addr, 16).isEmpty())
    }
}
