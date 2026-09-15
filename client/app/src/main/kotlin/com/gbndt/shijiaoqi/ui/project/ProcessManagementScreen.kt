package com.gbndt.shijiaoqi.ui.project

import androidx.activity.compose.BackHandler
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.KeyboardArrowDown
import androidx.compose.material.icons.filled.Share
import androidx.compose.material.icons.filled.NoteAdd
import androidx.compose.material.icons.filled.FileDownload
import androidx.compose.material3.*
import androidx.compose.foundation.layout.*
import androidx.compose.runtime.*
import androidx.compose.ui.platform.LocalContext
import com.gbndt.shijiaoqi.model.WeldProcess
import com.gbndt.shijiaoqi.ui.welding.WeldViewModelInterface
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.io.File
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.provider.OpenableColumns
import androidx.compose.material.icons.filled.CloudDownload
import androidx.core.content.FileProvider

import androidx.compose.material.icons.filled.Home
import com.gbndt.shijiaoqi.ui.component.FileExplorer

@Composable
fun ProcessManagementScreen(
    viewModel: WeldViewModelInterface,
    onBack: () -> Unit,
    isSelectionMode: Boolean = false
) {
    // 处理系统返回键
    BackHandler {
        if (viewModel.processCurrentPath.isEmpty()) {
            onBack()
        } else {
            viewModel.navigateProcessBack()
        }
    }

    LaunchedEffect(Unit) {
        viewModel.refreshProcessExplorer()
    }

    // 状态管理
    var editingProcess by remember { mutableStateOf<WeldProcess?>(null) }
    var showNewProcessDialog by remember { mutableStateOf(false) }

    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    
    val importLauncher = rememberLauncherForActivityResult(
        contract = ActivityResultContracts.OpenDocument()
    ) { uri: Uri? ->
        uri?.let {
            val fileName = getFileName(context, it) ?: "imported_process.json"
            scope.launch(Dispatchers.IO) {
                if (fileName.endsWith(".zip", ignoreCase = true)) {
                    viewModel.importProcessZip(it)
                } else {
                    viewModel.importProcess(it)
                }
            }
        }
    }

    val isStandardPath = viewModel.processCurrentPath == "Standard" || viewModel.processCurrentPath.startsWith("Standard/")

    FileExplorer(
        currentPath = viewModel.processCurrentPath,
        items = viewModel.processItems,
        onNavigate = { item -> viewModel.navigateProcess(item) },
        onBack = {
             if (viewModel.processCurrentPath.isEmpty()) {
                 onBack()
             } else {
                 viewModel.navigateProcessBack()
             }
        },
        onCreateFolder = if (isStandardPath) null else { { name -> viewModel.createProcessFolder(name) } },
        onSelectItem = { item ->
            if (item.isProcess) {
                if (isSelectionMode) {
                    viewModel.selectProcess(item)
                    onBack()
                } else {
                    // 管理模式：加载文件并打开编辑
                    val process = viewModel.loadProcess(item.path)
                    if (process != null) {
                        editingProcess = process
                    }
                }
            }
        },
        onLongSelectItem = { item ->
            if (item.isProcess) {
                // 长按：总是进入编辑模式（即使在选择模式下）
                val process = viewModel.loadProcess(item.path)
                if (process != null) {
                    editingProcess = process
                }
            }
        },
        onDeleteItem = { item -> 
            if (item.path == "Standard" || item.path.startsWith("Standard/")) {
                // Ignore delete for standard library
            } else {
                viewModel.deleteProcessItem(item) 
            }
        },
        onShareItem = { item ->
            scope.launch(Dispatchers.IO) {
                if (item.isDirectory) {
                    val zipFile = viewModel.exportProcessZip(item)
                    if (zipFile != null) {
                        withContext(Dispatchers.Main) {
                            shareFile(context, zipFile)
                        }
                    }
                } else {
                    val file = viewModel.exportProcess(item)
                    if (file != null) {
                        withContext(Dispatchers.Main) {
                            shareFile(context, file)
                        }
                    }
                }
            }
        },
        title = if (isSelectionMode) "选择工艺" else "工艺管理",
        leadingActions = {
            Row {
                IconButton(onClick = { onBack() }) {
                    Icon(Icons.Filled.Home, contentDescription = "Back to Home")
                }
                if (!isSelectionMode) {
                     if (!isStandardPath) {
                         IconButton(onClick = {
                            importLauncher.launch(arrayOf("*/*")) // Allow all types, check extension later
                        }) {
                            Icon(Icons.Filled.FileDownload, contentDescription = "Import Process")
                        }
                     }
                    IconButton(onClick = {
                        val timestamp = System.currentTimeMillis()
                        viewModel.updateStandardProcessLibrary("http://cdn.gbndt.com/sjqgy/StandardProcesses.zip?t=$timestamp") 
                    }) {
                        Icon(androidx.compose.material.icons.Icons.Filled.CloudDownload, contentDescription = "Update Standard Library")
                    }
                }
            }
        },
        actionButton = {
            if (!isSelectionMode && !isStandardPath) {
                IconButton(onClick = { 
                    // 新建工艺
                    editingProcess = WeldProcess(name = "新工艺")
                }) {
                    Icon(Icons.Filled.NoteAdd, contentDescription = "New Process")
                }
            }
        }
    )

    // 编辑对话框
    if (editingProcess != null) {
        ProcessEditorDialog(
            isVisible = true,
            onDismiss = { editingProcess = null },
            initialProcess = editingProcess!!,
            onSave = { updatedProcess ->
                if (isStandardPath) {
                    viewModel.processCurrentPath = "User"
                    viewModel.saveProcess(updatedProcess)
                    viewModel.refreshProcessExplorer() // Refresh to show contents of User folder
                    android.widget.Toast.makeText(context, "标准库不可直接修改，已自动另存到 User 文件夹", android.widget.Toast.LENGTH_LONG).show()
                } else {
                    viewModel.saveProcess(updatedProcess)
                }
                editingProcess = null
            }
        )
    }
}

private fun getFileName(context: Context, uri: Uri): String? {
    var result: String? = null
    if (uri.scheme == "content") {
        val cursor = context.contentResolver.query(uri, null, null, null, null)
        try {
            if (cursor != null && cursor.moveToFirst()) {
                val index = cursor.getColumnIndex(OpenableColumns.DISPLAY_NAME)
                if (index >= 0) {
                    result = cursor.getString(index)
                }
            }
        } catch (e: Exception) {
            e.printStackTrace()
        } finally {
            cursor?.close()
        }
    }
    if (result == null) {
        result = uri.path
        val cut = result?.lastIndexOf('/')
        if (cut != null && cut != -1) {
            result = result?.substring(cut + 1)
        }
    }
    return result
}

private fun shareFile(context: Context, file: File) {
    try {
        val uri = FileProvider.getUriForFile(
            context,
            "${context.packageName}.fileprovider",
            file
        )
        val intent = Intent(Intent.ACTION_SEND).apply {
            type = "*/*" 
            putExtra(Intent.EXTRA_STREAM, uri)
            addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
        }
        context.startActivity(Intent.createChooser(intent, "分享工艺"))
    } catch (e: Exception) {
        e.printStackTrace()
    }
}
