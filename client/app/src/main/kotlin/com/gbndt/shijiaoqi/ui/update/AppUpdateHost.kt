package com.gbndt.shijiaoqi.ui.update

import android.widget.Toast
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.ui.platform.LocalContext
import com.gbndt.shijiaoqi.data.log.PadLog
import com.gbndt.shijiaoqi.ui.component.UpdateDialog

/** 挂在导航外壳上：弹窗和提示，不进焊接 VM。 */
@Composable
fun AppUpdateHost(update: AppUpdateViewModel) {
    val context = LocalContext.current
    LaunchedEffect(update) {
        update.toastEvent.collect {
            PadLog.toast(it)
            Toast.makeText(context, it, Toast.LENGTH_SHORT).show()
        }
    }
    update.pending?.let { info ->
        UpdateDialog(
            updateInfo = info,
            onConfirm = { update.startDownload() },
            onDismiss = { update.dismiss() },
        )
    }
}
