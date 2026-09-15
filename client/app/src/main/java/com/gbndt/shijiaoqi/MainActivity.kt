package com.gbndt.shijiaoqi

import android.content.Intent
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.os.Environment
import android.provider.Settings
import android.util.Log
import android.view.InputDevice
import android.view.KeyEvent
import android.view.MotionEvent
import android.widget.Toast
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.Spacer
import androidx.compose.material3.Button
import androidx.compose.material3.Text
import androidx.compose.ui.unit.sp
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.runtime.Composable
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
import androidx.core.view.WindowCompat
import androidx.core.view.WindowInsetsCompat
import androidx.core.view.WindowInsetsControllerCompat
import androidx.lifecycle.viewmodel.compose.viewModel
import androidx.lifecycle.lifecycleScope
import kotlinx.coroutines.launch
import com.gbndt.shijiaoqi.ui.components.CustomStatusBar
import com.gbndt.shijiaoqi.ui.components.ProcessManagementScreen
import com.gbndt.shijiaoqi.ui.components.ProjectManagementScreen
import com.gbndt.shijiaoqi.ui.components.ToolEditDialog
import com.gbndt.shijiaoqi.ui.components.ToolListDialog
import com.gbndt.shijiaoqi.ui.components.PositionSelectionDialog
import com.gbndt.shijiaoqi.ui.components.SpeedSelectionDialog
import com.gbndt.shijiaoqi.ui.components.InstallPosSelectionDialog
import com.gbndt.shijiaoqi.ui.components.WeldPathScreen
import com.gbndt.shijiaoqi.ui.components.LoginGate
import com.gbndt.shijiaoqi.ui.components.SplashScreen
import com.gbndt.shijiaoqi.ui.components.UpdateDialog
import com.gbndt.shijiaoqi.ui.test.RobotTestScreen
import com.gbndt.shijiaoqi.ui.test.RobotTestViewModel
import com.gbndt.shijiaoqi.ui.theme.ShiJiaoQiTheme
import com.gbndt.shijiaoqi.ui.viewmodel.WeldPathViewModel

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.ui.text.font.FontWeight

sealed class AppScreen {
    object SplashScreen : AppScreen()
    object ModeSelection : AppScreen()
    object WeldPath : AppScreen()
    object ProjectManagement : AppScreen()
    data class ProcessManagement(val isSelectionMode: Boolean = false) : AppScreen()
    object RobotTest : AppScreen()
}

class MainActivity : ComponentActivity() {
    var joy_X1: Float = 0f
    var joy_Y1: Float = 0f
    var joy_X2: Float = 0f
    var joy_Y2: Float = 0f
    var 按钮上 = 0
    var 按钮下 = 0
    var 按钮左 = 0
    var 按钮右 = 0
    var 按钮A = 0
    var 按钮B = 0
    var 按钮X = 0
    var 按钮Y = 0
    var 按钮L1 = 0
    var 按钮L2 = 0
    var 按钮R1 = 0
    var 按钮R2 = 0
    var 按钮Start = 0
    var 按钮Select = 0

    var 按钮THUMBL = 0
    var 按钮THUMBR = 0

    // Long press handling for Button Y
    private val longPressHandler = android.os.Handler(android.os.Looper.getMainLooper())
    private val longPressRunnable = Runnable {
        if (::viewModel.isInitialized) {
            Log.d("GameController", "Button Y Long Pressed - Executing MoveL")
            viewModel.sendMoveLCommand()
        }
    }
    private var isLongPressTriggered = false

    // Long press handling for Button B (simulate welding)
    private val longPressBRunnable = Runnable {
        if (::viewModel.isInitialized && 按钮R1 == 0) {
            Log.d("GameController", "Long Press B - Simulate Welding")
            viewModel.startSimulation()
        }
    }

    // Long press handling for Button B + R1 (arc welding)
    private val longPressBR1Runnable = Runnable {
        if (::viewModel.isInitialized && 按钮R1 == 1) {
            Log.d("GameController", "Long Press B + R1 - Arc Welding")
            viewModel.stopControllerActive()
            viewModel.startArcWelding()
        }
    }

    private lateinit var viewModel: WeldPathViewModel

