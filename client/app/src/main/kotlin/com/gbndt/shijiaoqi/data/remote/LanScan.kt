package com.gbndt.shijiaoqi.data.remote

import com.gbndt.shijiaoqi.model.FactoryOffer
import java.net.DatagramPacket
import java.net.DatagramSocket
import java.net.Inet4Address
import java.net.InetAddress
import java.net.InetSocketAddress
import java.net.NetworkInterface
import java.net.URI
import java.util.Collections
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit

/** 厂服探活命中：地址与身份由服务给出，App 不手填。 */
object LanScan {
    const val UDP_PORT = 52082
    const val PROBE = "WMESH-DISCOVER/1"
    const val REPLY_PREFIX = "WMESH-FACTORY/1"
    val HTTP_PORTS = intArrayOf(8080, 8081, 52081)

    fun parseReply(raw: String): Int? {
        val line = raw.trim()
        if (!line.startsWith(REPLY_PREFIX)) return null
        val port = line.removePrefix(REPLY_PREFIX).trim().toIntOrNull() ?: return null
        if (port !in 1..65535) return null
        return port
    }

    fun find(savedBase: String, probe: (String) -> List<FactoryOffer>): List<FactoryOffer> {
        val bases = LinkedHashSet<String>()
        normalizeBase(savedBase)?.let { bases.add(it) }
        bases.addAll(udpBases())
        val found = LinkedHashMap<String, FactoryOffer>()
        fun absorb(base: String) {
            probe(base).filter { it.factoryId.isNotBlank() }.forEach { o ->
                val key = o.httpBase.trimEnd('/') + "/" + o.factoryId
                val cur = found[key]
                if (cur == null || (!cur.belongs && o.belongs)) found[key] = o
            }
        }
        bases.forEach { runCatching { absorb(it) } }
        if (found.isEmpty()) {
            probeAll(httpCandidates(savedBase), probe).forEach { o ->
                found[o.factoryId + "/" + o.clientId] = o
            }
        }
        return found.values.toList()
    }

    internal fun httpCandidates(savedBase: String): List<String> {
        val ports = LinkedHashSet<Int>()
        HTTP_PORTS.forEach { ports.add(it) }
        portOf(savedBase)?.let { ports.add(it) }
        val out = LinkedHashSet<String>()
        for (iface in ifaces()) {
            val addr = iface.address
            val prefix = iface.networkPrefixLength.toInt()
            if (prefix < 24 || prefix > 30) continue
            val mask = -1 shl (32 - prefix)
            val raw = ipv4(addr)
            val network = raw and mask
            val broadcast = network or mask.inv()
            var host = network + 1
            while (host < broadcast) {
                val ip = inet4(host).hostAddress
                for (p in ports) out.add("http://$ip:$p")
                host++
            }
        }
        return out.toList()
    }

    private fun udpBases(): List<String> {
        val sock = DatagramSocket(null).apply {
            reuseAddress = true
            broadcast = true
            soTimeout = 400
            bind(InetSocketAddress(0))
        }
        return try {
            val payload = PROBE.toByteArray(Charsets.US_ASCII)
            val targets = LinkedHashSet<InetAddress>()
            targets.add(InetAddress.getByName("255.255.255.255"))
            ifaces().mapTo(targets) { it.broadcast }
            for (to in targets) {
                val pkt = DatagramPacket(payload, payload.size, to, UDP_PORT)
                runCatching { sock.send(pkt) }
            }
            val seen = ConcurrentHashMap.newKeySet<String>()
            val buf = ByteArray(128)
            val deadline = System.currentTimeMillis() + 500
            while (System.currentTimeMillis() < deadline) {
                val pkt = DatagramPacket(buf, buf.size)
                try {
                    sock.receive(pkt)
                } catch (_: Exception) {
                    break
                }
                val port = parseReply(String(pkt.data, 0, pkt.length, Charsets.US_ASCII)) ?: continue
                val host = pkt.address.hostAddress ?: continue
                seen.add("http://$host:$port")
            }
            seen.toList()
        } finally {
            sock.close()
        }
    }

    private data class Iface(val address: Inet4Address, val networkPrefixLength: Short, val broadcast: InetAddress)

    private fun ifaces(): List<Iface> {
        val out = ArrayList<Iface>()
        for (ni in Collections.list(NetworkInterface.getNetworkInterfaces())) {
            if (!ni.isUp || ni.isLoopback) continue
            for (addr in ni.interfaceAddresses) {
                val v4 = addr.address as? Inet4Address ?: continue
                if (v4.isLoopbackAddress) continue
                val bcast = addr.broadcast ?: continue
                out.add(Iface(v4, addr.networkPrefixLength, bcast))
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
        jobs.forEach { runCatching { it.get(3, TimeUnit.SECONDS) } }
    } finally {
        pool.shutdownNow()
    }
    return found.values.toList()
}
