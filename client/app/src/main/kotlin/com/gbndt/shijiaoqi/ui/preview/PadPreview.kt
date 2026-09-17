package com.gbndt.shijiaoqi.ui.preview

import androidx.compose.runtime.Composable
import androidx.compose.ui.tooling.preview.Preview
import com.gbndt.shijiaoqi.ui.theme.ShiJiaoQiTheme

/**
 * 工位平板：物理 1080×1920 @ 380dpi。
 * 应用锁横屏，预览按旋转后的 1920×1080 画。
 */
@Preview(
    name = "Pad",
    device = "spec:width=1920px,height=1080px,dpi=380",
    showBackground = true,
    showSystemUi = false,
)
annotation class PadPreview

/** 预览用固定浅色主题，不跟系统动态色。 */
@Composable
fun PadPreviewTheme(content: @Composable () -> Unit) {
    ShiJiaoQiTheme(darkTheme = false, dynamicColor = false, content = content)
}
