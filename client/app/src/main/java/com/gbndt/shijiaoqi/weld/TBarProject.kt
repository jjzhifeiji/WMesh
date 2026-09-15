package com.gbndt.shijiaoqi.weld

import com.gbndt.shijiaoqi.data.models.WeldPath
import com.gbndt.shijiaoqi.data.models.WeldPathSurrogate
import com.gbndt.shijiaoqi.data.models.toSurrogate
import com.gbndt.shijiaoqi.data.models.toWeldPath
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.decodeFromJsonElement
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive

/** 从闭包工程正文解 T 排；只认 T 排模版身份，不把单层 gapBands 当 T 排。 */
object TBarProject {
    private val json = Json {
        ignoreUnknownKeys = true
        isLenient = true
        coerceInputValues = true
        allowSpecialFloatingPointValues = true
    }

    fun parse(bytes: ByteArray): List<WeldPath> {
        val text = bytes.decodeToString()
        if (text.isBlank() || text.trim() == "[]") return emptyList()
        val element = json.parseToJsonElement(text)
        val arr = element as? JsonArray ?: return emptyList()
        return arr.mapNotNull { item ->
            val obj = item.jsonObject
            if (!isTBar(obj)) {
                null
            } else {
                try {
                    json.decodeFromJsonElement<WeldPathSurrogate>(item).toWeldPath()
                } catch (_: Exception) {
                    null
                }
            }
        }
    }

    private fun isTBar(obj: kotlinx.serialization.json.JsonObject): Boolean {
        return try {
            val kind = obj["kind"]?.jsonPrimitive?.contentOrNull
            val templateId = obj["templateId"]?.jsonPrimitive?.contentOrNull
            kind == TBarRun.KIND || templateId == TBarRun.TEMPLATE_ID
        } catch (_: Exception) {
            false
        }
    }

    fun encode(paths: List<WeldPath>): ByteArray =
        json.encodeToString(
            paths.map {
                it.toSurrogate().copy(kind = TBarRun.KIND, templateId = TBarRun.TEMPLATE_ID)
            },
        ).encodeToByteArray()
}
