package com.gbndt.shijiaoqi.data.prefs

import android.content.Context
import androidx.datastore.preferences.SharedPreferencesMigration
import androidx.datastore.preferences.core.MutablePreferences
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.booleanPreferencesKey
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringPreferencesKey
import androidx.datastore.preferences.preferencesDataStore
import com.gbndt.shijiaoqi.config.AppConfig
import com.gbndt.shijiaoqi.data.session.PadDevice
import com.gbndt.shijiaoqi.data.session.SavedSession
import com.gbndt.shijiaoqi.data.session.SessionVault
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
    name = AppConfig.Prefs.DEVICE_STORE,
    produceMigrations = { context ->
        listOf(
            SharedPreferencesMigration(
                context = context,
                sharedPreferencesName = AppConfig.Prefs.IDENTITY_STORE,
            ),
            SharedPreferencesMigration(
                context = context,
                sharedPreferencesName = AppConfig.Prefs.DEVICE_STORE,
                keysToMigrate = setOf("app_settings", "robot_test_settings"),
            ),
        )
    },
)

/** 身份与跟机设置：内存缓存，落盘走 DataStore，不含工艺正文。 */
@Singleton
class DeviceSettingsStore @Inject constructor(
    @param:ApplicationContext private val context: Context,
) : IdentityStore, SessionVault {
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
    private val keyRemember = booleanPreferencesKey("rememberLogin")
    private val keyLoginName = stringPreferencesKey("loginName")
    private val keyLoginPassword = stringPreferencesKey("loginPassword")
    private val keySessionToken = stringPreferencesKey("sessionToken")
    private val keySessionExpires = stringPreferencesKey("sessionExpiresAt")
    private val keySessionPersonId = stringPreferencesKey("sessionPersonId")
    private val keySessionPersonName = stringPreferencesKey("sessionPersonName")
    private val keySessionLoginName = stringPreferencesKey("sessionLoginName")
    private val keySessionRoles = stringPreferencesKey("sessionRoles")
    private val keySessionPersist = booleanPreferencesKey("sessionPersistUnwrap")
    private val keySessionEncrypt = booleanPreferencesKey("sessionEncryptPouch")
    private val keySessionTtl = stringPreferencesKey("sessionKeyTtl")
    private val keySessionLoginAt = stringPreferencesKey("sessionLoggedInAt")
    private val keySessionPouch = stringPreferencesKey("sessionPouchKey")
    private val keySessionDevices = stringPreferencesKey("sessionDevices")

    @Volatile private var hydrated = false
    @Volatile private var url: String = ""
    @Volatile private var factory: String = ""
    @Volatile private var client: String = ""
    @Volatile private var shortCode: String = ""
    @Volatile private var app: AppSettings = AppSettings()
    @Volatile private var robot: RobotTestSettings = RobotTestSettings()
    @Volatile private var rememberLogin: Boolean = false
    @Volatile private var padLogin: String = ""
    @Volatile private var padPassword: String = ""
    @Volatile private var session: SavedSession? = null

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

    /** 勾选记住时返回上次登录名和密码，否则空。 */
    fun loadRememberedLogin(): Pair<String, String>? {
        ensureHydrated()
        if (!rememberLogin) return null
        return padLogin to padPassword
    }

    /** 登录成功后按勾选落盘；取消记住则清掉账号密码。 */
    fun saveRememberedLogin(remember: Boolean, name: String, password: String) {
        ensureHydrated()
        rememberLogin = remember
        padLogin = if (remember) name else ""
        padPassword = if (remember) password else ""
        persistSync {
            this[keyRemember] = rememberLogin
            this[keyLoginName] = padLogin
            this[keyLoginPassword] = padPassword
        }
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
        rememberLogin = snap[keyRemember] ?: false
        padLogin = snap[keyLoginName].orEmpty()
        padPassword = snap[keyLoginPassword].orEmpty()
        session = readSession(snap)
    }

    override fun load(): SavedSession? {
        ensureHydrated()
        val cur = session ?: return null
        return cur.copy(devices = cur.devices.map { it.copy(unwrapKey = it.unwrapKey.copyOf()) })
    }

    override fun save(session: SavedSession) {
        ensureHydrated()
        this.session = session.copy(devices = session.devices.map { it.copy(unwrapKey = it.unwrapKey.copyOf()) })
        persistSync { writeSession(this, session) }
    }

    override fun clear() {
        ensureHydrated()
        session = null
        persistSync { writeSession(this, null) }
    }

    private fun readSession(snap: Preferences): SavedSession? {
        val token = snap[keySessionToken].orEmpty()
        if (token.isBlank()) return null
        return SavedSession(
            token = token,
            expiresAtMillis = snap[keySessionExpires]?.toLongOrNull() ?: 0L,
            personId = snap[keySessionPersonId].orEmpty(),
            personName = snap[keySessionPersonName].orEmpty(),
            loginName = snap[keySessionLoginName].orEmpty(),
            roles = snap[keySessionRoles].orEmpty().split(',').map { it.trim() }.filter { it.isNotEmpty() },
            persistUnwrapKey = snap[keySessionPersist] ?: false,
            encryptPouch = snap[keySessionEncrypt] ?: true,
            keyTtlSeconds = snap[keySessionTtl]?.toLongOrNull() ?: 0L,
            loggedInAtMillis = snap[keySessionLoginAt]?.toLongOrNull() ?: 0L,
            devices = decodeDevices(snap[keySessionDevices].orEmpty()),
        )
    }

    private fun writeSession(prefs: MutablePreferences, session: SavedSession?) {
        if (session == null) {
            prefs.remove(keySessionToken)
            prefs.remove(keySessionExpires)
            prefs.remove(keySessionPersonId)
            prefs.remove(keySessionPersonName)
            prefs.remove(keySessionLoginName)
            prefs.remove(keySessionRoles)
            prefs.remove(keySessionPersist)
            prefs.remove(keySessionEncrypt)
            prefs.remove(keySessionTtl)
            prefs.remove(keySessionLoginAt)
            prefs.remove(keySessionPouch)
            prefs.remove(keySessionDevices)
            return
        }
        prefs[keySessionToken] = session.token
        prefs[keySessionExpires] = session.expiresAtMillis.toString()
        prefs[keySessionPersonId] = session.personId
        prefs[keySessionPersonName] = session.personName
        prefs[keySessionLoginName] = session.loginName
        prefs[keySessionRoles] = session.roles.joinToString(",")
        prefs[keySessionPersist] = session.persistUnwrapKey
        prefs[keySessionEncrypt] = session.encryptPouch
        prefs[keySessionTtl] = session.keyTtlSeconds.toString()
        prefs[keySessionLoginAt] = session.loggedInAtMillis.toString()
        prefs.remove(keySessionPouch)
        prefs[keySessionDevices] = encodeDevices(session.devices)
    }

    private fun encodeDevices(rows: List<PadDevice>): String =
        rows.joinToString("\n") { "${it.id}\u001f${it.name}\u001f${it.deviceSerial}\u001f${it.shortCode}" }

    private fun decodeDevices(raw: String): List<PadDevice> {
        if (raw.isBlank()) return emptyList()
        return raw.lineSequence().mapNotNull { line ->
            val p = line.split('\u001f')
            if (p.size < 4) return@mapNotNull null
            PadDevice(p[0], p[1], p[2], p[3])
        }.toList()
    }

    /** 身份必须落盘再返回，避免刚登录就杀进程丢厂址。 */
    private fun persistSync(block: MutablePreferences.() -> Unit) {
        runBlocking { store.edit(block) }
    }

    private fun persistAsync(block: MutablePreferences.() -> Unit) {
        io.launch { store.edit(block) }
    }
}
