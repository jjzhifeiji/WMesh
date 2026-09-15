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
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.zIndex
import androidx.core.view.WindowCompat
import androidx.core.view.WindowInsetsCompat
import androidx.core.view.WindowInsetsControllerCompat
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.lifecycleScope
import kotlinx.coroutines.launch
import com.gbndt.shijiaoqi.data.repository.SessionRepository
import com.gbndt.shijiaoqi.data.session.DeviceSerialHolder
import com.gbndt.shijiaoqi.ui.login.LoginGate
import com.gbndt.shijiaoqi.ui.navigation.LocalWeldInput
import com.gbndt.shijiaoqi.ui.navigation.WMeshNavHost
import com.gbndt.shijiaoqi.ui.theme.ShiJiaoQiTheme
import com.gbndt.shijiaoqi.ui.welding.WeldViewModelInterface
import dagger.hilt.android.AndroidEntryPoint
import javax.inject.Inject

/** 唯一的 Activity：权限、全屏、手柄按键，其余都交给导航图。 */
@AndroidEntryPoint
class MainActivity : ComponentActivity() {
    @Inject
    lateinit var sessionRepository: SessionRepository

    @Inject
    lateinit var deviceSerial: DeviceSerialHolder

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
        if (activeWeld != null) {
            Log.d("GameController", "Button Y Long Pressed - Executing MoveL")
            activeWeld?.sendMoveLCommand()
        }
    }
    private var isLongPressTriggered = false

    // Long press handling for Button B (simulate welding)
    private val longPressBRunnable = Runnable {
        if (activeWeld != null && 按钮R1 == 0) {
            Log.d("GameController", "Long Press B - Simulate Welding")
            activeWeld?.startSimulation()
        }
    }

    // Long press handling for Button B + R1 (arc welding)
    private val longPressBR1Runnable = Runnable {
        if (activeWeld != null && 按钮R1 == 1) {
            Log.d("GameController", "Long Press B + R1 - Arc Welding")
            activeWeld?.stopControllerActive()
            activeWeld?.startArcWelding()
        }
    }

    /** 当前在屏的焊接 ViewModel；手柄按键只发给它。 */
    @Volatile
    var activeWeld: WeldViewModelInterface? = null

    private fun updateViewModelInput() {
        if (activeWeld != null) {
            activeWeld?.joyX1 = joy_X1
            activeWeld?.joyY1 = joy_Y1
            activeWeld?.joyX2 = joy_X2
            activeWeld?.joyY2 = joy_Y2
            
            activeWeld?.btnUp = 按钮上 == 1
            activeWeld?.btnDown = 按钮下 == 1
            activeWeld?.btnLeft = 按钮左 == 1
            activeWeld?.btnRight = 按钮右 == 1
            activeWeld?.btnL1 = 按钮L1 == 1
            activeWeld?.btnL2 = 按钮L2 == 1
            activeWeld?.btnR2 = 按钮R2 == 1
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
            if (!sessionRepository.state.value.loggedIn) return true

            if (event.repeatCount == 0) {
                if (event.action == KeyEvent.ACTION_DOWN) {
                    var buttonName = ""
                    var isDpad = false
                    when (event.keyCode) {

                        KeyEvent.KEYCODE_BUTTON_A -> { 
                            按钮A = 1
                            buttonName = "A" 
                            if (activeWeld != null) {
                                activeWeld?.stopWelding(true)
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
                            if (activeWeld != null) {
                                activeWeld?.collectData()
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
                            if (activeWeld != null) {
                                activeWeld?.toggleControllerActive()
                            }
                        }
                        KeyEvent.KEYCODE_BUTTON_SELECT -> {
                            按钮Select = 1
                            buttonName = "Select"
                            if (activeWeld != null) {
                                this@MainActivity.lifecycleScope.launch {
                                    activeWeld?.sendManualCommand(303, "Mode(1)")
                                    kotlinx.coroutines.delay(50)
                                    activeWeld?.sendManualCommand(302, "RobotEnable(1)")
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
            if (!sessionRepository.state.value.loggedIn) return true

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
            sessionRepository.logout()
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

        // 全屏、隐藏系统栏
        WindowCompat.setDecorFitsSystemWindows(window, false)
        val windowInsetsController = WindowCompat.getInsetsController(window, window.decorView)
        windowInsetsController.systemBarsBehavior = WindowInsetsControllerCompat.BEHAVIOR_SHOW_TRANSIENT_BARS_BY_SWIPE
        windowInsetsController.hide(WindowInsetsCompat.Type.systemBars())

        setContent {
            ShiJiaoQiTheme {
                Surface(modifier = Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) {
                    val session by sessionRepository.state.collectAsStateWithLifecycle()
                    var splashDone by remember { mutableStateOf(false) }
                    val register = remember { { vm: WeldViewModelInterface? -> activeWeld = vm } }

                    Box(modifier = Modifier.fillMaxSize()) {
                        CompositionLocalProvider(LocalWeldInput provides register) {
                            WMeshNavHost(
                                session = sessionRepository,
                                deviceSerial = deviceSerial,
                                onSplashFinished = { splashDone = true },
                            )
                        }

                        // 未登录就盖住整屏，开屏动画期间不盖
                        if (splashDone && !session.loggedIn) {
                            Box(
                                modifier = Modifier
                                    .fillMaxSize()
                                    .background(Color.Black.copy(alpha = 0.5f))
                                    .zIndex(200f),
                                contentAlignment = Alignment.Center,
                            ) {
                                LoginGate()
                            }
                        }
                    }
                }
            }
        }
    }
}
