package com.gbndt.shijiaoqi.ui.login

import androidx.compose.foundation.background
import androidx.compose.foundation.Image
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import com.gbndt.shijiaoqi.R
import com.gbndt.shijiaoqi.ui.welding.WeldViewModelInterface
import kotlinx.coroutines.delay

@Composable
fun RegistrationDialog(
    viewModel: WeldViewModelInterface
) {
    var inputCode by remember { mutableStateOf("") }
    var showError by remember { mutableStateOf(false) }
    
    // Fetch Machine Code when dialog opens
    LaunchedEffect(Unit) {
        while (viewModel.machineCode.isEmpty()) {
            viewModel.checkRegistration()
            delay(2000) // Retry every 2 seconds
        }
    }

    Dialog(
        onDismissRequest = { /* Prevent dismiss */ },
        properties = DialogProperties(
            dismissOnBackPress = false,
            dismissOnClickOutside = false,
            usePlatformDefaultWidth = false // Add this to allow custom width
        )
    ) {
        Card(
            modifier = Modifier
                .fillMaxWidth(0.6f) // Takes up 60% of the screen width
                .padding(16.dp),
            shape = RoundedCornerShape(16.dp),
            colors = CardDefaults.cardColors(containerColor = Color.White),
            elevation = CardDefaults.cardElevation(defaultElevation = 8.dp)
        ) {
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(24.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(24.dp)
            ) {
                // Left side: WeChat QR Code
                Column(
                    horizontalAlignment = Alignment.CenterHorizontally,
                    modifier = Modifier.weight(1f)
                ) {
                    Image(
                        painter = painterResource(id = R.drawable.weixin),
                        contentDescription = "WeChat QR Code",
                        modifier = Modifier.size(200.dp)
                    )
                    Spacer(modifier = Modifier.height(8.dp))
                    Text(
                        text = "扫码联系管理员获取注册码",
                        fontSize = 14.sp,
                        color = Color.Gray
                    )
                }

                // Right side: Registration Form
                Column(
                    modifier = Modifier.weight(1.5f),
                    horizontalAlignment = Alignment.CenterHorizontally,
                    verticalArrangement = Arrangement.spacedBy(16.dp)
                ) {
                    Text(
                        text = "软件注册",
                        fontSize = 20.sp,
                        fontWeight = FontWeight.Bold,
                        color = Color(0xFF333333)
                    )

                    Divider()

                    Column(horizontalAlignment = Alignment.CenterHorizontally) {
                        Text("机器码", fontSize = 14.sp, color = Color.Gray)
                        Spacer(modifier = Modifier.height(8.dp))
                        if (viewModel.machineCode.isNotEmpty()) {
                            Text(
                                text = viewModel.machineCode,
                                fontSize = 18.sp,
                                fontWeight = FontWeight.Bold,
                                color = Color(0xFF2196F3)
                            )
                        } else {
                            CircularProgressIndicator(
                                modifier = Modifier.size(24.dp),
                                strokeWidth = 2.dp
                            )
                            Text("正在获取机器码...", fontSize = 12.sp, color = Color.Gray, modifier = Modifier.padding(top = 4.dp))
                        }
                    }

                    OutlinedTextField(
                        value = inputCode,
                        onValueChange = { 
                            inputCode = it
                            showError = false
                        },
                        label = { Text("请输入注册码") },
                        singleLine = true,
                        modifier = Modifier.fillMaxWidth(),
                        isError = showError
                    )

                    if (showError) {
                        Text(
                            text = "注册码错误，请重试",
                            color = Color.Red,
                            fontSize = 12.sp
                        )
                    }

                    Button(
                        onClick = {
                            if (viewModel.register(inputCode)) {
                                // Registration successful
                            } else {
                                showError = true
                            }
                        },
                        modifier = Modifier.fillMaxWidth(),
                        enabled = viewModel.machineCode.isNotEmpty() && inputCode.isNotEmpty()
                    ) {
                        Text("注册")
                    }
                }
            }
        }
    }
}
