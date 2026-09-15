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
import androidx.compose.runtime.setValue
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
import com.gbndt.shijiaoqi.ui.component.UpdateDialog
import com.gbndt.shijiaoqi.ui.navigation.LocalWeldInput
import com.gbndt.shijiaoqi.ui.project.PouchProcessScreen
import com.gbndt.shijiaoqi.ui.project.PouchProjectScreen

/** 焊接屏内部的三级页面：焊道、工程、工艺。 */
sealed class WeldPage {
    object Path : WeldPage()
    object Project : WeldPage()
    data class Process(val picking: Boolean) : WeldPage()
}

/**
 * 三个焊接模式共用的外壳：状态栏、工具与姿态对话框、工程/工艺子屏、
 * 升级弹窗、点动红框，以及登录后的设备号匹配。模式自己只画焊道那一块。
 */
@Composable
fun WeldingShell(
    viewModel: WeldViewModelInterface,
    session: SessionRepository,
    onExit: () -> Unit,
    content: @Composable (onProject: () -> Unit, onProcess: (Boolean) -> Unit) -> Unit,
) {
    val context = LocalContext.current
    val sessionState by session.state.collectAsStateWithLifecycle()
    var page by remember { mutableStateOf<WeldPage>(WeldPage.Path) }

    // 手柄按键由 Activity 收，转给当前在屏的这个 ViewModel
    val register = LocalWeldInput.current
    DisposableEffect(viewModel) {
        register(viewModel)
        onDispose { register(null) }
    }

    LaunchedEffect(viewModel) {
        viewModel.toastEvent.collect { message ->
            Toast.makeText(context, message, Toast.LENGTH_SHORT).show()
        }
    }

    // 读到机械臂识别号后才匹配本机，匹配过再按本机袋装工程
    LaunchedEffect(viewModel.machineCode, sessionState.loggedIn) {
        session.setDeviceSerial(viewModel.machineCode)
        if (sessionState.loggedIn && viewModel.machineCode.isNotBlank()) {
            runCatching { session.matchArm(viewModel.machineCode) }
                .onFailure {
                    Toast.makeText(context, session.state.value.error ?: it.message ?: "设备不匹配", Toast.LENGTH_SHORT).show()
                }
            viewModel.syncFromPouch()
        }
    }
    LaunchedEffect(sessionState.loggedIn, sessionState.armMatched) {
        if (sessionState.loggedIn) viewModel.syncFromPouch()
    }

    if (sessionState.loggedIn) {
        ToolListDialog(viewModel = viewModel, onDismiss = { viewModel.isToolListDialogVisible = false })
        ToolEditDialog(viewModel = viewModel)
        PositionSelectionDialog(viewModel = viewModel, onDismiss = { viewModel.isPositionDialogVisible = false })
        SpeedSelectionDialog(viewModel = viewModel, onDismiss = { viewModel.isSpeedDialogVisible = false })
        InstallPosSelectionDialog(viewModel = viewModel, onDismiss = { viewModel.isInstallPosDialogVisible = false })
    }

    Box(modifier = Modifier.fillMaxSize()) {
        Column(modifier = Modifier.fillMaxSize()) {
            CustomStatusBar(
                viewModel = viewModel,
                onProjectClick = { page = WeldPage.Project },
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
                            projects = viewModel.pouchProjects,
                            onRefresh = { viewModel.refreshPouchLists() },
                            onOpen = {
                                viewModel.activatePouchProject(it)
                                page = WeldPage.Path
                            },
                            onBack = { page = WeldPage.Path },
                        )
                    }

                    is WeldPage.Process -> {
                        PouchProcessScreen(
                            processes = viewModel.pouchProcesses,
                            picking = current.picking,
                            onRefresh = { viewModel.refreshPouchLists() },
                            onPick = { id ->
                                if (current.picking) viewModel.bindProcessFromPouch(id)
                                page = WeldPage.Path
                            },
                            onBack = {
                                viewModel.cancelAddProcessVariant()
                                page = WeldPage.Path
                            },
                        )
                    }
                }
            }
        }

        viewModel.updateInfo?.let { info ->
            if (viewModel.isUpdateDialogVisible) {
                UpdateDialog(
                    updateInfo = info,
                    onConfirm = { viewModel.startUpdateDownload() },
                    onDismiss = { viewModel.isUpdateDialogVisible = false },
                )
            }
        }

        // 点动中整屏红框
        if (viewModel.isControllerActive) {
            Box(
                modifier = Modifier
                    .fillMaxSize()
                    .border(8.dp, Color.Red)
                    .zIndex(100f),
            )
        }
    }
}
