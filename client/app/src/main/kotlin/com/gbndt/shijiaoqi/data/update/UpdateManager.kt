package com.gbndt.shijiaoqi.data.update

import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import androidx.core.content.FileProvider
import com.gbndt.shijiaoqi.config.AppConfig
import com.gbndt.shijiaoqi.data.log.PadLog
import java.io.File

/** 把厂服下来的 APK 写进应用私有目录，再交给系统安装器。 */
class UpdateManager(private val context: Context) {
    /** 本机 versionCode，用来跟厂服版本比。 */
    fun currentVersionCode(): Int {
        val packageInfo = context.packageManager.getPackageInfo(context.packageName, 0)
        return if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) {
            packageInfo.longVersionCode.toInt()
        } else {
            @Suppress("DEPRECATION")
            packageInfo.versionCode
        }
    }

    /** 写到 filesDir/updates，失败返回 null。 */
    fun saveApk(bytes: ByteArray, fileName: String = "update.apk"): File? {
        if (bytes.isEmpty()) return null
        return try {
            val dest = privateUpdateFile(context.filesDir, fileName)
            dest.parentFile?.mkdirs()
            val tmp = File(dest.parentFile, dest.name + ".part")
            tmp.outputStream().use { it.write(bytes) }
            if (dest.exists() && !dest.delete()) return null
            if (!tmp.renameTo(dest)) {
                tmp.copyTo(dest, overwrite = true)
                tmp.delete()
            }
            dest.takeIf { it.isFile && it.length() > 0 }
        } catch (e: Exception) {
            PadLog.error("UpdateManager", "save apk failed", e)
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
