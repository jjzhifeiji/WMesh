package com.gbndt.shijiaoqi.data.storage

import android.content.Context
import com.gbndt.shijiaoqi.data.models.WeldProcess

/** 已废弃：工艺不得再写外部 filesDir JSON。 */
class ProcessStorage(@Suppress("unused") context: Context) {
    fun saveProcess(process: WeldProcess): Boolean = false

    fun loadProcess(name: String): WeldProcess? = null

    fun deleteProcess(name: String): Boolean = false

    fun listProcesses(): List<String> = emptyList()

    fun copyProcess(sourceName: String, targetName: String): Boolean = false
}
