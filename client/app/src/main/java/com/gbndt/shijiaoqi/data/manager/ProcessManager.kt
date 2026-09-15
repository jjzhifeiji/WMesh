package com.gbndt.shijiaoqi.data.manager

import android.content.Context
import android.net.Uri
import com.gbndt.shijiaoqi.data.models.FileSystemItem
import com.gbndt.shijiaoqi.data.models.WeldProcess
import java.io.File

/** 工艺只进本机袋信封，不再建目录、不再写 JSON/zip。 */
class ProcessManager(@Suppress("unused") context: Context? = null) {
    fun getRootDirectory(): File = File("")

    fun getFile(relativePath: String): File = File("")

    fun listContents(relativePath: String): List<FileSystemItem> = emptyList()

    fun createFolder(parentPath: String, name: String): Boolean = false

    fun saveProcess(parentPath: String, process: WeldProcess) {}

    fun loadProcess(path: String): WeldProcess? = null

    fun deleteItem(path: String): Boolean = false

    fun checkProcessExists(path: String): Boolean = false

    fun zipFileOrFolder(sourcePath: String, zipFile: File): Boolean = false

    fun unzip(zipUri: Uri, destPath: String): Boolean = false

    fun importFile(uri: Uri, destPath: String, fileName: String): Boolean = false
}
