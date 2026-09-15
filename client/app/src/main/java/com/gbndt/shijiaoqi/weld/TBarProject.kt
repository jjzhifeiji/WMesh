package com.gbndt.shijiaoqi.weld

import com.gbndt.shijiaoqi.data.models.WeldPath
import com.gbndt.shijiaoqi.data.models.WeldPathSurrogate
import com.gbndt.shijiaoqi.data.models.toWeldPath
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.decodeFromJsonElement
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive

/** 从闭包工程正文解 T 排；只认 gapBands 的工艺身份。 */
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
            if (kind == TBarRun.KIND || templateId == TBarRun.TEMPLATE_ID) return true
            if (obj.containsKey("gapBands")) return true
            val points = obj["points"] as? JsonArray ?: return false
            points.any { pt ->
                pt.jsonObject["type"]?.jsonPrimitive?.contentOrNull == "GROOVE_A_LOWER"
            }
        } catch (_: Exception) {
            false
        }
    }
}
