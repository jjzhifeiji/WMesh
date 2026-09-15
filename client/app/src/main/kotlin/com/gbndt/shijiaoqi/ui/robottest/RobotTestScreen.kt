package com.gbndt.shijiaoqi.ui.robottest

import android.widget.Toast
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.gbndt.shijiaoqi.model.Oscillation
import com.gbndt.shijiaoqi.model.Pose
import com.gbndt.shijiaoqi.ui.component.ActionButton
import com.gbndt.shijiaoqi.ui.project.ProcessParamDropdown
import com.gbndt.shijiaoqi.ui.project.ProcessParamRow
import java.util.Locale

@Composable
fun RobotTestScreen(
    viewModel: RobotTestViewModel,
    onBack: () -> Unit
) {
    val context = LocalContext.current
    LaunchedEffect(Unit) {
        viewModel.toastEvent.collect { message ->
            Toast.makeText(context, message, Toast.LENGTH_SHORT).show()
        }
    }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .background(Color(0xFFF5F5F5))
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .height(44.dp)
                .background(Color(0xFF333333))
                .padding(horizontal = 12.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.SpaceBetween
        ) {
            TextButton(onClick = onBack) {
                Text("返回", color = Color.White)
            }
            Text("指令测试", color = Color.White, fontSize = 18.sp, fontWeight = FontWeight.Bold)
            Row(horizontalArrangement = Arrangement.spacedBy(16.dp), verticalAlignment = Alignment.CenterVertically) {
                Text(
                    text = "报警 ${viewModel.alarmStatus}",
                    color = if (viewModel.alarmStatus == "无报警" || viewModel.alarmStatus == "无故障") Color(0xFF81C784) else Color(0xFFEF9A9A),
                    fontSize = 13.sp
                )
                Text(
                    text = "连接 ${viewModel.connectionStatus}",
                    color = if (viewModel.connectionStatus == "已连接") Color(0xFF81C784) else Color(0xFFEF9A9A),
                    fontSize = 13.sp,
                    modifier = Modifier.clickable {
                        if (viewModel.connectionStatus != "已连接") viewModel.reconnect()
                    }
                )
            }
        }

        Row(
            modifier = Modifier
                .fillMaxSize()
                .padding(12.dp),
            horizontalArrangement = Arrangement.spacedBy(12.dp)
        ) {
            Column(
                modifier = Modifier
                    .weight(1.15f)
                    .fillMaxHeight()
                    .verticalScroll(rememberScrollState()),
                verticalArrangement = Arrangement.spacedBy(10.dp)
            ) {
                InfoCard(title = "实时位置 / 姿态") {
                    PoseText(viewModel.livePose)
                }
                InfoCard(title = "实时关节角度") {
                    JointsText(viewModel.liveJoints)
                }
                InfoCard(title = "采集起点") {
                    val p = viewModel.startPoint
                    if (p == null) {
                        Text("未采集", color = Color.Gray, fontSize = 13.sp)
                    } else {
                        PoseText(p.pose)
                        Spacer(modifier = Modifier.height(4.dp))
                        JointsText(p.joints)
                    }
                }
                InfoCard(title = "采集终点") {
                    val p = viewModel.endPoint
                    if (p == null) {
                        Text("未采集", color = Color.Gray, fontSize = 13.sp)
                    } else {
                        PoseText(p.pose)
                        Spacer(modifier = Modifier.height(4.dp))
                        JointsText(p.joints)
                    }
                }
                InfoCard(title = "摆动参数") {
                    Text("类型: ${viewModel.oscillation.type}", fontSize = 13.sp)
                    Text(
                        text = String.format(
                            Locale.US,
                            "频率 %.1f Hz   幅度 %.1f mm",
                            viewModel.oscillation.frequency,
                            viewModel.oscillation.amplitude
                        ),
                        fontSize = 13.sp
                    )
                    Text("速度 SetSpeed(${viewModel.speedPercent})    偏移步进 ${viewModel.offsetStepMm} mm", fontSize = 13.sp)
                    Text(
                        text = String.format(
                            Locale.US,
                            "摆动偏移  X=%.1f  Y=%.1f  Z=%.1f",
                            viewModel.weaveOffsetX, viewModel.weaveOffsetY, viewModel.weaveOffsetZ
                        ),
                        fontSize = 13.sp
                    )
                }
            }

            Column(
                modifier = Modifier
                    .weight(1f)
                    .fillMaxHeight()
                    .verticalScroll(rememberScrollState()),
                verticalArrangement = Arrangement.spacedBy(8.dp)
            ) {
                ButtonRow {
                    ActionButton("采集起点", onClick = { viewModel.captureStart() }, modifier = Modifier.weight(1f), height = 52.dp)
                    ActionButton("采集终点", onClick = { viewModel.captureEnd() }, modifier = Modifier.weight(1f), height = 52.dp)
                }
                ButtonRow {
                    ActionButton(
                        "开始运行",
                        onClick = { viewModel.startRun() },
                        modifier = Modifier.weight(1f),
                        height = 52.dp,
                        containerColor = Color(0xFF2E7D32)
                    )
                    ActionButton(
                        "停止运行",
                        onClick = { viewModel.stopRun() },
                        modifier = Modifier.weight(1f),
                        height = 52.dp,
                        containerColor = Color(0xFFC62828)
                    )
                }
                ButtonRow {
                    ActionButton(
                        "设置摆动参数",
                        onClick = { viewModel.showOscillationDialog = true },
                        modifier = Modifier.weight(1f),
                        height = 52.dp,
                        containerColor = Color(0xFF1565C0)
                    )
                }
                ButtonRow {
                    ActionButton("摆幅 +", onClick = { viewModel.increaseAmplitude() }, modifier = Modifier.weight(1f), height = 52.dp, containerColor = Color(0xFF00897B))
                    ActionButton("摆幅 -", onClick = { viewModel.decreaseAmplitude() }, modifier = Modifier.weight(1f), height = 52.dp, containerColor = Color(0xFF00897B))
                }
                ButtonRow {
                    ActionButton("频率 +", onClick = { viewModel.increaseFrequency() }, modifier = Modifier.weight(1f), height = 52.dp, containerColor = Color(0xFF00897B))
                    ActionButton("频率 -", onClick = { viewModel.decreaseFrequency() }, modifier = Modifier.weight(1f), height = 52.dp, containerColor = Color(0xFF00897B))
                }
                ButtonRow {
                    ActionButton("速度增加", onClick = { viewModel.increaseSpeed() }, modifier = Modifier.weight(1f), height = 52.dp)
                    ActionButton("速度减小", onClick = { viewModel.decreaseSpeed() }, modifier = Modifier.weight(1f), height = 52.dp)
                }
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    Text("偏移步进", fontSize = 13.sp)
                    OutlinedTextField(
                        value = viewModel.offsetStepMm.toString(),
                        onValueChange = { viewModel.offsetStepMm = it.toDoubleOrNull() ?: viewModel.offsetStepMm },
                        modifier = Modifier.width(100.dp).height(52.dp),
                        singleLine = true,
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Decimal),
                        suffix = { Text("mm", fontSize = 12.sp) }
                    )
                }
                ButtonRow {
                    ActionButton("X 正向", onClick = { viewModel.sendOffset('X', true) }, modifier = Modifier.weight(1f), height = 52.dp)
                    ActionButton("X 反向", onClick = { viewModel.sendOffset('X', false) }, modifier = Modifier.weight(1f), height = 52.dp)
                }
                ButtonRow {
                    ActionButton("Y 正向", onClick = { viewModel.sendOffset('Y', true) }, modifier = Modifier.weight(1f), height = 52.dp)
                    ActionButton("Y 反向", onClick = { viewModel.sendOffset('Y', false) }, modifier = Modifier.weight(1f), height = 52.dp)
                }
                ButtonRow {
                    ActionButton("Z 正向", onClick = { viewModel.sendOffset('Z', true) }, modifier = Modifier.weight(1f), height = 52.dp)
                    ActionButton("Z 反向", onClick = { viewModel.sendOffset('Z', false) }, modifier = Modifier.weight(1f), height = 52.dp)
                }
                ButtonRow {
                    ActionButton("预留1", onClick = { viewModel.onReservedButton(1) }, modifier = Modifier.weight(1f), height = 52.dp, containerColor = Color(0xFF78909C))
                    ActionButton("预留2", onClick = { viewModel.onReservedButton(2) }, modifier = Modifier.weight(1f), height = 52.dp, containerColor = Color(0xFF78909C))
                }
                ButtonRow {
                    ActionButton("预留3", onClick = { viewModel.onReservedButton(3) }, modifier = Modifier.weight(1f), height = 52.dp, containerColor = Color(0xFF78909C))
                    ActionButton("预留4", onClick = { viewModel.onReservedButton(4) }, modifier = Modifier.weight(1f), height = 52.dp, containerColor = Color(0xFF78909C))
                }
            }
        }
    }

    if (viewModel.showOscillationDialog) {
        OscillationEditDialog(
            initial = viewModel.oscillation,
            onDismiss = { viewModel.showOscillationDialog = false },
            onConfirm = { osc ->
                viewModel.updateOscillation(osc)
                viewModel.showOscillationDialog = false
                viewModel.sendOscillationParams()
            }
        )
    }
}

