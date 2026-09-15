package com.gbndt.shijiaoqi.ui.navigation

import androidx.compose.runtime.Composable
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.navigation.NavHostController
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.rememberNavController
import com.gbndt.shijiaoqi.data.repository.SessionRepository
import com.gbndt.shijiaoqi.data.session.DeviceSerialHolder
import com.gbndt.shijiaoqi.ui.login.SplashScreen
import com.gbndt.shijiaoqi.ui.robottest.RobotTestScreen
import com.gbndt.shijiaoqi.ui.robottest.RobotTestViewModel
import com.gbndt.shijiaoqi.ui.welding.WeldingShell
import com.gbndt.shijiaoqi.ui.welding.multilayer.MultiLayerScreen
import com.gbndt.shijiaoqi.ui.welding.multilayer.MultiLayerViewModel
import com.gbndt.shijiaoqi.ui.welding.single.WeldPathScreen
import com.gbndt.shijiaoqi.ui.welding.single.WeldPathViewModel
import com.gbndt.shijiaoqi.ui.welding.tbar.TBarScreen
import com.gbndt.shijiaoqi.ui.welding.tbar.TBarViewModel

/** 顶层去处；焊接屏内部的工程/工艺子页仍由 WeldingShell 自己管。 */
object Route {
    const val SPLASH = "splash"
    const val MODE = "mode"
    const val SINGLE = "weld/single"
    const val MULTILAYER = "weld/multilayer"
    const val TBAR = "weld/tbar"
    const val ROBOT_TEST = "robot-test"
}

/** 一个 Activity 一张图：开屏 → 选模式 → 三种焊接或指令测试。 */
@Composable
fun WMeshNavHost(
    session: SessionRepository,
    deviceSerial: DeviceSerialHolder,
    onSplashFinished: () -> Unit,
    navController: NavHostController = rememberNavController(),
) {
    NavHost(navController = navController, startDestination = Route.SPLASH) {
        composable(Route.SPLASH) {
            SplashScreen(
                onSplashFinished = {
                    onSplashFinished()
                    navController.navigate(Route.MODE) {
                        popUpTo(Route.SPLASH) { inclusive = true }
                    }
                },
            )
        }

        composable(Route.MODE) {
            ModeSelectionScreen(
                onSingleLayerClick = { navController.navigate(Route.SINGLE) },
                onMultiLayerClick = { navController.navigate(Route.MULTILAYER) },
                onTBarClick = { navController.navigate(Route.TBAR) },
                onRobotTestClick = { navController.navigate(Route.ROBOT_TEST) },
            )
        }

        composable(Route.SINGLE) {
            val vm: WeldPathViewModel = hiltViewModel()
            WeldingShell(vm, session, deviceSerial, onExit = { navController.popBackStack() }) { onProject, onProcess ->
                WeldPathScreen(vm, onProject, onProcess)
            }
        }

        composable(Route.MULTILAYER) {
            val vm: MultiLayerViewModel = hiltViewModel()
            WeldingShell(vm, session, deviceSerial, onExit = { navController.popBackStack() }) { onProject, onProcess ->
                MultiLayerScreen(vm, onProject, onProcess)
            }
        }

        composable(Route.TBAR) {
            val vm: TBarViewModel = hiltViewModel()
            WeldingShell(vm, session, deviceSerial, onExit = { navController.popBackStack() }) { onProject, onProcess ->
                TBarScreen(vm, onProject, onProcess)
            }
        }

        composable(Route.ROBOT_TEST) {
            RobotTestScreen(
                viewModel = hiltViewModel<RobotTestViewModel>(),
                onBack = { navController.popBackStack() },
            )
        }
    }
}
