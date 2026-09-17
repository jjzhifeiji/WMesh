package com.gbndt.shijiaoqi.ui.account

import android.widget.Toast
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material.icons.filled.Visibility
import androidx.compose.material.icons.filled.VisibilityOff
import androidx.compose.material3.Button
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.gbndt.shijiaoqi.BuildConfig
import kotlinx.coroutines.launch

private val PageBg = Color(0xFFF5F7FA)
private val BarBg = Color.White
private val TitleColor = Color(0xFF2C3E50)

/** 个人中心整页：账户资料、改密、退出、查更新；后续功能往分组里加。 */
@Composable
fun AccountScreen(
    personName: String,
    loginName: String,
    roles: List<String>,
    onBack: () -> Unit,
    onChangePassword: () -> Unit,
    onLogout: () -> Unit,
    onCheckUpdate: () -> Unit,
) {
    BackHandler(onBack = onBack)
    Column(
        modifier = Modifier
            .fillMaxSize()
            .background(PageBg),
    ) {
        AccountTopBar(title = "个人中心", onBack = onBack)
        Column(
            modifier = Modifier
                .fillMaxSize()
                .verticalScroll(rememberScrollState())
                .padding(horizontal = 24.dp, vertical = 16.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            Column(modifier = Modifier.widthIn(max = 640.dp).fillMaxWidth()) {
                AccountHeader(personName = personName, loginName = loginName)
                Spacer(Modifier.height(16.dp))
                SectionCard(title = "账户") {
                    InfoLine("姓名", personName.ifBlank { "未设置" })
                    HorizontalDivider(color = Color(0xFFE8EEF4))
                    InfoLine("登录名", loginName.ifBlank { "—" })
                    HorizontalDivider(color = Color(0xFFE8EEF4))
                    InfoLine("角色", roles.joinToString("、", transform = ::roleLabel).ifBlank { "—" })
                }
                Spacer(Modifier.height(12.dp))
                SectionCard(title = "安全") {
                    ActionLine("修改密码", onClick = onChangePassword)
                }
                Spacer(Modifier.height(12.dp))
                SectionCard(title = "关于") {
                    ActionLine("版本 ${BuildConfig.VERSION_NAME}", hint = "检查更新", onClick = onCheckUpdate)
                }
                Spacer(Modifier.height(24.dp))
                Box(
                    modifier = Modifier
                        .fillMaxWidth()
                        .clip(RoundedCornerShape(12.dp))
                        .background(Color.White)
                        .clickable(onClick = onLogout)
                        .padding(vertical = 16.dp),
                    contentAlignment = Alignment.Center,
                ) {
                    Text("退出登录", color = Color(0xFFD32F2F), fontSize = 16.sp, fontWeight = FontWeight.Medium)
                }
            }
        }
    }
}

/** 改密码整页，成功后返回个人中心。 */
@Composable
fun AccountPasswordScreen(
    onBack: () -> Unit,
    onChangePassword: suspend (String) -> Unit,
) {
    BackHandler(onBack = onBack)
    var first by remember { mutableStateOf("") }
    var second by remember { mutableStateOf("") }
    var show by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    var busy by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    val context = LocalContext.current
    val transform = if (show) VisualTransformation.None else PasswordVisualTransformation()
    Column(
        modifier = Modifier
            .fillMaxSize()
            .background(PageBg),
    ) {
        AccountTopBar(title = "修改密码", onBack = onBack)
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(24.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            Column(
                modifier = Modifier.widthIn(max = 480.dp).fillMaxWidth(),
                verticalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                val eye = @Composable {
                    IconButton(onClick = { show = !show }) {
                        Icon(
                            imageVector = if (show) Icons.Filled.VisibilityOff else Icons.Filled.Visibility,
                            contentDescription = if (show) "隐藏密码" else "显示密码",
                        )
                    }
                }
                OutlinedTextField(
                    first,
                    { first = it; error = null },
                    label = { Text("新密码") },
                    singleLine = true,
                    visualTransformation = transform,
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password),
                    trailingIcon = eye,
                    modifier = Modifier.fillMaxWidth(),
                )
                OutlinedTextField(
                    second,
                    { second = it; error = null },
                    label = { Text("再输一次") },
                    singleLine = true,
                    visualTransformation = transform,
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password),
                    trailingIcon = eye,
                    modifier = Modifier.fillMaxWidth(),
                )
                error?.let { Text(it, color = MaterialTheme.colorScheme.error, fontSize = 13.sp) }
                Button(
                    enabled = !busy,
                    modifier = Modifier.fillMaxWidth().padding(top = 8.dp),
                    onClick = {
                        when {
                            first.isBlank() -> error = "新密码不能为空"
                            first != second -> error = "两次密码不一致"
                            else -> scope.launch {
                                busy = true
                                runCatching { onChangePassword(first) }
                                    .onSuccess {
                                        Toast.makeText(context, "密码已修改", Toast.LENGTH_SHORT).show()
                                        onBack()
                                    }
                                    .onFailure { error = it.message ?: "修改失败" }
                                busy = false
                            }
                        }
                    },
                ) { Text(if (busy) "提交中" else "确定") }
            }
        }
    }
}

