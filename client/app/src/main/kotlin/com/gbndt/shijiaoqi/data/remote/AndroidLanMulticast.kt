package com.gbndt.shijiaoqi.data.remote

import android.content.Context
import android.net.ConnectivityManager
import android.net.wifi.WifiManager
import dagger.hilt.android.qualifiers.ApplicationContext
import java.net.Inet4Address
import java.net.InetAddress
import javax.inject.Inject
import javax.inject.Singleton

/** Android 当前 WiFi 网段：扫描不能只靠 NetworkInterface。 */
@Singleton
class AndroidLanMulticast @Inject constructor(
    @ApplicationContext context: Context,
) : LanMulticast {
    private val app = context.applicationContext
    private val wifi = app.getSystemService(Context.WIFI_SERVICE) as WifiManager
    private val connectivity = app.getSystemService(Context.CONNECTIVITY_SERVICE) as ConnectivityManager

    override fun links(): List<LanLink> {
        val out = LinkedHashMap<String, LanLink>()
        val nets = LinkedHashSet<android.net.Network>()
        connectivity.activeNetwork?.let { nets.add(it) }
        connectivity.allNetworks.forEach { nets.add(it) }
        for (n in nets) {
            val lp = connectivity.getLinkProperties(n) ?: continue
            for (la in lp.linkAddresses) {
                val v4 = la.address as? Inet4Address ?: continue
                if (v4.isLoopbackAddress) continue
                out[v4.hostAddress.orEmpty()] = LanLink(v4, la.prefixLength)
            }
        }
        if (out.isEmpty()) dhcpLink()?.let { out[it.address.hostAddress.orEmpty()] = it }
        return out.values.toList()
    }

    private fun dhcpLink(): LanLink? {
        val info = wifi.dhcpInfo ?: return null
        if (info.ipAddress == 0) return null
        val addr = inet4le(info.ipAddress) ?: return null
        val prefix = if (info.netmask == 0) 24 else Integer.bitCount(info.netmask)
        return LanLink(addr, prefix.coerceIn(8, 32))
    }

    private fun inet4le(le: Int): Inet4Address? {
        val b = byteArrayOf(
            (le and 0xff).toByte(),
            (le shr 8 and 0xff).toByte(),
            (le shr 16 and 0xff).toByte(),
            (le shr 24 and 0xff).toByte(),
        )
        return InetAddress.getByAddress(b) as? Inet4Address
    }
}
