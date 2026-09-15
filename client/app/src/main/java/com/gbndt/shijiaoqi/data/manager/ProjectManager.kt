package com.gbndt.shijiaoqi.data.manager

import android.content.Context
import android.os.Environment
import android.util.Log
import com.gbndt.shijiaoqi.data.models.*
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.decodeFromJsonElement
import kotlinx.serialization.json.jsonObject
import java.io.File

class ProjectManager(context: Context) {
    // Modify to use device root storage -> ShiJiaoQi/projects
    private val baseDir = File(Environment.getExternalStorageDirectory(), "ShiJiaoQi/projects")
    private val singleLayerDir = File(baseDir, "single_layer")
    private val multiLayerDir = File(baseDir, "multi_layer")
    private val tBarDir = File(baseDir, "tbar")
    
    // For backward compatibility or general settings
    private val rootDir = baseDir 

    private val json = Json { 
        ignoreUnknownKeys = true
        prettyPrint = true
        isLenient = true
        allowSpecialFloatingPointValues = true
        coerceInputValues = true
    }

    init {
        if (!singleLayerDir.exists()) singleLayerDir.mkdirs()
        if (!multiLayerDir.exists()) multiLayerDir.mkdirs()
        if (!tBarDir.exists()) tBarDir.mkdirs()
        if (!rootDir.exists()) rootDir.mkdirs()
    }
    
    private fun getRootDir(type: String): File {
        return when (type) {
            "multi" -> multiLayerDir
            "tbar" -> tBarDir
            "single" -> singleLayerDir
            else -> singleLayerDir
        }
    }

    // 获取指定路径下的内容（文件夹和工程）
    fun listContents(relativePath: String, type: String = "single"): List<FileSystemItem> {
        val root = getRootDir(type)
        if (!root.exists()) root.mkdirs()

        val targetDir = if (relativePath.isEmpty()) root else File(root, relativePath)
        if (!targetDir.exists() || !targetDir.isDirectory) return emptyList()

        return targetDir.listFiles()?.map { file ->
            val isProject = File(file, "project_data.json").exists()
            val isMultiLayerProject = File(file, "multilayer_data.json").exists()
            // Filter based on type?
            // Actually, if we are in singleLayerDir, we expect project_data.json
            // If in multiLayerDir, we expect multilayer_data.json
            // But we should report what it IS.
            
            FileSystemItem(
                name = file.name,
                path = if (relativePath.isEmpty()) file.name else "$relativePath/${file.name}",
                isDirectory = file.isDirectory,
                isProject = isProject,
                isMultiLayerProject = isMultiLayerProject
            )
        }?.sortedWith(compareBy({ !it.isDirectory }, { it.name })) ?: emptyList()
    }

    // 创建文件夹
    fun createFolder(parentPath: String, name: String, type: String = "single"): Boolean {
        val root = getRootDir(type)
        val parentDir = if (parentPath.isEmpty()) root else File(root, parentPath)
        val newDir = File(parentDir, name)
        return if (!newDir.exists()) {
            newDir.mkdirs()
        } else {
            false
        }
    }

    // 新建工程（创建文件夹）
    fun createProject(parentPath: String, name: String, type: String = "single"): Boolean {
        val root = getRootDir(type)
        val parentDir = if (parentPath.isEmpty()) root else File(root, parentPath)
        val projectDir = File(parentDir, name)
        return if (!projectDir.exists()) {
            projectDir.mkdirs()
        } else {
            false // 已存在
        }
    }

    // 保存普通焊接工程数据
    fun saveStandardProject(projectPath: String, weldPaths: List<WeldPath>, type: String = "single") {
        try {
            val projectDir = File(getRootDir(type), projectPath)
            if (!projectDir.exists()) {
                projectDir.mkdirs()
            }
            
            val file = File(projectDir, "project_data.json")
            val surrogates = weldPaths.map { it.toSurrogate() }
            val jsonString = json.encodeToString(surrogates)
            file.writeText(jsonString)
        } catch (e: Exception) {
            e.printStackTrace()
        }
    }

    // 保存多层多道工程数据
    fun saveMultiLayerProject(projectPath: String, multiLayerPaths: List<MultiLayerWeldPath>) {
        try {
            val projectDir = File(multiLayerDir, projectPath)
            if (!projectDir.exists()) {
                projectDir.mkdirs()
            }
            
            val multiLayerFile = File(projectDir, "multilayer_data.json")
            val multiLayerSurrogates = multiLayerPaths.map { it.toSurrogate() }
            val multiLayerJson = json.encodeToString(multiLayerSurrogates)
            multiLayerFile.writeText(multiLayerJson)
        } catch (e: Exception) {
            e.printStackTrace()
        }
    }

    // Deprecated: kept for compatibility but unsafe for mixed projects
    fun saveProject(projectPath: String, weldPaths: List<WeldPath>, multiLayerPaths: List<MultiLayerWeldPath> = emptyList()) {
        // This old method probably assumes old rootDir. 
        // We should deprecate it or map it to "single" for now?
        // Let's assume it writes to singleLayerDir for now to avoid data loss if called.
        saveStandardProject(projectPath, weldPaths)
    }

    // 加载工程数据 (Generic - kept for compatibility but adapted)
    fun loadProject(projectPath: String): Pair<List<WeldPath>, List<MultiLayerWeldPath>> {
        // Default to Single Layer if generic load called
        return loadStandardProject(projectPath) to emptyList()
    }
    
