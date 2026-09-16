package com.gbndt.shijiaoqi.ui.welding

import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import com.gbndt.shijiaoqi.model.Pose
import com.gbndt.shijiaoqi.ui.teach.TeachSession
import java.util.Locale

@OptIn(ExperimentalFoundationApi::class)
@Composable
fun ToolListDialog(
    teach: TeachSession,
    onDismiss: () -> Unit
) {
    if (!teach.isToolListDialogVisible) return

    Dialog(
        onDismissRequest = onDismiss,
        properties = DialogProperties(usePlatformDefaultWidth = false)
    ) {
        Card(
            modifier = Modifier
                .fillMaxWidth(0.6f)
                .fillMaxHeight(0.95f)
                .padding(16.dp),
            shape = RoundedCornerShape(16.dp),
        ) {
            Column(
                modifier = Modifier
                    .fillMaxSize()
                    .padding(16.dp)
            ) {
                Text(
                    text = "选择工具坐标系",
                    style = MaterialTheme.typography.titleLarge,
                    modifier = Modifier.padding(bottom = 16.dp)
                )

                LazyColumn(
                    modifier = Modifier.weight(1f)
                ) {
                    items(14) { index ->
                        val pose = teach.toolCoordinates[index]
                        val remark = teach.toolRemarks[index]
                        val isSelected = teach.toolCoordinateSystem == "工具${index + 1}"
                        
                        Card(
                            modifier = Modifier
                                .fillMaxWidth()
                                .padding(vertical = 2.dp)
                                .combinedClickable(
                                    onClick = { teach.selectTool(index) },
                                    onLongClick = { 
                                        if (pose != null) {
                                            teach.openToolEdit(index)
                                        }
                                    }
                                ),
                            colors = CardDefaults.cardColors(
                                containerColor = if (isSelected) MaterialTheme.colorScheme.primaryContainer else MaterialTheme.colorScheme.surfaceVariant
                            )
                        ) {
                            Column(
                                modifier = Modifier
                                    .fillMaxWidth()
                                    .padding(12.dp)
                            ) {
                                Row(
                                    modifier = Modifier.fillMaxWidth(),
                                    verticalAlignment = Alignment.CenterVertically,
                                    horizontalArrangement = Arrangement.SpaceBetween
                                ) {
                                    Text(
                                        text = "工具${index + 1}",
                                        fontWeight = FontWeight.Bold,
                                        fontSize = 16.sp
                                    )
                                    if (remark.isNotEmpty()) {
                                        Text(
                                            text = remark,
                                            fontSize = 14.sp,
                                            color = MaterialTheme.colorScheme.onSurfaceVariant
                                        )
                                    }
                                }
                                Spacer(modifier = Modifier.height(4.dp))
                                if (pose != null) {
                                    Text(
                                        text = "X:%.1f Y:%.1f Z:%.1f RX:%.1f RY:%.1f RZ:%.1f".format(
                                            Locale.US, 
                                            pose.x, pose.y, pose.z, 
                                            pose.rx, pose.ry, pose.rz
                                        ),
                                        fontSize = 13.sp,
                                        color = MaterialTheme.colorScheme.primary,
                                        fontWeight = FontWeight.SemiBold
                                    )
                                } else {
                                    Text(
                                        text = "未设置 (点击设置)",
                                        fontSize = 12.sp,
                                        color = Color.Gray
                                    )
                                }
                            }
                        }
                    }
                }
                
                Spacer(modifier = Modifier.height(16.dp))
                
                Button(
                    onClick = onDismiss,
                    modifier = Modifier.fillMaxWidth()
                ) {
                    Text("关闭")
                }
            }
        }
    }
}

@Composable
fun PositionSelectionDialog(
    teach: TeachSession,
    onDismiss: () -> Unit
) {
    if (!teach.isPositionDialogVisible) return

    val options = listOf("前", "后", "左", "右")

    Dialog(onDismissRequest = onDismiss) {
        Card(
            modifier = Modifier
                .width(200.dp)
                .padding(16.dp),
            shape = RoundedCornerShape(16.dp),
        ) {
            Column(
                modifier = Modifier.padding(16.dp),
                horizontalAlignment = Alignment.CenterHorizontally
            ) {
                Text(
                    text = "选择位置",
                    style = MaterialTheme.typography.titleMedium,
                    modifier = Modifier.padding(bottom = 16.dp)
                )

                options.forEach { option ->
                    val isSelected = teach.positionMode == option
                    Button(
                        onClick = { teach.setPosition(option) },
                        modifier = Modifier.fillMaxWidth().padding(vertical = 4.dp),
                        colors = ButtonDefaults.buttonColors(
                            containerColor = if (isSelected) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.secondaryContainer,
                            contentColor = if (isSelected) MaterialTheme.colorScheme.onPrimary else MaterialTheme.colorScheme.onSecondaryContainer
                        )
                    ) {
                        Text(option)
                    }
                }
            }
        }
    }
}

@Composable
fun SpeedSelectionDialog(
    teach: TeachSession,
    onDismiss: () -> Unit
) {
    if (!teach.isSpeedDialogVisible) return

    val options = listOf("1倍", "3倍", "5倍")

    Dialog(onDismissRequest = onDismiss) {
        Card(
            modifier = Modifier
                .width(200.dp)
                .padding(16.dp),
            shape = RoundedCornerShape(16.dp),
        ) {
            Column(
                modifier = Modifier.padding(16.dp),
                horizontalAlignment = Alignment.CenterHorizontally
            ) {
                Text(
                    text = "选择速度",
                    style = MaterialTheme.typography.titleMedium,
                    modifier = Modifier.padding(bottom = 16.dp)
                )

                options.forEach { option ->
                    val isSelected = teach.speedMode == option
                    Button(
                        onClick = { teach.setSpeed(option) },
                        modifier = Modifier.fillMaxWidth().padding(vertical = 4.dp),
                        colors = ButtonDefaults.buttonColors(
                            containerColor = if (isSelected) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.secondaryContainer,
                            contentColor = if (isSelected) MaterialTheme.colorScheme.onPrimary else MaterialTheme.colorScheme.onSecondaryContainer
                        )
                    ) {
                        Text(option)
                    }
                }
            }
        }
    }
}

