package com.gbndt.shijiaoqi.data.manager

import android.content.Context
import android.os.Environment
import com.gbndt.shijiaoqi.data.models.WeldProcess
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import java.io.File
import java.io.BufferedOutputStream
import java.io.FileOutputStream
import java.io.FileInputStream
import java.io.BufferedInputStream
import java.util.zip.ZipEntry
import java.util.zip.ZipOutputStream
import java.util.zip.ZipInputStream
import java.nio.charset.Charset
import android.net.Uri

import com.gbndt.shijiaoqi.data.models.FileSystemItem

class ProcessManager(private val context: Context) {
    // Modify to use device root storage -> ShiJiaoQi/processes
    private val directory = File(Environment.getExternalStorageDirectory(), "ShiJiaoQi/processes")
    private val json = Json { 
        ignoreUnknownKeys = true
        prettyPrint = true
        isLenient = true
        allowSpecialFloatingPointValues = true
        coerceInputValues = true
    }

    init {
        if (!directory.exists()) {
            directory.mkdirs()
        }
    }
    
    fun getRootDirectory(): File {
        return directory
    }
    
    fun getFile(relativePath: String): File {
        return File(directory, relativePath)
    }

    fun listContents(relativePath: String): List<FileSystemItem> {
        // Ensure directory exists (in case permission was granted after init)
        if (!directory.exists()) {
            directory.mkdirs()
        }

        val targetDir = if (relativePath.isEmpty()) directory else File(directory, relativePath)
        if (!targetDir.exists()) return emptyList()

        return targetDir.listFiles()?.map { file ->
            FileSystemItem(
                name = file.name,
                path = if (relativePath.isEmpty()) file.name else "$relativePath/${file.name}",
                isDirectory = file.isDirectory,
                isProcess = file.extension == "json"
            )
        }?.sortedWith(compareBy({ !it.isDirectory }, { it.name })) ?: emptyList()
    }

    fun createFolder(parentPath: String, name: String): Boolean {
        val parentDir = if (parentPath.isEmpty()) directory else File(directory, parentPath)
        val newDir = File(parentDir, name)
        return if (!newDir.exists()) {
            newDir.mkdirs()
        } else {
            false
        }
    }

    fun saveProcess(parentPath: String, process: WeldProcess) {
        val parentDir = if (parentPath.isEmpty()) directory else File(directory, parentPath)
        if (!parentDir.exists()) {
            parentDir.mkdirs()
        }
        
        // 确保文件名合法
        val safeName = process.name.replace(Regex("[^a-zA-Z0-9\\u4e00-\\u9fa5_\\-.\\s()]"), "")
        val file = File(parentDir, "$safeName.json")
        val jsonString = json.encodeToString(process)
        file.writeText(jsonString)
    }

    fun loadProcess(path: String): WeldProcess? {
        val file = File(directory, path)
        return if (file.exists()) {
            try {
                val process = json.decodeFromString<WeldProcess>(file.readText())
                // Ensure internal name matches filename (without extension)
                // This prevents issues where copying a file externally doesn't update internal name,
                // leading to overwrites of the original file on save.
                val fileNameWithoutExt = file.nameWithoutExtension
                if (process.name != fileNameWithoutExt) {
                    process.copy(name = fileNameWithoutExt)
                } else {
                    process
                }
            } catch (e: Exception) {
                e.printStackTrace()
                null
            }
        } else {
            null
        }
    }

    fun deleteItem(path: String): Boolean {
        val file = File(directory, path)
        return if (file.exists()) {
            file.deleteRecursively()
        } else {
            false
        }
    }

    fun checkProcessExists(path: String): Boolean {
        return File(directory, path).exists()
    }

    fun zipFileOrFolder(sourcePath: String, zipFile: File): Boolean {
        val sourceFile = File(directory, sourcePath)
        if (!sourceFile.exists()) return false

        return try {
            ZipOutputStream(BufferedOutputStream(FileOutputStream(zipFile))).use { zos ->
                if (sourceFile.isFile) {
                    val entry = ZipEntry(sourceFile.name)
                    zos.putNextEntry(entry)
                    FileInputStream(sourceFile).use { fis -> fis.copyTo(zos) }
                    zos.closeEntry()
                } else {
                    zipRecursively(sourceFile, sourceFile.name, zos)
                }
            }
            true
        } catch (e: Exception) {
            e.printStackTrace()
            false
        }
    }

    private fun zipRecursively(file: File, path: String, zos: ZipOutputStream) {
        if (file.isDirectory) {
            val children = file.listFiles() ?: return
            for (child in children) {
                zipRecursively(child, "$path/${child.name}", zos)
            }
        } else {
            val entry = ZipEntry(path)
            zos.putNextEntry(entry)
            FileInputStream(file).use { fis ->
                fis.copyTo(zos)
            }
            zos.closeEntry()
        }
    }

    fun unzip(zipUri: Uri, destPath: String): Boolean {
        val destDir = if (destPath.isEmpty()) directory else File(directory, destPath)
        if (!destDir.exists()) destDir.mkdirs()

        return try {
            context.contentResolver.openInputStream(zipUri)?.use { inputStream ->
                // Use GBK charset to handle Chinese filenames created by Windows Zip tools
                val charset = if (android.os.Build.VERSION.SDK_INT >= android.os.Build.VERSION_CODES.N) {
                    Charset.forName("GBK")
                } else {
                    Charset.defaultCharset()
                }
                
                val zis = if (android.os.Build.VERSION.SDK_INT >= android.os.Build.VERSION_CODES.N) {
                    ZipInputStream(BufferedInputStream(inputStream), charset)
                } else {
                    ZipInputStream(BufferedInputStream(inputStream))
                }
                
                zis.use {
                    var entry = try {
                        it.nextEntry
                    } catch (e: IllegalArgumentException) {
                        // Fallback to default if GBK fails
                        null
                    }
                    
                    while (entry != null) {
                        val file = File(destDir, entry.name)
                        if (entry.isDirectory) {
                            file.mkdirs()
                        } else {
                            file.parentFile?.mkdirs()
                            FileOutputStream(file).use { fos ->
                                it.copyTo(fos)
                            }
                        }
                        it.closeEntry()
                        entry = it.nextEntry
                    }
                }
            }
            true
        } catch (e: Exception) {
            e.printStackTrace()
            false
        }
    }

    fun importFile(uri: Uri, destPath: String, fileName: String): Boolean {
        val destDir = if (destPath.isEmpty()) directory else File(directory, destPath)
        if (!destDir.exists()) destDir.mkdirs()
        
        val destFile = File(destDir, fileName)
        
        return try {
            context.contentResolver.openInputStream(uri)?.use { inputStream ->
                FileOutputStream(destFile).use { outputStream ->
                    inputStream.copyTo(outputStream)
                }
            }
            true
        } catch (e: Exception) {
            e.printStackTrace()
            false
        }
    }
}
