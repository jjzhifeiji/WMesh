package com.gbndt.shijiaoqi.robot

/** 探活：8080 通且 8083 在 1 秒内有数据；8082 不参与。 */
object RobotLink {
    const val UP = "已连接"
    const val DOWN = "未连接"
    const val ALIVE_MS = 1000L

    fun connected(port8080: Boolean, port8083: Boolean, last8083At: Long, now: Long): Boolean {
        if (!port8080 || !port8083) return false
        return now - last8083At <= ALIVE_MS
    }

    fun status(port8080: Boolean, port8083: Boolean, last8083At: Long, now: Long): String =
        if (connected(port8080, port8083, last8083At, now)) UP else DOWN
}
