package com.gbndt.shijiaoqi.ui.components

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.window.Dialog
import com.gbndt.shijiaoqi.weld.ProcessChoice
import com.gbndt.shijiaoqi.weld.ProjectChoice
import java.util.UUID

@Composable
fun PouchProjectScreen(
    projects: List<ProjectChoice>,
    onRefresh: () -> Unit,
    onOpen: (UUID) -> Unit,
    onBack: () -> Unit,
) {
    LaunchedEffect(Unit) { onRefresh() }
    Column(modifier = Modifier.fillMaxSize().padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Text("本机袋工程", style = MaterialTheme.typography.titleLarge)
        if (projects.isEmpty()) {
            Text("还没有缓存的工程闭包", color = MaterialTheme.colorScheme.onSurfaceVariant)
        } else {
            LazyColumn(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                items(projects, key = { it.id }) { row ->
                    PouchProjectRow(row) { onOpen(row.id) }
                }
            }
        }
        Button(onClick = onBack, modifier = Modifier.fillMaxWidth()) { Text("返回") }
    }
}

@Composable
private fun PouchProjectRow(row: ProjectChoice, onOpen: () -> Unit) {
    Card(
        modifier = Modifier.fillMaxWidth().clickable(onClick = onOpen),
        colors = CardDefaults.cardColors(
            containerColor = if (row.active) MaterialTheme.colorScheme.primaryContainer else MaterialTheme.colorScheme.surface
        ),
    ) {
        Column(modifier = Modifier.padding(12.dp)) {
            Text(row.name.ifBlank { row.id.toString() }, fontSize = 16.sp)
            Text(
                if (row.active) "当前激活  修订 ${row.revision}" else "修订 ${row.revision}",
                fontSize = 12.sp,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

@Composable
fun PouchProcessScreen(
    processes: List<ProcessChoice>,
    picking: Boolean,
    onRefresh: () -> Unit,
    onPick: (UUID?) -> Unit,
    onBack: () -> Unit,
) {
    LaunchedEffect(Unit) { onRefresh() }
    Column(modifier = Modifier.fillMaxSize().padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Text(if (picking) "选择闭包工艺" else "当前闭包工艺", style = MaterialTheme.typography.titleLarge)
        if (processes.isEmpty()) {
            Text("当前工程没有工艺成员", color = MaterialTheme.colorScheme.onSurfaceVariant)
        } else {
            LazyColumn(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                items(processes, key = { it.id }) { item ->
                    Card(
                        modifier = Modifier.fillMaxWidth().then(
                            if (picking) Modifier.clickable { onPick(item.id) } else Modifier
                        ),
                    ) {
                        Text(
                            item.name.ifBlank { item.id.toString() },
                            modifier = Modifier.padding(12.dp),
                            fontSize = 16.sp,
                        )
                    }
                }
            }
        }
        if (picking) {
            TextButton(onClick = { onPick(null) }, modifier = Modifier.fillMaxWidth()) { Text("不选工艺") }
        }
        Button(onClick = onBack, modifier = Modifier.fillMaxWidth()) { Text("返回") }
    }
}

@Composable
fun ClosureProcessPicker(
    visible: Boolean,
    processes: List<ProcessChoice>,
    onPick: (UUID?) -> Unit,
    onDismiss: () -> Unit,
) {
    if (!visible) return
    Dialog(onDismissRequest = onDismiss) {
        Card {
            Column(modifier = Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text("闭包工艺", style = MaterialTheme.typography.titleMedium)
                if (processes.isEmpty()) {
                    Text("当前工程没有工艺成员", color = MaterialTheme.colorScheme.onSurfaceVariant)
                } else {
                    processes.forEach { item ->
                        Text(
                            item.name.ifBlank { item.id.toString() },
                            modifier = Modifier.fillMaxWidth().clickable { onPick(item.id) }.padding(vertical = 8.dp),
                            fontSize = 16.sp,
                        )
                    }
                }
                Row(horizontalArrangement = Arrangement.End, modifier = Modifier.fillMaxWidth()) {
                    TextButton(onClick = { onPick(null) }) { Text("不选工艺") }
                    TextButton(onClick = onDismiss) { Text("取消") }
                }
            }
        }
    }
}