@Composable
private fun ButtonRow(content: @Composable RowScope.() -> Unit) {
    Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
        content = content
    )
}

@Composable
private fun InfoCard(title: String, content: @Composable ColumnScope.() -> Unit) {
    Card(
        colors = CardDefaults.cardColors(containerColor = Color.White),
        shape = RoundedCornerShape(8.dp)
    ) {
        Column(
            modifier = Modifier.fillMaxWidth().padding(12.dp),
            verticalArrangement = Arrangement.spacedBy(4.dp)
        ) {
            Text(title, fontWeight = FontWeight.Bold, fontSize = 14.sp, color = Color(0xFF1565C0))
            content()
        }
    }
}

@Composable
private fun PoseText(pose: Pose) {
    Text(
        text = String.format(
            Locale.US,
            "X=%8.2f  Y=%8.2f  Z=%8.2f\nRx=%7.2f  Ry=%7.2f  Rz=%7.2f",
            pose.x, pose.y, pose.z, pose.rx, pose.ry, pose.rz
        ),
        fontSize = 13.sp,
        fontFamily = FontFamily.Monospace,
        lineHeight = 18.sp
    )
}

@Composable
private fun JointsText(joints: List<Double>) {
    val values = joints.take(6) + List((6 - joints.size).coerceAtLeast(0)) { 0.0 }
    Text(
        text = String.format(
            Locale.US,
            "J1=%7.2f  J2=%7.2f  J3=%7.2f\nJ4=%7.2f  J5=%7.2f  J6=%7.2f",
            values[0], values[1], values[2], values[3], values[4], values[5]
        ),
        fontSize = 13.sp,
        fontFamily = FontFamily.Monospace,
        lineHeight = 18.sp
    )
}

