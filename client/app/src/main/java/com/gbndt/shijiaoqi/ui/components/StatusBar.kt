package com.gbndt.shijiaoqi.ui.components

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.layout.*
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.gbndt.shijiaoqi.ui.viewmodel.WeldViewModelInterface
import java.util.Locale

@Composable
fun CustomStatusBar(
    viewModel: WeldViewModelInterface,
    onProjectClick: () -> Unit
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .height(30.dp)
            .background(Color(0xFF333333)) // Dark background for status bar
            .padding(horizontal = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.SpaceBetween
    ) {
        // 1. Project Name
        StatusBarItem(
            label = "工程",
            value = viewModel.currentProjectName?.substringAfterLast("/") ?: "未打开",
            onClick = onProjectClick
        )

        // 2. Tool Coordinate System
        StatusBarItem(
            label = "工具",
            value = viewModel.toolCoordinateSystem,
            onClick = { viewModel.isToolListDialogVisible = true }
        )

        // 3. Operation Position
        StatusBarItem(
            label = "位置",
            value = viewModel.positionMode,
            onClick = { viewModel.isPositionDialogVisible = true }
        )

        // 4. Simulation Speed
        StatusBarItem(
            label = "速度",
            value = viewModel.speedMode,
            onClick = { viewModel.isSpeedDialogVisible = true }
        )

        // Install Position
        val installPosStr = when (viewModel.installPos) {
            1 -> "侧装"
            2 -> "挂装"
            else -> "平装"
        }
        StatusBarItem(
            label = "安装",
            value = installPosStr,
            onClick = { viewModel.isInstallPosDialogVisible = true }
        )
        
        // External Axis Info
        StatusBarItem(
            label = "外轴使能",
            value = if (viewModel.isExtAxisEnabled) "开启" else "关闭",
            valueColor = if (viewModel.isExtAxisEnabled) Color.Green else Color.Gray,
            onClick = { viewModel.toggleExtAxisEnabled() }
        )

        StatusBarItem(
            label = "外轴位置",
            value = String.format(Locale.US, "%.1f", viewModel.extAxisPos)
        )
        
        StatusBarItem(
            label = "外轴伺服",
            value = if (viewModel.extAxisReady) "就绪" else "未准备",
            valueColor = if (viewModel.extAxisReady) Color.Green else Color.Red
        )

        // 5. Alarm Status
        StatusBarItem(
            label = "报警",
            value = viewModel.alarmStatus,
            valueColor = if (viewModel.alarmStatus == "无报警" || viewModel.alarmStatus == "无故障") Color.Green else Color.Red,
            onClick = { 
                viewModel.resetAllError() 
            }
        )

        // 6. Connection Status
        StatusBarItem(
            label = "连接",
            value = viewModel.connectionStatus,
            valueColor = if (viewModel.connectionStatus == "已连接") Color.Green else Color.Red,
            onClick = {
                if (viewModel.connectionStatus == "未连接") {
                    viewModel.reconnect()
                }
            }
        )

        // 7. Welding Break Off State
        StatusBarItem(
            label = "焊接",
            value = viewModel.weldingBreakOffState,
            valueColor = if (viewModel.weldingBreakOffState == "正常") Color.Green else Color.Red
        )

        // 8. Weld Arc State
        StatusBarItem(
            label = "电弧",
            value = viewModel.weldArcState,
            valueColor = if (viewModel.weldArcState == "正常") Color.Green else Color.Red
        )
    }
}

@OptIn(ExperimentalFoundationApi::class)
@Composable
fun StatusBarItem(
    label: String,
    value: String,
    modifier: Modifier = Modifier,
    labelColor: Color = Color.LightGray,
    valueColor: Color = Color.White,
    onClick: (() -> Unit)? = null,
    onLongClick: (() -> Unit)? = null
) {
    Row(
        modifier = modifier
            .padding(horizontal = 4.dp)
            .then(
                if (onClick != null || onLongClick != null) {
                    Modifier.combinedClickable(
                        onClick = { onClick?.invoke() },
                        onLongClick = onLongClick
                    )
                } else {
                    Modifier
                }
            ),
        verticalAlignment = Alignment.CenterVertically
    ) {

        Text(
            text = "$label:",
            color = labelColor,
            fontSize = 12.sp,
            modifier = Modifier.padding(end = 2.dp)
        )
        Text(
            text = value,
            color = valueColor,
            fontSize = 12.sp,
            fontWeight = androidx.compose.ui.text.font.FontWeight.Bold
        )
    }
}
