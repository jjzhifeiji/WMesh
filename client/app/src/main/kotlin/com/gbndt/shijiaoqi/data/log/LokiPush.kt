package com.gbndt.shijiaoqi.data.log

import com.gbndt.shijiaoqi.config.AppConfig
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import java.io.OutputStreamWriter
import java.net.HttpURLConnection
import java.net.URI
import java.net.URL

/** Loki / Alloy 标准 push 体。 */
@Serializable
data class LokiPushBody(
    val streams: List<LokiStream>, // 按标签分的流
)

/** 一条流：低基数标签 + 纳秒时间戳行。 */
@Serializable
data class LokiStream(
    val stream: Map<String, String>, // job / factory / client / level
    val values: List<List<String>>, // [ts, line]
)

/** 把本机记录收成 Loki push；厂网直推 Alloy，不带登录令牌。 */
object LokiPush {
    private val json = Json { encodeDefaults = true }

    /** 厂 HTTP 根地址换成同机 Alloy push 地址。 */
    fun urlFromFactory(httpBase: String): String? {
        val host = runCatching { URI(httpBase.trim()).host }.getOrNull()?.takeIf { it.isNotBlank() } ?: return null
        return "http://$host:${AppConfig.Factory.ALLOY_PORT}/loki/api/v1/push"
    }

    /** 按 level 分流出包；空记录不发。 */
    fun body(records: List<LogRec>, factory: String, client: String): String? {
        if (records.isEmpty()) return null
        val streams = records.groupBy { it.level }.map { (level, rows) ->
            LokiStream(
                stream = labels(factory, client, level),
                values = rows.map { listOf(it.ts, it.line) },
            )
        }
        return json.encodeToString(LokiPushBody.serializer(), LokiPushBody(streams))
    }

    /** POST 标准 Loki push；2xx 算送达。 */
    fun post(url: String, body: String): Int {
        val conn = (URL(url).openConnection() as HttpURLConnection).apply {
            requestMethod = "POST"
            connectTimeout = 800
            readTimeout = 3_000
            doOutput = true
            setRequestProperty("Content-Type", "application/json")
        }
        return try {
            OutputStreamWriter(conn.outputStream, Charsets.UTF_8).use { it.write(body) }
            conn.responseCode
        } finally {
            conn.disconnect()
        }
    }

    internal fun labels(factory: String, client: String, level: String): Map<String, String> = buildMap {
        put("job", "shijiaoqi")
        put("level", level)
        if (factory.isNotBlank()) put("factory", factory)
        if (client.isNotBlank()) put("client", client)
    }

    internal fun nsTimestamp(epochMs: Long): String = (epochMs * 1_000_000L).toString()
}
