package com.gbndt.shijiaoqi.data.storage

import android.content.Context
import com.gbndt.shijiaoqi.data.models.WeldProcess
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import java.io.File
import java.io.IOException

class ProcessStorage(private val context: Context) {
    private val processesDirectory by lazy {
        File(context.getExternalFilesDir(null), "processes").apply {
            if (!exists()) {
                mkdirs()
            }
        }
    }

    fun saveProcess(process: WeldProcess): Boolean {
        return try {
            val json = Json.encodeToString(process)
            val file = File(processesDirectory, "${process.name}.json")
            file.writeText(json)
            true
        } catch (e: IOException) {
            e.printStackTrace()
            false
        }
    }

    fun loadProcess(name: String): WeldProcess? {
        return try {
            val file = File(processesDirectory, "$name.json")
            if (file.exists()) {
                val json = file.readText()
                Json.decodeFromString<WeldProcess>(json)
            } else {
                null
            }
        } catch (e: IOException) {
            e.printStackTrace()
            null
        }
    }

    fun deleteProcess(name: String): Boolean {
        return try {
            val file = File(processesDirectory, "$name.json")
            if (file.exists()) {
                file.delete()
            } else {
                false
            }
        } catch (e: IOException) {
            e.printStackTrace()
            false
        }
    }

    fun listProcesses(): List<String> {
        return try {
            processesDirectory.listFiles()?.filter { it.extension == "json" }?.map { it.nameWithoutExtension } ?: emptyList()
        } catch (e: IOException) {
            e.printStackTrace()
            emptyList()
        }
    }

    fun copyProcess(sourceName: String, targetName: String): Boolean {
        return try {
            val sourceFile = File(processesDirectory, "$sourceName.json")
            val targetFile = File(processesDirectory, "$targetName.json")
            if (sourceFile.exists()) {
                sourceFile.copyTo(targetFile, overwrite = true)
                true
            } else {
                false
            }
        } catch (e: IOException) {
            e.printStackTrace()
            false
        }
    }
}
