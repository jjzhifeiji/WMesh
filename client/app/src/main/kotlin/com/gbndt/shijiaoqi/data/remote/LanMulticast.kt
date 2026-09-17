package com.gbndt.shijiaoqi.data.remote

import java.net.Inet4Address

/** 本机一条 IPv4 网段，给扫描用。 */
data class LanLink(
    val address: Inet4Address,
    val prefix: Int,
)

/** 当前 WiFi/活动网的 IPv4；NetworkInterface 在部分平板上是空的。 */
interface LanMulticast {
    fun links(): List<LanLink> = emptyList()

    companion object {
        val None: LanMulticast = object : LanMulticast {}
    }
}
