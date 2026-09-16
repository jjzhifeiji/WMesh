package com.gbndt.shijiaoqi.ui.welding.tbar

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
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
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.gbndt.shijiaoqi.model.tbar.GapBand
import com.gbndt.shijiaoqi.ui.project.ClosureProcessPicker
import com.gbndt.shijiaoqi.domain.tbar.TBarPass
import com.gbndt.shijiaoqi.domain.tbar.TBarRun
import com.gbndt.shijiaoqi.ui.welding.*

@Composable
fun TBarProcessEditorScreen(
    viewModel: TBarWeldViewModel,
    onBack: () -> Unit,
) {
    BackHandler { onBack() }
    var selectedIndex by remember { mutableStateOf(0) }
    var pass by remember { mutableStateOf(TBarPass.ROOT) }
    var showPicker by remember { mutableStateOf(false) }
    val bands = viewModel.currentGapBands()
    val selected = bands.getOrNull(selectedIndex)

    Column(modifier = Modifier.fillMaxSize().background(Color(0xFFF5F5F5))) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .height(52.dp)
                .background(Color(0xFF1565C0))
                .padding(horizontal = 16.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.SpaceBetween,
        ) {
            Text("T排间隙带", color = Color.White, fontSize = 20.sp, fontWeight = FontWeight.Bold)
            TextButton(onClick = onBack) { Text("返回", color = Color.White) }
        }
        Row(modifier = Modifier.weight(1f).fillMaxWidth().padding(12.dp), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            Card(
                modifier = Modifier.width(280.dp).fillMaxHeight(),
                colors = CardDefaults.cardColors(containerColor = Color.White),
                shape = RoundedCornerShape(8.dp),
            ) {
                Column(modifier = Modifier.fillMaxSize().padding(12.dp)) {
                    Text("工程正文里的间隙带", fontWeight = FontWeight.Bold, fontSize = 16.sp, color = Color(0xFF1565C0))
                    Text(
                        "焊接时按坡口间隙匹配打底、盖面工艺。",
                        fontSize = 12.sp,
                        color = Color.Gray,
                        modifier = Modifier.padding(top = 4.dp, bottom = 8.dp),
                    )
                    if (bands.isEmpty()) {
                        Text("当前工程没有间隙带", fontSize = 13.sp, color = Color(0xFFB00020))
                    } else {
                        LazyColumn(verticalArrangement = Arrangement.spacedBy(4.dp)) {
                            itemsIndexed(bands, key = { _, item -> bandKey(item) }) { index, band ->
                                val on = index == selectedIndex
                                Text(
                                    text = TBarRun.bandLabel(band),
                                    modifier = Modifier
                                        .fillMaxWidth()
                                        .background(if (on) Color(0xFFE3F2FD) else Color.Transparent, RoundedCornerShape(4.dp))
                                        .clickable { selectedIndex = index }
                                        .padding(horizontal = 10.dp, vertical = 10.dp),
                                    fontSize = 15.sp,
                                    fontWeight = if (on) FontWeight.Bold else FontWeight.Normal,
                                    color = if (on) Color(0xFF1565C0) else Color(0xFF333333),
                                )
                            }
                        }
                    }
                }
            }
            Card(
                modifier = Modifier.weight(1f).fillMaxHeight(),
                colors = CardDefaults.cardColors(containerColor = Color.White),
                shape = RoundedCornerShape(8.dp),
            ) {
                if (selected == null) {
                    Box(modifier = Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                        Text("请选择左侧间隙带", color = Color.Gray)
                    }
                } else {
                    Column(modifier = Modifier.fillMaxSize().padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                        Text("工艺引用", fontWeight = FontWeight.Bold, fontSize = 18.sp, color = Color(0xFF1565C0))
                        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                            TextButton(onClick = { pass = TBarPass.ROOT }) {
                                Text(if (pass == TBarPass.ROOT) "打底 ●" else "打底")
                            }
                            TextButton(onClick = { pass = TBarPass.CAP }) {
                                Text(if (pass == TBarPass.CAP) "盖面 ●" else "盖面")
                            }
                        }
                        val id = if (pass == TBarPass.ROOT) selected.rootProcessId else selected.capProcessId
                        val name = viewModel.pouchProcesses.firstOrNull { it.id.toString() == id }?.name
                        Text(
                            if (id.isBlank()) "未选择" else (name ?: id),
                            fontSize = 16.sp,
                        )
                        TextButton(onClick = {
                            viewModel.refreshPouchLists()
                            showPicker = true
                        }) { Text("从闭包选择") }
                    }
                }
            }
        }
    }
    ClosureProcessPicker(
        visible = showPicker,
        processes = viewModel.pouchProcesses,
        onPick = { id ->
            viewModel.bindGapBandProcess(selectedIndex, pass, id)
            showPicker = false
        },
        onDismiss = { showPicker = false },
    )
}

private fun bandKey(band: GapBand): String =
    "${band.layer}-${band.minGap}-${band.maxGap}-${band.rootProcessId}-${band.capProcessId}"