@Composable
private fun OscillationEditDialog(
    initial: Oscillation,
    onDismiss: () -> Unit,
    onConfirm: (Oscillation) -> Unit
) {
    var editing by remember(initial) { mutableStateOf(initial.copy()) }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("摆动参数") },
        text = {
            Column(
                modifier = Modifier.fillMaxWidth().heightIn(max = 420.dp).verticalScroll(rememberScrollState()),
                verticalArrangement = Arrangement.spacedBy(4.dp)
            ) {
                ProcessParamDropdown(
                    label = "摆动类型",
                    value = editing.type,
                    options = listOf(
                        "无摆动", "三角波摆动", "直角L型三角波摆动",
                        "圆形摆动-顺时针", "圆形摆动-逆时针",
                        "正弦波摆动", "垂直L型正弦波摆动", "立焊三角摆动"
                    ),
                    onValueChange = { editing = editing.copy(type = it) }
                )
                ProcessParamDropdown(
                    label = "等待时间",
                    value = editing.waitTime,
                    options = listOf("不包括", "包括"),
                    onValueChange = { editing = editing.copy(waitTime = it) }
                )
                ProcessParamDropdown(
                    label = "位置等待",
                    value = editing.positionWait,
                    options = listOf("等待时间内位置继续移动", "等待时间内位置静止"),
                    onValueChange = { editing = editing.copy(positionWait = it) }
                )
                ProcessParamRow("摆动频率", editing.frequency, { editing = editing.copy(frequency = it) }, "Hz")
                ProcessParamRow("摆动幅度", editing.amplitude, { editing = editing.copy(amplitude = it) }, "mm")
                ProcessParamRow("左停时间", editing.leftStopTime, { editing = editing.copy(leftStopTime = it) }, "ms")
                ProcessParamRow("右停时间", editing.rightStopTime, { editing = editing.copy(rightStopTime = it) }, "ms")
                ProcessParamRow("左侧边长", editing.leftSideLength, { editing = editing.copy(leftSideLength = it) }, "mm")
                ProcessParamRow("右侧边长", editing.rightSideLength, { editing = editing.copy(rightSideLength = it) }, "mm")
                ProcessParamRow("零点时间", editing.zeroTime, { editing = editing.copy(zeroTime = it) }, "ms")
                ProcessParamRow("回调比例", editing.callbackRatio, { editing = editing.copy(callbackRatio = it) }, "%")
                ProcessParamRow("方位角", editing.azimuth, { editing = editing.copy(azimuth = it) }, "°")
                ProcessParamRow("侧倾角", editing.inclination, { editing = editing.copy(inclination = it) }, "°")
            }
        },
        confirmButton = {
            Button(onClick = { onConfirm(editing) }) { Text("发送") }
        },
        dismissButton = {
            TextButton(onClick = onDismiss) { Text("取消") }
        }
    )
}
