package com.gbndt.shijiaoqi.ui.project

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import com.gbndt.shijiaoqi.model.WeldProcess

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ProcessEditorDialog(
    isVisible: Boolean,
    onDismiss: () -> Unit,
    initialProcess: WeldProcess,
    onSave: (WeldProcess) -> Unit
) {
    // 使用副本进行编辑，避免直接修改原对象
    var editingProcess by remember(initialProcess) { mutableStateOf(initialProcess.copy(oscillation = initialProcess.oscillation.copy())) }

    if (isVisible) {
        Dialog(
            onDismissRequest = onDismiss,
            properties = DialogProperties(usePlatformDefaultWidth = false)
        ) {
            Surface(
                modifier = Modifier
                    .fillMaxSize(),
                shape = RoundedCornerShape(0.dp),
                color = MaterialTheme.colorScheme.surface,
                tonalElevation = 6.dp
            ) {
                Column(
                    modifier = Modifier
                        .padding(16.dp)
                        .fillMaxSize()
                ) {
                    // Header with Actions
                    Row(
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(bottom = 8.dp),
                        horizontalArrangement = Arrangement.SpaceBetween,
                        verticalAlignment = Alignment.CenterVertically
                    ) {
                        Text(
                            text = "工艺参数编辑",
                            style = MaterialTheme.typography.titleLarge,
                            fontWeight = FontWeight.Bold
                        )
                        Row {
                            TextButton(
                                onClick = onDismiss,
                                modifier = Modifier.padding(end = 8.dp)
                            ) {
                                Text("取消")
                            }
                            Button(
                                onClick = {
                                    onSave(editingProcess)
                                }
                            ) {
                                Text("保存")
                            }
                        }
                    }

                    // Content
                    Column(
                        modifier = Modifier
                            .weight(1f)
                            .verticalScroll(rememberScrollState()),
                        verticalArrangement = Arrangement.spacedBy(8.dp)
                    ) {
                        // 工艺基本信息
                        Card(
                            colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.5f))
                        ) {
                            Row(
                                modifier = Modifier
                                    .fillMaxWidth()
                                    .padding(8.dp),
                                verticalAlignment = Alignment.CenterVertically,
                                horizontalArrangement = Arrangement.spacedBy(8.dp)
                            ) {
                                Text("工艺名称:", fontWeight = FontWeight.Bold, fontSize = 14.sp)
                                BasicTextField(
                                    value = editingProcess.name,
                                    onValueChange = { editingProcess = editingProcess.copy(name = it) },
                                    modifier = Modifier
                                        .weight(1f)
                                        .height(36.dp)
                                        .border(
                                            BorderStroke(1.dp, MaterialTheme.colorScheme.outline),
                                            RoundedCornerShape(4.dp)
                                        )
                                        .background(Color.Transparent),
                                    textStyle = LocalTextStyle.current.copy(
                                        fontSize = 14.sp,
                                        color = MaterialTheme.colorScheme.onSurface
                                    ),
                                    singleLine = true,
                                    cursorBrush = SolidColor(MaterialTheme.colorScheme.primary),
                                    decorationBox = { innerTextField ->
                                        Box(
                                            modifier = Modifier
                                                .fillMaxSize()
                                                .padding(horizontal = 8.dp),
                                            contentAlignment = Alignment.CenterStart
                                        ) {
                                            if (editingProcess.name.isEmpty()) {
                                                 Text("名称", fontSize = 12.sp, color = MaterialTheme.colorScheme.onSurfaceVariant)
                                            }
                                            innerTextField()
                                        }
                                    }
                                )
                            }
                        }

                        // 参数区域
                        Row(
                            modifier = Modifier.fillMaxWidth(),
                            horizontalArrangement = Arrangement.spacedBy(16.dp)
                        ) {
                            // 第一列：位置参数
                            Column(modifier = Modifier.weight(1f)) {
                                Text(
                                    "位置参数",
                                    fontSize = 16.sp,
                                    fontWeight = FontWeight.Bold,
                                    color = MaterialTheme.colorScheme.primary
                                )
                                Spacer(modifier = Modifier.height(8.dp))
                                
                                ProcessParamRow("X", editingProcess.offsetX, { editingProcess = editingProcess.copy(offsetX = it) }, "mm")
                                ProcessParamRow("Y", editingProcess.offsetY, { editingProcess = editingProcess.copy(offsetY = it) }, "mm")
                                ProcessParamRow("Z", editingProcess.offsetZ, { editingProcess = editingProcess.copy(offsetZ = it) }, "mm")
                            }

                            // 第二列：焊接参数
                            Column(modifier = Modifier.weight(1f)) {
                                Text(
                                    "焊接参数",
                                    fontSize = 16.sp,
                                    fontWeight = FontWeight.Bold,
                                    color = MaterialTheme.colorScheme.primary
                                )
                                Spacer(modifier = Modifier.height(8.dp))
                                
                                ProcessParamRow("焊接电流", editingProcess.current, { editingProcess = editingProcess.copy(current = it) }, "A")
                                ProcessParamRow("焊接电压", editingProcess.voltage, { editingProcess = editingProcess.copy(voltage = it) }, "V")
                                ProcessParamRow("焊接速度", editingProcess.speed, { editingProcess = editingProcess.copy(speed = it) }, "mm/s")
                                ProcessParamRow("起弧时间", editingProcess.startArcTime, { editingProcess = editingProcess.copy(startArcTime = it) }, "ms")
                                ProcessParamRow("收弧时间", editingProcess.endArcTime, { editingProcess = editingProcess.copy(endArcTime = it) }, "ms")
                                ProcessParamRow("起弧电流", editingProcess.startArcCurrent, { editingProcess = editingProcess.copy(startArcCurrent = it) }, "A")
                                ProcessParamRow("起弧电压", editingProcess.startArcVoltage, { editingProcess = editingProcess.copy(startArcVoltage = it) }, "V")
                                ProcessParamRow("收弧电流", editingProcess.endArcCurrent, { editingProcess = editingProcess.copy(endArcCurrent = it) }, "A")
                                ProcessParamRow("收弧电压", editingProcess.endArcVoltage, { editingProcess = editingProcess.copy(endArcVoltage = it) }, "V")
                            }

                            // 第三列：摆动参数
                            Column(modifier = Modifier.weight(2f)) {
                                Text(
                                    "摆动参数",
                                    fontSize = 16.sp,
                                    fontWeight = FontWeight.Bold,
                                    color = MaterialTheme.colorScheme.primary
                                )
                                Spacer(modifier = Modifier.height(8.dp))

                                ProcessParamDropdown(
                                    label = "摆动类型",
                                    value = editingProcess.oscillation.type,
                                    options = listOf(
                                        "无摆动", "三角波摆动", "直角L型三角波摆动",
                                        "圆形摆动-顺时针", "圆形摆动-逆时针",
                                        "正弦波摆动", "垂直L型正弦波摆动", "立焊三角摆动"
                                    ),
                                    onValueChange = { editingProcess = editingProcess.copy(oscillation = editingProcess.oscillation.copy(type = it)) }
                                )

                                Row(
                                    modifier = Modifier.fillMaxWidth(),
                                    horizontalArrangement = Arrangement.spacedBy(16.dp)
                                ) {
                                    // 摆动参数 - 左列
                                    Column(modifier = Modifier.weight(1f)) {
                                        ProcessParamDropdown(
                                            label = "等待时间",
                                            value = editingProcess.oscillation.waitTime,
                                            options = listOf("不包括", "包括"),
                                            onValueChange = { editingProcess = editingProcess.copy(oscillation = editingProcess.oscillation.copy(waitTime = it)) }
                                        )
                                        ProcessParamDropdown(
                                            label = "位置等待",
                                            value = editingProcess.oscillation.positionWait,
                                            options = listOf("等待时间内位置继续移动", "等待时间内位置静止"),
                                            onValueChange = { editingProcess = editingProcess.copy(oscillation = editingProcess.oscillation.copy(positionWait = it)) }
                                        )
                                        ProcessParamRow("摆动频率", editingProcess.oscillation.frequency, { editingProcess = editingProcess.copy(oscillation = editingProcess.oscillation.copy(frequency = it)) }, "Hz")
                                        ProcessParamRow("摆动幅度", editingProcess.oscillation.amplitude, { editingProcess = editingProcess.copy(oscillation = editingProcess.oscillation.copy(amplitude = it)) }, "mm")
                                        ProcessParamRow("左停时间", editingProcess.oscillation.leftStopTime, { editingProcess = editingProcess.copy(oscillation = editingProcess.oscillation.copy(leftStopTime = it)) }, "ms")
                                        ProcessParamRow("右停时间", editingProcess.oscillation.rightStopTime, { editingProcess = editingProcess.copy(oscillation = editingProcess.oscillation.copy(rightStopTime = it)) }, "ms")
                                    }

                                    // 摆动参数 - 右列
                                    Column(modifier = Modifier.weight(1f)) {
                                        ProcessParamRow("左侧边长", editingProcess.oscillation.leftSideLength, { editingProcess = editingProcess.copy(oscillation = editingProcess.oscillation.copy(leftSideLength = it)) }, "mm")
                                        ProcessParamRow("右侧边长", editingProcess.oscillation.rightSideLength, { editingProcess = editingProcess.copy(oscillation = editingProcess.oscillation.copy(rightSideLength = it)) }, "mm")
                                        ProcessParamRow("零点时间", editingProcess.oscillation.zeroTime, { editingProcess = editingProcess.copy(oscillation = editingProcess.oscillation.copy(zeroTime = it)) }, "ms")
                                        ProcessParamRow("回调比例", editingProcess.oscillation.callbackRatio, { editingProcess = editingProcess.copy(oscillation = editingProcess.oscillation.copy(callbackRatio = it)) }, "%")
                                        ProcessParamRow("方位角", editingProcess.oscillation.azimuth, { editingProcess = editingProcess.copy(oscillation = editingProcess.oscillation.copy(azimuth = it)) }, "°")
                                        ProcessParamRow("侧倾角", editingProcess.oscillation.inclination, { editingProcess = editingProcess.copy(oscillation = editingProcess.oscillation.copy(inclination = it)) }, "°")
                                    }
                                }
                            }
                        }
                    }


                }
            }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ProcessParamDropdown(
    label: String,
    value: String,
    options: List<String>,
    onValueChange: (String) -> Unit
) {
    var expanded by remember { mutableStateOf(false) }

    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(vertical = 2.dp),
        verticalAlignment = Alignment.CenterVertically
    ) {
        Text(
            text = label,
            modifier = Modifier.weight(0.4f),
            fontSize = 12.sp
        )

        ExposedDropdownMenuBox(
            expanded = expanded,
            onExpandedChange = { expanded = it },
            modifier = Modifier.weight(0.4f)
        ) {
            BasicTextField(
                value = value,
                onValueChange = {},
                readOnly = true,
                modifier = Modifier
                    .menuAnchor()
                    .fillMaxWidth()
                    .height(36.dp)
                    .border(
                        BorderStroke(1.dp, MaterialTheme.colorScheme.outline),
                        RoundedCornerShape(4.dp)
                    )
                    .background(Color.Transparent),
                textStyle = LocalTextStyle.current.copy(
                    fontSize = 12.sp,
                    color = MaterialTheme.colorScheme.onSurface
                ),
                singleLine = true,
                decorationBox = { innerTextField ->
                    Row(
                        modifier = Modifier
                            .fillMaxSize()
                            .padding(start = 8.dp, end = 4.dp),
                        verticalAlignment = Alignment.CenterVertically
                    ) {
                        Box(modifier = Modifier.weight(1f)) {
                            innerTextField()
                        }
                        ExposedDropdownMenuDefaults.TrailingIcon(expanded = expanded)
                    }
                }
            )

            ExposedDropdownMenu(
                expanded = expanded,
                onDismissRequest = { expanded = false }
            ) {
                options.forEach { option ->
                    DropdownMenuItem(
                        text = { Text(option, fontSize = 12.sp) },
                        onClick = {
                            onValueChange(option)
                            expanded = false
                        },
                        contentPadding = ExposedDropdownMenuDefaults.ItemContentPadding
                    )
                }
            }
        }

        Spacer(modifier = Modifier.weight(0.2f))
    }
}