    private fun updateViewModelInput() {
        if (::viewModel.isInitialized) {
            viewModel.joyX1 = joy_X1
            viewModel.joyY1 = joy_Y1
            viewModel.joyX2 = joy_X2
            viewModel.joyY2 = joy_Y2
            
            viewModel.btnUp = 按钮上 == 1
            viewModel.btnDown = 按钮下 == 1
            viewModel.btnLeft = 按钮左 == 1
            viewModel.btnRight = 按钮右 == 1
            viewModel.btnL1 = 按钮L1 == 1
            viewModel.btnL2 = 按钮L2 == 1
            viewModel.btnR2 = 按钮R2 == 1
        }
    }

    override fun dispatchKeyEvent(event: KeyEvent): Boolean {
        Log.d("GameController", "KeyEvent: ${event.keyCode} ${event.action} ${event.repeatCount}")
        val isGameKey = when (event.keyCode) {
            KeyEvent.KEYCODE_DPAD_LEFT, KeyEvent.KEYCODE_DPAD_RIGHT,
            KeyEvent.KEYCODE_DPAD_UP, KeyEvent.KEYCODE_DPAD_DOWN,
            KeyEvent.KEYCODE_BUTTON_A, KeyEvent.KEYCODE_BUTTON_B,
            KeyEvent.KEYCODE_BUTTON_X, KeyEvent.KEYCODE_BUTTON_Y,
            KeyEvent.KEYCODE_BUTTON_R1, KeyEvent.KEYCODE_BUTTON_R2,
            KeyEvent.KEYCODE_BUTTON_L1, KeyEvent.KEYCODE_BUTTON_L2,
            KeyEvent.KEYCODE_BUTTON_START, KeyEvent.KEYCODE_BUTTON_SELECT,
            KeyEvent.KEYCODE_BUTTON_THUMBR, KeyEvent.KEYCODE_BUTTON_THUMBL -> true
            else -> false
        }

        if (isGameKey) {

            if (event.repeatCount == 0) {
                if (event.action == KeyEvent.ACTION_DOWN) {
                    var buttonName = ""
                    var isDpad = false
                    when (event.keyCode) {

                        KeyEvent.KEYCODE_BUTTON_A -> { 
                            按钮A = 1
                            buttonName = "A" 
                            if (::viewModel.isInitialized) {
                                viewModel.stopWelding(true)
                            }
                        }
                        KeyEvent.KEYCODE_BUTTON_B -> { 
                            按钮B = 1
                            buttonName = "B"
                            // Start Long Press Detection
                            longPressHandler.postDelayed(longPressBRunnable, 1000)
                            longPressHandler.postDelayed(longPressBR1Runnable, 1000)
                        }
                        KeyEvent.KEYCODE_BUTTON_X -> { 
                            按钮X = 1
                            buttonName = "X"
                            if (::viewModel.isInitialized) {
                                viewModel.collectData()
                            }
                        }
                        KeyEvent.KEYCODE_BUTTON_Y -> { 
                            按钮Y = 1
                            buttonName = "Y"
                            // Start Long Press Detection
                            isLongPressTriggered = false
                            longPressHandler.postDelayed(longPressRunnable, 1000) // 1 second for long press
                        }
                        KeyEvent.KEYCODE_BUTTON_R1 -> { 按钮R1 = 1; buttonName = "R1" }
                        KeyEvent.KEYCODE_BUTTON_R2 -> {
                            按钮R2 = 1
                            buttonName = "R2"
                        }
                        KeyEvent.KEYCODE_BUTTON_L1 -> { 按钮L1 = 1; buttonName = "L1" }
                        KeyEvent.KEYCODE_BUTTON_L2 -> {
                            按钮L2 = 1
                            buttonName = "L2"
                        }
                        KeyEvent.KEYCODE_BUTTON_START -> { 
                            按钮Start = 1
                            buttonName = "Start"
                            if (::viewModel.isInitialized) {
                                viewModel.toggleControllerActive()
                            }
                        }
                        KeyEvent.KEYCODE_BUTTON_SELECT -> {
                            按钮Select = 1
                            buttonName = "Select"
                            if (::viewModel.isInitialized) {
                                this@MainActivity.lifecycleScope.launch {
                                    viewModel.sendManualCommand(303, "Mode(1)")
                                    kotlinx.coroutines.delay(50)
                                    viewModel.sendManualCommand(302, "RobotEnable(1)")
                                }
                            }
                        }
                        KeyEvent.KEYCODE_BUTTON_THUMBR -> { 按钮THUMBR = 1; buttonName = "ThumbR" }
                        KeyEvent.KEYCODE_BUTTON_THUMBL -> { 按钮THUMBL = 1; buttonName = "ThumbL" }
                        KeyEvent.KEYCODE_DPAD_LEFT -> { 按钮左 = 1; isDpad = true }
                        KeyEvent.KEYCODE_DPAD_RIGHT -> { 按钮右 = 1; isDpad = true }
                        KeyEvent.KEYCODE_DPAD_UP -> { 按钮上 = 1; isDpad = true }
                        KeyEvent.KEYCODE_DPAD_DOWN -> { 按钮下 = 1; isDpad = true }
                    }
                    
                    if (isDpad) {
                        Log.d("GameController", "Move: L($joy_X1, $joy_Y1) R($joy_X2, $joy_Y2) Dpad($按钮左, $按钮右, $按钮上, $按钮下)")
                    } else if (buttonName.isNotEmpty()) {
                        Log.d("GameController", "Button Down: $buttonName")
                    }
                    updateViewModelInput()
                } else if (event.action == KeyEvent.ACTION_UP) {
                     var isDpad = false
                     when (event.keyCode) {
                        KeyEvent.KEYCODE_DPAD_LEFT -> { 按钮左 = 0; isDpad = true }
                        KeyEvent.KEYCODE_DPAD_RIGHT -> { 按钮右 = 0; isDpad = true }
                        KeyEvent.KEYCODE_DPAD_UP -> { 按钮上 = 0; isDpad = true }
                        KeyEvent.KEYCODE_DPAD_DOWN -> { 按钮下 = 0; isDpad = true }
                        KeyEvent.KEYCODE_BUTTON_A -> 按钮A = 0
                        KeyEvent.KEYCODE_BUTTON_B -> {
                            按钮B = 0
                            longPressHandler.removeCallbacks(longPressBRunnable)
                            longPressHandler.removeCallbacks(longPressBR1Runnable)
                        }
                        KeyEvent.KEYCODE_BUTTON_X -> 按钮X = 0
                        KeyEvent.KEYCODE_BUTTON_Y -> {
                            按钮Y = 0
                            longPressHandler.removeCallbacks(longPressRunnable)
                        }
                        KeyEvent.KEYCODE_BUTTON_R1 -> 按钮R1 = 0
                        KeyEvent.KEYCODE_BUTTON_R2 -> {
                            按钮R2 = 0
                        }
                        KeyEvent.KEYCODE_BUTTON_L1 -> 按钮L1 = 0
                        KeyEvent.KEYCODE_BUTTON_L2 -> {
                            按钮L2 = 0
                        }
                        KeyEvent.KEYCODE_BUTTON_START -> 按钮Start = 0
                        KeyEvent.KEYCODE_BUTTON_SELECT -> 按钮Select = 0
                        KeyEvent.KEYCODE_BUTTON_THUMBR -> 按钮THUMBR = 0
                        KeyEvent.KEYCODE_BUTTON_THUMBL -> 按钮THUMBL = 0
                    }
                    if (isDpad) {
                        Log.d("GameController", "Move: L($joy_X1, $joy_Y1) R($joy_X2, $joy_Y2) Dpad($按钮左, $按钮右, $按钮上, $按钮下)")
                    }
                    updateViewModelInput()
                }
            }
            return true
        }
        return super.dispatchKeyEvent(event)
    }

