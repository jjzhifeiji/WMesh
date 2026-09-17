package com.gbndt.shijiaoqi.data.log

import java.net.URI

/** 厂 HTTP 入参/回包落盘：抹掉密码、令牌、解封钥和信封正文。 */
object HttpLog {
    private val secretJson = Regex(
        """"(password|token|unwrapKey|content|wrap)"\s*:\s*"[^"]*"""",
        RegexOption.IGNORE_CASE,
    )

    /** 正文打码后截断，避免密钥和工艺正文进日志。 */
    fun redact(raw: String): String = secretJson.replace(raw) { m ->
        "\"${m.groupValues[1]}\":\"***\""
    }.take(2_000)

    /** 只留 scheme/host/port/path，查询串丢掉。 */
    fun pathOf(url: String): String {
        val u = runCatching { URI(url.trim()) }.getOrNull() ?: return url.take(200)
        val host = u.host ?: return (u.rawPath ?: url).take(200)
        val port = if (u.port >= 0) ":${u.port}" else ""
        val path = u.rawPath.orEmpty().ifBlank { "/" }
        return "${u.scheme}://$host$port$path"
    }

    fun req(method: String, path: String, body: String? = null) {
        val extra = if (body.isNullOrEmpty()) "" else " body=${redact(body)}"
        PadLog.info("HTTP", "req $method $path$extra")
    }

    fun rsp(method: String, path: String, code: Int, body: String) {
        PadLog.info("HTTP", "rsp $method $path code=$code bytes=${body.length} body=${redact(body)}")
    }

    /** APK 等二进制只记长度，不落正文。 */
    fun rspBytes(method: String, path: String, code: Int, bytes: Long) {
        PadLog.info("HTTP", "rsp $method $path code=$code bytes=$bytes")
    }

    fun fail(method: String, path: String, err: String, quiet: Boolean = false) {
        if (quiet) return
        PadLog.warn("HTTP", "fail $method $path $err")
    }

    fun isDiscover(path: String): Boolean = path.contains("/v1/discover")
}
