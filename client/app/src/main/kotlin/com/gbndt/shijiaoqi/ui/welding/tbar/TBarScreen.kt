package com.gbndt.shijiaoqi.ui.welding.tbar

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
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.DragHandle
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.zIndex
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.gbndt.shijiaoqi.model.WeldPointType
import com.gbndt.shijiaoqi.data.log.PadLog
import com.gbndt.shijiaoqi.model.displayName
import com.gbndt.shijiaoqi.model.tbar.isTBarCollectable
import com.gbndt.shijiaoqi.ui.component.*
import com.gbndt.shijiaoqi.ui.login.*
import com.gbndt.shijiaoqi.ui.project.*
import com.gbndt.shijiaoqi.ui.navigation.LocalTeach
import com.gbndt.shijiaoqi.ui.theme.KeepFullscreen
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.launch
import kotlinx.coroutines.withTimeout
import kotlinx.coroutines.TimeoutCancellationException
import kotlin.math.roundToInt
import androidx.compose.ui.platform.LocalViewConfiguration
import androidx.compose.ui.text.input.KeyboardType
import com.gbndt.shijiaoqi.ui.welding.*
import com.gbndt.shijiaoqi.ui.preview.PadPreview
import com.gbndt.shijiaoqi.ui.preview.PadPreviewTheme
import com.gbndt.shijiaoqi.ui.preview.PadSamples
import com.gbndt.shijiaoqi.ui.teach.TeachSession

@OptIn(ExperimentalMaterial3Api::class, ExperimentalFoundationApi::class)
@Composable
fun TBarScreen(
    viewModel: TBarWeldViewModel,
    onNavigateToProjectManagement: () -> Unit,
    onNavigateToProcessManagement: (Boolean) -> Unit,
    onCheckUpdate: () -> Unit,
) {
    val teach = LocalTeach.current
    val ui by viewModel.uiState.collectAsStateWithLifecycle()
    LaunchedEffect(viewModel.selectedWeldPathIndex) {
        viewModel.snapToCollectablePoint(viewModel.selectedWeldPathIndex)
    }
    TBarScreen(
        ui = ui,
        viewModel = viewModel,
        teach = teach,
        isControllerActive = viewModel.isControllerActive,
        onNavigateToProjectManagement = onNavigateToProjectManagement,
        onNavigateToProcessManagement = onNavigateToProcessManagement,
        onCheckUpdate = onCheckUpdate,
    )
}

