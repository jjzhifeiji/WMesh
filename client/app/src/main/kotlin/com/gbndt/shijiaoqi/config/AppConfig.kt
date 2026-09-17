package com.gbndt.shijiaoqi.config

/** 全局配置：厂服、机械臂、更新与本机落盘名。协议码与业务枚举不放这里。 */
object AppConfig {
    /** 机械臂路由器固定地址与三路端口。 */
    object Robot {
        const val IP = "192.168.57.2"
        const val PORT_CONTROL = 8080
        const val PORT_BATCH = 8082
        const val PORT_DATA = 8083
        const val LUA_PATH = "/fruser/111.lua"
    }

    /** 厂服 HTTP：先扫这份预置地址，再扫本网段，只 GET /v1/discover。 */
    object Factory {
        val PRESET_HOSTS = listOf("192.168.123.150")
        val PRESET_BASES = PRESET_HOSTS.map { "http://$it:$HTTP_PORT" }
        const val HTTP_PORT = 52081
        val HTTP_PORTS = intArrayOf(HTTP_PORT)
    }

    /** 厂内发布清单。 */
    const val UPDATE_MANIFEST_URL = "http://cdn.gbndt.com/sjqapk/update.json"

    /** 更新包在 filesDir 下的子目录，不进共有存储。 */
    const val UPDATE_DIR = "updates"

    /** 本地信封库文件名。 */
    const val POUCH_DB_NAME = "wmesh-pouch.db"

    /** 解封钥二进制文件，不进库。 */
    const val UNWRAP_KEY_FILE = "wmesh-unwrap.bin"

    /** 本机身份与设置的 DataStore 名。 */
    object Prefs {
        const val DEVICE_STORE = "wmesh.device"
        const val IDENTITY_STORE = "wmesh.identity"
    }

    /** 包角二次确认口令。 */
    const val CORNER_PASSWORD = "bd888888"
}
