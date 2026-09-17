package com.gbndt.shijiaoqi.ui.welding

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.*
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.window.DialogProperties
import com.gbndt.shijiaoqi.ui.theme.FullscreenDialog
import java.util.Locale
import com.gbndt.shijiaoqi.ui.component.ActionButton

@Composable
fun FineTuneDialog(fineTune: FineTuneSupport) {
    if (!fineTune.isDialogVisible) return

    FullscreenDialog(
        onDismissRequest = { },
        properties = DialogProperties(
            dismissOnBackPress = false,
            dismissOnClickOutside = false,
            usePlatformDefaultWidth = false
        )
    ) {
        Surface(
            modifier = Modifier.fillMaxSize(),
            color = Color(0xFFF5F5F5)
        ) {
            Column(modifier = Modifier.fillMaxSize()) {
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .height(52.dp)
                        .background(Color(0xFF1565C0))
                        .padding(horizontal = 16.dp),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.SpaceBetween
                ) {
                    Text("焊接微调", color = Color.White, fontSize = 20.sp, fontWeight = FontWeight.Bold)
                    Button(
                        onClick = { fineTune.close() },
                        colors = ButtonDefaults.buttonColors(containerColor = Color.White)
                    ) {
                        Text("关闭", color = Color(0xFF1565C0), fontWeight = FontWeight.Bold)
                    }
                }

                Row(
                    modifier = Modifier
                        .fillMaxSize()
                        .padding(16.dp),
                    horizontalArrangement = Arrangement.spacedBy(16.dp)
                ) {
                    Column(
                        modifier = Modifier
                            .weight(1f)
                            .fillMaxHeight()
                            .verticalScroll(rememberScrollState()),
                        verticalArrangement = Arrangement.spacedBy(10.dp)
                    ) {
                        InfoCard("电流 / 电压") {
                            Text(
                                text = String.format(
                                    Locale.US,
                                    "反馈  电流 %.1f A    电压 %.1f V",
                                    fineTune.liveCurrent,
                                    fineTune.liveVoltage
                                ),
                                fontSize = 15.sp,
                                fontFamily = FontFamily.Monospace
                            )
                            Text(
                                text = String.format(
                                    Locale.US,
                                    "设定  电流 %.1f A    电压 %.1f V",
                                    fineTune.setCurrent,
                                    fineTune.setVoltage
                                ),
                                fontSize = 15.sp,
                                fontFamily = FontFamily.Monospace,
                                fontWeight = FontWeight.Bold
                            )
                        }
                        InfoCard("摆动") {
                            Text("类型: ${fineTune.oscillationType}", fontSize = 15.sp)
                            Text(
                                text = String.format(
                                    Locale.US,
                                    "摆幅 %.0f mm    频率 %.0f Hz",
                                    fineTune.amplitude,
                                    fineTune.frequency
                                ),
                                fontSize = 15.sp,
                                fontFamily = FontFamily.Monospace
                            )
                        }
                        InfoCard("速度 / 位置") {
                            Text("SetSpeed(${fineTune.speedPercent})", fontSize = 15.sp)
                            Text(
                                text = String.format(
                                    Locale.US,
                                    "摆动偏移  X=%.1f  Y=%.1f  Z=%.1f",
                                    fineTune.weaveOffsetX,
                                    fineTune.weaveOffsetY,
                                    fineTune.weaveOffsetZ
                                ),
                                fontSize = 15.sp,
                                fontFamily = FontFamily.Monospace
                            )
                            Text("偏移步进 ${fineTune.offsetStepMm} mm", fontSize = 14.sp, color = Color.Gray)
                        }
                    }

                    Column(
                        modifier = Modifier
                            .weight(1.1f)
                            .fillMaxHeight()
                            .verticalScroll(rememberScrollState()),
                        verticalArrangement = Arrangement.spacedBy(8.dp)
                    ) {
                        ButtonRow {
                            ActionButton("电流 +", onClick = { fineTune.increaseCurrent() }, modifier = Modifier.weight(1f), height = 52.dp, containerColor = Color(0xFFE65100))
                            ActionButton("电流 -", onClick = { fineTune.decreaseCurrent() }, modifier = Modifier.weight(1f), height = 52.dp, containerColor = Color(0xFFE65100))
                        }
                        ButtonRow {
                            ActionButton("电压 +", onClick = { fineTune.increaseVoltage() }, modifier = Modifier.weight(1f), height = 52.dp, containerColor = Color(0xFFEF6C00))
                            ActionButton("电压 -", onClick = { fineTune.decreaseVoltage() }, modifier = Modifier.weight(1f), height = 52.dp, containerColor = Color(0xFFEF6C00))
                        }
                        ButtonRow {
                            ActionButton("摆幅 +", onClick = { fineTune.increaseAmplitude() }, modifier = Modifier.weight(1f), height = 52.dp, containerColor = Color(0xFF00897B))
                            ActionButton("摆幅 -", onClick = { fineTune.decreaseAmplitude() }, modifier = Modifier.weight(1f), height = 52.dp, containerColor = Color(0xFF00897B))
                        }
                        ButtonRow {
                            ActionButton("频率 +", onClick = { fineTune.increaseFrequency() }, modifier = Modifier.weight(1f), height = 52.dp, containerColor = Color(0xFF00897B))
                            ActionButton("频率 -", onClick = { fineTune.decreaseFrequency() }, modifier = Modifier.weight(1f), height = 52.dp, containerColor = Color(0xFF00897B))
                        }
                        ButtonRow {
                            ActionButton("速度增加", onClick = { fineTune.increaseSpeed() }, modifier = Modifier.weight(1f), height = 52.dp)
                            ActionButton("速度减小", onClick = { fineTune.decreaseSpeed() }, modifier = Modifier.weight(1f), height = 52.dp)
                        }
                        Row(
                            verticalAlignment = Alignment.CenterVertically,
                            horizontalArrangement = Arrangement.spacedBy(8.dp)
                        ) {
                            Text("偏移步进", fontSize = 13.sp)
                            OutlinedTextField(
                                value = fineTune.offsetStepMm.toString(),
                                onValueChange = { fineTune.offsetStepMm = it.toDoubleOrNull() ?: fineTune.offsetStepMm },
                                modifier = Modifier.width(110.dp).height(52.dp),
                                singleLine = true,
                                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Decimal),
                                suffix = { Text("mm", fontSize = 12.sp) }
                            )
                        }
                        ButtonRow {
                            ActionButton("X 正向", onClick = { fineTune.sendOffset('X', true) }, modifier = Modifier.weight(1f), height = 52.dp)
                            ActionButton("X 反向", onClick = { fineTune.sendOffset('X', false) }, modifier = Modifier.weight(1f), height = 52.dp)
                        }
                        ButtonRow {
                            ActionButton("Y 正向", onClick = { fineTune.sendOffset('Y', true) }, modifier = Modifier.weight(1f), height = 52.dp)
                            ActionButton("Y 反向", onClick = { fineTune.sendOffset('Y', false) }, modifier = Modifier.weight(1f), height = 52.dp)
                        }
                        ButtonRow {
                            ActionButton("Z 正向", onClick = { fineTune.sendOffset('Z', true) }, modifier = Modifier.weight(1f), height = 52.dp)
                            ActionButton("Z 反向", onClick = { fineTune.sendOffset('Z', false) }, modifier = Modifier.weight(1f), height = 52.dp)
                        }
                    }
                }
            }
        }
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
            modifier = Modifier.fillMaxWidth().padding(14.dp),
            verticalArrangement = Arrangement.spacedBy(6.dp)
        ) {
            Text(title, fontWeight = FontWeight.Bold, fontSize = 16.sp, color = Color(0xFF1565C0))
            content()
        }
    }
}
