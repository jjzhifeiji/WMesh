package com.gbndt.shijiaoqi.ui.multilayer

import com.gbndt.shijiaoqi.ui.components.*
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.gestures.detectVerticalDragGestures
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.clickable
import androidx.compose.foundation.combinedClickable
import androidx.compose.ui.hapticfeedback.HapticFeedbackType
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.DragHandle
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.zIndex
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.gbndt.shijiaoqi.data.models.WeldPointType
import kotlinx.coroutines.launch
import kotlinx.coroutines.withTimeout
import kotlinx.coroutines.TimeoutCancellationException
import kotlin.math.roundToInt
import androidx.compose.ui.platform.LocalViewConfiguration
import androidx.compose.ui.text.input.KeyboardType

@OptIn(ExperimentalMaterial3Api::class, ExperimentalFoundationApi::class)
@Composable
fun MultiLayerScreen(
    viewModel: MultiLayerViewModel,
    onNavigateToProjectManagement: () -> Unit,
    onNavigateToProcessManagement: (Boolean) -> Unit
) {
    var showDeleteDialog by remember { mutableStateOf(false) }
    var show3DViewer by remember { mutableStateOf(false) }
    var weldPathToDeleteIndex by remember { mutableStateOf(-1) }
    var showProcessPicker by remember { mutableStateOf(false) }

    if (show3DViewer) {
        Path3DViewerDialog(
            weldPaths = viewModel.multiLayerWeldPaths.map { it.basePath },
            onDismiss = { show3DViewer = false }
        )
    }

    Column(modifier = Modifier.fillMaxSize()) {
        // 主内容区，分为左右两部分
        Row(modifier = Modifier.weight(1f)) {
            // 左边部分，包含焊道列表
            Column(modifier = Modifier.weight(1f).fillMaxHeight().background(Color(0xFFF5F5F5))) {
                // 焊道列表
                val listState = rememberLazyListState(
                    initialFirstVisibleItemIndex = viewModel.listScrollIndex,
                    initialFirstVisibleItemScrollOffset = viewModel.listScrollOffset
                )

                LaunchedEffect(listState) {
                    snapshotFlow { listState.firstVisibleItemIndex to listState.firstVisibleItemScrollOffset }
                        .collect { (index, offset) ->
                            viewModel.listScrollIndex = index
                            viewModel.listScrollOffset = offset
                        }
                }

                // Listen for Scroll Events (e.g., when adding new path)
                LaunchedEffect(Unit) {
                    viewModel.scrollToIndexEvent.collect { index ->
                        if (index >= 0) {
                            listState.animateScrollToItem(index)
                        }
                    }
                }

                LazyColumn(
                    modifier = Modifier.fillMaxWidth().weight(1f),
                    state = listState
                ) {
                    items(
                        items = viewModel.multiLayerWeldPaths,
                        key = { it.id }
                    ) { multiPath ->
                        val index = viewModel.multiLayerWeldPaths.indexOf(multiPath)
                        MultiLayerWeldPathItem(
                            multiPath = multiPath,
                            isSelected = index == viewModel.selectedMultiLayerPathIndex,
                            selectedPassIndex = if (index == viewModel.selectedMultiLayerPathIndex) viewModel.selectedPassIndex else -1,
                            onSelect = { 
                                viewModel.selectedMultiLayerPathIndex = index 
                                viewModel.selectedPassIndex = -1
                            },
                            onSelectPass = { passIndex -> 
                                viewModel.selectedMultiLayerPathIndex = index
                                viewModel.selectedPassIndex = passIndex
                            },
                            onProcessBase = {
                                viewModel.selectedMultiLayerPathIndex = index
                                viewModel.selectedPassIndex = -1
                                viewModel.refreshPouchLists()
                                showProcessPicker = true
                            },
                            onProcessPass = { passIndex ->
                                viewModel.selectedMultiLayerPathIndex = index
                                viewModel.selectedPassIndex = passIndex
                                viewModel.refreshPouchLists()
                                showProcessPicker = true
                            },
                            onRename = {
                                viewModel.selectedMultiLayerPathIndex = index
                                viewModel.newWeldPathName = multiPath.name
                                viewModel.isRenameDialogVisible = true
                            },
                            onSelectPointBase = { pointIndex -> viewModel.selectPoint(index, pointIndex) },
                            onDeletePointBase = { pointIndex -> viewModel.deletePoint(index, pointIndex) },
                            onToggleEnabledBase = { viewModel.toggleWeldPathEnabled(index) },
                            onToggleEnabledPass = { passIndex -> 
                                val pass = multiPath.passes[passIndex]
                                multiPath.passes[passIndex] = pass.copy(isEnabled = !pass.isEnabled)
                                viewModel.saveCurrentProject()
                            },
                            index = index,
                            totalCount = viewModel.multiLayerWeldPaths.size,
                            onMove = { from, to -> viewModel.moveWeldPath(from, to) },
                            selectedRefPointType = viewModel.selectedRefPointType,
                            onSelectRefPoint = { type -> viewModel.selectRefPointType(type) },
                            onDeleteRefPoint = { type -> viewModel.deleteRefPoint(type) },
                            onUpdatePassOffset = { idx, field, value -> viewModel.updatePassOffset(index, idx, field, value) },
                            modifier = Modifier.animateItemPlacement()
                        )
                    }
                }
            }

            // Right side buttons
            Column(modifier = Modifier.width(220.dp).fillMaxHeight().background(Color(0xFFFFFFFF)).padding(8.dp)) {
                Column(
                    modifier = Modifier
                        .fillMaxWidth()
                        .verticalScroll(rememberScrollState()),
                    verticalArrangement = Arrangement.spacedBy(12.dp)
                ) {
                    // 1. 工程工艺区
                    Column(modifier = Modifier.fillMaxWidth()) {
                        TwoColumnGrid(
                            spacing = 2.dp,
                            items = listOf(
                                { m -> ActionButton("工程管理", onClick = { onNavigateToProjectManagement() }, modifier = m) },
                                { m -> ActionButton("工艺管理", onClick = { onNavigateToProcessManagement(false) }, modifier = m) }
                            )
                        )
                        Spacer(modifier = Modifier.height(8.dp))
                        ActionButton(
                            "微调",
                            onClick = { viewModel.fineTune.open() },
                            modifier = Modifier.fillMaxWidth(),
                            containerColor = Color(0xFF1565C0)
                        )
                    }

                    Divider()

                    // 2. 模拟与焊接区
                    if (viewModel.isWelding || viewModel.isSimulating) {
                        // Welding/Simulation Mode
                        if (viewModel.isPaused) {
                            // Paused: Continue + Stop
                            Row(horizontalArrangement = Arrangement.spacedBy(2.dp)) {
                                ActionButton(
                                    "长按继续",
                                    onClick = { },
                                    onLongClick = { viewModel.continueWelding() },
                                    containerColor = Color(0xFF4CAF50),
                                    modifier = Modifier.weight(1f),
                                    height = 122.dp
                                )
                                ActionButton(
                                    "停止",
                                    onClick = { viewModel.stopWelding() },
                                    containerColor = Color(0xFFB00020),
                                    modifier = Modifier.weight(1f),
                                    height = 122.dp
                                )
                            }
                        } else {
                            // Running: Pause + Stop
                            Row(horizontalArrangement = Arrangement.spacedBy(2.dp)) {
                                ActionButton(
                                    "暂停",
                                    onClick = { viewModel.pauseWelding() },
                                    containerColor = Color(0xFFFF9800),
                                    modifier = Modifier.weight(1f),
                                    height = 122.dp
                                )
                                ActionButton(
                                    "停止",
                                    onClick = { viewModel.stopWelding() },
                                    containerColor = Color(0xFFB00020),
                                    modifier = Modifier.weight(1f),
                                    height = 122.dp
                                )
                            }
                        }
                    } else {
                        // Normal Mode: Simulate/Arc + Stop
                        Row(horizontalArrangement = Arrangement.spacedBy(2.dp)) {
                            ActionButton(
                                "模拟焊接", 
                                onClick = { /* Short press action if needed */ },
                                onLongClick = { viewModel.startSimulation() },
                                modifier = Modifier.weight(1f)
                            )
                            ActionButton(
                                "起弧焊接", 
                                onClick = { }, 
                                onLongClick = {
                                    viewModel.stopControllerActive()
                                    viewModel.startArcWelding()
                                },
                                modifier = Modifier.weight(1f)
                            )
                        }
                        Spacer(modifier = Modifier.height(2.dp))
                        ActionButton(
                            "停止",
                            onClick = { viewModel.stopWelding() },
                            containerColor = Color(0xFFB00020),
                            modifier = Modifier.fillMaxWidth()
                        )
                        if (viewModel.isControllerActive) {
                            Spacer(modifier = Modifier.height(2.dp))
                            ActionButton(
                                "伺服使能",
                                onClick = { viewModel.enableExtAxisServo() },
                                containerColor = Color(0xFF2196F3),
                                modifier = Modifier.fillMaxWidth()
                            )
                        }
                    }

                    Divider()

                    // 3. 焊道操作区
                    val isWeldPathSelected = viewModel.multiLayerWeldPaths.isNotEmpty() && viewModel.selectedMultiLayerPathIndex in viewModel.multiLayerWeldPaths.indices
                    
                    TwoColumnGrid(
                        spacing = 2.dp,
                        items = listOf(
                            { m -> ActionButton("添加直线焊道", onClick = { viewModel.addLinearWeldPath() }, modifier = m) },
                            { m -> ActionButton("添加圆弧焊道", onClick = { viewModel.addCircularWeldPath() }, modifier = m) },
                            { m -> ActionButton("3D预览", onClick = { show3DViewer = true }, containerColor = Color(0xFF673AB7), modifier = m) }
                        )
                    )

                    ActionButton(
                        "删除多层焊道", 
                        onClick = {
                            if (isWeldPathSelected) {
                                weldPathToDeleteIndex = viewModel.selectedMultiLayerPathIndex
                                showDeleteDialog = true
                            }
                        }, 
                        containerColor = Color(0xFFF44336), 
                        enabled = isWeldPathSelected, 
                        modifier = Modifier.fillMaxWidth()
                    )

                    TwoColumnGrid(
                        spacing = 2.dp,
                        items = listOf(
                            { m -> ActionButton("添加焊道层", onClick = { viewModel.addWeldPassOffset() }, enabled = isWeldPathSelected, modifier = m) },
                            { m -> ActionButton("删除焊道层", onClick = { viewModel.deleteWeldPassOffset() }, containerColor = Color(0xFFF44336), enabled = isWeldPathSelected && viewModel.selectedPassIndex >= 0, modifier = m) }
                        )
                    )

                    // Coordinate System Teaching (Multi-Layer Base Path Only) - REMOVED (Moved to Item)


                    // 4. 焊接操作区 (Point Ops)
                    // Multi Mode Variables
                    val currentMultiPath = viewModel.multiLayerWeldPaths.getOrNull(viewModel.selectedMultiLayerPathIndex)
                    val currentBasePath = currentMultiPath?.basePath
                    val selectedBasePointIndex = currentBasePath?.selectedPointIndex ?: -1
                    val selectedBasePoint = currentBasePath?.points?.getOrNull(selectedBasePointIndex)
                    
                    val isPassSelected = viewModel.selectedPassIndex >= 0
                    
                    // Determine effective point for Point Mode
                    val isPointMode = viewModel.selectedPassIndex == -1
                    val effectivePoint = if (isPointMode) selectedBasePoint else null
                    
                    val isDataClearable = effectivePoint?.pose != null

                    if (!isPassSelected) {
                        TwoColumnGrid(
                            spacing = 2.dp,
                            items = listOf(
                                { m -> ActionButton("添加中间点", onClick = { viewModel.addMiddlePoint() }, enabled = isPointMode, modifier = m) },
                                { m -> ActionButton("添加圆弧点", onClick = { viewModel.addArcMiddlePoint() }, enabled = isPointMode, modifier = m) }
                            )
                        )
                        
                        ActionButton("删除中间点", onClick = { viewModel.deletePoint(viewModel.selectedMultiLayerPathIndex, selectedBasePointIndex) }, containerColor = Color(0xFFF44336), enabled = isPointMode && selectedBasePoint?.type == WeldPointType.MIDDLE || selectedBasePoint?.type == WeldPointType.ARC_MIDDLE, modifier = Modifier.fillMaxWidth())

                        TwoColumnGrid(
                            spacing = 2.dp,
                            items = listOf(
                                { m -> ActionButton("采集数据", onClick = { viewModel.collectData() }, modifier = m) },
                                { m -> ActionButton("清除数据", onClick = { viewModel.clearPointData() }, containerColor = if (isDataClearable) MaterialTheme.colorScheme.primary else Color.Gray, enabled = isDataClearable, modifier = m) }
                            )
                        )
                    }

                    Divider()
                    
                    // 5. 手动控制区
                    TwoColumnGrid(
                        spacing = 2.dp,
                        items = listOf(
                            { m -> HoldButton("送丝", onPress = { viewModel.sendManualCommand(268, "SetForwardWireFeed(0,1)") }, onRelease = { viewModel.sendManualCommand(268, "SetForwardWireFeed(0,0)") }, modifier = m) },
                            { m -> HoldButton("退丝", onPress = { viewModel.sendManualCommand(269, "SetReverseWireFeed(0,1)") }, onRelease = { viewModel.sendManualCommand(269, "SetReverseWireFeed(0,0)") }, modifier = m) },
                            { m -> HoldButton("送气", onPress = { viewModel.sendManualCommand(270, "SetAspirated(0,1)") }, onRelease = { viewModel.sendManualCommand(270, "SetAspirated(0,0)") }, modifier = m) },
                            { m -> HoldButton("起弧", onPress = { viewModel.sendManualCommand(247, "ARCStart(0,0,10000)") }, onRelease = { viewModel.sendManualCommand(102, "STOP") }, modifier = m) }
                        )
                    )
                    
                    Spacer(modifier = Modifier.height(2.dp))
                    
                    Divider()
                    
                    // 6. Current/Voltage Setting
                    Button(
                        onClick = {
                            viewModel.inputCurrent = viewModel.savedCurrent.let { if (it % 1.0 == 0.0) it.toInt().toString() else it.toString() }
                            viewModel.inputVoltage = viewModel.savedVoltage.let { if (it % 1.0 == 0.0) it.toInt().toString() else it.toString() }
                            viewModel.isCurrentVoltageDialogVisible = true
                        },
                        modifier = Modifier.fillMaxWidth().height(50.dp),
                        shape = RoundedCornerShape(8.dp),
                        colors = ButtonDefaults.buttonColors(containerColor = MaterialTheme.colorScheme.primary),
                        contentPadding = PaddingValues(horizontal = 4.dp)
                    ) {
                        Column(horizontalAlignment = Alignment.CenterHorizontally) {
                            Text("设置电流电压", fontSize = 13.sp, fontWeight = FontWeight.Bold)
                            val c = viewModel.savedCurrent.let { if (it % 1.0 == 0.0) it.toInt().toString() else it.toString() }
                            val v = viewModel.savedVoltage.let { if (it % 1.0 == 0.0) it.toInt().toString() else it.toString() }
                            val subtitle = if (viewModel.savedCurrent > 0 || viewModel.savedVoltage > 0) "$c A  $v V" else "电流电压"
                            Text(subtitle, fontSize = 11.sp)
                        }
                    }

                    Divider()

                    // 7. 统计信息
                    Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
                        Text("焊长:", fontSize = 12.sp, color = Color.Gray)
                        Text(
                            "%.1f m".format(java.util.Locale.US, viewModel.weldingLength), 
                            fontSize = 12.sp, 
                            fontWeight = FontWeight.Bold,
                            modifier = Modifier.clickable { viewModel.clearStats() }
                        )
                    }
                    val duration = viewModel.weldingDuration
                    val hours = duration / 3600
                    val minutes = (duration % 3600) / 60
                    val seconds = duration % 60
                    val timeText = "%02d:%02d:%02d".format(hours, minutes, seconds)
                    Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
                        Text("时长:", fontSize = 12.sp, color = Color.Gray)
                        Text(
                            timeText, 
                            fontSize = 12.sp, 
                            fontWeight = FontWeight.Bold,
                            modifier = Modifier.clickable { viewModel.clearStats() }
                        )
                    }
                    ActionButton(
                        text = "系统更新",
                        onClick = { viewModel.checkForUpdate() },
                        modifier = Modifier.fillMaxWidth()
                    )
                }
            }
        }
    }

    // 删除确认对话框
    if (showDeleteDialog) {
        AlertDialog(
            onDismissRequest = { showDeleteDialog = false },
            title = { Text("确认删除") },
            text = { Text("确定要删除该焊道吗？此操作无法撤销。") },
            confirmButton = {
                Button(
                    onClick = {
                        if (weldPathToDeleteIndex != -1) {
                            viewModel.deleteWeldPath(weldPathToDeleteIndex)
                        }
                        showDeleteDialog = false
                    },
                    colors = ButtonDefaults.buttonColors(Color(0xFFF44336))
                ) {
                    Text("删除", color = Color.White)
                }
            },
            dismissButton = {
                Button(
                    onClick = { showDeleteDialog = false },
                    colors = ButtonDefaults.buttonColors(Color.Gray)
                ) {
                    Text("取消", color = Color.White)
                }
            }
        )
    }

    ClosureProcessPicker(
        visible = showProcessPicker,
        processes = viewModel.pouchProcesses,
        onPick = { id ->
            viewModel.bindProcessFromPouch(id)
            showProcessPicker = false
        },
        onDismiss = { showProcessPicker = false },
    )

    // Missing Process Dialog
    if (viewModel.isMissingProcessDialogVisible) {
        AlertDialog(
            onDismissRequest = { viewModel.isMissingProcessDialogVisible = false },
            title = { Text("闭包里没有这条工艺") },
            text = { Text(viewModel.missingProcessMessage) },
            confirmButton = {
                Button(
                    onClick = { viewModel.isMissingProcessDialogVisible = false }
                ) {
                    Text("确定")
                }
            }
        )
    }

    // 重命名对话框
    if (viewModel.isRenameDialogVisible) {
        AlertDialog(
            onDismissRequest = { viewModel.isRenameDialogVisible = false },
            title = { Text("修改焊道名称") },
            text = {
                Column {
                    Text("请输入新的焊道名称：")
                    Spacer(modifier = Modifier.height(8.dp))
                    TextField(
                        value = viewModel.newWeldPathName,
                        onValueChange = { viewModel.newWeldPathName = it },
                        modifier = Modifier.fillMaxWidth()
                    )
                }
            },
            confirmButton = {
                Button(
                    onClick = {
                        viewModel.renameWeldPath(viewModel.newWeldPathName)
                        viewModel.isRenameDialogVisible = false
                    }
                ) {
                    Text("确定")
                }
            },
            dismissButton = {
                Button(
                    onClick = { viewModel.isRenameDialogVisible = false },
                    colors = ButtonDefaults.buttonColors(Color.Gray)
                ) {
                    Text("取消")
                }
            }
        )
    }



    // Set Current/Voltage Dialog
    if (viewModel.isRobotErrorDialogVisible && viewModel.currentRobotErrors.isNotEmpty()) {
        RobotErrorDialog(
            viewModel = viewModel,
            errors = viewModel.currentRobotErrors
        )
    }

    if (viewModel.isCurrentVoltageDialogVisible) {
        AlertDialog(
            onDismissRequest = { viewModel.isCurrentVoltageDialogVisible = false },
            title = { Text("设置电流电压") },
            text = {
                Column {
                    OutlinedTextField(
                        value = viewModel.inputCurrent,
                        onValueChange = { viewModel.inputCurrent = it },
                        label = { Text("焊接电流 (A) [0-1000]") },
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                        modifier = Modifier.fillMaxWidth()
                    )
                    Spacer(modifier = Modifier.height(8.dp))
                    OutlinedTextField(
                        value = viewModel.inputVoltage,
                        onValueChange = { viewModel.inputVoltage = it },
                        label = { Text("焊接电压 (V) [0-1000]") },
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                        modifier = Modifier.fillMaxWidth()
                    )
                }
            },
            confirmButton = {
                Button(
                    onClick = { viewModel.setWeldingCurrentVoltage() }
                ) {
                    Text("确定")
                }
            },
            dismissButton = {
                Button(
                    onClick = { viewModel.isCurrentVoltageDialogVisible = false },
                    colors = ButtonDefaults.buttonColors(Color.Gray)
                ) {
                    Text("取消")
                }
            }
        )
    }

    FineTuneDialog(viewModel.fineTune)
}






