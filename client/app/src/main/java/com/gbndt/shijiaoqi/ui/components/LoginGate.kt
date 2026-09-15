package com.gbndt.shijiaoqi.ui.components

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import com.gbndt.shijiaoqi.platform.session.BagSession
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

@Composable
fun LoginGate(
    bag: BagSession,
    serial: String,
    onRefreshSerial: () -> Unit,
) {
    var url by remember { mutableStateOf(bag.savedUrl()) }
    var factoryId by remember { mutableStateOf(bag.savedFactoryId()) }
    var clientId by remember { mutableStateOf(bag.savedClientId()) }
    var login by remember { mutableStateOf("") }
    var password by remember { mutableStateOf("") }
    val scope = rememberCoroutineScope()

    LaunchedEffect(serial) {
        while (serial.isBlank()) {
            onRefreshSerial()
            delay(2000)
        }
    }

    Dialog(
        onDismissRequest = {},
        properties = DialogProperties(dismissOnBackPress = false, dismissOnClickOutside = false, usePlatformDefaultWidth = false),
    ) {
        Card(
            modifier = Modifier
                .widthIn(max = 720.dp)
                .padding(16.dp),
            shape = RoundedCornerShape(16.dp),
            colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surface),
            elevation = CardDefaults.cardElevation(defaultElevation = 8.dp),
        ) {
            Column(
                modifier = Modifier.padding(24.dp),
                verticalArrangement = Arrangement.spacedBy(10.dp),
            ) {
                Text("本厂登录", fontSize = 20.sp, fontWeight = FontWeight.Bold)
                Text(
                    if (serial.isBlank()) "正在读取设备号…" else "设备号 $serial",
                    fontSize = 14.sp,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                OutlinedTextField(url, { url = it }, label = { Text("厂地址") }, singleLine = true, modifier = Modifier.fillMaxWidth())
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp), modifier = Modifier.fillMaxWidth()) {
                    OutlinedTextField(factoryId, { factoryId = it }, label = { Text("工厂身份") }, singleLine = true, modifier = Modifier.weight(1f))
                    OutlinedTextField(clientId, { clientId = it }, label = { Text("Client 身份") }, singleLine = true, modifier = Modifier.weight(1f))
                }
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp), modifier = Modifier.fillMaxWidth()) {
                    OutlinedTextField(login, { login = it }, label = { Text("登录名") }, singleLine = true, modifier = Modifier.weight(1f))
                    OutlinedTextField(
                        password,
                        { password = it },
                        label = { Text("密码") },
                        singleLine = true,
                        visualTransformation = PasswordVisualTransformation(),
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password),
                        modifier = Modifier.weight(1f),
                    )
                }
                bag.error?.let { Text(it, color = MaterialTheme.colorScheme.error, fontSize = 13.sp) }
                Button(
                    onClick = {
                        scope.launch {
                            runCatching {
                                withContext(Dispatchers.IO) {
                                    bag.login(url, factoryId, clientId, login, password)
                                }
                            }
                        }
                    },
                    enabled = !bag.busy && serial.isNotBlank() && url.isNotBlank() && factoryId.isNotBlank() && clientId.isNotBlank() && login.isNotBlank() && password.isNotBlank(),
                    modifier = Modifier.align(Alignment.End),
                ) {
                    if (bag.busy) CircularProgressIndicator(modifier = Modifier.size(16.dp).padding(end = 8.dp), strokeWidth = 2.dp)
                    Text("登录")
                }
            }
        }
    }
}
