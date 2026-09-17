package com.gbndt.shijiaoqi.data.remote

import com.gbndt.shijiaoqi.config.AppConfig
import com.gbndt.shijiaoqi.model.FactoryOffer
import android.util.Log
import java.net.Inet4Address
import java.net.InetAddress
import java.net.NetworkInterface
import java.net.URI
import java.util.Collections
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit

/** 扫局域网厂服务：先预置地址，再扫本网段，只 GET /v1/discover。 */
object LanScan {
    fun find(
        savedBase: String,
        probe: (String) -> List<FactoryOffer>,
        multicast: LanMulticast = LanMulticast.None,
    ): List<FactoryOffer> {
        val found = LinkedHashMap<String, FactoryOffer>()
        fun absorb(hits: List<FactoryOffer>) {
            hits.filter { it.factoryId.isNotBlank() }.forEach { o ->
                val key = o.httpBase.trimEnd('/') + "/" + o.factoryId
                val cur = found[key]
                if (cur == null || (!cur.belongs && o.belongs)) found[key] = o
            }
        }
        val links = mergeLinks(multicast.links())
        val seeds = seedBases(savedBase)
        log("presets=${seeds.size} links=${links.size} saved=$savedBase")
        absorb(probeAll(seeds, probe))
        val seedsSet = seeds.toHashSet()
        val subnet = httpCandidates(savedBase, links).filter { it !in seedsSet }
        log("subnet=${subnet.size}")
        absorb(probeAll(subnet, probe))
        log("hits=${found.size}")
        return found.values.toList()
    }

    internal fun seedBases(savedBase: String): List<String> {
        val out = LinkedHashSet<String>()
        AppConfig.Factory.PRESET_BASES.forEach { normalizeBase(it)?.let(out::add) }
        normalizeBase(savedBase)?.let(out::add)
        return out.toList()
    }

    internal fun httpCandidates(savedBase: String, extra: List<LanLink> = emptyList()): List<String> {
        val ports = LinkedHashSet<Int>()
        AppConfig.Factory.HTTP_PORTS.forEach { ports.add(it) }
        portOf(savedBase)?.let { ports.add(it) }
        val out = LinkedHashSet<String>()
        for (iface in mergeLinks(extra)) {
            for (host in hostsInSubnet(iface.address, iface.prefix)) {
                val ip = host.hostAddress ?: continue
                for (p in ports) out.add("http://$ip:$p")
            }
        }
        return out.toList()
    }

    internal fun hostsInSubnet(addr: Inet4Address, prefix: Int): List<Inet4Address> {
        if (prefix !in 24..30) return emptyList()
        val mask = -1 shl (32 - prefix)
        val network = ipv4(addr) and mask
        val broadcast = network or mask.inv()
        val out = ArrayList<Inet4Address>(broadcast - network - 1)
        var host = network + 1
        while (host < broadcast) {
            out.add(inet4(host))
            host++
        }
        return out
    }

    private fun mergeLinks(extra: List<LanLink>): List<LanLink> {
        val out = LinkedHashMap<String, LanLink>()
        (nics() + extra).forEach { out[it.address.hostAddress.orEmpty()] = it }
        return out.values.toList()
    }

    private fun nics(): List<LanLink> {
        val nics = runCatching { Collections.list(NetworkInterface.getNetworkInterfaces()) }.getOrDefault(emptyList())
        val out = ArrayList<LanLink>()
        for (ni in nics) {
            if (runCatching { !ni.isUp || ni.isLoopback }.getOrDefault(true)) continue
            for (addr in ni.interfaceAddresses) {
                val v4 = addr.address as? Inet4Address ?: continue
                if (v4.isLoopbackAddress) continue
                val prefix = addr.networkPrefixLength.toInt()
                if (prefix !in 8..32) continue
                out.add(LanLink(v4, prefix))
            }
        }
        return out
    }

    private fun normalizeBase(raw: String): String? {
        val t = raw.trim().trimEnd('/')
        if (t.isBlank()) return null
        return if (t.contains("://")) t else "http://$t"
    }

    private fun portOf(raw: String): Int? {
        val base = normalizeBase(raw) ?: return null
        return runCatching { URI(base).port.takeIf { it > 0 } }.getOrNull()
    }

    private fun ipv4(addr: Inet4Address): Int {
        val b = addr.address
        return ((b[0].toInt() and 0xff) shl 24) or
            ((b[1].toInt() and 0xff) shl 16) or
            ((b[2].toInt() and 0xff) shl 8) or
            (b[3].toInt() and 0xff)
    }

    private fun inet4(raw: Int): Inet4Address {
        val b = byteArrayOf(
            (raw ushr 24).toByte(),
            (raw ushr 16).toByte(),
            (raw ushr 8).toByte(),
            raw.toByte(),
        )
        return InetAddress.getByAddress(b) as Inet4Address
    }

    private fun log(msg: String) {
        runCatching { Log.i(TAG, msg) }
    }

    private const val TAG = "WMeshScan"
}

/** 并行探活候选地址；失败的丢掉。 */
fun probeAll(bases: Collection<String>, probe: (String) -> List<FactoryOffer>): List<FactoryOffer> {
    if (bases.isEmpty()) return emptyList()
    val pool = Executors.newFixedThreadPool(32.coerceAtMost(bases.size))
    val found = ConcurrentHashMap<String, FactoryOffer>()
    try {
        val jobs = bases.map { base ->
            pool.submit {
                runCatching {
                    probe(base).filter { it.factoryId.isNotBlank() }.forEach { o ->
                        val key = o.httpBase.trimEnd('/') + "/" + o.factoryId
                        val cur = found[key]
                        if (cur == null || (!cur.belongs && o.belongs)) found[key] = o
                    }
                }
            }
        }
        jobs.forEach { runCatching { it.get(4, TimeUnit.SECONDS) } }
    } finally {
        pool.shutdownNow()
    }
    return found.values.toList()
}
