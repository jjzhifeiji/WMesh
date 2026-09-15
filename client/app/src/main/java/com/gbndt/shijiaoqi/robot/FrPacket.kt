package com.gbndt.shijiaoqi.robot

/** 控制器 ASCII 帧：`/f/bIII{id}III{type}III{len}III{payload}III/b/f`。 */
data class FrPacket(
    val id: Int,
    val type: Int,
    val payload: String,
) {
    fun encode(): String = encode(id, type, payload)

    companion object {
        private val ERR = Regex("robot errcode:(\\d+)")

        fun encode(id: Int, type: Int, payload: String): String =
            "/f/bIII${id}III${type}III${payload.length}III${payload}III/b/f"

        fun decode(raw: String): FrPacket? {
            val s = raw.trim()
            if (!s.startsWith("/f/bIII") || !s.endsWith("III/b/f")) return null
            val inner = s.removePrefix("/f/bIII").removeSuffix("III/b/f")
            val parts = inner.split("III", limit = 4)
            if (parts.size != 4) return null
            val id = parts[0].toIntOrNull() ?: return null
            val type = parts[1].toIntOrNull() ?: return null
            val len = parts[2].toIntOrNull() ?: return null
            val payload = parts[3]
            if (payload.length != len) return null
            return FrPacket(id, type, payload)
        }

        /** 成功回执：len=1 且 payload=1。 */
        fun isSuccessAck(raw: String): Boolean {
            val p = decode(raw) ?: return false
            return p.payload == "1"
        }

        /** 忽略 0 与 18「程序正在运行」；没有错误码返回 null。 */
        fun errorCode(raw: String): Int? {
            val n = ERR.find(raw)?.groupValues?.get(1)?.toIntOrNull() ?: return null
            if (n == 0 || n == 18) return null
            return n
        }
    }
}
