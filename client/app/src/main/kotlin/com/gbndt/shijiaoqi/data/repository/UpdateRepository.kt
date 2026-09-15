package com.gbndt.shijiaoqi.data.repository

import android.content.Context
import android.net.Uri
import com.gbndt.shijiaoqi.data.update.UpdateManager
import com.gbndt.shijiaoqi.model.UpdateInfo
import dagger.hilt.android.qualifiers.ApplicationContext
import javax.inject.Inject
import javax.inject.Singleton

/** 软件更新的唯一出口：查版本、下载、安装。 */
@Singleton
class UpdateRepository @Inject constructor(
    @ApplicationContext context: Context,
) {
    private val manager = UpdateManager(context)

    suspend fun checkUpdate(configUrl: String): UpdateInfo? = manager.checkUpdate(configUrl)

    fun downloadApk(url: String, fileName: String = "update.apk"): Long = manager.downloadApk(url, fileName)

    fun getDownloadedUri(downloadId: Long): Uri? = manager.getDownloadedUri(downloadId)

    fun installApk(uri: Uri) = manager.installApk(uri)
}
