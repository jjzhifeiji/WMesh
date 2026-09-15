package com.gbndt.shijiaoqi.domain.weld

import com.gbndt.shijiaoqi.model.MultiLayerWeldPath
import com.gbndt.shijiaoqi.model.MultiLayerWeldPathSurrogate
import com.gbndt.shijiaoqi.model.toMultiLayerWeldPath
import com.gbndt.shijiaoqi.model.toSurrogate
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.decodeFromJsonElement
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive

/** 从闭包工程正文解多层焊缝；只认 processId。 */
object MultiLayerProject {
    private val json = Json {
        ignoreUnknownKeys = true
        isLenient = true
        coerceInputValues = true
        allowSpecialFloatingPointValues = true
    }

    fun parse(bytes: ByteArray): List<MultiLayerWeldPath> {
        val text = bytes.decodeToString()
        if (text.isBlank() || text.trim() == "[]") return emptyList()
        val element = json.parseToJsonElement(text)
        val arr = element as? JsonArray ?: return emptyList()
        return arr.mapNotNull { item ->
            val obj = item.jsonObject
            val kind = obj["kind"]?.jsonPrimitive?.contentOrNull
            if (!obj.containsKey("basePath") && kind != "multi") {
                null
            } else {
                try {
                    json.decodeFromJsonElement<MultiLayerWeldPathSurrogate>(item).toMultiLayerWeldPath()
                } catch (_: Exception) {
                    null
                }
            }
        }
    }

    fun encode(paths: List<MultiLayerWeldPath>): ByteArray =
        json.encodeToString(paths.map { it.toSurrogate() }).encodeToByteArray()
}