@OptIn(ExperimentalMaterial3Api::class, ExperimentalFoundationApi::class)
@Composable
fun TBarScreen(
    ui: TBarUiState,
    viewModel: TBarWeldViewModel? = null,
    teach: TeachSession? = null,
    isControllerActive: Boolean = false,
    onNavigateToProjectManagement: () -> Unit = {},
    onNavigateToProcessManagement: (Boolean) -> Unit = {},
    onCheckUpdate: () -> Unit = {},
) {
    var showDeleteDialog by remember { mutableStateOf(false) }
    var show3DViewer by remember { mutableStateOf(false) }
    var weldPathToDeleteIndex by remember { mutableStateOf(-1) }
    var showGapEditor by remember { mutableStateOf(false) }

    if (show3DViewer) {
        Path3DViewerDialog(
            weldPaths = ui.weldPaths,
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
                    initialFirstVisibleItemIndex = ui.listScrollIndex,
                    initialFirstVisibleItemScrollOffset = ui.listScrollOffset
                )

                LaunchedEffect(listState) {
                    snapshotFlow { listState.firstVisibleItemIndex to listState.firstVisibleItemScrollOffset }
                        .collect { (index, offset) ->
                            viewModel?.listScrollIndex = index
                            viewModel?.listScrollOffset = offset
                        }
                }

                // Listen for Scroll Events (e.g., when adding new path)
                LaunchedEffect(viewModel) {
                    val vm = viewModel ?: return@LaunchedEffect
                    vm.scrollToIndexEvent.collect { index ->
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
                        items = ui.weldPaths,
                        key = { it.id }
                    ) { weldPath ->
                        val index = ui.weldPaths.indexOf(weldPath)
                        WeldPathItem(
                            weldPath = weldPath,
                            isSelected = index == ui.selectedWeldPathIndex,
                            processVisible = { ui.shell.pouchProcesses.copyableOf(it) },
                            onSelect = { viewModel?.selectedWeldPathIndex = index },
                            onProcess = {
                                viewModel?.selectedWeldPathIndex = index
                                showGapEditor = true
                            },
                            onAddProcessVariant = {
                                viewModel?.selectedWeldPathIndex = index
                                viewModel?.beginAddProcessVariant(index)
                                onNavigateToProcessManagement(true)
                            },
                            onReplaceExtraProcess = { extraIdx ->
                                viewModel?.beginReplaceExtraProcess(index, extraIdx)
                                onNavigateToProcessManagement(true)
                            },
                            onToggleExtraEnabled = { extraIdx -> viewModel?.toggleExtraProcessEnabled(index, extraIdx) },
                            onDeleteExtraProcess = { extraIdx -> viewModel?.deleteExtraProcess(index, extraIdx) },
                            onRename = {
                                viewModel?.selectedWeldPathIndex = index
                                viewModel?.newWeldPathName = weldPath.name
                                viewModel?.isRenameDialogVisible = true
                            },
                            onSelectPoint = { pointIndex -> viewModel?.selectPoint(index, pointIndex) },
                            onDeletePoint = { pointIndex -> viewModel?.deletePoint(index, pointIndex) },
                            onToggleEnabled = { viewModel?.toggleWeldPathEnabled(index) },
                            onUpdateCorner = null,
                            index = index,
                            totalCount = ui.weldPaths.size,
                            onMove = { from, to -> viewModel?.moveWeldPath(from, to) },
                            modifier = Modifier.animateItem()
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
                                { m -> ActionButton("工艺管理", onClick = {
                                    viewModel?.cancelAddProcessVariant()
                                    onNavigateToProcessManagement(false)
                                }, modifier = m) }
                            )
                        )
                        Spacer(modifier = Modifier.height(8.dp))
                        ActionButton(
                            "微调",
                            onClick = { viewModel?.fineTune?.open() },
                            modifier = Modifier.fillMaxWidth(),
                            containerColor = Color(0xFF1565C0)
                        )

                    }

                    Divider()

                    // 2. 模拟与焊接区 (Moved Up)
                    if (ui.run.isWelding || ui.run.isSimulating) {
                        // Welding/Simulation Mode
                        if (ui.run.isPaused) {
                            // Paused: Continue + Stop
                            Row(horizontalArrangement = Arrangement.spacedBy(2.dp)) {
                                ActionButton(
                                    "长按继续",
                                    onClick = { },
                                    onLongClick = { viewModel?.continueWelding() },
                                    containerColor = Color(0xFF4CAF50),
                                    modifier = Modifier.weight(1f),
                                    height = 122.dp
                                )
                                ActionButton(
                                    "停止",
                                    onClick = { viewModel?.stopWelding() },
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
                                    onClick = { viewModel?.pauseWelding() },
                                    containerColor = Color(0xFFFF9800),
                                    modifier = Modifier.weight(1f),
                                    height = 122.dp
                                )
                                ActionButton(
                                    "停止",
                                    onClick = { viewModel?.stopWelding() },
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
                                onLongClick = { viewModel?.startSimulation() },
                                modifier = Modifier.weight(1f)
                            )
                            ActionButton(
                                "起弧焊接", 
                                onClick = { }, 
                                onLongClick = {
                                    teach?.stopController()
                                    viewModel?.startArcWelding()
                                },
                                modifier = Modifier.weight(1f)
                            )
                        }
                        Spacer(modifier = Modifier.height(2.dp))
                        ActionButton(
                            "停止",
                            onClick = { viewModel?.stopWelding() },
                            containerColor = Color(0xFFB00020),
                            modifier = Modifier.fillMaxWidth()
                        )
                        if (isControllerActive) {
                            Spacer(modifier = Modifier.height(2.dp))
                            ActionButton(
                                "伺服使能",
                                onClick = { teach?.enableExtAxisServo() },
                                containerColor = Color(0xFF2196F3),
                                modifier = Modifier.fillMaxWidth()
                            )
                        }
                    }

                    Divider()

                    // 3. 焊道操作区
                    val currentWeldPathForGap = ui.weldPaths.getOrNull(ui.selectedWeldPathIndex)
                    val isWeldPathSelected = ui.weldPaths.isNotEmpty() && ui.selectedWeldPathIndex >= 0 && ui.selectedWeldPathIndex < ui.weldPaths.size
                    
                    TwoColumnGrid(
                        spacing = 2.dp,
                        items = listOf(
                            { m -> ActionButton("添加焊道", onClick = { viewModel?.addWeldPath() }, modifier = m) },
                            { m -> ActionButton("删除焊道", onClick = {
                                if (isWeldPathSelected) {
                                    weldPathToDeleteIndex = ui.selectedWeldPathIndex
                                    showDeleteDialog = true
                                }
                            }, containerColor = Color(0xFFF44336), enabled = isWeldPathSelected, modifier = m) }
                        )
                    )
                    Spacer(modifier = Modifier.height(2.dp))
                    ActionButton("3D预览", onClick = { show3DViewer = true }, containerColor = Color(0xFF673AB7), modifier = Modifier.fillMaxWidth())
                    if (currentWeldPathForGap != null) {
                        Text(viewModel?.tBarGapText(currentWeldPathForGap).orEmpty(), fontSize = 12.sp, color = Color.Gray, modifier = Modifier.padding(top = 4.dp))
                    }

                    Divider()

                    // 4. 焊接操作区 (Point Ops)
                    val currentWeldPath = ui.weldPaths.getOrNull(ui.selectedWeldPathIndex)
                    val selectedPointIndex = currentWeldPath?.selectedPointIndex ?: -1
                    val selectedPoint = currentWeldPath?.points?.getOrNull(selectedPointIndex)
                    
                    val effectivePoint = selectedPoint
                    
                    val isPointDeletable = effectivePoint?.type == WeldPointType.MIDDLE || effectivePoint?.type == WeldPointType.ARC_MIDDLE
                    val isDataClearable = effectivePoint?.type?.isTBarCollectable() == true && effectivePoint.pose != null
                    // Only show RefX buttons for START or MIDDLE points
                    val isRefXApplicable = effectivePoint?.type == WeldPointType.START || effectivePoint?.type == WeldPointType.MIDDLE
                    val hasRefX = effectivePoint?.refPointX != null

                    TwoColumnGrid(
                        spacing = 2.dp,
                        items = listOf(
                            { m -> ActionButton("采集数据", onClick = { viewModel?.collectData() }, modifier = m) },
                            { m -> ActionButton("清除数据", onClick = { viewModel?.clearPointData() }, containerColor = if (isDataClearable) MaterialTheme.colorScheme.primary else Color.Gray, enabled = isDataClearable, modifier = m) }
                        )
                    )

                    Divider()
                    
                    // 5. 手动控制区
                    TwoColumnGrid(
                        spacing = 2.dp,
                        items = listOf(
                            { m -> HoldButton("送丝", onPress = { teach?.startWireFeed() }, onRelease = { teach?.stopWireFeed() }, modifier = m) },
                            { m -> HoldButton("退丝", onPress = { teach?.reverseWireFeed(true) }, onRelease = { teach?.reverseWireFeed(false) }, modifier = m) },
                            { m -> HoldButton("送气", onPress = { viewModel?.sendManualCommand(270, "SetAspirated(0,1)") }, onRelease = { viewModel?.sendManualCommand(270, "SetAspirated(0,0)") }, modifier = m) },
                            { m -> HoldButton("起弧", onPress = { viewModel?.sendManualCommand(247, "ARCStart(0,0,10000)") }, onRelease = { viewModel?.sendManualCommand(102, "STOP") }, modifier = m) }
                        )
                    )
                    
                    Divider()
                    
                    // 6. Current/Voltage Setting
                    Button(
                        onClick = {
                            viewModel?.inputCurrent = ui.savedCurrent.let { if (it % 1.0 == 0.0) it.toInt().toString() else it.toString() }
                            viewModel?.inputVoltage = ui.savedVoltage.let { if (it % 1.0 == 0.0) it.toInt().toString() else it.toString() }
                            viewModel?.isCurrentVoltageDialogVisible = true
                        },
                        modifier = Modifier.fillMaxWidth().height(50.dp),
                        shape = RoundedCornerShape(8.dp),
                        colors = ButtonDefaults.buttonColors(containerColor = MaterialTheme.colorScheme.primary),
                        contentPadding = PaddingValues(horizontal = 4.dp)
                    ) {
                        Column(horizontalAlignment = Alignment.CenterHorizontally) {
                            Text("设置电流电压", fontSize = 13.sp, fontWeight = FontWeight.Bold)
                            val c = ui.savedCurrent.let { if (it % 1.0 == 0.0) it.toInt().toString() else it.toString() }
                            val v = ui.savedVoltage.let { if (it % 1.0 == 0.0) it.toInt().toString() else it.toString() }
                            val subtitle = if (ui.savedCurrent > 0 || ui.savedVoltage > 0) "$c A  $v V" else "电流电压"
                            Text(subtitle, fontSize = 11.sp)
                        }
                    }

                    Divider()

                    // 7. 统计信息
                    Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
                        Text("焊长:", fontSize = 12.sp, color = Color.Gray)
                        Text(
                            "%.1f m".format(java.util.Locale.US, ui.weldingLength), 
                            fontSize = 12.sp, 
                            fontWeight = FontWeight.Bold,
                            modifier = Modifier.clickable { viewModel?.clearStats() }
                        )
                    }
                    val duration = ui.weldingDuration
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
                            modifier = Modifier.clickable { viewModel?.clearStats() }
                        )
                    }
                    ActionButton(
                        text = "系统更新",
                        onClick = { onCheckUpdate() },
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
            title = { KeepFullscreen(); Text("确认删除") },
            text = { Text("确定要删除该焊道吗？此操作无法撤销。") },
            confirmButton = {
                Button(
                    onClick = {
                        if (weldPathToDeleteIndex != -1) {
                            viewModel?.deleteWeldPath(weldPathToDeleteIndex)
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

    if (ui.isRobotErrorDialogVisible && ui.currentRobotErrors.isNotEmpty()) {
        RobotErrorDialog(
            errors = ui.currentRobotErrors,
            onDismiss = {
                viewModel?.currentRobotErrors?.clear()
                viewModel?.isRobotErrorDialogVisible = false
            },
        )
    }

    // Missing Process Dialog
    if (ui.isMissingProcessDialogVisible) {
        AlertDialog(
            onDismissRequest = { viewModel?.isMissingProcessDialogVisible = false },
            title = { KeepFullscreen(); Text("工艺文件缺失") },
            text = { Text(ui.missingProcessMessage) },
            confirmButton = {
                Button(
                    onClick = { viewModel?.isMissingProcessDialogVisible = false }
                ) {
                    Text("确定")
                }
            }
        )
    }

    // 重命名对话框
    if (ui.isRenameDialogVisible) {
        AlertDialog(
            onDismissRequest = { viewModel?.isRenameDialogVisible = false },
            title = { KeepFullscreen(); Text("修改焊道名称") },
            text = {
                Column {
                    Text("请输入新的焊道名称：")
                    Spacer(modifier = Modifier.height(8.dp))
                    TextField(
                        value = ui.newWeldPathName,
                        onValueChange = { viewModel?.newWeldPathName = it },
                        modifier = Modifier.fillMaxWidth()
                    )
                }
            },
            confirmButton = {
                Button(
                    onClick = {
                        viewModel?.renameWeldPath(ui.newWeldPathName)
                        viewModel?.isRenameDialogVisible = false
                    }
                ) {
                    Text("确定")
                }
            },
            dismissButton = {
                Button(
                    onClick = { viewModel?.isRenameDialogVisible = false },
                    colors = ButtonDefaults.buttonColors(Color.Gray)
                ) {
                    Text("取消")
                }
            }
        )
    }



    // Set Current/Voltage Dialog
    if (ui.isCurrentVoltageDialogVisible) {
        AlertDialog(
            onDismissRequest = { viewModel?.isCurrentVoltageDialogVisible = false },
            title = { KeepFullscreen(); Text("设置电流电压") },
            text = {
                Column {
                    OutlinedTextField(
                        value = ui.inputCurrent,
                        onValueChange = { viewModel?.inputCurrent = it },
                        label = { Text("焊接电流 (A) [0-1000]") },
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                        modifier = Modifier.fillMaxWidth()
                    )
                    Spacer(modifier = Modifier.height(8.dp))
                    OutlinedTextField(
                        value = ui.inputVoltage,
                        onValueChange = { viewModel?.inputVoltage = it },
                        label = { Text("焊接电压 (V) [0-1000]") },
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                        modifier = Modifier.fillMaxWidth()
                    )
                }
            },
            confirmButton = {
                Button(
                    onClick = { viewModel?.setWeldingCurrentVoltage() }
                ) {
                    Text("确定")
                }
            },
            dismissButton = {
                Button(
                    onClick = { viewModel?.isCurrentVoltageDialogVisible = false },
                    colors = ButtonDefaults.buttonColors(Color.Gray)
                ) {
                    Text("取消")
                }
            }
        )
    }

    viewModel?.let { FineTuneDialog(it.fineTune) }

    if (showGapEditor) {
        Box(modifier = Modifier.fillMaxSize().zIndex(20f)) {
            viewModel?.let { vm ->
                TBarProcessEditorScreen(vm, onBack = { showGapEditor = false })
            }
        }
    }
}

@PadPreview
@Composable
private fun TBarScreenPreview() {
    PadPreviewTheme {
        TBarScreen(ui = PadSamples.tbarUi)
    }
}







@OptIn(ExperimentalLayoutApi::class)
@Composable
fun WeldPathItem(
    weldPath: com.gbndt.shijiaoqi.model.single.WeldPath,
    isSelected: Boolean,
    onSelect: () -> Unit,
    onProcess: () -> Unit,
    onAddProcessVariant: () -> Unit,
    onReplaceExtraProcess: (Int) -> Unit,
    onToggleExtraEnabled: (Int) -> Unit,
    onDeleteExtraProcess: (Int) -> Unit,
    onRename: () -> Unit,
    onSelectPoint: (Int) -> Unit,
    onDeletePoint: (Int) -> Unit,
    onToggleEnabled: () -> Unit,
    onUpdateCorner: (() -> Unit)? = null,
    index: Int,
    totalCount: Int,
    onMove: (Int, Int) -> Unit,
    modifier: Modifier = Modifier,
    processVisible: (String) -> Boolean = { true },
) {
    var itemHeight by remember { mutableStateOf(0) }
    var dragOffset by remember { mutableStateOf(0f) }

    val currentIndex by rememberUpdatedState(index)
    val currentTotalCount by rememberUpdatedState(totalCount)

    Card(
        modifier = modifier
            .onSizeChanged { itemHeight = it.height }
            .offset { androidx.compose.ui.unit.IntOffset(0, dragOffset.roundToInt()) }
            .zIndex(if (dragOffset != 0f) 1f else 0f)
            .fillMaxWidth()
            .padding(8.dp)
            .border(
                width = 2.dp,
                color = if (isSelected) Color(0xFF2196F3) else Color.Transparent,
                shape = RoundedCornerShape(8.dp)
            )
            .clickable { onSelect() },
        shape = RoundedCornerShape(8.dp),
        elevation = CardDefaults.cardElevation(defaultElevation = 4.dp)
    ) {
            Column(modifier = Modifier.padding(8.dp)) {
                // 焊道名称和工艺信息
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.SpaceBetween,
                    verticalAlignment = Alignment.CenterVertically
                ) {
                    Column(modifier = Modifier.weight(1f)) {
                        Text(
                            text = weldPath.name,
                            fontSize = 16.sp,
                            fontWeight = FontWeight.Bold,
                            color = Color(0xFF333333),
                            modifier = Modifier.clickable { onRename() }
                        )
                        Text(
                            text = processParamsCaption(
                                weldPath.process.name.ifBlank { "闭包工艺" },
                                weldPath.process.current,
                                weldPath.process.voltage,
                                weldPath.process.speed,
                                weldPath.process.oscillation.type,
                                processVisible(weldPath.processId),
                            ),
                            fontSize = 12.sp,
                            color = Color(0xFF666666)
                        )
                    }
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        if (weldPath.cornerGroupParams?.isMaster == true && onUpdateCorner != null) {
                            Button(
                                onClick = onUpdateCorner,
                                modifier = Modifier.padding(end = 8.dp).height(32.dp),
                                contentPadding = PaddingValues(horizontal = 12.dp, vertical = 0.dp),
                                shape = RoundedCornerShape(4.dp),
                                colors = ButtonDefaults.buttonColors(Color(0xFF9C27B0))
                            ) {
                                Text("更新包角", fontSize = 12.sp)
                            }
                        }
                        Button(
                            onClick = onProcess,
                            modifier = Modifier.padding(end = 8.dp).height(32.dp),
                            contentPadding = PaddingValues(horizontal = 12.dp, vertical = 0.dp),
                            shape = RoundedCornerShape(4.dp),
                            colors = ButtonDefaults.buttonColors(Color(0xFFFFC107))
                        ) {
                            Text("工艺", fontSize = 12.sp)
                        }
                        Button(
                            onClick = onToggleEnabled,
                            modifier = Modifier.height(32.dp),
                            contentPadding = PaddingValues(horizontal = 12.dp, vertical = 0.dp),
                            shape = RoundedCornerShape(4.dp),
                            colors = ButtonDefaults.buttonColors(
                                containerColor = if (weldPath.isEnabled) Color(0xFF4CAF50) else Color.Gray
                            )
                        ) {
                            Text(if (weldPath.isEnabled) "焊接" else "跳过", fontSize = 12.sp)
                        }
                        
                        Spacer(modifier = Modifier.width(8.dp))
                        Icon(
                            imageVector = Icons.Filled.DragHandle,
                            contentDescription = "Reorder",
                            modifier = Modifier
                                .size(32.dp)
                                .padding(4.dp)
                                .pointerInput(Unit) {
                                    detectVerticalDragGestures(
                                        onDragEnd = { dragOffset = 0f },
                                        onDragCancel = { dragOffset = 0f }
                                    ) { change, dragAmount ->
                                        change.consume()
                                        dragOffset += dragAmount
                                        if (itemHeight > 0) {
                                            if (dragOffset > itemHeight * 0.5f) {
                                                if (currentIndex < currentTotalCount - 1) {
                                                    onMove(currentIndex, currentIndex + 1)
                                                    dragOffset -= itemHeight
                                                }
                                            } else if (dragOffset < -itemHeight * 0.5f) {
                                                if (currentIndex > 0) {
                                                    onMove(currentIndex, currentIndex - 1)
                                                    dragOffset += itemHeight
                                                }
                                            }
                                        }
                                    }
                                }
                        )
                    }
                }

                // 点位按钮列表
                if (weldPath.isEnabled) {
                    androidx.compose.foundation.layout.FlowRow(
                        modifier = Modifier.fillMaxWidth().padding(top = 8.dp),
//                    mainAxisSpacing = 4.dp,
//                    crossAxisSpacing = 4.dp
                    ) {
                        val collectablePoints = weldPath.points.mapIndexed { index, point -> index to point }
                            .filter { it.second.type.isTBarCollectable() }
                        collectablePoints.forEach { (pointIndex, point) ->
                            PointButton(
                                point = point,
                                isSelected = isSelected && pointIndex == weldPath.selectedPointIndex,
                                onClick = { onSelectPoint(pointIndex) },
                                onDelete = { onDeletePoint(pointIndex) },
                                index = pointIndex,
                                totalCount = weldPath.points.size
                            )
                        }
                    }
                    Text(
                        "起点、终点由 A下/B下、A上/B上计算，无需采集",
                        fontSize = 11.sp,
                        color = Color.Gray,
                        modifier = Modifier.padding(top = 4.dp)
                    )
                }

                if (false && (weldPath.extraProcesses.isNotEmpty() || isSelected)) {
                    Column(
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(top = 8.dp),
                        verticalArrangement = Arrangement.spacedBy(6.dp)
                    ) {
                        weldPath.extraProcesses.forEachIndexed { extraIdx, slot ->
                            ExtraProcessRow(
                                slot = slot,
                                index = extraIdx + 1,
                                onProcess = { onReplaceExtraProcess(extraIdx) },
                                onToggleEnabled = { onToggleExtraEnabled(extraIdx) },
                                onDelete = { onDeleteExtraProcess(extraIdx) },
                                copyable = processVisible(slot.processId),
                            )
                        }
                        if (isSelected) {
                            Button(
                                onClick = onAddProcessVariant,
                                modifier = Modifier
                                    .fillMaxWidth()
                                    .height(40.dp),
                                contentPadding = PaddingValues(horizontal = 12.dp, vertical = 0.dp),
                                shape = RoundedCornerShape(4.dp),
                                colors = ButtonDefaults.buttonColors(containerColor = Color(0xFF009688))
                            ) {
                                Icon(
                                    imageVector = Icons.Filled.Add,
                                    contentDescription = null,
                                    modifier = Modifier.size(18.dp)
                                )
                                Spacer(modifier = Modifier.width(4.dp))
                                Text("添加工艺", fontSize = 14.sp, fontWeight = FontWeight.Bold)
                            }
                        }
                    }
                }
            }
        }
}

@Composable
private fun ExtraProcessRow(
    slot: com.gbndt.shijiaoqi.model.single.WeldPathProcessSlot,
    index: Int,
    onProcess: () -> Unit,
    onToggleEnabled: () -> Unit,
    onDelete: () -> Unit,
    copyable: Boolean = true,
) {
    val processLabel = slot.process.name.ifBlank { slot.processId.ifBlank { "未选工艺" } }
    Surface(
        modifier = Modifier.fillMaxWidth(),
        shape = RoundedCornerShape(6.dp),
        color = Color(0xFFE0F2F1),
        border = BorderStroke(1.dp, Color(0xFF80CBC4))
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = 8.dp, vertical = 6.dp),
            verticalAlignment = Alignment.CenterVertically
        ) {
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = "附加工艺$index  $processLabel",
                    fontSize = 13.sp,
                    fontWeight = FontWeight.Bold,
                    color = Color(0xFF00695C)
                )
                Text(
                    text = processParamsCaption(
                        slot.process.name.ifBlank { slot.processId.ifBlank { "未选工艺" } },
                        slot.process.current,
                        slot.process.voltage,
                        slot.process.speed,
                        slot.process.oscillation.type,
                        copyable,
                    ),
                    fontSize = 11.sp,
                    color = Color(0xFF546E7A)
                )
            }
            Button(
                onClick = onProcess,
                modifier = Modifier.padding(end = 4.dp).height(28.dp),
                contentPadding = PaddingValues(horizontal = 8.dp, vertical = 0.dp),
                shape = RoundedCornerShape(4.dp),
                colors = ButtonDefaults.buttonColors(Color(0xFFFFC107))
            ) {
                Text("工艺", fontSize = 11.sp)
            }
            Button(
                onClick = onToggleEnabled,
                modifier = Modifier.padding(end = 4.dp).height(28.dp),
                contentPadding = PaddingValues(horizontal = 8.dp, vertical = 0.dp),
                shape = RoundedCornerShape(4.dp),
                colors = ButtonDefaults.buttonColors(
                    containerColor = if (slot.isEnabled) Color(0xFF4CAF50) else Color.Gray
                )
            ) {
                Text(if (slot.isEnabled) "焊接" else "跳过", fontSize = 11.sp)
            }
            IconButton(
                onClick = onDelete,
                modifier = Modifier.size(28.dp)
            ) {
                Icon(
                    imageVector = Icons.Filled.Delete,
                    contentDescription = "删除工艺",
                    tint = Color(0xFFB00020),
                    modifier = Modifier.size(18.dp)
                )
            }
        }
    }
}