    // New Load Methods
    fun loadStandardProject(projectPath: String, type: String = "single"): List<WeldPath> {
        val projectDir = File(getRootDir(type), projectPath)
        val file = File(projectDir, "project_data.json")
        if (!file.exists()) return emptyList()
        val text = file.readText()
        if (text.isBlank() || text.trim() == "[]") return emptyList()
        return try {
            json.decodeFromString<List<WeldPathSurrogate>>(text).map { it.toWeldPath() }
        } catch (e: Exception) {
            Log.e("ProjectManager", "Primary project decode failed: ${e.message}", e)
            try {
                decodeWeldPathsCompat(text)
            } catch (e2: Exception) {
                Log.e("ProjectManager", "Compat project decode failed: ${e2.message}", e2)
                throw IllegalStateException("工程文件无法解析: ${e.message}")
            }
        }
    }

    private fun decodeWeldPathsCompat(text: String): List<WeldPath> {
        val element = json.parseToJsonElement(text)
        val arr = element as? JsonArray ?: throw IllegalStateException("工程文件不是焊道列表")
        return arr.map { item ->
            val raw = item.jsonObject.toMutableMap()
            raw.remove("extraProcesses")
            json.decodeFromJsonElement<WeldPathSurrogate>(JsonObject(raw)).toWeldPath()
        }
    }

    fun loadMultiLayerProject(projectPath: String): List<MultiLayerWeldPath> {
        val projectDir = File(multiLayerDir, projectPath)
        val file = File(projectDir, "multilayer_data.json")
        return if (file.exists()) {
            try {
                val surrogates = json.decodeFromString<List<MultiLayerWeldPathSurrogate>>(file.readText())
                surrogates.map { it.toMultiLayerWeldPath() }
            } catch (e: Exception) {
                e.printStackTrace()
                emptyList()
            }
        } else {
            emptyList()
        }
    }

    // 删除工程或文件夹
    fun deleteItem(path: String, type: String = "single"): Boolean {
        val root = getRootDir(type)
        val item = File(root, path)
        return if (item.exists()) {
            item.deleteRecursively()
        } else {
            false
        }
    }

    // 复制工程
    fun copyProject(srcPath: String, destParentPath: String, newName: String, type: String = "single"): Boolean {
        val root = getRootDir(type)
        val srcDir = File(root, srcPath)
        val destParentDir = if (destParentPath.isEmpty()) root else File(root, destParentPath)
        val destDir = File(destParentDir, newName)
        
        if (!srcDir.exists() || destDir.exists()) {
            return false
        }

        return try {
            srcDir.copyRecursively(destDir, overwrite = true)
            true
        } catch (e: Exception) {
            e.printStackTrace()
            false
        }
    }

    // 保存应用设置
    fun saveAppSettings(settings: AppSettings) {
        try {
            val file = File(rootDir, "app_settings.json")
            val jsonString = json.encodeToString(settings)
            file.writeText(jsonString)
        } catch (e: Exception) {
            e.printStackTrace()
        }
    }

    // 加载应用设置
    fun loadAppSettings(): AppSettings {
        val file = File(rootDir, "app_settings.json")
        // Try to migrate from tool_settings.json if app_settings.json doesn't exist
        if (!file.exists()) {
            val toolFile = File(rootDir, "tool_settings.json")
            if (toolFile.exists()) {
                try {
                    // Temporary data class for migration if needed, but we can just try to decode manually or ignore
                    // For simplicity, just return default, user can re-set. 
                    // Or we could try to read it. But since I changed the class name, the json structure might differ?
                    // No, structure is compatible if I didn't change field names of existing fields.
                    // But I changed the class name in code, not in JSON. JSON doesn't store class name unless polymorphic.
                    // Let's just start fresh or return default.
                } catch (e: Exception) { }
            }
        }
        
        return if (file.exists()) {
            try {
                json.decodeFromString<AppSettings>(file.readText())
            } catch (e: Exception) {
                e.printStackTrace()
                AppSettings()
            }
        } else {
            AppSettings()
        }
    }

    fun saveRobotTestSettings(settings: RobotTestSettings) {
        try {
            val file = File(rootDir, "robot_test_settings.json")
            file.writeText(json.encodeToString(settings))
        } catch (e: Exception) {
            e.printStackTrace()
        }
    }

    fun loadRobotTestSettings(): RobotTestSettings {
        val file = File(rootDir, "robot_test_settings.json")
        if (!file.exists()) return RobotTestSettings()
        return try {
            json.decodeFromString<RobotTestSettings>(file.readText())
        } catch (e: Exception) {
            e.printStackTrace()
            RobotTestSettings()
        }
    }

    // 保存注册License
    fun saveLicense(license: String) {
        try {
            val file = File(rootDir, "license.key")
            file.writeText(license)
        } catch (e: Exception) {
            e.printStackTrace()
        }
    }

    // 加载注册License
    fun loadLicense(): String {
        val file = File(rootDir, "license.key")
        return if (file.exists()) {
            try {
                file.readText().trim()
            } catch (e: Exception) {
                ""
            }
        } else {
            ""
        }
    }

    // 检查是否有License文件
    fun hasLicense(): Boolean {
        return File(rootDir, "license.key").exists()
    }
    
    fun clearLicense() {
        val file = File(rootDir, "license.key")
        if (file.exists()) {
            file.delete()
        }
    }
}
