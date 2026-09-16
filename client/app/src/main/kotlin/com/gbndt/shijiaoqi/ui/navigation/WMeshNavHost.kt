package com.gbndt.shijiaoqi.ui.navigation

import androidx.compose.foundation.layout.Box
import androidx.compose.runtime.Composable
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import androidx.lifecycle.viewmodel.navigation3.rememberViewModelStoreNavEntryDecorator
import androidx.navigation3.runtime.NavBackStack
import androidx.navigation3.runtime.NavKey
import androidx.navigation3.runtime.entryProvider
import androidx.navigation3.runtime.rememberNavBackStack
import androidx.navigation3.runtime.rememberSaveableStateHolderNavEntryDecorator
import androidx.navigation3.ui.NavDisplay
import com.gbndt.shijiaoqi.data.repository.SessionRepository
import com.gbndt.shijiaoqi.ui.login.SplashScreen
import com.gbndt.shijiaoqi.ui.robottest.RobotTestScreen
import com.gbndt.shijiaoqi.ui.robottest.RobotTestViewModel
import com.gbndt.shijiaoqi.ui.update.AppUpdateHost
import com.gbndt.shijiaoqi.ui.update.AppUpdateViewModel
import com.gbndt.shijiaoqi.ui.welding.WeldingShell
import com.gbndt.shijiaoqi.ui.welding.multilayer.MultiLayerScreen
import com.gbndt.shijiaoqi.ui.welding.multilayer.MultiLayerWeldViewModel
import com.gbndt.shijiaoqi.ui.welding.single.SingleWeldViewModel
import com.gbndt.shijiaoqi.ui.welding.single.WeldPathScreen
import com.gbndt.shijiaoqi.ui.welding.tbar.TBarScreen
import com.gbndt.shijiaoqi.ui.welding.tbar.TBarWeldViewModel

/** 一个 Activity 一张栈：开屏 → 选模式 → 三种焊接或指令测试。 */
@Composable
fun WMeshNavHost(
    session: SessionRepository,
    onSplashFinished: () -> Unit,
    backStack: NavBackStack<NavKey> = rememberNavBackStack(SplashKey),
) {
    val update: AppUpdateViewModel = hiltViewModel()
    Box {
        NavDisplay(
            backStack = backStack,
            onBack = { if (backStack.size > 1) backStack.removeLastOrNull() },
            entryDecorators = listOf(
                rememberSaveableStateHolderNavEntryDecorator(),
                rememberViewModelStoreNavEntryDecorator(),
            ),
            entryProvider = entryProvider {
                entry<SplashKey> {
                    SplashScreen(
                        onSplashFinished = {
                            onSplashFinished()
                            backStack.clear()
                            backStack.add(ModeKey)
                        },
                    )
                }
                entry<ModeKey> {
                    ModeSelectionScreen(
                        onSingleLayerClick = { backStack.add(SingleWeldKey) },
                        onMultiLayerClick = { backStack.add(MultiLayerKey) },
                        onTBarClick = { backStack.add(TBarKey) },
                        onRobotTestClick = { backStack.add(RobotTestKey) },
                    )
                }
                entry<SingleWeldKey> {
                    val vm: SingleWeldViewModel = hiltViewModel()
                    WeldingShell(vm, vm, session, onExit = { backStack.removeLastOrNull() }) { onProject, onProcess ->
                        WeldPathScreen(vm, onProject, onProcess, onCheckUpdate = update::check)
                    }
                }
                entry<MultiLayerKey> {
                    val vm: MultiLayerWeldViewModel = hiltViewModel()
                    WeldingShell(vm, vm, session, onExit = { backStack.removeLastOrNull() }) { onProject, onProcess ->
                        MultiLayerScreen(vm, onProject, onProcess, onCheckUpdate = update::check)
                    }
                }
                entry<TBarKey> {
                    val vm: TBarWeldViewModel = hiltViewModel()
                    WeldingShell(vm, vm, session, onExit = { backStack.removeLastOrNull() }) { onProject, onProcess ->
                        TBarScreen(vm, onProject, onProcess, onCheckUpdate = update::check)
                    }
                }
                entry<RobotTestKey> {
                    RobotTestScreen(
                        viewModel = hiltViewModel<RobotTestViewModel>(),
                        onBack = { backStack.removeLastOrNull() },
                    )
                }
            },
        )
        AppUpdateHost(update)
    }
}