    override fun dispatchGenericMotionEvent(event: MotionEvent): Boolean {
        // Log generic motion events to debug D-pad issues
        if (event.action == MotionEvent.ACTION_MOVE) {
             // Log.d("GameController", "GenericMotion: ${event.source}")
        }

        if (event.source and InputDevice.SOURCE_JOYSTICK != 0) {
            // Process Joystick Axes
            val newJoyX1 = event.getAxisValue(MotionEvent.AXIS_X)
            val newJoyY1 = event.getAxisValue(MotionEvent.AXIS_Y)
            val newJoyX2 = event.getAxisValue(MotionEvent.AXIS_Z)
            val newJoyY2 = event.getAxisValue(MotionEvent.AXIS_RZ)

            // Process Hat Switch (D-Pad)
            // Many controllers report D-Pad as Hat Switch axes instead of KeyEvents
            val hatX = event.getAxisValue(MotionEvent.AXIS_HAT_X)
            val hatY = event.getAxisValue(MotionEvent.AXIS_HAT_Y)

            // Map Hat to Buttons
            // Hat Y: -1 (Up), 1 (Down)
            // Hat X: -1 (Left), 1 (Right)
            val newBtnUp = if (hatY < -0.5f) 1 else 0
            val newBtnDown = if (hatY > 0.5f) 1 else 0
            val newBtnLeft = if (hatX < -0.5f) 1 else 0
            val newBtnRight = if (hatX > 0.5f) 1 else 0

            var changed = false

            if (joy_X1 != newJoyX1 || joy_Y1 != newJoyY1 || joy_X2 != newJoyX2 || joy_Y2 != newJoyY2) {
                joy_X1 = newJoyX1
                joy_Y1 = newJoyY1
                joy_X2 = newJoyX2
                joy_Y2 = newJoyY2
                changed = true
            }

            // Update D-Pad buttons from Hat Switch if they changed
            // We use bitwise OR to combine with KeyEvent state if needed, 
            // but here we just update if the Hat state is explicit.
            // Note: If KeyEvents are working, this might overlap, but since user says KeyEvents are missing, this is the fix.
            if (按钮上 != newBtnUp || 按钮下 != newBtnDown || 按钮左 != newBtnLeft || 按钮右 != newBtnRight) {
                // Only update if Hat actually has a value or if we need to clear it.
                // To avoid overriding KeyEvents if Hat is 0 but Key is pressed, strictly we should merge.
                // But typically a controller uses one or the other.
                // Let's assume Hat takes precedence if it's active.
                
                // However, simply assigning is safer if we assume Hat is the only source working.
                按钮上 = newBtnUp
                按钮下 = newBtnDown
                按钮左 = newBtnLeft
                按钮右 = newBtnRight
                changed = true
            }

            if (changed) {
                Log.d("GameController", "Move: L($joy_X1, $joy_Y1) R($joy_X2, $joy_Y2) Dpad($按钮左, $按钮右, $按钮上, $按钮下)")
                updateViewModelInput()
            }

            return true
        }
        return super.dispatchGenericMotionEvent(event)
    }

