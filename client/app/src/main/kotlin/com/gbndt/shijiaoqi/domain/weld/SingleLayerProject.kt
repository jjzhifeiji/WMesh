package com.gbndt.shijiaoqi.domain.weld

import com.gbndt.shijiaoqi.model.WeldPath
import com.gbndt.shijiaoqi.model.WeldPathSurrogate
import com.gbndt.shijiaoqi.model.toSurrogate
import com.gbndt.shijiaoqi.model.toWeldPath
import kotlinx.serialization.encodeToString
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

    fun encode(paths: List<WeldPath>): ByteArray =
        json.encodeToString(paths.map { it.toSurrogate() }).encodeToByteArray()
}
