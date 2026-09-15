package com.gbndt.shijiaoqi.data.legacy

import android.content.Context
import com.gbndt.shijiaoqi.model.AppSettings
import com.gbndt.shijiaoqi.model.FileSystemItem
import com.gbndt.shijiaoqi.model.MultiLayerWeldPath
import com.gbndt.shijiaoqi.model.RobotTestSettings
import com.gbndt.shijiaoqi.model.WeldPath
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json

/** 工程不再写 sdcard；跟机设置进本机 prefs，不含工艺正文。 */
class ProjectManager(context: Context? = null) {
    private val prefs = context?.applicationContext?.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
    private val json = Json {
        ignoreUnknownKeys = true
        isLenient = true
        coerceInputValues = true
        allowSpecialFloatingPointValues = true
    }

    fun listContents(relativePath: String, type: String = "single"): List<FileSystemItem> = emptyList()

    fun createFolder(parentPath: String, name: String, type: String = "single"): Boolean = false

    fun createProject(parentPath: String, name: String, type: String = "single"): Boolean = false

    fun saveStandardProject(projectPath: String, weldPaths: List<WeldPath>, type: String = "single") {}

    fun saveMultiLayerProject(projectPath: String, multiLayerPaths: List<MultiLayerWeldPath>) {}

    fun saveProject(projectPath: String, weldPaths: List<WeldPath>, multiLayerPaths: List<MultiLayerWeldPath> = emptyList()) {}

    fun loadProject(projectPath: String): Pair<List<WeldPath>, List<MultiLayerWeldPath>> = emptyList<WeldPath>() to emptyList()

    fun loadStandardProject(projectPath: String, type: String = "single"): List<WeldPath> = emptyList()

    fun loadMultiLayerProject(projectPath: String): List<MultiLayerWeldPath> = emptyList()

    fun deleteItem(path: String, type: String = "single"): Boolean = false

    fun copyProject(srcPath: String, destParentPath: String, newName: String, type: String = "single"): Boolean = false

    fun saveAppSettings(settings: AppSettings) {
        val store = prefs ?: return
        store.edit().putString(KEY_APP, json.encodeToString(settings)).apply()
    }

    fun loadAppSettings(): AppSettings {
        val raw = prefs?.getString(KEY_APP, null) ?: return AppSettings()
        return try {
            json.decodeFromString(AppSettings.serializer(), raw)
        } catch (_: Exception) {
            AppSettings()
        }
    }

    fun saveRobotTestSettings(settings: RobotTestSettings) {
        val store = prefs ?: return
        store.edit().putString(KEY_ROBOT, json.encodeToString(settings)).apply()
    }

    fun loadRobotTestSettings(): RobotTestSettings {
        val raw = prefs?.getString(KEY_ROBOT, null) ?: return RobotTestSettings()
        return try {
            json.decodeFromString(RobotTestSettings.serializer(), raw)
        } catch (_: Exception) {
            RobotTestSettings()
        }
    }

    fun saveLicense(license: String) {}

    fun loadLicense(): String = ""

    fun hasLicense(): Boolean = false

    fun clearLicense() {}

    companion object {
        private const val PREFS = "wmesh.device"
        private const val KEY_APP = "app_settings"
        private const val KEY_ROBOT = "robot_test_settings"
    }
}
