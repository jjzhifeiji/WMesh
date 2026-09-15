package com.gbndt.shijiaoqi.data.manager

import android.app.DownloadManager
import android.content.BroadcastReceiver
import android.content.ContentResolver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.net.Uri
import android.os.Build
import android.os.Environment
import android.util.Log
import androidx.core.content.FileProvider
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import java.io.File
import java.net.HttpURLConnection
import java.net.URL

@Serializable
data class UpdateInfo(
    val versionCode: Int,
    val versionName: String,
    val downloadUrl: String,
    val description: String
)

class UpdateManager(private val context: Context) {
    private val json = Json { ignoreUnknownKeys = true }

    // Check for updates from a URL (e.g., http://192.168.1.100:8080/update.json)
    suspend fun checkUpdate(configUrl: String): UpdateInfo? {
        return withContext(Dispatchers.IO) {
            try {
                val url = URL(configUrl)
                val connection = url.openConnection() as HttpURLConnection
                connection.requestMethod = "GET"
                connection.connectTimeout = 5000
                connection.readTimeout = 5000
                
                if (connection.responseCode == HttpURLConnection.HTTP_OK) {
                    val text = connection.inputStream.bufferedReader().use { it.readText() }
                    val info = json.decodeFromString<UpdateInfo>(text)
                    
                    // Compare with current version
                    val packageInfo = context.packageManager.getPackageInfo(context.packageName, 0)
                    val currentVersionCode = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) {
                        packageInfo.longVersionCode.toInt()
                    } else {
                        @Suppress("DEPRECATION")
                        packageInfo.versionCode
                    }

                    if (info.versionCode > currentVersionCode) {
                        return@withContext info
                    }
                }
                null
            } catch (e: Exception) {
                Log.e("UpdateManager", "Check update failed", e)
                null
            }
        }
    }

    // Download APK using DownloadManager
    fun downloadApk(url: String, fileName: String = "update.apk"): Long {
        val request = DownloadManager.Request(Uri.parse(url))
            .setTitle("软件更新")
            .setDescription("正在下载新版本...")
            .setNotificationVisibility(DownloadManager.Request.VISIBILITY_VISIBLE_NOTIFY_COMPLETED)
            .setDestinationInExternalPublicDir(Environment.DIRECTORY_DOWNLOADS, fileName)
            .setAllowedOverMetered(true)
            .setAllowedOverRoaming(true)

        val downloadManager = context.getSystemService(Context.DOWNLOAD_SERVICE) as DownloadManager
        return downloadManager.enqueue(request)
    }

    // Install APK from File
    fun installApk(file: File) {
        if (!file.exists()) {
            Log.e("UpdateManager", "APK file not found: ${file.absolutePath}")
            return
        }

        val uri: Uri
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.N) {
            uri = FileProvider.getUriForFile(
                context,
                "${context.packageName}.fileprovider",
                file
            )
        } else {
            uri = Uri.fromFile(file)
        }
        installApk(uri)
    }

    // Install APK from URI
    fun installApk(uri: Uri) {
        var installUri = uri
        // If it is a file:// URI, ensure we use FileProvider on N+
        if (ContentResolver.SCHEME_FILE == uri.scheme && Build.VERSION.SDK_INT >= Build.VERSION_CODES.N) {
            val path = uri.path
            if (path != null) {
                installUri = FileProvider.getUriForFile(
                    context,
                    "${context.packageName}.fileprovider",
                    File(path)
                )
            }
        }

        Log.d("UpdateManager", "Installing APK from URI: $installUri")

        val intent = Intent(Intent.ACTION_VIEW)
        intent.setDataAndType(installUri, "application/vnd.android.package-archive")
        intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        intent.addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_GRANT_WRITE_URI_PERMISSION)
        
        try {
            Log.d("UpdateManager", "Starting install activity with intent: $intent")
            context.startActivity(intent)
        } catch (e: Exception) {
            Log.e("UpdateManager", "Failed to start installation", e)
        }
    }

    // Get the URI of the downloaded file (Robust method)
    fun getDownloadedUri(downloadId: Long): Uri? {
        val downloadManager = context.getSystemService(Context.DOWNLOAD_SERVICE) as DownloadManager
        val query = DownloadManager.Query().setFilterById(downloadId)
        val cursor = downloadManager.query(query)
        try {
            if (cursor.moveToFirst()) {
                val statusIndex = cursor.getColumnIndex(DownloadManager.COLUMN_STATUS)
                if (cursor.getInt(statusIndex) == DownloadManager.STATUS_SUCCESSFUL) {
                    // Use the API to get the URI, which handles content:// vs file:// correctly
                    val uri = downloadManager.getUriForDownloadedFile(downloadId)
                    Log.d("UpdateManager", "Got downloaded URI: $uri")
                    return uri
                }
            }
        } catch (e: Exception) {
            Log.e("UpdateManager", "Get downloaded URI failed", e)
        } finally {
            cursor.close()
        }
        return null
    }

    // 标准工艺库不再解到本机文件树；工艺只从闭包进袋。
    suspend fun downloadAndExtractStandardProcessLibrary(zipUrl: String): Boolean = false

    // Query the actual file path from DownloadManager
    fun queryDownloadedFile(downloadId: Long): File? {
        val downloadManager = context.getSystemService(Context.DOWNLOAD_SERVICE) as DownloadManager
        val query = DownloadManager.Query().setFilterById(downloadId)
        val cursor = downloadManager.query(query)
        
        try {
            if (cursor.moveToFirst()) {
                val statusIndex = cursor.getColumnIndex(DownloadManager.COLUMN_STATUS)
                val status = cursor.getInt(statusIndex)
                Log.d("UpdateManager", "Download Status: $status")

                if (status == DownloadManager.STATUS_SUCCESSFUL) {
                    
                    // 1. Try COLUMN_LOCAL_FILENAME (Deprecated but often works for absolute path)
                    try {
                        val filenameIndex = cursor.getColumnIndex(DownloadManager.COLUMN_LOCAL_FILENAME)
                        if (filenameIndex >= 0) {
                            val filename = cursor.getString(filenameIndex)
                            Log.d("UpdateManager", "COLUMN_LOCAL_FILENAME: $filename")
                            if (filename != null) {
                                return File(filename)
                            }
                        }
                    } catch (e: Exception) {
                         Log.w("UpdateManager", "COLUMN_LOCAL_FILENAME query failed: ${e.message}")
                    }
                    
                    // 2. Try COLUMN_LOCAL_URI
                    val uriIndex = cursor.getColumnIndex(DownloadManager.COLUMN_LOCAL_URI)
                    if (uriIndex >= 0) {
                        val uriString = cursor.getString(uriIndex)
                        Log.d("UpdateManager", "COLUMN_LOCAL_URI: $uriString")
                        if (uriString != null) {
                            val uri = Uri.parse(uriString)
                            Log.d("UpdateManager", "Parsed URI: $uri, Scheme: ${uri.scheme}, Path: ${uri.path}")
                            if (uri.scheme == "file") {
                                return File(uri.path!!)
                            }
                        }
                    }
                } else {
                    val reasonIndex = cursor.getColumnIndex(DownloadManager.COLUMN_REASON)
                    val reason = cursor.getInt(reasonIndex)
                    Log.d("UpdateManager", "Download failed/paused. Reason: $reason")
                }
            } else {
                Log.d("UpdateManager", "Cursor is empty for downloadId: $downloadId")
            }
        } catch (e: Exception) {
            Log.e("UpdateManager", "Query downloaded file failed", e)
        } finally {
            cursor.close()
        }
        return null
    }
    

}
