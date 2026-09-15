package com.gbndt.shijiaoqi.ui.components

import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.gbndt.shijiaoqi.data.models.FileSystemItem

import androidx.compose.material.icons.filled.CreateNewFolder

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun FileExplorer(
    currentPath: String,
    items: List<FileSystemItem>,
    onNavigate: (FileSystemItem) -> Unit,
    onBack: () -> Unit,
    onCreateFolder: ((String) -> Unit)? = null,
    onSelectItem: (FileSystemItem) -> Unit,
    onLongSelectItem: ((FileSystemItem) -> Unit)? = null,
    onDeleteItem: ((FileSystemItem) -> Unit)? = null,
    onShareItem: ((FileSystemItem) -> Unit)? = null,
    title: String,
    leadingActions: @Composable RowScope.() -> Unit = {},
    actionButton: @Composable RowScope.() -> Unit = {}
) {
    var showNewFolderDialog by remember { mutableStateOf(false) }
    var newFolderName by remember { mutableStateOf("") }
    var showDeleteDialog by remember { mutableStateOf(false) }
    var itemToDelete by remember { mutableStateOf<FileSystemItem?>(null) }

    Column(modifier = Modifier.fillMaxSize()) {
        // Top Bar
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .background(Color(0xFFE0E0E0))
                .padding(8.dp),
            verticalAlignment = Alignment.CenterVertically
        ) {
            IconButton(onClick = onBack) {
                Icon(Icons.Default.ArrowBack, contentDescription = "Back")
            }
            Text(
                text = if (currentPath.isEmpty()) title else "$title > $currentPath",
                modifier = Modifier.weight(1f).padding(start = 8.dp),
                fontWeight = FontWeight.Bold,
                maxLines = 1
            )
            leadingActions()
            if (onCreateFolder != null) {
                IconButton(onClick = { showNewFolderDialog = true }) {
                    Icon(Icons.Filled.CreateNewFolder, contentDescription = "New Folder")
                }
            }
            actionButton()
        }

        // File List
        LazyColumn(modifier = Modifier.weight(1f)) {
            items(items) { item ->
                FileItemRow(
                    item = item,
                    onClick = {
                        if (item.isDirectory && !item.isProject && !item.isMultiLayerProject) {
                            onNavigate(item)
                        } else {
                            onSelectItem(item)
                        }
                    },
                    onLongClick = {
                        if (!item.isDirectory || item.isProject || item.isMultiLayerProject) {
                            onLongSelectItem?.invoke(item)
                        }
                    },
                    onDelete = if (onDeleteItem != null && !(item.path == "Standard" || item.path.startsWith("Standard/"))) {
                        {
                            itemToDelete = item
                            showDeleteDialog = true
                        }
                    } else null,
                    onShare = if (onShareItem != null) { { onShareItem(item) } } else null
                )
                Divider()
            }
        }
    }

    if (showNewFolderDialog) {
        AlertDialog(
            onDismissRequest = { showNewFolderDialog = false },
            title = { Text("新建文件夹") },
            text = {
                TextField(
                    value = newFolderName,
                    onValueChange = { newFolderName = it },
                    label = { Text("文件夹名称") }
                )
            },
            confirmButton = {
                Button(onClick = {
                    if (newFolderName.isNotEmpty()) {
                        onCreateFolder?.invoke(newFolderName)
                        newFolderName = ""
                        showNewFolderDialog = false
                    }
                }) {
                    Text("创建")
                }
            },
            dismissButton = {
                Button(onClick = { showNewFolderDialog = false }) {
                    Text("取消")
                }
            }
        )
    }

    if (showDeleteDialog && itemToDelete != null) {
        AlertDialog(
            onDismissRequest = { showDeleteDialog = false },
            title = { Text("确认删除") },
            text = { Text("确定要删除 ${itemToDelete?.name} 吗？") },
            confirmButton = {
                Button(onClick = {
                    itemToDelete?.let { onDeleteItem?.invoke(it) }
                    showDeleteDialog = false
                    itemToDelete = null
                }, colors = ButtonDefaults.buttonColors(Color.Red)) {
                    Text("删除")
                }
            },
            dismissButton = {
                Button(onClick = { showDeleteDialog = false }) {
                    Text("取消")
                }
            }
        )
    }
}

@OptIn(ExperimentalFoundationApi::class)
@Composable
fun FileItemRow(
    item: FileSystemItem,
    onClick: () -> Unit,
    onLongClick: (() -> Unit)? = null,
    onDelete: (() -> Unit)? = null,
    onShare: (() -> Unit)? = null
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .combinedClickable(
                onClick = onClick,
                onLongClick = onLongClick
            )
            .padding(16.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.SpaceBetween
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Icon(
                imageVector = when {
                    item.isMultiLayerProject -> Icons.Default.List // Use same icon or different one
                    item.isProject -> Icons.Default.List // Project Icon
                    item.isDirectory -> Icons.Default.Home // Folder Icon
                    else -> Icons.Default.Menu // File Icon
                },
                contentDescription = null,
                tint = if (item.isMultiLayerProject) Color(0xFF2196F3) else if (item.isProject) Color(0xFF4CAF50) else if (item.isDirectory) Color(0xFFFFC107) else Color.Gray,
                modifier = Modifier.size(24.dp)
            )
            Spacer(modifier = Modifier.width(16.dp))
            Text(
                text = if (item.isMultiLayerProject) "${item.name} (多层)" else item.name,
                fontSize = 16.sp
            )
        }
        Row {
            if (onShare != null) {
                IconButton(onClick = onShare) {
                    Icon(Icons.Default.Share, contentDescription = "Share", tint = Color.Gray)
                }
            }
            if (onDelete != null) {
                IconButton(onClick = onDelete) {
                    Icon(Icons.Default.Delete, contentDescription = "Delete", tint = Color.Gray)
                }
            }
        }
    }
}
