package com.gbndt.shijiaoqi.ui.tbar

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.gbndt.shijiaoqi.data.models.WeldProcess
import com.gbndt.shijiaoqi.ui.components.ProcessParamDropdown
import com.gbndt.shijiaoqi.ui.components.ProcessParamRow
import com.gbndt.shijiaoqi.utils.TBarGapFolder
import com.gbndt.shijiaoqi.utils.TBarPass

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun TBarProcessEditorScreen(
    viewModel: TBarViewModel,
    onBack: () -> Unit
) {
    BackHandler { onBack() }

    var folders by remember {
        mutableStateOf(viewModel.loadTBarGapFolders().sortedWith(compareBy({ it.layer }, { it.minGap })))
    }
    var selectedIndex by remember { mutableStateOf(0) }
    var pass by remember { mutableStateOf(TBarPass.ROOT) }

    val selectedFolder = folders.getOrNull(selectedIndex)

    var editing by remember(selectedFolder?.folderPath, pass) {
        mutableStateOf(copyProcess(selectedFolder, pass))
    }
    val sourcePath = when {
        selectedFolder == null -> ""
        pass == TBarPass.ROOT -> selectedFolder.rootPath
        else -> selectedFolder.capPath
    }

    fun reload() {
        folders = viewModel.loadTBarGapFolders().sortedWith(compareBy({ it.layer }, { it.minGap }))
        val folder = folders.getOrNull(selectedIndex.coerceAtMost(folders.lastIndex.coerceAtLeast(0)))
        editing = copyProcess(folder, pass)
    }

    fun save(showApplyToast: Boolean) {
        val folder = selectedFolder ?: return
        viewModel.saveTBarPass(folder, pass, editing)
        if (showApplyToast) {
            viewModel.showToast("已保存，焊接时按坡口缝隙自动匹配打底和盖面")
        }
        reload()
    }

    Column(modifier = Modifier.fillMaxSize().background(Color(0xFFF5F5F5))) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .height(52.dp)
                .background(Color(0xFF1565C0))
                .padding(horizontal = 16.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.SpaceBetween
        ) {
            Text("T排立对接工艺", color = Color.White, fontSize = 20.sp, fontWeight = FontWeight.Bold)
            Text(
                "工艺库  ${viewModel.resolveTBarProcessFolder()}",
                color = Color.White.copy(alpha = 0.85f),
                fontSize = 13.sp
            )
        }

        Row(modifier = Modifier.weight(1f).fillMaxWidth().padding(12.dp), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            Card(
                modifier = Modifier.width(280.dp).fillMaxHeight(),
                colors = CardDefaults.cardColors(containerColor = Color.White),
                shape = RoundedCornerShape(8.dp)
            ) {
                Column(modifier = Modifier.fillMaxSize().padding(12.dp)) {
                    Text("缝隙工艺包", fontWeight = FontWeight.Bold, fontSize = 16.sp, color = Color(0xFF1565C0))
                    Text(
                        "焊接时按坡口缝隙自动匹配打底、盖面。",
                        fontSize = 12.sp,
                        color = Color.Gray,
                        modifier = Modifier.padding(top = 4.dp, bottom = 8.dp)
                    )
                    if (folders.isEmpty()) {
                        Text(
                            "未找到 Standard/6T1.2-T排立对接 下的缝隙文件夹（如 1H-3-4）。请确认工艺管理 → Standard 中已有该工艺包。",
                            fontSize = 13.sp,
                            color = Color(0xFFB00020)
                        )
                    } else {
                        LazyColumn(verticalArrangement = Arrangement.spacedBy(4.dp)) {
                            itemsIndexed(folders, key = { _, item -> item.folderPath }) { index, folder ->
                                val selected = index == selectedIndex
                                Text(
                                    text = folder.displayLabel(),
                                    modifier = Modifier
                                        .fillMaxWidth()
                                        .background(
                                            if (selected) Color(0xFFE3F2FD) else Color.Transparent,
                                            RoundedCornerShape(4.dp)
                                        )
                                        .clickable { selectedIndex = index }
                                        .padding(horizontal = 10.dp, vertical = 10.dp),
                                    fontSize = 15.sp,
                                    fontWeight = if (selected) FontWeight.Bold else FontWeight.Normal,
                                    color = if (selected) Color(0xFF1565C0) else Color(0xFF333333)
                                )
                            }
                        }
                    }
                }
            }

            Card(
                modifier = Modifier.weight(1f).fillMaxHeight(),
                colors = CardDefaults.cardColors(containerColor = Color.White),
                shape = RoundedCornerShape(8.dp)
            ) {
                if (selectedFolder == null) {
                    Box(modifier = Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                        Text("请选择左侧工艺包", color = Color.Gray)
                    }
                } else {
                    Column(modifier = Modifier.fillMaxSize().padding(16.dp)) {
                        Row(
                            modifier = Modifier.fillMaxWidth(),
                            verticalAlignment = Alignment.CenterVertically,
                            horizontalArrangement = Arrangement.spacedBy(12.dp)
                        ) {
                            Text("WeaveSetPara 参数", fontWeight = FontWeight.Bold, fontSize = 18.sp, color = Color(0xFF1565C0))
                            Spacer(modifier = Modifier.weight(1f))
                            Text("当前道", fontSize = 14.sp)
                            var passExpanded by remember { mutableStateOf(false) }
                            ExposedDropdownMenuBox(expanded = passExpanded, onExpandedChange = { passExpanded = it }) {
                                OutlinedTextField(
                                    value = if (pass == TBarPass.ROOT) "打底(第1道)" else "盖面(第2道)",
                                    onValueChange = {},
                                    readOnly = true,
                                    modifier = Modifier.menuAnchor().width(160.dp),
                                    trailingIcon = { ExposedDropdownMenuDefaults.TrailingIcon(passExpanded) },
                                    singleLine = true
                                )
                                ExposedDropdownMenu(expanded = passExpanded, onDismissRequest = { passExpanded = false }) {
                                    DropdownMenuItem(
                                        text = { Text("打底(第1道)") },
                                        onClick = { pass = TBarPass.ROOT; passExpanded = false }
                                    )
                                    DropdownMenuItem(
                                        text = { Text("盖面(第2道)") },
                                        onClick = { pass = TBarPass.CAP; passExpanded = false }
                                    )
                                }
                            }
                        }

                        Text(
                            "来源文件  $sourcePath",
                            fontSize = 12.sp,
                            color = Color.Gray,
                            modifier = Modifier.padding(top = 4.dp, bottom = 8.dp)
                        )

                        key(sourcePath) {
                            Column(
                                modifier = Modifier.weight(1f).verticalScroll(rememberScrollState()),
                                verticalArrangement = Arrangement.spacedBy(4.dp)
                            ) {
                                Row(horizontalArrangement = Arrangement.spacedBy(16.dp)) {
                                    Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                                        ProcessParamDropdown(
                                            label = "摆动类型",
                                            value = editing.oscillation.type,
                                            options = listOf(
                                                "无摆动", "三角波摆动", "直角L型三角波摆动",
                                                "圆形摆动-顺时针", "圆形摆动-逆时针",
                                                "正弦波摆动", "垂直L型正弦波摆动", "立焊三角摆动"
                                            ),
                                            onValueChange = { editing = editing.copy(oscillation = editing.oscillation.copy(type = it)) }
                                        )
                                        ProcessParamDropdown(
                                            label = "等待模式",
                                            value = editing.oscillation.waitTime,
                                            options = listOf("不包括", "包括"),
                                            onValueChange = { editing = editing.copy(oscillation = editing.oscillation.copy(waitTime = it)) }
                                        )
                                        ProcessParamRow("左弦长", editing.oscillation.leftSideLength, { editing = editing.copy(oscillation = editing.oscillation.copy(leftSideLength = it)) }, "mm")
                                        ProcessParamRow("垂点停留", editing.oscillation.zeroTime, { editing = editing.copy(oscillation = editing.oscillation.copy(zeroTime = it)) }, "ms")
                                        ProcessParamRow("左停", editing.oscillation.leftStopTime, { editing = editing.copy(oscillation = editing.oscillation.copy(leftStopTime = it)) }, "ms")
                                        ProcessParamDropdown(
                                            label = "位置等待",
                                            value = editing.oscillation.positionWait,
                                            options = listOf("等待时间内位置继续移动", "等待时间内位置静止"),
                                            onValueChange = { editing = editing.copy(oscillation = editing.oscillation.copy(positionWait = it)) }
                                        )
                                        ProcessParamRow("旋转角", editing.oscillation.inclination, { editing = editing.copy(oscillation = editing.oscillation.copy(inclination = it)) }, "°")
                                        ProcessParamRow("焊接电流", editing.current, { editing = editing.copy(current = it) }, "A")
                                        ProcessParamRow("起弧电流", editing.startArcCurrent, { editing = editing.copy(startArcCurrent = it) }, "A")
                                        ProcessParamRow("收弧电流", editing.endArcCurrent, { editing = editing.copy(endArcCurrent = it) }, "A")
                                    }
                                    Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                                        ProcessParamRow("频率", editing.oscillation.frequency, { editing = editing.copy(oscillation = editing.oscillation.copy(frequency = it)) }, "Hz")
                                        ProcessParamRow("摆幅", editing.oscillation.amplitude, { editing = editing.copy(oscillation = editing.oscillation.copy(amplitude = it)) }, "mm")
                                        ProcessParamRow("右弦长", editing.oscillation.rightSideLength, { editing = editing.copy(oscillation = editing.oscillation.copy(rightSideLength = it)) }, "mm")
                                        ProcessParamRow("圆形回调", editing.oscillation.callbackRatio, { editing = editing.copy(oscillation = editing.oscillation.copy(callbackRatio = it)) }, "%")
                                        ProcessParamRow("右停", editing.oscillation.rightStopTime, { editing = editing.copy(oscillation = editing.oscillation.copy(rightStopTime = it)) }, "ms")
                                        ProcessParamRow("方位角", editing.oscillation.azimuth, { editing = editing.copy(oscillation = editing.oscillation.copy(azimuth = it)) }, "°")
                                        ProcessParamRow("焊速", editing.speed, { editing = editing.copy(speed = it) }, "mm/s")
                                        ProcessParamRow("焊接电压", editing.voltage, { editing = editing.copy(voltage = it) }, "V")
                                        ProcessParamRow("起弧电压", editing.startArcVoltage, { editing = editing.copy(startArcVoltage = it) }, "V")
                                        ProcessParamRow("收弧电压", editing.endArcVoltage, { editing = editing.copy(endArcVoltage = it) }, "V")
                                    }
                                }
                                Spacer(modifier = Modifier.height(8.dp))
                                Text(
                                    text = "${selectedFolder.displayLabel()} / ${if (pass == TBarPass.ROOT) "打底" else "盖面"}\n" +
                                        "摆动 ${editing.oscillation.type}  频率 ${editing.oscillation.frequency}Hz  摆幅 ${editing.oscillation.amplitude}mm\n" +
                                        "电流 ${editing.current}A  电压 ${editing.voltage}V  焊速 ${editing.speed}mm/s  左右停 ${editing.oscillation.leftStopTime.toInt()}/${editing.oscillation.rightStopTime.toInt()}ms",
                                    fontSize = 13.sp,
                                    fontFamily = FontFamily.Monospace,
                                    color = Color(0xFF333333)
                                )
                                ProcessParamRow("收弧时间", editing.endArcTime, { editing = editing.copy(endArcTime = it) }, "ms")
                                ProcessParamRow("起弧时间", editing.startArcTime, { editing = editing.copy(startArcTime = it) }, "ms")
                            }
                        }

                        Row(
                            modifier = Modifier.fillMaxWidth().padding(top = 12.dp),
                            horizontalArrangement = Arrangement.spacedBy(12.dp, Alignment.End)
                        ) {
                            Button(
                                onClick = { save(false) },
                                colors = ButtonDefaults.buttonColors(containerColor = Color(0xFF2E7D32))
                            ) { Text("保存配置") }
                            Button(
                                onClick = { save(true) },
                                colors = ButtonDefaults.buttonColors(containerColor = Color(0xFF1565C0))
                            ) { Text("应用到焊接") }
                            Button(
                                onClick = onBack,
                                colors = ButtonDefaults.buttonColors(containerColor = Color.Gray)
                            ) { Text("关闭") }
                        }
                    }
                }
            }
        }
    }
}

private fun copyProcess(folder: TBarGapFolder?, pass: TBarPass): WeldProcess {
    val src = if (pass == TBarPass.ROOT) folder?.rootProcess else folder?.capProcess
    return src?.copy(oscillation = src.oscillation.copy()) ?: WeldProcess()
}
