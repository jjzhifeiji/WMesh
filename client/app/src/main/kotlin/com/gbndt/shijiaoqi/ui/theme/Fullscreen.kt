package com.gbndt.shijiaoqi.ui.theme

import android.app.Activity
import android.graphics.Color
import android.graphics.drawable.ColorDrawable
import android.os.Build
import android.view.View
import android.view.Window
import android.view.WindowManager
import androidx.compose.runtime.Composable
import androidx.compose.runtime.SideEffect
import androidx.compose.ui.platform.LocalView
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import androidx.compose.ui.window.DialogWindowProvider
import androidx.core.view.WindowCompat
import androidx.core.view.WindowInsetsCompat
import androidx.core.view.WindowInsetsControllerCompat

/** 系统栏藏进边缘，全屏铺开。 */
@Suppress("DEPRECATION")
fun hideSystemBars(window: Window, view: View = window.decorView) {
    WindowCompat.setDecorFitsSystemWindows(window, false)
    window.statusBarColor = Color.TRANSPARENT
    window.navigationBarColor = Color.TRANSPARENT
    if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) {
        window.attributes = window.attributes.apply {
            layoutInDisplayCutoutMode =
                WindowManager.LayoutParams.LAYOUT_IN_DISPLAY_CUTOUT_MODE_SHORT_EDGES
        }
    }
    val bars = WindowCompat.getInsetsController(window, view)
    bars.systemBarsBehavior = WindowInsetsControllerCompat.BEHAVIOR_SHOW_TRANSIENT_BARS_BY_SWIPE
    bars.hide(WindowInsetsCompat.Type.systemBars())
}

private fun View.findDialogWindow(): Window? {
    var current: Any? = parent
    while (current != null) {
        if (current is DialogWindowProvider) return current.window
        current = (current as? android.view.ViewParent)?.parent
    }
    return null
}

/** 当前窗口（含弹窗）保持全屏。 */
@Composable
fun KeepFullscreen() {
    val view = LocalView.current
    if (view.isInEditMode) return
    SideEffect {
        val dialogWindow = view.findDialogWindow()
        val window = dialogWindow
            ?: (view.context as? Activity)?.window
            ?: return@SideEffect
        hideSystemBars(window, view)
        // 弹窗窗口默认透明，焊道会透过来。
        if (dialogWindow != null) {
            dialogWindow.setBackgroundDrawable(ColorDrawable(0xFFF5F5F5.toInt()))
            dialogWindow.statusBarColor = 0xFFF5F5F5.toInt()
            dialogWindow.navigationBarColor = 0xFFF5F5F5.toInt()
        }
    }
}

/** 弹窗铺满，不给系统栏留缝。 */
fun fullscreenDialogProperties(
    dismissOnBackPress: Boolean = true,
    dismissOnClickOutside: Boolean = true,
    usePlatformDefaultWidth: Boolean = true,
): DialogProperties = DialogProperties(
    dismissOnBackPress = dismissOnBackPress,
    dismissOnClickOutside = dismissOnClickOutside,
    usePlatformDefaultWidth = usePlatformDefaultWidth,
    decorFitsSystemWindows = false,
)

/** 业务弹窗：藏系统栏后再画内容。 */
@Composable
fun FullscreenDialog(
    onDismissRequest: () -> Unit,
    properties: DialogProperties = fullscreenDialogProperties(),
    content: @Composable () -> Unit,
) {
    Dialog(
        onDismissRequest = onDismissRequest,
        properties = DialogProperties(
            dismissOnBackPress = properties.dismissOnBackPress,
            dismissOnClickOutside = properties.dismissOnClickOutside,
            usePlatformDefaultWidth = properties.usePlatformDefaultWidth,
            decorFitsSystemWindows = false,
        ),
    ) {
        KeepFullscreen()
        content()
    }
}
