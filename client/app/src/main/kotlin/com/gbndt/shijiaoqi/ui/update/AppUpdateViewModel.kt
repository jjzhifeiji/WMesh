package com.gbndt.shijiaoqi.ui.update

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.gbndt.shijiaoqi.data.log.PadLog
import com.gbndt.shijiaoqi.data.repository.UpdateRepository
import com.gbndt.shijiaoqi.model.UpdateInfo
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.launch
import javax.inject.Inject

/** App 外壳的软件更新：问厂服、下载、安装。 */
@HiltViewModel
class AppUpdateViewModel @Inject constructor(
    private val updates: UpdateRepository,
) : ViewModel() {
    var pending by mutableStateOf<UpdateInfo?>(null)
        private set

    private val _toast = MutableSharedFlow<String>()
    val toastEvent = _toast.asSharedFlow()

    private var downloading = false

    /** 向本厂问有没有新包。 */
    fun check() {
        PadLog.info("Update", "check start")
        if (!updates.loggedIn()) {
            toast("先登录厂网再检查更新")
            return
        }
        viewModelScope.launch {
            val info = runCatching { updates.checkUpdate() }.getOrElse { e ->
                PadLog.warn("Update", "check failed ${e.message}")
                toast("检查更新失败")
                return@launch
            }
            if (info != null) {
                PadLog.info("Update", "check found version=${info.versionName}")
                pending = info
            } else {
                PadLog.info("Update", "check latest")
                toast("当前已是最新版本")
            }
        }
    }

    /** 关掉发现新版本的弹窗。 */
    fun dismiss() {
        pending = null
    }

    /** 下到私有目录，下完调系统安装。 */
    fun startDownload() {
        val info = pending ?: return
        if (downloading) return
        if (updates.welding()) {
            toast("焊完再更新")
            return
        }
        PadLog.info("Update", "download start version=${info.versionName}")
        pending = null
        downloading = true
        viewModelScope.launch {
            toast("开始下载更新...")
            val file = runCatching { updates.downloadApk(info) }.getOrElse { e ->
                PadLog.warn("Update", "download failed ${e.message}")
                null
            }
            downloading = false
            if (file == null) {
                PadLog.warn("Update", "download failed")
                toast("更新下载失败")
                return@launch
            }
            PadLog.info("Update", "download ok")
            toast("下载完成，正在准备安装...")
            updates.installApk(file)
        }
    }

    private fun toast(msg: String) {
        PadLog.toast(msg)
        viewModelScope.launch { _toast.emit(msg) }
    }
}
