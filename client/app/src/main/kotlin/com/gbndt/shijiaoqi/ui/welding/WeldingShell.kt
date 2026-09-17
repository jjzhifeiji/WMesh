package com.gbndt.shijiaoqi.ui.welding

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.border
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import kotlinx.coroutines.launch
import com.gbndt.shijiaoqi.model.WeldProcess
import com.gbndt.shijiaoqi.ui.project.ProcessEditorDialog
import java.util.UUID
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import androidx.compose.ui.zIndex
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import android.widget.Toast
import com.gbndt.shijiaoqi.data.repository.SessionRepository
import com.gbndt.shijiaoqi.ui.component.CustomStatusBar
import com.gbndt.shijiaoqi.ui.navigation.LocalTeach
import com.gbndt.shijiaoqi.ui.navigation.LocalWeldInput
import com.gbndt.shijiaoqi.ui.project.PouchProcessScreen
import com.gbndt.shijiaoqi.ui.project.PouchProjectScreen
import com.gbndt.shijiaoqi.data.log.PadLog

/** 焊接屏内部的三级页面：焊道、工程、工艺。 */
sealed class WeldPage {
    object Path : WeldPage()
    object Project : WeldPage()
    data class Process(val picking: Boolean) : WeldPage()
}

/**
 * 三个焊接模式共用的外壳：状态栏、工具与姿态对话框、工程/工艺子屏、
 * 点动红框，以及登录后的设备号匹配。模式自己只画焊道那一块。
 */
@Composable
fun WeldingShell(
    host: WeldShellHost,
    pad: WeldPad,
    session: SessionRepository,
    onExit: () -> Unit,
    content: @Composable (onProject: () -> Unit, onProcess: (Boolean) -> Unit) -> Unit,
) {
    val context = LocalContext.current
    val sessionState by session.state.collectAsStateWithLifecycle()
    val teach = LocalTeach.current
    val teachUi by teach.uiState.collectAsStateWithLifecycle()
    val shell by host.shellUi.collectAsStateWithLifecycle()
    var page by remember { mutableStateOf<WeldPage>(WeldPage.Path) }
    var editingId by remember { mutableStateOf<UUID?>(null) }
    var editorProcess by remember { mutableStateOf<WeldProcess?>(null) }
    val scope = rememberCoroutineScope()

    val register = LocalWeldInput.current
    DisposableEffect(pad) {
        register(pad)
        onDispose { register(null) }
    }

    LaunchedEffect(host) {
        host.toastEvent.collect { message ->
            Toast.makeText(context, message, Toast.LENGTH_SHORT).show()
        }
    }

    LaunchedEffect(teachUi.machineCode, sessionState.loggedIn) {
        session.setDeviceSerial(teachUi.machineCode)
        if (sessionState.loggedIn && teachUi.machineCode.isNotBlank()) {
            runCatching { session.matchArm(teach.machineCode) }
            host.syncFromPouch()
        }
    }
    LaunchedEffect(sessionState.loggedIn, sessionState.armMatched) {
        if (sessionState.loggedIn) host.syncFromPouch()
    }

    if (sessionState.loggedIn) {
        if (teachUi.isToolListDialogVisible) {
            ToolListDialog(teach = teach, onDismiss = { teach.isToolListDialogVisible = false })
        }
        if (teachUi.isToolEditDialogVisible) {
            ToolEditDialog(teach = teach)
        }
        if (teachUi.isPositionDialogVisible) {
            PositionSelectionDialog(teach = teach, onDismiss = { teach.isPositionDialogVisible = false })
        }
        if (teachUi.isSpeedDialogVisible) {
            SpeedSelectionDialog(teach = teach, onDismiss = { teach.isSpeedDialogVisible = false })
        }
        if (teachUi.isInstallPosDialogVisible) {
            InstallPosSelectionDialog(teach = teach, onDismiss = { teach.isInstallPosDialogVisible = false })
        }
    }

    Box(modifier = Modifier.fillMaxSize()) {
        Column(modifier = Modifier.fillMaxSize()) {
            CustomStatusBar(
                teach = teach,
                host = host,
                onProjectClick = {
                    PadLog.info("Nav", "open project list")
                    page = WeldPage.Project
                },
            )
            Box(modifier = Modifier.weight(1f)) {
                when (val current = page) {
                    is WeldPage.Path -> {
                        BackHandler { onExit() }
                        content(
                            { page = WeldPage.Project },
                            { picking -> page = WeldPage.Process(picking) },
                        )
                    }

                    is WeldPage.Project -> {
                        PouchProjectScreen(
                            projects = shell.pouchProjects,
                            onRefresh = { host.refreshPouchLists() },
                            onOpen = {
                                host.activatePouchProject(it)
                                page = WeldPage.Path
                            },
                            onBack = { page = WeldPage.Path },
                        )
                    }

                    is WeldPage.Process -> {
                        PouchProcessScreen(
                            processes = shell.pouchProcesses,
                            picking = current.picking,
                            onRefresh = { host.refreshPouchLists() },
                            onPick = { id ->
                                if (current.picking) host.bindProcessFromPouch(id)
                                page = WeldPage.Path
                            },
                            onBack = {
                                host.cancelAddProcessVariant()
                                page = WeldPage.Path
                            },
                            onEdit = { item ->
                                scope.launch {
                                    editorProcess = host.loadProcessFromPouch(item.id) ?: WeldProcess(name = item.name)
                                    editingId = item.id
                                }
                            },
                            onCreate = {
                                editingId = null
                                editorProcess = WeldProcess()
                            },
                            onSecret = {
                                Toast.makeText(context, "保密工艺只能焊接，不能查看或修改", Toast.LENGTH_SHORT).show()
                            },
                        )
                    }
                }
            }
        }

        // 点动中整屏红框
        if (teachUi.isControllerActive) {
            Box(
                modifier = Modifier
                    .fillMaxSize()
                    .border(8.dp, Color.Red)
                    .zIndex(100f),
            )
        }
    }

    editorProcess?.let { draft ->
        ProcessEditorDialog(
            title = if (editingId == null) "新建个人工艺" else "编辑工艺",
            initial = draft,
            onSave = { saved ->
                host.saveProcessFromPouch(editingId, saved)
                editorProcess = null
                editingId = null
            },
            onDismiss = {
                editorProcess = null
                editingId = null
            },
        )
    }
}
