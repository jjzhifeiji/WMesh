package com.gbndt.shijiaoqi.ui.components

import androidx.activity.compose.BackHandler
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Home
import androidx.compose.material.icons.filled.NoteAdd
import androidx.compose.material3.*
import androidx.compose.runtime.*
import com.gbndt.shijiaoqi.ui.viewmodel.WeldViewModelInterface

@Composable
fun ProjectManagementScreen(
    viewModel: WeldViewModelInterface,
    onBack: () -> Unit
) {
    // 处理系统返回键
    BackHandler {
        if (viewModel.projectCurrentPath.isEmpty()) {
            onBack()
        } else {
            viewModel.navigateProjectBack()
        }
    }

    LaunchedEffect(Unit) {
        viewModel.refreshProjectExplorer()
    }

    var showNewProjectDialog by remember { mutableStateOf(false) }
    var newProjectName by remember { mutableStateOf("") }

    FileExplorer(
        currentPath = viewModel.projectCurrentPath,
        items = viewModel.projectItems,
        onNavigate = { item -> viewModel.navigateProject(item) },
        onBack = {
             if (viewModel.projectCurrentPath.isEmpty()) {
                 onBack()
             } else {
                 viewModel.navigateProjectBack()
             }
        },
        onCreateFolder = { name -> viewModel.createProjectFolder(name) },
        onSelectItem = { item ->
            if (item.isProject || item.isMultiLayerProject) {
                viewModel.openProject(item.path)
                onBack()
            }
        },
        onDeleteItem = { item -> viewModel.deleteProjectItem(item) },
        title = "工程管理",
        leadingActions = {
            IconButton(onClick = { onBack() }) {
                Icon(Icons.Filled.Home, contentDescription = "Back to Home")
            }
        },
        actionButton = {
            IconButton(onClick = { showNewProjectDialog = true }) {
                Icon(Icons.Filled.NoteAdd, contentDescription = "New Project")
            }
        }
    )

    if (showNewProjectDialog) {
        AlertDialog(
            onDismissRequest = { showNewProjectDialog = false },
            title = { Text("新建工程") },
            text = {
                TextField(
                    value = newProjectName,
                    onValueChange = { newProjectName = it },
                    label = { Text("工程名称") }
                )
            },
            confirmButton = {
                Button(onClick = {
                    if (newProjectName.isNotEmpty()) {
                        viewModel.createProject(newProjectName)
                        showNewProjectDialog = false
                        newProjectName = ""
                        onBack()
                    }
                }) {
                    Text("创建")
                }
            },
            dismissButton = {
                Button(onClick = { showNewProjectDialog = false }) {
                    Text("取消")
                }
            }
        )
    }
}
