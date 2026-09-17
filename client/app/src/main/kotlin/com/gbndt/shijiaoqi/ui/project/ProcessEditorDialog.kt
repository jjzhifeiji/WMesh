package com.gbndt.shijiaoqi.ui.project

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import com.gbndt.shijiaoqi.model.Oscillation
import com.gbndt.shijiaoqi.model.WeldProcess
import com.gbndt.shijiaoqi.ui.theme.FullscreenDialog

private val oscTypes = listOf(
    "无摆动", "三角波摆动", "直角L型三角波摆动",
    "圆形摆动-顺时针", "圆形摆动-逆时针",
    "正弦波摆动", "垂直L型正弦波摆动", "立焊三角摆动",
)

/** 工艺参数编辑；保存时由袋决定另存个人级还是改自己的。 */
@Composable
fun ProcessEditorDialog(
    title: String,
    initial: WeldProcess,
    onSave: (WeldProcess) -> Unit,
    onDismiss: () -> Unit,
) {
    var name by remember { mutableStateOf(initial.name) }
    var current by remember { mutableStateOf(initial.current) }
    var voltage by remember { mutableStateOf(initial.voltage) }
    var speed by remember { mutableStateOf(initial.speed) }
    var startArcTime by remember { mutableStateOf(initial.startArcTime) }
    var endArcTime by remember { mutableStateOf(initial.endArcTime) }
    var startArcCurrent by remember { mutableStateOf(initial.startArcCurrent) }
    var endArcCurrent by remember { mutableStateOf(initial.endArcCurrent) }
    var startArcVoltage by remember { mutableStateOf(initial.startArcVoltage) }
    var endArcVoltage by remember { mutableStateOf(initial.endArcVoltage) }
    var oscType by remember { mutableStateOf(initial.oscillation.type) }
    var frequency by remember { mutableStateOf(initial.oscillation.frequency) }
    var amplitude by remember { mutableStateOf(initial.oscillation.amplitude) }

    FullscreenDialog(onDismissRequest = onDismiss) {
        Column(
            modifier = Modifier.fillMaxSize().padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            Text(title, style = MaterialTheme.typography.titleLarge)
            Column(
                modifier = Modifier.weight(1f).verticalScroll(rememberScrollState()),
                verticalArrangement = Arrangement.spacedBy(4.dp),
            ) {
                ProcessParamRow("名称", name, { name = it })
                ProcessParamRow("电流", current, { current = it }, "A")
                ProcessParamRow("电压", voltage, { voltage = it }, "V")
                ProcessParamRow("速度", speed, { speed = it }, "mm/s")
                ProcessParamRow("起弧时间", startArcTime, { startArcTime = it }, "ms")
                ProcessParamRow("收弧时间", endArcTime, { endArcTime = it }, "ms")
                ProcessParamRow("起弧电流", startArcCurrent, { startArcCurrent = it }, "A")
                ProcessParamRow("收弧电流", endArcCurrent, { endArcCurrent = it }, "A")
                ProcessParamRow("起弧电压", startArcVoltage, { startArcVoltage = it }, "V")
                ProcessParamRow("收弧电压", endArcVoltage, { endArcVoltage = it }, "V")
                ProcessParamDropdown("摆动类型", oscType, oscTypes) { oscType = it }
                ProcessParamRow("摆动频率", frequency, { frequency = it }, "Hz")
                ProcessParamRow("摆动幅度", amplitude, { amplitude = it }, "mm")
            }
            Row(horizontalArrangement = Arrangement.End, modifier = Modifier.fillMaxWidth()) {
                TextButton(onClick = onDismiss) { Text("取消") }
                Button(
                    onClick = {
                        onSave(
                            initial.copy(
                                name = name,
                                current = current,
                                voltage = voltage,
                                speed = speed,
                                startArcTime = startArcTime,
                                endArcTime = endArcTime,
                                startArcCurrent = startArcCurrent,
                                endArcCurrent = endArcCurrent,
                                startArcVoltage = startArcVoltage,
                                endArcVoltage = endArcVoltage,
                                oscillation = Oscillation(
                                    type = oscType,
                                    waitTime = initial.oscillation.waitTime,
                                    positionWait = initial.oscillation.positionWait,
                                    frequency = frequency,
                                    amplitude = amplitude,
                                    leftStopTime = initial.oscillation.leftStopTime,
                                    rightStopTime = initial.oscillation.rightStopTime,
                                    leftSideLength = initial.oscillation.leftSideLength,
                                    rightSideLength = initial.oscillation.rightSideLength,
                                    zeroTime = initial.oscillation.zeroTime,
                                    callbackRatio = initial.oscillation.callbackRatio,
                                    azimuth = initial.oscillation.azimuth,
                                    inclination = initial.oscillation.inclination,
                                ),
                            ),
                        )
                    },
                ) { Text("保存") }
            }
        }
    }
}
