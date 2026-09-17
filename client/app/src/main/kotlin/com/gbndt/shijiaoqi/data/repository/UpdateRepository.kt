package com.gbndt.shijiaoqi.data.repository

import android.content.Context
import com.gbndt.shijiaoqi.data.pouch.Digest
import com.gbndt.shijiaoqi.data.session.BagSession
import com.gbndt.shijiaoqi.data.update.UpdateManager
import com.gbndt.shijiaoqi.model.UpdateInfo
import dagger.hilt.android.qualifiers.ApplicationContext
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.io.File
import javax.inject.Inject
import javax.inject.Singleton

/** 软件更新的唯一出口：问厂服、下载、安装。 */
@Singleton
class UpdateRepository @Inject constructor(
    @ApplicationContext context: Context,
    private val session: BagSession,
) {
    private val manager = UpdateManager(context)

    fun loggedIn(): Boolean = session.loggedIn

    fun welding(): Boolean = session.isWelding()

    /** 厂服有更高版本才返回；未登录或没有则空。 */
    suspend fun checkUpdate(): UpdateInfo? = withContext(Dispatchers.IO) {
        val remote = session.pendingClientSoftware() ?: return@withContext null
        if (remote.version <= manager.currentVersionCode()) return@withContext null
        UpdateInfo(
            versionCode = remote.version.toInt(),
            versionName = remote.versionName,
            digest = remote.digest,
            description = "厂服下发的客户端包",
        )
    }

    /** 从厂服拉 APK，摘要不对不落盘。 */
    suspend fun downloadApk(info: UpdateInfo): File? = withContext(Dispatchers.IO) {
        val body = session.pullClientApk(info.versionCode.toLong())
        if (!Digest.match(body, info.digest)) return@withContext null
        manager.saveApk(body, "ShiJiaoQi_v${info.versionCode}.apk")
    }

    fun installApk(file: File) = manager.installApk(file)
}
