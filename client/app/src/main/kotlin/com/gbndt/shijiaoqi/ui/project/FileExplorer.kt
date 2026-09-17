package com.gbndt.shijiaoqi.ui.project

import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.automirrored.filled.List
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material.icons.filled.Home
import androidx.compose.material.icons.filled.Menu
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.Text
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

/** 老示教器文件浏览流程：灰条、返回、删确认。 */
@Composable
fun FileExplorer(
    currentPath: String,
    items: List<ExplorerItem>,
    onNavigate: (ExplorerItem) -> Unit,
    onBack: () -> Unit,
    onSelectItem: (ExplorerItem) -> Unit,
    onLongSelectItem: ((ExplorerItem) -> Unit)? = null,
    onDeleteItem: ((ExplorerItem) -> Unit)? = null,
    title: String,
    leadingActions: @Composable RowScope.() -> Unit = {},
    actionButton: @Composable RowScope.() -> Unit = {},
) {
    var showDeleteDialog by remember { mutableStateOf(false) }
    var itemToDelete by remember { mutableStateOf<ExplorerItem?>(null) }

    Column(modifier = Modifier.fillMaxSize()) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .background(Color(0xFFE0E0E0))
                .padding(8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            IconButton(onClick = onBack) {
                Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "返回")
            }
            Text(
                text = if (currentPath.isEmpty()) title else "$title > $currentPath",
                modifier = Modifier.weight(1f).padding(start = 8.dp),
                fontWeight = FontWeight.Bold,
                maxLines = 1,
            )
            leadingActions()
            actionButton()
        }

        LazyColumn(modifier = Modifier.weight(1f)) {
            items(items, key = { it.key }) { item ->
                FileItemRow(
                    item = item,
                    onClick = {
                        if (item.isDirectory && !item.isProject) {
                            onNavigate(item)
                        } else {
                            onSelectItem(item)
                        }
                    },
                    onLongClick = {
                        if (!item.isDirectory || item.isProject) {
                            onLongSelectItem?.invoke(item)
                        }
                    },
                    onDelete = if (onDeleteItem != null && item.deletable) {
                        {
                            itemToDelete = item
                            showDeleteDialog = true
                        }
                    } else {
                        null
                    },
                )
                HorizontalDivider()
            }
        }
    }

    if (showDeleteDialog && itemToDelete != null) {
        AlertDialog(
            onDismissRequest = { showDeleteDialog = false },
            title = { Text("确认删除") },
            text = { Text("确定要删除 ${itemToDelete?.name} 吗？") },
            confirmButton = {
                Button(
                    onClick = {
                        itemToDelete?.let { onDeleteItem?.invoke(it) }
                        showDeleteDialog = false
                        itemToDelete = null
                    },
                    colors = ButtonDefaults.buttonColors(Color.Red),
                ) { Text("删除") }
            },
            dismissButton = {
                Button(onClick = { showDeleteDialog = false }) { Text("取消") }
            },
        )
    }
}

@OptIn(ExperimentalFoundationApi::class)
@Composable
private fun FileItemRow(
    item: ExplorerItem,
    onClick: () -> Unit,
    onLongClick: (() -> Unit)? = null,
    onDelete: (() -> Unit)? = null,
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .combinedClickable(onClick = onClick, onLongClick = onLongClick)
            .padding(16.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.SpaceBetween,
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Icon(
                imageVector = when {
                    item.isProject -> Icons.AutoMirrored.Filled.List
                    item.isDirectory -> Icons.Default.Home
                    else -> Icons.Default.Menu
                },
                contentDescription = null,
                tint = when {
                    item.isProject -> Color(0xFF4CAF50)
                    item.isDirectory -> Color(0xFFFFC107)
                    else -> Color.Gray
                },
                modifier = Modifier.size(24.dp),
            )
            Spacer(modifier = Modifier.width(16.dp))
            Text(text = item.name, fontSize = 16.sp)
        }
        if (onDelete != null) {
            IconButton(onClick = onDelete) {
                Icon(Icons.Default.Delete, contentDescription = "删除", tint = Color.Gray)
            }
        }
    }
}
