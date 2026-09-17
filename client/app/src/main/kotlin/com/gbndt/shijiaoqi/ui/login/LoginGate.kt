package com.gbndt.shijiaoqi.ui.login

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.selection.toggleable
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.Checkbox
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Visibility
import androidx.compose.material.icons.filled.VisibilityOff
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ExposedDropdownMenuBox
import androidx.compose.material3.ExposedDropdownMenuDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.window.DialogProperties
import com.gbndt.shijiaoqi.model.FactoryOffer
import com.gbndt.shijiaoqi.ui.theme.FullscreenDialog
import com.gbndt.shijiaoqi.data.log.PadLog
import com.gbndt.shijiaoqi.ui.preview.PadPreview
import com.gbndt.shijiaoqi.ui.preview.PadPreviewTheme
import com.gbndt.shijiaoqi.ui.preview.PadSamples
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle

@Composable
fun LoginGate(
    viewModel: LoginViewModel = hiltViewModel(),
) {
    val state by viewModel.uiState.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) {
        if (state.scan == ScanStatus.Ready && state.offers.isEmpty()) viewModel.scan()
    }
    FullscreenDialog(
        onDismissRequest = {},
        properties = DialogProperties(dismissOnBackPress = false, dismissOnClickOutside = false, usePlatformDefaultWidth = false),
    ) {
        LoginGate(
            state = state,
            onSelect = viewModel::select,
            onScan = viewModel::scan,
            onLoginName = viewModel::setLoginName,
            onPassword = viewModel::setPassword,
            onRemember = viewModel::setRemember,
            onLogin = viewModel::login,
        )
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun LoginGate(
    state: LoginUiState,
    onSelect: (FactoryOffer) -> Unit = {},
    onScan: () -> Unit = {},
    onLoginName: (String) -> Unit = {},
    onPassword: (String) -> Unit = {},
    onRemember: (Boolean) -> Unit = {},
    onLogin: () -> Unit = {},
) {
    var menuOpen by remember { mutableStateOf(false) }
    var passwordVisible by remember { mutableStateOf(false) }
    val offers = state.offers
    val selected = state.selected
    val scanTone = when (state.scan) {
        ScanStatus.Empty, ScanStatus.Unavailable -> MaterialTheme.colorScheme.error
        ScanStatus.Scanning -> MaterialTheme.colorScheme.primary
        ScanStatus.Ready -> if (state.loggingIn) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.onSurfaceVariant
    }
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
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                if (state.scan == ScanStatus.Scanning) {
                    CircularProgressIndicator(
                        modifier = Modifier.size(18.dp),
                        strokeWidth = 2.dp,
                        color = scanTone,
                    )
                }
                Text(state.scanCaption, fontSize = 14.sp, color = scanTone)
            }
            Row(
                horizontalArrangement = Arrangement.spacedBy(8.dp),
                verticalAlignment = Alignment.CenterVertically,
                modifier = Modifier.fillMaxWidth(),
            ) {
                ExposedDropdownMenuBox(
                    expanded = menuOpen && offers.isNotEmpty() && state.canScan,
                    onExpandedChange = { if (offers.isNotEmpty() && state.canScan) menuOpen = !menuOpen },
                    modifier = Modifier.weight(1f),
                ) {
                    OutlinedTextField(
                        value = selected?.label().orEmpty(),
                        onValueChange = {},
                        readOnly = true,
                        singleLine = true,
                        label = { Text("厂服务") },
                        placeholder = { Text(state.scanCaption) },
                        trailingIcon = { ExposedDropdownMenuDefaults.TrailingIcon(expanded = menuOpen && offers.isNotEmpty()) },
                        modifier = Modifier
                            .menuAnchor()
                            .fillMaxWidth(),
                        enabled = offers.isNotEmpty() && state.canScan,
                    )
                    ExposedDropdownMenu(
                        expanded = menuOpen && offers.isNotEmpty() && state.canScan,
                        onDismissRequest = { menuOpen = false },
                    ) {
                        offers.forEach { o ->
                            DropdownMenuItem(
                                text = { Text(o.label()) },
                                onClick = {
                                    onSelect(o)
                                    menuOpen = false
                                },
                            )
                        }
                    }
                }
                OutlinedButton(
                    onClick = {
                        PadLog.click("scan")
                        onScan()
                    },
                    enabled = state.canScan,
                ) {
                    Text(if (state.scan == ScanStatus.Scanning) "扫描中" else "扫描")
                }
            }
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp), modifier = Modifier.fillMaxWidth()) {
                OutlinedTextField(
                    state.loginName,
                    onLoginName,
                    label = { Text("登录名") },
                    singleLine = true,
                    modifier = Modifier.weight(1f),
                )
                OutlinedTextField(
                    state.password,
                    onPassword,
                    label = { Text("密码") },
                    singleLine = true,
                    visualTransformation = if (passwordVisible) VisualTransformation.None else PasswordVisualTransformation(),
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password),
                    trailingIcon = {
                        IconButton(onClick = { passwordVisible = !passwordVisible }) {
                            Icon(
                                imageVector = if (passwordVisible) Icons.Filled.VisibilityOff else Icons.Filled.Visibility,
                                contentDescription = if (passwordVisible) "隐藏密码" else "显示密码",
                            )
                        }
                    },
                    modifier = Modifier.weight(1f),
                )
            }
            state.error?.let { Text(it, color = MaterialTheme.colorScheme.error, fontSize = 13.sp) }
            Row(
                modifier = Modifier.fillMaxWidth(),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                Row(
                    modifier = Modifier
                        .weight(1f)
                        .toggleable(
                            value = state.remember,
                            role = Role.Checkbox,
                            onValueChange = onRemember,
                        ),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Checkbox(checked = state.remember, onCheckedChange = null)
                    Text("记住账号密码", fontSize = 14.sp)
                }
                Button(
                    onClick = {
                        PadLog.click("login")
                        onLogin()
                    },
                    enabled = state.canLogin && state.loginName.isNotBlank() && state.password.isNotBlank(),
                ) {
                    Row(
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(8.dp),
                    ) {
                        if (state.loggingIn) {
                            CircularProgressIndicator(modifier = Modifier.size(18.dp), strokeWidth = 2.dp)
                        }
                        Text("登录")
                    }
                }
            }
        }
    }
}

@PadPreview
@Composable
private fun LoginGatePreview() {
    PadPreviewTheme {
        Box(modifier = Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
            LoginGate(state = PadSamples.login)
        }
    }
}