    override fun onDestroy() {
        if (isFinishing) {
            (application as ShiJiaoQiApp).bag.logout()
        }
        super.onDestroy()
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        
        // Keep screen on
        window.addFlags(android.view.WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
        
        // Request Storage Permissions
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
            if (!Environment.isExternalStorageManager()) {
                try {
                    val intent = Intent(Settings.ACTION_MANAGE_APP_ALL_FILES_ACCESS_PERMISSION)
                    intent.addCategory("android.intent.category.DEFAULT")
                    intent.data = Uri.parse("package:$packageName")
                    startActivity(intent)
                } catch (e: Exception) {
                    val intent = Intent(Settings.ACTION_MANAGE_ALL_FILES_ACCESS_PERMISSION)
                    startActivity(intent)
                }
            }
        } else {
            if (checkSelfPermission(android.Manifest.permission.WRITE_EXTERNAL_STORAGE) != android.content.pm.PackageManager.PERMISSION_GRANTED) {
                requestPermissions(
                    arrayOf(
                        android.Manifest.permission.WRITE_EXTERNAL_STORAGE,
                        android.Manifest.permission.READ_EXTERNAL_STORAGE
                    ), 1001
                )
            }
        }

        // Hide system bars and enable full screen
        val windowInsetsController = WindowCompat.getInsetsController(window, window.decorView)
        windowInsetsController.systemBarsBehavior = WindowInsetsControllerCompat.BEHAVIOR_SHOW_TRANSIENT_BARS_BY_SWIPE
        windowInsetsController.hide(WindowInsetsCompat.Type.systemBars())

        setContent {
            ShiJiaoQiTheme {
                // A surface container using the 'background' color from the theme
                Surface(modifier = Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) {
                    viewModel = viewModel<WeldPathViewModel>()
                    val robotTestViewModel = viewModel<RobotTestViewModel>()
                    val context = LocalContext.current
                    
                    LaunchedEffect(Unit) {
                        viewModel.toastEvent.collect { message ->
                            Toast.makeText(context, message, Toast.LENGTH_SHORT).show()
                        }
                    }

                    var currentScreen by remember { mutableStateOf<AppScreen>(AppScreen.SplashScreen) }

                    // Global Dialogs
                    ToolListDialog(
                        viewModel = viewModel,
                        onDismiss = { viewModel.isToolListDialogVisible = false }
                    )
                    ToolEditDialog(viewModel = viewModel)
                    
                    PositionSelectionDialog(
                        viewModel = viewModel,
                        onDismiss = { viewModel.isPositionDialogVisible = false }
                    )
                    
                    SpeedSelectionDialog(
                        viewModel = viewModel,
                        onDismiss = { viewModel.isSpeedDialogVisible = false }
                    )

                    InstallPosSelectionDialog(
                        viewModel = viewModel,
                        onDismiss = { viewModel.isInstallPosDialogVisible = false }
                    )

                    Box(modifier = Modifier.fillMaxSize()) {
                        if (currentScreen is AppScreen.SplashScreen) {
                            SplashScreen(onSplashFinished = { currentScreen = AppScreen.ModeSelection })
                        } else if (currentScreen is AppScreen.ModeSelection) {
                            ModeSelectionScreen(
                                onSingleLayerClick = { currentScreen = AppScreen.WeldPath },
                                onMultiLayerClick = {
                                    val intent = Intent(this@MainActivity, com.gbndt.shijiaoqi.ui.multilayer.MultiLayerActivity::class.java)
                                    startActivity(intent)
                                },
                                onTBarClick = {
                                    val intent = Intent(this@MainActivity, com.gbndt.shijiaoqi.ui.tbar.TBarActivity::class.java)
                                    startActivity(intent)
                                },
                                onRobotTestClick = { currentScreen = AppScreen.RobotTest }
                            )
                        } else if (currentScreen is AppScreen.RobotTest) {
                            androidx.activity.compose.BackHandler {
                                currentScreen = AppScreen.ModeSelection
                            }
                            RobotTestScreen(
                                viewModel = robotTestViewModel,
                                onBack = { currentScreen = AppScreen.ModeSelection }
                            )
                        } else {
                            Column(modifier = Modifier.fillMaxSize()) {
                                // Custom Status Bar at the top
                                CustomStatusBar(
                                    viewModel = viewModel,
                                    onProjectClick = { currentScreen = AppScreen.ProjectManagement }
                                )
                                
                                // Screen content takes the rest of the space
                                Box(modifier = Modifier.weight(1f)) {
                                    when (val screen = currentScreen) {
                                        is AppScreen.WeldPath -> {
                                            androidx.activity.compose.BackHandler {
                                                currentScreen = AppScreen.ModeSelection
                                            }
                                            WeldPathScreen(
                                                viewModel = viewModel,
                                                onNavigateToProjectManagement = { currentScreen = AppScreen.ProjectManagement },
                                                onNavigateToProcessManagement = { isSelectionMode -> 
                                                    currentScreen = AppScreen.ProcessManagement(isSelectionMode) 
                                                }
                                            )
                                        }
                                        is AppScreen.ProjectManagement -> {
                                            ProjectManagementScreen(
                                                viewModel = viewModel,
                                                onBack = { currentScreen = AppScreen.WeldPath }
                                            )
                                        }
                                        is AppScreen.ProcessManagement -> {
                                            ProcessManagementScreen(
                                                viewModel = viewModel,
                                                onBack = {
                                                    viewModel.cancelAddProcessVariant()
                                                    currentScreen = AppScreen.WeldPath
                                                },
                                                isSelectionMode = screen.isSelectionMode
                                            )
                                        }
                                        else -> {}
                                    }
                                }
                            }

                            // Registration Check - Blocks Interaction if Not Registered
                            if (! (application as ShiJiaoQiApp).bag.loggedIn) {
                                (application as ShiJiaoQiApp).serial = viewModel.machineCode
                                Box(
                                    modifier = Modifier
                                        .fillMaxSize()
                                        .background(Color.Black.copy(alpha = 0.5f))
                                        .zIndex(200f),
                                    contentAlignment = Alignment.Center
                                ) {
                                    LoginGate(
                                        bag = (application as ShiJiaoQiApp).bag,
                                        serial = viewModel.machineCode,
                                        onRefreshSerial = { viewModel.checkRegistration() },
                                    )
                                 }
                             }
                             
                             // Update Dialog
                             if (viewModel.isUpdateDialogVisible && viewModel.updateInfo != null) {
                                 UpdateDialog(
                                     updateInfo = viewModel.updateInfo!!,
                                     onConfirm = { viewModel.startUpdateDownload() },
                                     onDismiss = { viewModel.isUpdateDialogVisible = false }
                                 )
                             }
                         }

                        // Controller Active Indicator (Red Border)
                        if (viewModel.isControllerActive) {
                            Box(
                                modifier = Modifier
                                    .fillMaxSize()
                                    .border(8.dp, Color.Red)
                                    .zIndex(100f) // Ensure it's on top
                            )
                        }
                    }
                }
            }
        }
    }
}

