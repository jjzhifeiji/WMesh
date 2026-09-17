package com.gbndt.shijiaoqi.data.update

import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import com.gbndt.shijiaoqi.data.log.HttpLog
import com.gbndt.shijiaoqi.data.log.PadLog
import androidx.core.content.FileProvider
import com.gbndt.shijiaoqi.config.AppConfig
import com.gbndt.shijiaoqi.model.UpdateInfo
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.json.Json
import java.io.File
import java.net.HttpURLConnection
import java.net.URL

/** 查版本并下 APK 到应用私有目录，再交给系统安装器。 */
class UpdateManager(private val context: Context) {
    private val json = Json { ignoreUnknownKeys = true }

    /** 对照本机版本号，有新包才返回。 */
    suspend fun checkUpdate(configUrl: String): UpdateInfo? {
        return withContext(Dispatchers.IO) {
            try {
                val path = HttpLog.pathOf(configUrl)
                HttpLog.req("GET", path)
                val url = URL(configUrl)
                val connection = url.openConnection() as HttpURLConnection
                connection.requestMethod = "GET"
                connection.connectTimeout = 5000
                connection.readTimeout = 5000
                try {
                    val code = connection.responseCode
                    val text = (if (code in 200..299) connection.inputStream else connection.errorStream)
                        ?.bufferedReader()?.readText().orEmpty()
                    HttpLog.rsp("GET", path, code, text)
                    if (code != HttpURLConnection.HTTP_OK) return@withContext null
                    val info = json.decodeFromString<UpdateInfo>(text)
                    val packageInfo = context.packageManager.getPackageInfo(context.packageName, 0)
                    val currentVersionCode = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) {
                        packageInfo.longVersionCode.toInt()
                    } else {
                        @Suppress("DEPRECATION")
                        packageInfo.versionCode
                    }
                    if (info.versionCode > currentVersionCode) info else null
                } finally {
                    connection.disconnect()
                }
            } catch (e: Exception) {
                HttpLog.fail("GET", HttpLog.pathOf(configUrl), e.javaClass.simpleName)
                PadLog.error("UpdateManager", "check update failed", e)
                null
            }
        }
    }

    /** 下到 filesDir/updates，失败返回 null。 */
    suspend fun downloadApk(url: String, fileName: String = "update.apk"): File? = withContext(Dispatchers.IO) {
        try {
            val dest = privateUpdateFile(context.filesDir, fileName)
            dest.parentFile?.mkdirs()
            val tmp = File(dest.parentFile, dest.name + ".part")
            val path = HttpLog.pathOf(url)
            HttpLog.req("GET", path)
            val connection = URL(url).openConnection() as HttpURLConnection
            connection.connectTimeout = 15_000
            connection.readTimeout = 60_000
            try {
                val code = connection.responseCode
                if (code !in 200..299) {
                    HttpLog.fail("GET", path, "code=$code")
                    return@withContext null
                }
                connection.inputStream.use { input ->
                    tmp.outputStream().use { output -> input.copyTo(output) }
                }
                HttpLog.rspBytes("GET", path, code, tmp.length())
            } finally {
                connection.disconnect()
            }
            if (dest.exists() && !dest.delete()) return@withContext null
            if (!tmp.renameTo(dest)) {
                tmp.copyTo(dest, overwrite = true)
                tmp.delete()
            }
            dest.takeIf { it.isFile && it.length() > 0 }
        } catch (e: Exception) {
            HttpLog.fail("GET", HttpLog.pathOf(url), e.javaClass.simpleName)
            PadLog.error("UpdateManager", "download apk failed", e)
            null
        }
    }

    /** 只把私有文件的读权限临时交给安装器。 */
    fun installApk(file: File) {
        if (!file.exists()) {
            PadLog.error("UpdateManager", "apk missing")
            return
        }
        val uri = FileProvider.getUriForFile(context, "${context.packageName}.fileprovider", file)
        val intent = Intent(Intent.ACTION_VIEW).apply {
            setDataAndType(uri, "application/vnd.android.package-archive")
            addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
            addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
        }
        grantInstallRead(uri)
        try {
            context.startActivity(intent)
        } catch (e: Exception) {
            PadLog.error("UpdateManager", "install start failed", e)
        }
    }

    private fun grantInstallRead(uri: Uri) {
        val flags = Intent.FLAG_GRANT_READ_URI_PERMISSION
        val res = context.packageManager.queryIntentActivities(
            Intent(Intent.ACTION_VIEW).setDataAndType(uri, "application/vnd.android.package-archive"),
            PackageManager.MATCH_DEFAULT_ONLY,
        )
        for (ri in res) {
            context.grantUriPermission(ri.activityInfo.packageName, uri, flags)
        }
    }
}

/** 更新包锁在 filesDir/updates，文件名去掉路径。 */
internal fun privateUpdateFile(filesDir: File, fileName: String): File {
    val dir = File(filesDir, AppConfig.UPDATE_DIR)
    val dest = File(dir, File(fileName).name.ifBlank { "update.apk" })
    val root = filesDir.canonicalFile
    val out = dest.canonicalFile
    check(out.path.startsWith(root.path + File.separator)) { "update file escaped private dir" }
    return dest
}
