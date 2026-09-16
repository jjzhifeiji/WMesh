package com.gbndt.shijiaoqi.ui.update

import android.app.DownloadManager
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.gbndt.shijiaoqi.data.repository.UpdateRepository
import com.gbndt.shijiaoqi.model.UpdateInfo
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import javax.inject.Inject

/** 厂内发布清单地址；更新不碰焊接与机械臂。 */
private const val MANIFEST_URL = "http://cdn.gbndt.com/sjqapk/update.json"

/** App 外壳的软件更新：查版本、下载、安装。 */
@HiltViewModel
class AppUpdateViewModel @Inject constructor(
    private val updates: UpdateRepository,
) : ViewModel() {
    var pending by mutableStateOf<UpdateInfo?>(null)
        private set

    private val _toast = MutableSharedFlow<String>()
    val toastEvent = _toast.asSharedFlow()

    private var downloadId = -1L
    private var downloading = false
    private var polling: Job? = null

    /** 向发布清单问有没有新包。 */
    fun check() {
        viewModelScope.launch {
            val info = updates.checkUpdate(MANIFEST_URL)
            if (info != null) {
                pending = info
            } else {
                _toast.emit("当前已是最新版本")
            }
        }
    }

    /** 关掉发现新版本的弹窗。 */
    fun dismiss() {
        pending = null
    }

    /** 开始下 APK，下完调系统安装。 */
    fun startDownload() {
        val info = pending ?: return
        if (downloading) return
        pending = null
        downloading = true
        viewModelScope.launch { _toast.emit("开始下载更新...") }
        downloadId = updates.downloadApk(info.downloadUrl, "ShiJiaoQi_v${info.versionCode}.apk")
        polling?.cancel()
        polling = viewModelScope.launch {
            while (isActive && downloading) {
                delay(2000)
                val uri = updates.getDownloadedUri(downloadId)
                if (uri != null) {
                    downloading = false
                    _toast.emit("下载完成，正在准备安装...")
                    updates.installApk(uri)
                    return@launch
                }
                if (updates.downloadStatus(downloadId) == DownloadManager.STATUS_FAILED) {
                    downloading = false
                    _toast.emit("更新下载失败")
                    return@launch
                }
            }
        }
    }

    override fun onCleared() {
        polling?.cancel()
        super.onCleared()
    }
}