@Composable
fun InstallPosSelectionDialog(
    teach: TeachSession,
    onDismiss: () -> Unit
) {
    if (!teach.isInstallPosDialogVisible) return

    val options = listOf("平装" to 0, "侧装" to 1, "挂装" to 2)

    Dialog(onDismissRequest = onDismiss) {
        Card(
            modifier = Modifier
                .width(200.dp)
                .padding(16.dp),
            shape = RoundedCornerShape(16.dp),
        ) {
            Column(
                modifier = Modifier.padding(16.dp),
                horizontalAlignment = Alignment.CenterHorizontally
            ) {
                Text(
                    text = "选择安装方式",
                    style = MaterialTheme.typography.titleMedium,
                    modifier = Modifier.padding(bottom = 16.dp)
                )

                options.forEach { (label, pos) ->
                    val isSelected = teach.installPos == pos
                    Button(
                        onClick = { teach.updateInstallPos(pos) },
                        modifier = Modifier.fillMaxWidth().padding(vertical = 4.dp),
                        colors = ButtonDefaults.buttonColors(
                            containerColor = if (isSelected) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.secondaryContainer,
                            contentColor = if (isSelected) MaterialTheme.colorScheme.onPrimary else MaterialTheme.colorScheme.onSecondaryContainer
                        )
                    ) {
                        Text(label)
                    }
                }
            }
        }
    }
}

@Composable
fun ToolEditDialog(
    teach: TeachSession
) {
    if (!teach.isToolEditDialogVisible) return

    var x by remember { mutableStateOf(teach.editingToolPose.x.toString()) }
    var y by remember { mutableStateOf(teach.editingToolPose.y.toString()) }
    var z by remember { mutableStateOf(teach.editingToolPose.z.toString()) }
    var rx by remember { mutableStateOf(teach.editingToolPose.rx.toString()) }
    var ry by remember { mutableStateOf(teach.editingToolPose.ry.toString()) }
    var rz by remember { mutableStateOf(teach.editingToolPose.rz.toString()) }
    var remark by remember { mutableStateOf(teach.editingToolRemark) }

    Dialog(
        onDismissRequest = { teach.cancelToolEdit() },
        properties = DialogProperties(usePlatformDefaultWidth = false)
    ) {
        Card(
            modifier = Modifier
                .fillMaxWidth(0.6f) // Increased width slightly
                .padding(10.dp),
            shape = RoundedCornerShape(16.dp),
        ) {
            Column(
                modifier = Modifier
                    .padding(16.dp)
                    .verticalScroll(rememberScrollState())
            ) {
                Text(
                    text = "修改工具${teach.editingToolIndex + 1}参数",
                    style = MaterialTheme.typography.titleLarge,
                    modifier = Modifier.padding(bottom = 16.dp)
                )
                
                // Remark Input
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(bottom = 16.dp),
                    verticalAlignment = Alignment.CenterVertically
                ) {
                    Text(
                        text = "备注",
                        modifier = Modifier.width(40.dp),
                        fontWeight = FontWeight.Bold
                    )
                    OutlinedTextField(
                        value = remark,
                        onValueChange = { remark = it },
                        modifier = Modifier
                            .weight(1f)
                            .height(56.dp),
                        singleLine = true,
                        placeholder = { Text("输入工具备注信息") }
                    )
                }

                Row(modifier = Modifier.fillMaxWidth()) {
                    Column(modifier = Modifier.weight(1f)) {
                        CoordinateInputRow("X", x) { x = it }
                        CoordinateInputRow("Y", y) { y = it }
                        CoordinateInputRow("Z", z) { z = it }
                    }
                    Spacer(modifier = Modifier.width(16.dp))
                    Column(modifier = Modifier.weight(1f)) {
                        CoordinateInputRow("RX", rx) { rx = it }
                        CoordinateInputRow("RY", ry) { ry = it }
                        CoordinateInputRow("RZ", rz) { rz = it }
                    }
                }

                Spacer(modifier = Modifier.height(24.dp))

                Row(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.End
                ) {
                    TextButton(onClick = { teach.cancelToolEdit() }) {
                        Text("取消")
                    }
                    Spacer(modifier = Modifier.width(8.dp))
                    Button(onClick = {
                        val newPose = Pose(
                            x.toDoubleOrNull() ?: 0.0,
                            y.toDoubleOrNull() ?: 0.0,
                            z.toDoubleOrNull() ?: 0.0,
                            rx.toDoubleOrNull() ?: 0.0,
                            ry.toDoubleOrNull() ?: 0.0,
                            rz.toDoubleOrNull() ?: 0.0
                        )
                        teach.saveToolEdit(newPose, remark)
                    }) {
                        Text("确定")
                    }
                }
            }
        }
    }
}

@Composable
fun CoordinateInputRow(
    label: String,
    value: String,
    onValueChange: (String) -> Unit
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(vertical = 2.dp),
        verticalAlignment = Alignment.CenterVertically
    ) {
        Text(
            text = label,
            modifier = Modifier.width(40.dp),
            fontWeight = FontWeight.Bold
        )
        OutlinedTextField(
            value = value,
            onValueChange = onValueChange,
            modifier = Modifier
                .weight(1f)
                .height(50.dp), // Fix height to reduce size
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
            singleLine = true
        )
    }
}
