package com.gbndt.shijiaoqi.weld

import com.gbndt.shijiaoqi.data.models.WeldPath
import com.gbndt.shijiaoqi.data.models.WeldPathSurrogate
import com.gbndt.shijiaoqi.data.models.toWeldPath
import kotlinx.serialization.json.Json

/** 从闭包工程正文解单层焊道；只认 processId。 */
object SingleLayerProject {
    private val json = Json {
        ignoreUnknownKeys = true
        isLenient = true
        coerceInputValues = true
        allowSpecialFloatingPointValues = true
    }

    fun parse(bytes: ByteArray): List<WeldPath> {
        val text = bytes.decodeToString()
        if (text.isBlank() || text.trim() == "[]") return emptyList()
        return json.decodeFromString<List<WeldPathSurrogate>>(text).map { it.toWeldPath() }
    }
}
