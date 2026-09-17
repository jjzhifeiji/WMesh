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
import androidx.compose.runtime.getValue
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.gbndt.shijiaoqi.data.repository.SessionRepository
import com.gbndt.shijiaoqi.ui.account.AccountPasswordScreen
import com.gbndt.shijiaoqi.ui.account.AccountScreen
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
import com.gbndt.shijiaoqi.data.log.PadLog
import kotlinx.coroutines.launch
import androidx.compose.runtime.rememberCoroutineScope

/** 一个 Activity 一张栈：冷启动开屏 → 选模式 → 三种焊接或指令测试。 */
@Composable
fun WMeshNavHost(
    session: SessionRepository,
    onSplashFinished: () -> Unit,
    startKey: NavKey = SplashKey,
    backStack: NavBackStack<NavKey> = rememberNavBackStack(startKey),
) {
    val update: AppUpdateViewModel = hiltViewModel()
    val scope = rememberCoroutineScope()
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
                            PadLog.info("Nav", "splash done")
                            onSplashFinished()
                            backStack.clear()
                            backStack.add(ModeKey)
                        },
                    )
                }
                entry<ModeKey> {
                    val sess by session.state.collectAsStateWithLifecycle()
                    ModeSelectionScreen(
                        personName = sess.personName,
                        loginName = sess.loginName,
                        onOpenAccount = {
                            PadLog.info("Nav", "open account")
                            backStack.add(AccountKey)
                        },
                        onSingleLayerClick = {
                            PadLog.info("Nav", "open single")
                            backStack.add(SingleWeldKey)
                        },
                        onMultiLayerClick = {
                            PadLog.info("Nav", "open multilayer")
                            backStack.add(MultiLayerKey)
                        },
                        onTBarClick = {
                            PadLog.info("Nav", "open tbar")
                            backStack.add(TBarKey)
                        },
                        onRobotTestClick = {
                            PadLog.info("Nav", "open robot-test")
                            backStack.add(RobotTestKey)
                        },
                    )
                }
                entry<AccountKey> {
                    val sess by session.state.collectAsStateWithLifecycle()
                    AccountScreen(
                        personName = sess.personName,
                        loginName = sess.loginName,
                        roles = sess.roles,
                        onBack = { backStack.removeLastOrNull() },
                        onChangePassword = {
                            PadLog.info("Nav", "open change-password")
                            backStack.add(AccountPasswordKey)
                        },
                        onLogout = {
                            scope.launch {
                                session.logout()
                                backStack.removeLastOrNull()
                            }
                        },
                        onCheckUpdate = update::check,
                    )
                }
                entry<AccountPasswordKey> {
                    AccountPasswordScreen(
                        onBack = { backStack.removeLastOrNull() },
                        onChangePassword = session::changePassword,
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