@Composable
private fun AccountTopBar(title: String, onBack: () -> Unit) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .height(56.dp)
            .background(BarBg)
            .padding(horizontal = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        IconButton(onClick = onBack) {
            Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "返回", tint = TitleColor)
        }
        Text(title, fontSize = 18.sp, fontWeight = FontWeight.Bold, color = TitleColor)
    }
}

@Composable
private fun AccountHeader(personName: String, loginName: String) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(12.dp))
            .background(Color.White)
            .padding(20.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        Box(
            modifier = Modifier
                .size(56.dp)
                .clip(CircleShape)
                .background(Color(0xFFE3F2FD)),
            contentAlignment = Alignment.Center,
        ) {
            val mark = personName.ifBlank { loginName }.take(1).ifBlank { "帐" }
            Text(mark, fontSize = 22.sp, fontWeight = FontWeight.Bold, color = Color(0xFF1565C0))
        }
        Column {
            Text(personName.ifBlank { loginName.ifBlank { "未登录" } }, fontSize = 20.sp, fontWeight = FontWeight.Bold, color = TitleColor)
            if (personName.isNotBlank() && loginName.isNotBlank()) {
                Text(loginName, fontSize = 14.sp, color = Color.Gray)
            }
        }
    }
}

@Composable
private fun SectionCard(title: String, content: @Composable () -> Unit) {
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Text(title, fontSize = 13.sp, color = Color.Gray, modifier = Modifier.padding(start = 4.dp))
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .clip(RoundedCornerShape(12.dp))
                .background(Color.White),
        ) { content() }
    }
}

@Composable
private fun InfoLine(label: String, value: String) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 14.dp),
        horizontalArrangement = Arrangement.SpaceBetween,
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(label, fontSize = 15.sp, color = Color.Gray)
        Text(value, fontSize = 15.sp, color = TitleColor)
    }
}

@Composable
private fun ActionLine(title: String, hint: String = "", onClick: () -> Unit) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clickable(onClick = onClick)
            .padding(horizontal = 16.dp, vertical = 14.dp),
        horizontalArrangement = Arrangement.SpaceBetween,
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(title, fontSize = 15.sp, color = TitleColor)
        Row(verticalAlignment = Alignment.CenterVertically) {
            if (hint.isNotBlank()) Text(hint, fontSize = 13.sp, color = Color.Gray)
            Icon(Icons.AutoMirrored.Filled.KeyboardArrowRight, contentDescription = null, tint = Color(0xFFB0BEC5))
        }
    }
}

internal fun roleLabel(role: String): String = when (role) {
    "factory_super_admin" -> "工厂超管"
    "org_admin" -> "管理员"
    "org_lead" -> "组织负责人"
    "process_engineer" -> "工艺工程师"
    "operator" -> "操作员"
    "auditor" -> "审计员"
    else -> role
}
