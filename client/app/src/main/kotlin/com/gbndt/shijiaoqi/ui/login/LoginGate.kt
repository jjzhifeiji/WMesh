package com.gbndt.shijiaoqi.ui.login

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
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ExposedDropdownMenuBox
import androidx.compose.material3.ExposedDropdownMenuDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
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
import com.gbndt.shijiaoqi.data.session.BagSession
import com.gbndt.shijiaoqi.data.remote.FactoryOffer
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun LoginGate(
    bag: BagSession,
) {
    var login by remember { mutableStateOf("") }
    var password by remember { mutableStateOf("") }
    var offers by remember { mutableStateOf<List<FactoryOffer>>(emptyList()) }
    var selected by remember { mutableStateOf<FactoryOffer?>(null) }
    var scanned by remember { mutableStateOf(false) }
    var menuOpen by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()

    fun scan() {
        scope.launch {
            val hits = runCatching {
                withContext(Dispatchers.IO) { bag.findFactories() }
            }.getOrDefault(emptyList())
            offers = hits
            selected = hits.singleOrNull() ?: hits.firstOrNull()
            scanned = true
        }
    }

    LaunchedEffect(Unit) {
        if (!scanned && !bag.busy) scan()
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
                Row(
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                    verticalAlignment = Alignment.CenterVertically,
                    modifier = Modifier.fillMaxWidth(),
                ) {
                    ExposedDropdownMenuBox(
                        expanded = menuOpen && offers.isNotEmpty(),
                        onExpandedChange = { if (offers.isNotEmpty()) menuOpen = !menuOpen },
                        modifier = Modifier.weight(1f),
                    ) {
                        OutlinedTextField(
                            value = selected?.label().orEmpty(),
                            onValueChange = {},
                            readOnly = true,
                            singleLine = true,
                            label = { Text("厂服务") },
                            placeholder = {
                                Text(
                                    when {
                                        bag.busy && !scanned -> "正在扫描…"
                                        scanned -> "未发现厂服务"
                                        else -> "扫描后选择"
                                    },
                                )
                            },
                            trailingIcon = { ExposedDropdownMenuDefaults.TrailingIcon(expanded = menuOpen && offers.isNotEmpty()) },
                            modifier = Modifier
                                .menuAnchor()
                                .fillMaxWidth(),
                            enabled = offers.isNotEmpty(),
                        )
                        ExposedDropdownMenu(
                            expanded = menuOpen && offers.isNotEmpty(),
                            onDismissRequest = { menuOpen = false },
                        ) {
                            offers.forEach { o ->
                                DropdownMenuItem(
                                    text = { Text(o.label()) },
                                    onClick = {
                                        selected = o
                                        menuOpen = false
                                    },
                                )
                            }
                        }
                    }
                    OutlinedButton(
                        onClick = { scan() },
                        enabled = !bag.busy,
                    ) {
                        Text("扫描")
                    }
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
                        val hit = selected ?: return@Button
                        scope.launch {
                            runCatching {
                                withContext(Dispatchers.IO) {
                                    bag.login(hit.httpBase, hit.factoryId, login, password)
                                }
                            }
                        }
                    },
                    enabled = !bag.busy && selected != null && login.isNotBlank() && password.isNotBlank(),
                    modifier = Modifier.align(Alignment.End),
                ) {
                    if (bag.busy) CircularProgressIndicator(modifier = Modifier.size(16.dp).padding(end = 8.dp), strokeWidth = 2.dp)
                    Text("登录")
                }
            }
        }
    }
}
