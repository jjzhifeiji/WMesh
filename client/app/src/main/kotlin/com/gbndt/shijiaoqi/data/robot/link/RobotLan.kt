package com.gbndt.shijiaoqi.data.robot.link

import com.gbndt.shijiaoqi.data.remote.LanLink
import java.net.Inet4Address
import java.net.InetAddress

/** 机械臂网段：本机已连上臂路由才去打 8080/8082/8083。 */
internal fun onRobotSubnet(links: List<LanLink>, robotIp: String): Boolean {
    val robot = InetAddress.getByName(robotIp) as? Inet4Address ?: return false
    return links.any { sameSubnet(it.address, it.prefix, robot) }
}

internal fun sameSubnet(local: Inet4Address, prefix: Int, target: Inet4Address): Boolean {
    if (prefix !in 0..32) return false
    val mask = if (prefix == 0) 0 else -1 shl (32 - prefix)
    return (ipv4(local) and mask) == (ipv4(target) and mask)
}

private fun ipv4(addr: Inet4Address): Int {
    val b = addr.address
    return ((b[0].toInt() and 0xff) shl 24) or
        ((b[1].toInt() and 0xff) shl 16) or
        ((b[2].toInt() and 0xff) shl 8) or
        (b[3].toInt() and 0xff)
}
