package com.gbndt.shijiaoqi.ui.project

import android.widget.Toast
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.NoteAdd
import androidx.compose.material.icons.filled.Home
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TextField
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.gbndt.shijiaoqi.data.log.PadLog
import com.gbndt.shijiaoqi.domain.shared.ProcessChoice
import com.gbndt.shijiaoqi.domain.shared.ProjectChoice
import com.gbndt.shijiaoqi.model.WeldProcess
import com.gbndt.shijiaoqi.ui.preview.PadPreview
import com.gbndt.shijiaoqi.ui.preview.PadPreviewTheme
import com.gbndt.shijiaoqi.ui.preview.PadSamples
import com.gbndt.shijiaoqi.ui.theme.FullscreenDialog
import java.util.UUID
import kotlinx.coroutines.launch

@Composable
fun PouchProjectScreen(
    projects: List<ProjectChoice>,
    onRefresh: () -> Unit,
    onOpen: (UUID) -> Unit,
    onBack: () -> Unit,
    onCreate: (String) -> Unit,
    onDelete: (UUID) -> Unit,
) {
    var path by remember { mutableStateOf("") }
    var showNew by remember { mutableStateOf(false) }
    var newName by remember { mutableStateOf("") }

    BackHandler {
        if (path.isEmpty()) onBack() else path = ""
    }
    LaunchedEffect(Unit) { onRefresh() }

    FileExplorer(
        currentPath = path,
        items = PouchFolders.projects(projects, path),
        onNavigate = { path = it.key },
        onBack = { if (path.isEmpty()) onBack() else path = "" },
        onSelectItem = { item ->
            val id = item.assetId ?: return@FileExplorer
            PadLog.info("Pouch", "open project id=$id name=${item.name}")
            onOpen(id)
        },
        onDeleteItem = { item -> item.assetId?.let(onDelete) },
        title = "工程管理",
        leadingActions = {
            IconButton(onClick = onBack) {
                Icon(Icons.Filled.Home, contentDescription = "回到焊道")
            }
        },
        actionButton = {
            if (PouchFolders.isPersonal(path)) {
                IconButton(onClick = { showNew = true }) {
                    Icon(Icons.AutoMirrored.Filled.NoteAdd, contentDescription = "新建工程")
                }
            }
        },
    )

    if (showNew) {
        AlertDialog(
            onDismissRequest = { showNew = false },
            title = { Text("新建工程") },
            text = {
                TextField(
                    value = newName,
                    onValueChange = { newName = it },
                    label = { Text("工程名称") },
                )
            },
            confirmButton = {
                Button(onClick = {
                    if (newName.isNotEmpty()) {
                        onCreate(newName)
                        showNew = false
                        newName = ""
                        onBack()
                    }
                }) { Text("创建") }
            },
            dismissButton = {
                Button(onClick = { showNew = false }) { Text("取消") }
            },
        )
    }
}

@Composable
fun PouchProcessScreen(
    processes: List<ProcessChoice>,
    picking: Boolean,
    onRefresh: () -> Unit,
    onPick: (UUID) -> Unit,
    onBack: () -> Unit,
    onLoad: suspend (UUID) -> WeldProcess?,
    onSave: (UUID?, WeldProcess) -> Unit,
    onDelete: (UUID) -> Unit,
) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var path by remember { mutableStateOf(if (picking) PouchFolders.PERSONAL else "") }
    var editingId by remember { mutableStateOf<UUID?>(null) }
    var editing by remember { mutableStateOf<WeldProcess?>(null) }

    fun toast(msg: String, long: Boolean = false) {
        PadLog.toast(msg)
        Toast.makeText(context, msg, if (long) Toast.LENGTH_LONG else Toast.LENGTH_SHORT).show()
    }

    BackHandler {
        if (path.isEmpty()) onBack() else path = ""
    }
    LaunchedEffect(Unit) { onRefresh() }

    fun openEditor(id: UUID?) {
        if (id == null) {
            editingId = null
            editing = WeldProcess(name = "新工艺")
            return
        }
        scope.launch {
            val loaded = onLoad(id)
            if (loaded == null) {
                toast("工艺打不开")
                return@launch
            }
            editingId = id
            editing = loaded
        }
    }

    FileExplorer(
        currentPath = path,
        items = PouchFolders.processes(processes, path),
        onNavigate = { path = it.key },
        onBack = { if (path.isEmpty()) onBack() else path = "" },
        onSelectItem = { item ->
            val id = item.assetId ?: return@FileExplorer
            PadLog.info("Pouch", "process id=$id name=${item.name} copyable=${item.copyable}")
            when {
                picking -> onPick(id)
                !item.copyable -> toast("保密工艺只能焊接，不能查看或修改")
                else -> openEditor(id)
            }
        },
        onLongSelectItem = { item ->
            val id = item.assetId ?: return@FileExplorer
            if (!item.copyable) {
                toast("保密工艺只能焊接，不能查看或修改")
            } else {
                openEditor(id)
            }
        },
        onDeleteItem = { item -> item.assetId?.let(onDelete) },
        title = if (picking) "选择工艺" else "工艺管理",
        leadingActions = {
            IconButton(onClick = onBack) {
                Icon(Icons.Filled.Home, contentDescription = "回到焊道")
            }
        },
        actionButton = {
            if (!picking && PouchFolders.isPersonal(path)) {
                IconButton(onClick = {
                    PadLog.click("process create")
                    openEditor(null)
                }) {
                    Icon(Icons.AutoMirrored.Filled.NoteAdd, contentDescription = "新建工艺")
                }
            }
        },
    )

    editing?.let { draft ->
        ProcessEditorDialog(
            initial = draft,
            onSave = { saved ->
                if (PouchFolders.isLocked(path)) {
                    toast("不可直接修改，已自动另存到个人级", long = true)
                }
                onSave(editingId, saved)
                editing = null
                editingId = null
            },
            onDismiss = {
                editing = null
                editingId = null
            },
        )
    }
}

@PadPreview
@Composable
private fun PouchProjectScreenPreview() {
    PadPreviewTheme {
        PouchProjectScreen(
            projects = PadSamples.projects,
            onRefresh = {},
            onOpen = { _ -> },
            onBack = {},
            onCreate = {},
            onDelete = {},
        )
    }
}

@PadPreview
@Composable
private fun PouchProcessScreenPreview() {
    PadPreviewTheme {
        PouchProcessScreen(
            processes = PadSamples.processes,
            picking = false,
            onRefresh = {},
            onPick = { _ -> },
            onBack = {},
            onLoad = { null },
            onSave = { _, _ -> },
            onDelete = {},
        )
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
    FullscreenDialog(onDismissRequest = onDismiss) {
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
