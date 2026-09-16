package com.gbndt.shijiaoqi.data.prefs

import android.content.Context
import androidx.datastore.preferences.SharedPreferencesMigration
import androidx.datastore.preferences.core.MutablePreferences
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringPreferencesKey
import androidx.datastore.preferences.preferencesDataStore
import com.gbndt.shijiaoqi.data.session.IdentityStore
import com.gbndt.shijiaoqi.model.AppSettings
import com.gbndt.shijiaoqi.model.RobotTestSettings
import dagger.hilt.android.qualifiers.ApplicationContext
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.Json
import javax.inject.Inject
import javax.inject.Singleton

private val Context.deviceDataStore by preferencesDataStore(
    name = "wmesh.device",
    produceMigrations = { context ->
        listOf(
            SharedPreferencesMigration(
                context = context,
                sharedPreferencesName = "wmesh.identity",
            ),
            SharedPreferencesMigration(
                context = context,
                sharedPreferencesName = "wmesh.device",
                keysToMigrate = setOf("app_settings", "robot_test_settings"),
            ),
        )
    },
)

/** 身份与跟机设置：内存缓存，落盘走 DataStore，不含工艺正文。 */
@Singleton
class DeviceSettingsStore @Inject constructor(
    @param:ApplicationContext private val context: Context,
) : IdentityStore {
    private val store = context.deviceDataStore
    private val json = Json {
        ignoreUnknownKeys = true
        encodeDefaults = true
        isLenient = true
        coerceInputValues = true
        allowSpecialFloatingPointValues = true
    }
    private val io = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private val gate = Any()

    private val keyUrl = stringPreferencesKey("factoryUrl")
    private val keyFactory = stringPreferencesKey("factoryId")
    private val keyClient = stringPreferencesKey("clientId")
    private val keyShort = stringPreferencesKey("clientShortCode")
    private val keyApp = stringPreferencesKey("app_settings")
    private val keyRobot = stringPreferencesKey("robot_test_settings")

    @Volatile private var hydrated = false
    @Volatile private var url: String = ""
    @Volatile private var factory: String = ""
    @Volatile private var client: String = ""
    @Volatile private var shortCode: String = ""
    @Volatile private var app: AppSettings = AppSettings()
    @Volatile private var robot: RobotTestSettings = RobotTestSettings()

    init {
        io.launch { ensureHydrated() }
    }

    override var factoryUrl: String
        get() {
            ensureHydrated()
            return url
        }
        set(value) {
            ensureHydrated()
            url = value
            persistSync { this[keyUrl] = value }
        }

    override var factoryId: String
        get() {
            ensureHydrated()
            return factory
        }
        set(value) {
            ensureHydrated()
            factory = value
            persistSync { this[keyFactory] = value }
        }

    override var clientId: String
        get() {
            ensureHydrated()
            return client
        }
        set(value) {
            ensureHydrated()
            client = value
            persistSync { this[keyClient] = value }
        }

    override var clientShortCode: String
        get() {
            ensureHydrated()
            return shortCode
        }
        set(value) {
            ensureHydrated()
            shortCode = value
            persistSync { this[keyShort] = value }
        }

    /** 跟机界面设置：内存缓存，不碰工艺正文。 */
    fun loadAppSettings(): AppSettings {
        ensureHydrated()
        return app
    }

    /** 立刻改内存，落盘交给 DataStore。 */
    fun saveAppSettings(settings: AppSettings) {
        ensureHydrated()
        app = settings
        persistAsync { this[keyApp] = json.encodeToString(AppSettings.serializer(), settings) }
    }

    /** 指令测试页上次填过的参数。 */
    fun loadRobotTestSettings(): RobotTestSettings {
        ensureHydrated()
        return robot
    }

    /** 立刻改内存，落盘交给 DataStore。 */
    fun saveRobotTestSettings(settings: RobotTestSettings) {
        ensureHydrated()
        robot = settings
        persistAsync { this[keyRobot] = json.encodeToString(RobotTestSettings.serializer(), settings) }
    }

    /** 构造不堵主线程；第一次读写才等到缓存填好。 */
    private fun ensureHydrated() {
        if (hydrated) return
        synchronized(gate) {
            if (hydrated) return
            apply(runBlocking { store.data.first() })
            hydrated = true
        }
    }

    private fun apply(snap: Preferences) {
        url = snap[keyUrl].orEmpty()
        factory = snap[keyFactory].orEmpty()
        client = snap[keyClient].orEmpty()
        shortCode = snap[keyShort].orEmpty()
        app = snap[keyApp]?.let { runCatching { json.decodeFromString(AppSettings.serializer(), it) }.getOrNull() } ?: AppSettings()
        robot = snap[keyRobot]?.let { runCatching { json.decodeFromString(RobotTestSettings.serializer(), it) }.getOrNull() } ?: RobotTestSettings()
    }

    /** 身份必须落盘再返回，避免刚登录就杀进程丢厂址。 */
    private fun persistSync(block: MutablePreferences.() -> Unit) {
        runBlocking { store.edit(block) }
    }

    private fun persistAsync(block: MutablePreferences.() -> Unit) {
        io.launch { store.edit(block) }
    }
}