@Composable
fun ModeSelectionScreen(
    onSingleLayerClick: () -> Unit,
    onMultiLayerClick: () -> Unit,
    onTBarClick: () -> Unit,
    onRobotTestClick: () -> Unit
) {
    Box(
        modifier = Modifier
            .fillMaxSize()
            .background(
                brush = androidx.compose.ui.graphics.Brush.verticalGradient(
                    colors = listOf(
                        Color(0xFFF5F7FA),
                        Color(0xFFC3CFE2)
                    )
                )
            ),
        contentAlignment = Alignment.Center
    ) {
        Column(
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.Center
        ) {
            Text(
                text = "焊接作业模式选择",
                fontSize = 32.sp,
                fontWeight = FontWeight.Bold,
                color = Color(0xFF2C3E50),
                modifier = Modifier.padding(bottom = 48.dp)
            )

            Row(
                horizontalArrangement = Arrangement.spacedBy(32.dp),
                verticalAlignment = Alignment.CenterVertically
            ) {
                ModeCard(
                    title = "单层单道",
                    subtitle = "基础焊接示教",
                    color = Color(0xFF4CAF50),
                    onClick = onSingleLayerClick
                )
                
                ModeCard(
                    title = "多层多道",
                    subtitle = "复杂堆焊示教",
                    color = Color(0xFF2196F3),
                    onClick = onMultiLayerClick
                )

                ModeCard(
                    title = "T排对接",
                    subtitle = "坡口对接焊接",
                    color = Color(0xFF00897B),
                    onClick = onTBarClick
                )

                // 发布时隐藏指令测试入口，需要时再打开
                // ModeCard(
                //     title = "指令测试",
                //     subtitle = "机械臂指令调试",
                //     color = Color(0xFFFF9800),
                //     onClick = onRobotTestClick
                // )
            }
        }
    }
}