@Composable
fun PointButton(
    point: com.gbndt.shijiaoqi.model.WeldPoint,
    isSelected: Boolean,
    onClick: () -> Unit,
    onDelete: () -> Unit,
    index: Int,
    totalCount: Int
) {
    val at = rememberLogSite()
    val hasData = point.pose != null

    val color = when (point.type) {
        WeldPointType.START_SAFE, WeldPointType.END_SAFE -> Color(0xFF9E9E9E)
        WeldPointType.START, WeldPointType.END -> Color(0xFFF44336)
        WeldPointType.MIDDLE -> Color(0xFF2196F3)
        WeldPointType.ARC_MIDDLE -> Color(0xFFFF9800)
        WeldPointType.GROOVE_A_LOWER, WeldPointType.GROOVE_B_LOWER,
        WeldPointType.GROOVE_A_UPPER, WeldPointType.GROOVE_B_UPPER -> Color(0xFF00897B)
    }

    val name = point.type.displayName()
    
    val hasRefX = point.refPointX != null

    Box {
        Column(
            modifier = Modifier.padding(1.dp),
            horizontalAlignment = Alignment.CenterHorizontally
        ) {
            Button(
                onClick = {
                    PadLog.click("point $name", at)
                    onClick()
                },
                contentPadding = PaddingValues(horizontal = 8.dp, vertical = 4.dp),
                colors = ButtonDefaults.buttonColors(
                    containerColor = if (isSelected) Color(0xFFFFFFFF) else color,
                    contentColor = if (isSelected) color else Color.White
                ),
                border = if (hasData) BorderStroke(2.dp, Color(0xFF4CAF50)) else null,
                shape = RoundedCornerShape(4.dp),
                elevation = ButtonDefaults.buttonElevation(defaultElevation = if (isSelected) 4.dp else 0.dp),
                modifier = Modifier.height(40.dp).width(80.dp)
            ) {
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.Center,
                    modifier = Modifier.fillMaxWidth()
                ) {
                    Text(name, fontSize = 14.sp)
                    if (hasData) {
                        Spacer(modifier = Modifier.width(4.dp))
                        Icon(
                            imageVector = Icons.Default.Check,
                            contentDescription = "已采集",
                            modifier = Modifier.size(16.dp),
                            tint = if (isSelected) Color(0xFF4CAF50) else Color.White
                        )
                    }
                    if (hasRefX) {
                        Spacer(modifier = Modifier.width(2.dp))
                        Text("X", fontSize = 10.sp, color = if (isSelected) Color(0xFF009688) else Color.White, fontWeight = FontWeight.Bold)
                    }
                }
            }
        }
    }
}