@file:Suppress("DEPRECATION")

package com.gbndt.shijiaoqi.ui.welding

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Lock
import androidx.compose.material.icons.filled.LockOpen
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.window.DialogProperties
import com.gbndt.shijiaoqi.ui.theme.FullscreenDialog
import com.gbndt.shijiaoqi.ui.theme.KeepFullscreen
import com.gbndt.shijiaoqi.ui.welding.single.SingleWeldViewModel
import com.gbndt.shijiaoqi.ui.project.ClosureProcessPicker
import androidx.lifecycle.compose.collectAsStateWithLifecycle

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun CornerWeldDialog(
    viewModel: SingleWeldViewModel,
    initialParams: com.gbndt.shijiaoqi.model.single.CornerGroupParams? = null,
    onDismiss: () -> Unit
) {
    val ui by viewModel.uiState.collectAsStateWithLifecycle()
    val weldPaths = ui.weldPaths

    var refPathAIndex by remember(initialParams) { mutableStateOf(if (initialParams != null) weldPaths.indexOfFirst { it.id == initialParams.refPathAId }.takeIf { it != -1 } ?: -1 else -1) }
    var refPathBIndex by remember(initialParams) { mutableStateOf(if (initialParams != null) weldPaths.indexOfFirst { it.id == initialParams.refPathBId }.takeIf { it != -1 } ?: -1 else -1) }

    var layerCount by remember(initialParams) { mutableStateOf(initialParams?.layerCount?.toString() ?: "4") }
    var initialLength by remember(initialParams) { mutableStateOf(initialParams?.initialLength?.toString() ?: "15.0") }
    var upwardOffset by remember(initialParams) { mutableStateOf(initialParams?.upwardOffset?.toString() ?: "2.0") }
    var lengthReduction by remember(initialParams) { mutableStateOf(initialParams?.lengthReduction?.toString() ?: "2.0") }

    var expandedRefA by remember { mutableStateOf(false) }
    var expandedRefB by remember { mutableStateOf(false) }

    var capturedRx by remember(initialParams) { mutableStateOf(initialParams?.torchRx) }
    var capturedRy by remember(initialParams) { mutableStateOf(initialParams?.torchRy) }
    var capturedRz by remember(initialParams) { mutableStateOf(initialParams?.torchRz) }

    val existingPath = remember(initialParams) {
        initialParams?.groupId?.let { gid ->
            weldPaths.firstOrNull { it.cornerGroupParams?.groupId == gid }
        }
    }
    val initProcessId = existingPath?.processId?.takeIf { it.isNotEmpty() }
        ?: initialParams?.processId.orEmpty()
    var selectedProcessId by remember(initialParams) { mutableStateOf(initProcessId) }

    var errorMessage by remember { mutableStateOf("") }
    var showProcessPicker by remember { mutableStateOf(false) }
    var showPasswordDialog by remember { mutableStateOf(false) }
    var passwordInput by remember { mutableStateOf("") }
    var passwordError by remember { mutableStateOf("") }

    val processLabel = when {
        selectedProcessId.isNotEmpty() ->
            ui.shell.pouchProcesses.firstOrNull { it.id.toString() == selectedProcessId }?.name
                ?: existingPath?.process?.name?.takeIf { it.isNotBlank() }
                ?: selectedProcessId
        else -> "未选择（将使用参考焊道 A 的工艺）"
    }

    FullscreenDialog(
        onDismissRequest = onDismiss,
        properties = DialogProperties(usePlatformDefaultWidth = false)
    ) {
        Surface(
            shape = MaterialTheme.shapes.medium,
            color = MaterialTheme.colorScheme.surface,
            modifier = Modifier.width(800.dp)
        ) {
            Column(
                modifier = Modifier.padding(16.dp),
                verticalArrangement = Arrangement.spacedBy(12.dp)
            ) {
                Text("生成包角工艺", style = MaterialTheme.typography.titleLarge)

                Row(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.spacedBy(16.dp)
                ) {
                    Column(
                        modifier = Modifier.weight(1f),
                        verticalArrangement = Arrangement.spacedBy(8.dp)
                    ) {
                        ExposedDropdownMenuBox(
                            expanded = expandedRefA,
                            onExpandedChange = { expandedRefA = !expandedRefA }
                        ) {
                            OutlinedTextField(
                                value = if (refPathAIndex >= 0 && refPathAIndex < weldPaths.size) weldPaths[refPathAIndex].name else "请选择平焊参考焊道",
                                onValueChange = {},
                                readOnly = true,
                                label = { Text("参考焊道 A (平焊)") },
                                trailingIcon = { ExposedDropdownMenuDefaults.TrailingIcon(expanded = expandedRefA) },
                                colors = ExposedDropdownMenuDefaults.outlinedTextFieldColors(),
                                modifier = Modifier.menuAnchor().fillMaxWidth()
                            )
                            ExposedDropdownMenu(
                                expanded = expandedRefA,
                                onDismissRequest = { expandedRefA = false }
                            ) {
                                weldPaths.forEachIndexed { index, path ->
                                    DropdownMenuItem(
                                        text = { Text(path.name) },
                                        onClick = {
                                            refPathAIndex = index
                                            expandedRefA = false
                                        }
                                    )
                                }
                            }
                        }

                        ExposedDropdownMenuBox(
                            expanded = expandedRefB,
                            onExpandedChange = { expandedRefB = it }
                        ) {
                            OutlinedTextField(
                                value = if (refPathBIndex >= 0 && refPathBIndex < weldPaths.size) weldPaths[refPathBIndex].name else "请选择立焊参考焊道",
                                onValueChange = {},
                                readOnly = true,
                                label = { Text("参考焊道 B (立焊)") },
                                trailingIcon = { ExposedDropdownMenuDefaults.TrailingIcon(expanded = expandedRefB) },
                                colors = ExposedDropdownMenuDefaults.outlinedTextFieldColors(),
                                modifier = Modifier.menuAnchor().fillMaxWidth()
                            )
                            ExposedDropdownMenu(
                                expanded = expandedRefB,
                                onDismissRequest = { expandedRefB = false }
                            ) {
                                weldPaths.forEachIndexed { index, path ->
                                    DropdownMenuItem(
                                        text = { Text(path.name) },
                                        onClick = {
                                            refPathBIndex = index
                                            expandedRefB = false
                                        }
                                    )
                                }
                            }
                        }

                        Box(modifier = Modifier.fillMaxWidth()) {
                            OutlinedTextField(
                                value = processLabel,
                                onValueChange = {},
                                readOnly = true,
                                label = { Text("焊接工艺") },
                                modifier = Modifier.fillMaxWidth()
                            )
                            Box(
                                modifier = Modifier
                                    .matchParentSize()
                                    .clickable {
                                        viewModel.refreshPouchLists()
                                        showProcessPicker = true
                                    }
                            )
                        }

                        Button(
                            onClick = {
                                if (viewModel.connectionStatus == "未连接") {
                                    errorMessage = "设备未连接，无法采集姿态"
                                    return@Button
                                }
                                val pose = viewModel.operationPosition
                                capturedRx = pose.rx
                                capturedRy = pose.ry
                                capturedRz = pose.rz
                                errorMessage = ""
                            },
                            modifier = Modifier.fillMaxWidth().height(44.dp),
                            colors = ButtonDefaults.buttonColors(containerColor = Color(0xFF009688))
                        ) {
                            Text("采集当前姿态", fontSize = 15.sp)
                        }

                        val rx = capturedRx
                        val ry = capturedRy
                        val rz = capturedRz
                        if (rx != null && ry != null && rz != null) {
                            Text(
                                text = String.format(
                                    java.util.Locale.US,
                                    "已采集  Rx=%.2f  Ry=%.2f  Rz=%.2f",
                                    rx, ry, rz
                                ),
                                fontSize = 13.sp,
                                color = Color(0xFF00695C)
                            )
                        } else {
                            Text(
                                text = "请将焊枪摆到包角起点/终点所需姿态后点击采集",
                                fontSize = 12.sp,
                                color = Color(0xFF757575)
                            )
                        }
                    }

                    Column(
                        modifier = Modifier.weight(1f),
                        verticalArrangement = Arrangement.spacedBy(8.dp)
                    ) {
                        Row(
                            modifier = Modifier.fillMaxWidth(),
                            horizontalArrangement = Arrangement.SpaceBetween,
                            verticalAlignment = Alignment.CenterVertically
                        ) {
                            Text("几何参数", style = MaterialTheme.typography.titleMedium)
                            TextButton(onClick = {
                                if (viewModel.isCornerParamsUnlocked) {
                                    viewModel.lockCornerParams()
                                } else {
                                    passwordInput = ""
                                    passwordError = ""
                                    showPasswordDialog = true
                                }
                            }) {
                                Icon(
                                    imageVector = if (viewModel.isCornerParamsUnlocked) Icons.Filled.LockOpen else Icons.Filled.Lock,
                                    contentDescription = null,
                                    modifier = Modifier.size(18.dp)
                                )
                                Spacer(modifier = Modifier.width(4.dp))
                                Text(if (viewModel.isCornerParamsUnlocked) "锁定" else "权限管理")
                            }
                        }

                        if (viewModel.isCornerParamsUnlocked) {
                            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                                OutlinedTextField(
                                    value = layerCount,
                                    onValueChange = { layerCount = it },
                                    label = { Text("层数") },
                                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                                    modifier = Modifier.weight(1f)
                                )
                                OutlinedTextField(
                                    value = initialLength,
                                    onValueChange = { initialLength = it },
                                    label = { Text("底层长度 L") },
                                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                                    modifier = Modifier.weight(1f)
                                )
                            }

                            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                                OutlinedTextField(
                                    value = upwardOffset,
                                    onValueChange = { upwardOffset = it },
                                    label = { Text("每层向上 ΔZ") },
                                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                                    modifier = Modifier.weight(1f)
                                )
                                OutlinedTextField(
                                    value = lengthReduction,
                                    onValueChange = { lengthReduction = it },
                                    label = { Text("长度递减 ΔL") },
                                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                                    modifier = Modifier.weight(1f)
                                )
                            }
                        } else {
                            Column(
                                modifier = Modifier
                                    .fillMaxWidth()
                                    .background(Color(0xFFF5F5F5), RoundedCornerShape(8.dp))
                                    .padding(16.dp),
                                horizontalAlignment = Alignment.CenterHorizontally,
                                verticalArrangement = Arrangement.spacedBy(8.dp)
                            ) {
                                Icon(
                                    imageVector = Icons.Filled.Lock,
                                    contentDescription = null,
                                    tint = Color(0xFF757575),
                                    modifier = Modifier.size(32.dp)
                                )
                                Text(
                                    text = "点击「权限管理」输入密码后可查看和修改",
                                    fontSize = 12.sp,
                                    color = Color(0xFF9E9E9E)
                                )
                            }
                        }
                    }
                }

                if (errorMessage.isNotEmpty()) {
                    Text(errorMessage, color = Color.Red, fontSize = 12.sp)
                }

                Row(
                    horizontalArrangement = Arrangement.End,
                    modifier = Modifier.fillMaxWidth()
                ) {
                    TextButton(onClick = onDismiss) {
                        Text("取消")
                    }
                    Spacer(modifier = Modifier.width(8.dp))
                    Button(onClick = {
                        try {
                            if (refPathAIndex == -1 || refPathBIndex == -1) {
                                errorMessage = "请选择参考焊道A和参考焊道B"
                                return@Button
                            }
                            if (capturedRx == null || capturedRy == null || capturedRz == null) {
                                errorMessage = "请先采集当前机械臂姿态，用于包角起点和终点"
                                return@Button
                            }

                            val layers = layerCount.toInt()
                            val L = initialLength.toDouble()
                            val dZ = upwardOffset.toDouble()
                            val dL = lengthReduction.toDouble()

                            viewModel.generateCornerWelds(
                                refPathAIndex = refPathAIndex,
                                refPathBIndex = refPathBIndex,
                                layerCount = layers,
                                initialLength = L,
                                upwardOffset = dZ,
                                lengthReduction = dL,
                                updateGroupId = initialParams?.groupId,
                                torchRx = capturedRx,
                                torchRy = capturedRy,
                                torchRz = capturedRz,
                                processId = selectedProcessId.takeIf { it.isNotEmpty() }
                            )
                            onDismiss()
                        } catch (e: Exception) {
                            errorMessage = "参数格式错误: ${e.message}"
                        }
                    }) {
                        Text(if (initialParams != null) "更新" else "生成")
                    }
                }
            }
        }
    }

    ClosureProcessPicker(
        visible = showProcessPicker,
        processes = ui.shell.pouchProcesses,
        onPick = { id ->
            selectedProcessId = id?.toString().orEmpty()
            showProcessPicker = false
            errorMessage = ""
        },
        onDismiss = { showProcessPicker = false },
    )

    if (showPasswordDialog) {
        AlertDialog(
            onDismissRequest = { showPasswordDialog = false },
            title = { KeepFullscreen(); Text("权限管理") },
            text = {
                Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    Text("请输入密码后查看和修改几何参数", fontSize = 14.sp)
                    OutlinedTextField(
                        value = passwordInput,
                        onValueChange = {
                            passwordInput = it
                            passwordError = ""
                        },
                        label = { Text("密码") },
                        singleLine = true,
                        visualTransformation = PasswordVisualTransformation(),
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password),
                        isError = passwordError.isNotEmpty(),
                        modifier = Modifier.fillMaxWidth()
                    )
                    if (passwordError.isNotEmpty()) {
                        Text(passwordError, color = Color.Red, fontSize = 12.sp)
                    }
                }
            },
            confirmButton = {
                Button(onClick = {
                    if (viewModel.unlockCornerParams(passwordInput)) {
                        showPasswordDialog = false
                        passwordInput = ""
                        passwordError = ""
                    } else {
                        passwordError = "密码错误"
                    }
                }) {
                    Text("确定")
                }
            },
            dismissButton = {
                TextButton(onClick = { showPasswordDialog = false }) {
                    Text("取消")
                }
            }
        )
    }
}