@OptIn(androidx.compose.material3.ExperimentalMaterial3Api::class)
@Composable
fun ModeCard(
    title: String,
    subtitle: String,
    color: Color,
    onClick: () -> Unit
) {
    Card(
        onClick = onClick,
        modifier = Modifier
            .size(220.dp, 180.dp),
        elevation = CardDefaults.cardElevation(defaultElevation = 6.dp),
        colors = CardDefaults.cardColors(containerColor = Color.White),
        shape = RoundedCornerShape(16.dp)
    ) {
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(16.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.Center
        ) {
            // Icon placeholder - Circle with letter
            Box(
                modifier = Modifier
                    .size(64.dp)
                    .background(color.copy(alpha = 0.1f), androidx.compose.foundation.shape.CircleShape),
                contentAlignment = Alignment.Center
            ) {
                Text(
                    text = title.take(1),
                    fontSize = 32.sp,
                    fontWeight = FontWeight.Bold,
                    color = color
                )
            }
            
            Spacer(modifier = Modifier.height(16.dp))
            
            Text(
                text = title,
                fontSize = 20.sp,
                fontWeight = FontWeight.Bold,
                color = Color(0xFF333333)
            )
            
            Spacer(modifier = Modifier.height(4.dp))
            
            Text(
                text = subtitle,
                fontSize = 12.sp,
                color = Color.Gray
            )
        }
    }
}
