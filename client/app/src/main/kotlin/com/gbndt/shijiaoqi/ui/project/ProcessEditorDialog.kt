package com.gbndt.shijiaoqi.ui.project

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.LocalTextStyle
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.gbndt.shijiaoqi.model.WeldProcess
import com.gbndt.shijiaoqi.ui.theme.FullscreenDialog
import com.gbndt.shijiaoqi.ui.theme.fullscreenDialogProperties

private val oscTypes = listOf(
    "无摆动", "三角波摆动", "直角L型三角波摆动",
    "圆形摆动-顺时针", "圆形摆动-逆时针",
    "正弦波摆动", "垂直L型正弦波摆动", "立焊三角摆动",
)

/** 工艺参数编辑：位置 / 焊接 / 摆动三列，跟老示教器同一页。 */
@Composable
fun ProcessEditorDialog(
    initial: WeldProcess,
    onSave: (WeldProcess) -> Unit,
    onDismiss: () -> Unit,
) {
    var editing by remember(initial) {
        mutableStateOf(initial.copy(oscillation = initial.oscillation.copy()))
    }

    FullscreenDialog(
        onDismissRequest = onDismiss,
        properties = fullscreenDialogProperties(usePlatformDefaultWidth = false),
    ) {
        Surface(
            modifier = Modifier.fillMaxSize(),
            shape = RoundedCornerShape(0.dp),
            color = MaterialTheme.colorScheme.surface,
            tonalElevation = 6.dp,
        ) {
            Column(
                modifier = Modifier.padding(16.dp).fillMaxSize(),
            ) {
                Row(
                    modifier = Modifier.fillMaxWidth().padding(bottom = 8.dp),
                    horizontalArrangement = Arrangement.SpaceBetween,
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text(
                        text = "工艺参数编辑",
                        style = MaterialTheme.typography.titleLarge,
                        fontWeight = FontWeight.Bold,
                    )
                    Row {
                        TextButton(onClick = onDismiss, modifier = Modifier.padding(end = 8.dp)) {
                            Text("取消")
                        }
                        Button(onClick = { onSave(editing) }) { Text("保存") }
                    }
                }

                Column(
                    modifier = Modifier.weight(1f).verticalScroll(rememberScrollState()),
                    verticalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    Card(
                        colors = CardDefaults.cardColors(
                            containerColor = MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.5f),
                        ),
                    ) {
                        Row(
                            modifier = Modifier.fillMaxWidth().padding(8.dp),
                            verticalAlignment = Alignment.CenterVertically,
                            horizontalArrangement = Arrangement.spacedBy(8.dp),
                        ) {
                            Text("工艺名称:", fontWeight = FontWeight.Bold, fontSize = 14.sp)
                            BasicTextField(
                                value = editing.name,
                                onValueChange = { editing = editing.copy(name = it) },
                                modifier = Modifier
                                    .weight(1f)
                                    .height(36.dp)
                                    .border(
                                        BorderStroke(1.dp, MaterialTheme.colorScheme.outline),
                                        RoundedCornerShape(4.dp),
                                    )
                                    .background(Color.Transparent),
                                textStyle = LocalTextStyle.current.copy(
                                    fontSize = 14.sp,
                                    color = MaterialTheme.colorScheme.onSurface,
                                ),
                                singleLine = true,
                                cursorBrush = SolidColor(MaterialTheme.colorScheme.primary),
                                decorationBox = { innerTextField ->
                                    Box(
                                        modifier = Modifier.fillMaxSize().padding(horizontal = 8.dp),
                                        contentAlignment = Alignment.CenterStart,
                                    ) {
                                        if (editing.name.isEmpty()) {
                                            Text("名称", fontSize = 12.sp, color = MaterialTheme.colorScheme.onSurfaceVariant)
                                        }
                                        innerTextField()
                                    }
                                },
                            )
                        }
                    }

                    Row(
                        modifier = Modifier.fillMaxWidth(),
                        horizontalArrangement = Arrangement.spacedBy(16.dp),
                    ) {
                        Column(modifier = Modifier.weight(1f)) {
                            Text("位置参数", fontSize = 16.sp, fontWeight = FontWeight.Bold, color = MaterialTheme.colorScheme.primary)
                            Spacer(modifier = Modifier.height(8.dp))
                            ProcessParamRow("X", editing.offsetX, { editing = editing.copy(offsetX = it) }, "mm")
                            ProcessParamRow("Y", editing.offsetY, { editing = editing.copy(offsetY = it) }, "mm")
                            ProcessParamRow("Z", editing.offsetZ, { editing = editing.copy(offsetZ = it) }, "mm")
                        }

                        Column(modifier = Modifier.weight(1f)) {
                            Text("焊接参数", fontSize = 16.sp, fontWeight = FontWeight.Bold, color = MaterialTheme.colorScheme.primary)
                            Spacer(modifier = Modifier.height(8.dp))
                            ProcessParamRow("焊接电流", editing.current, { editing = editing.copy(current = it) }, "A")
                            ProcessParamRow("焊接电压", editing.voltage, { editing = editing.copy(voltage = it) }, "V")
                            ProcessParamRow("焊接速度", editing.speed, { editing = editing.copy(speed = it) }, "mm/s")
                            ProcessParamRow("起弧时间", editing.startArcTime, { editing = editing.copy(startArcTime = it) }, "ms")
                            ProcessParamRow("收弧时间", editing.endArcTime, { editing = editing.copy(endArcTime = it) }, "ms")
                            ProcessParamRow("起弧电流", editing.startArcCurrent, { editing = editing.copy(startArcCurrent = it) }, "A")
                            ProcessParamRow("起弧电压", editing.startArcVoltage, { editing = editing.copy(startArcVoltage = it) }, "V")
                            ProcessParamRow("收弧电流", editing.endArcCurrent, { editing = editing.copy(endArcCurrent = it) }, "A")
                            ProcessParamRow("收弧电压", editing.endArcVoltage, { editing = editing.copy(endArcVoltage = it) }, "V")
                        }

                        Column(modifier = Modifier.weight(2f)) {
                            Text("摆动参数", fontSize = 16.sp, fontWeight = FontWeight.Bold, color = MaterialTheme.colorScheme.primary)
                            Spacer(modifier = Modifier.height(8.dp))
                            ProcessParamDropdown(
                                label = "摆动类型",
                                value = editing.oscillation.type,
                                options = oscTypes,
                                onValueChange = {
                                    editing = editing.copy(oscillation = editing.oscillation.copy(type = it))
                                },
                            )
                            Row(
                                modifier = Modifier.fillMaxWidth(),
                                horizontalArrangement = Arrangement.spacedBy(16.dp),
                            ) {
                                Column(modifier = Modifier.weight(1f)) {
                                    ProcessParamDropdown(
                                        label = "等待时间",
                                        value = editing.oscillation.waitTime,
                                        options = listOf("不包括", "包括"),
                                        onValueChange = {
                                            editing = editing.copy(oscillation = editing.oscillation.copy(waitTime = it))
                                        },
                                    )
                                    ProcessParamDropdown(
                                        label = "位置等待",
                                        value = editing.oscillation.positionWait,
                                        options = listOf("等待时间内位置继续移动", "等待时间内位置静止"),
                                        onValueChange = {
                                            editing = editing.copy(oscillation = editing.oscillation.copy(positionWait = it))
                                        },
                                    )
                                    ProcessParamRow("摆动频率", editing.oscillation.frequency, { editing = editing.copy(oscillation = editing.oscillation.copy(frequency = it)) }, "Hz")
                                    ProcessParamRow("摆动幅度", editing.oscillation.amplitude, { editing = editing.copy(oscillation = editing.oscillation.copy(amplitude = it)) }, "mm")
                                    ProcessParamRow("左停时间", editing.oscillation.leftStopTime, { editing = editing.copy(oscillation = editing.oscillation.copy(leftStopTime = it)) }, "ms")
                                    ProcessParamRow("右停时间", editing.oscillation.rightStopTime, { editing = editing.copy(oscillation = editing.oscillation.copy(rightStopTime = it)) }, "ms")
                                }
                                Column(modifier = Modifier.weight(1f)) {
                                    ProcessParamRow("左侧边长", editing.oscillation.leftSideLength, { editing = editing.copy(oscillation = editing.oscillation.copy(leftSideLength = it)) }, "mm")
                                    ProcessParamRow("右侧边长", editing.oscillation.rightSideLength, { editing = editing.copy(oscillation = editing.oscillation.copy(rightSideLength = it)) }, "mm")
                                    ProcessParamRow("零点时间", editing.oscillation.zeroTime, { editing = editing.copy(oscillation = editing.oscillation.copy(zeroTime = it)) }, "ms")
                                    ProcessParamRow("回调比例", editing.oscillation.callbackRatio, { editing = editing.copy(oscillation = editing.oscillation.copy(callbackRatio = it)) }, "%")
                                    ProcessParamRow("方位角", editing.oscillation.azimuth, { editing = editing.copy(oscillation = editing.oscillation.copy(azimuth = it)) }, "°")
                                    ProcessParamRow("侧倾角", editing.oscillation.inclination, { editing = editing.copy(oscillation = editing.oscillation.copy(inclination = it)) }, "°")
                                }
                            }
                        }
                    }
                }
            }
        }
    }
}
