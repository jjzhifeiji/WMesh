package com.gbndt.shijiaoqi.ui.welding

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.*
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.DialogProperties
import com.gbndt.shijiaoqi.model.RobotError
import com.gbndt.shijiaoqi.ui.welding.WeldViewModelInterface

@Composable
fun RobotErrorDialog(
    viewModel: WeldViewModelInterface,
    errors: List<RobotError>
) {
    AlertDialog(
        onDismissRequest = { 
            viewModel.currentRobotErrors.clear()
            viewModel.isRobotErrorDialogVisible = false 
        },
        properties = DialogProperties(usePlatformDefaultWidth = false),
        modifier = Modifier.fillMaxWidth(0.8f),
        title = {
            Text(
                text = "机器人故障报警",
                color = Color.Red,
                fontWeight = FontWeight.Bold
            )
        },
        text = {
            LazyColumn(
                modifier = Modifier.fillMaxWidth().heightIn(max = 600.dp),
                verticalArrangement = Arrangement.spacedBy(16.dp)
            ) {
                items(errors) { error ->
                    Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
                        Text(text = "错误码: ${error.code}", fontWeight = FontWeight.Bold)
                        Text(text = "描述: ${error.description}")
                        Text(text = "处理方式: ${error.solution}", color = MaterialTheme.colorScheme.primary)
                    }
                    Divider(modifier = Modifier.padding(top = 8.dp))
                }
            }
        },
        confirmButton = {
            Button(
                onClick = { 
                    viewModel.currentRobotErrors.clear()
                    viewModel.isRobotErrorDialogVisible = false 
                },
                colors = ButtonDefaults.buttonColors(containerColor = Color.Red)
            ) {
                Text("知道了", color = Color.White)
            }
        }
    )
}