@Composable
fun ProcessParamRow(
    label: String,
    value: Double,
    onValueChange: (Double) -> Unit,
    unit: String = ""
) {
    var textStr by remember { mutableStateOf(value.toString()) }

    // Sync logic: if external value changes significantly (not matching our current text), update text
    // This handles external resets while preserving empty string or partial edits like "01"
    val parsed = textStr.toDoubleOrNull() ?: 0.0
    if (kotlin.math.abs(value - parsed) > 0.000001) {
        textStr = value.toString()
    }

    ProcessParamRow(
        label = label,
        value = textStr,
        onValueChange = {
            // Allow digits and empty
            // We can also allow negative sign for offsets
            if (it.isEmpty() || it == "-" || it == "." || it == "-." || it.toDoubleOrNull() != null) {
                textStr = it
                onValueChange(it.toDoubleOrNull() ?: 0.0)
            }
        },
        unit = unit
    )
}

@Composable
fun ProcessParamRow(
    label: String,
    value: String,
    onValueChange: (String) -> Unit,
    unit: String = ""
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(vertical = 2.dp),
        verticalAlignment = Alignment.CenterVertically
    ) {
        Text(
            text = label,
            modifier = Modifier.weight(0.4f),
            fontSize = 12.sp
        )
        BasicTextField(
            value = value,
            onValueChange = onValueChange,
            modifier = Modifier
                .weight(0.4f)
                .height(36.dp)
                .border(
                    BorderStroke(1.dp, MaterialTheme.colorScheme.outline),
                    RoundedCornerShape(4.dp)
                )
                .background(Color.Transparent),
            textStyle = LocalTextStyle.current.copy(
                fontSize = 12.sp,
                color = MaterialTheme.colorScheme.onSurface
            ),
            singleLine = true,
            cursorBrush = SolidColor(MaterialTheme.colorScheme.primary),
            decorationBox = { innerTextField ->
                Box(
                    modifier = Modifier
                        .fillMaxSize()
                        .padding(horizontal = 8.dp),
                    contentAlignment = Alignment.CenterStart
                ) {
                    innerTextField()
                }
            }
        )
        if (unit.isNotEmpty()) {
            Text(
                text = unit,
                modifier = Modifier
                    .weight(0.2f)
                    .padding(start = 4.dp),
                fontSize = 10.sp,
                color = MaterialTheme.colorScheme.onSurfaceVariant
            )
        } else {
            Spacer(modifier = Modifier.weight(0.2f))
        }
    }
}
