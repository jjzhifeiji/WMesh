package com.gbndt.shijiaoqi.ui.welding.multilayer

import androidx.compose.foundation.text.BasicTextField
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.detectVerticalDragGestures
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.DragHandle
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.IntOffset
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.zIndex
import androidx.compose.foundation.BorderStroke
import com.gbndt.shijiaoqi.model.MultiLayerWeldPath
import com.gbndt.shijiaoqi.model.WeldPointType
import com.gbndt.shijiaoqi.model.displayName
import com.gbndt.shijiaoqi.model.WeldPoint
import kotlin.math.roundToInt
import com.gbndt.shijiaoqi.ui.welding.*

@OptIn(ExperimentalLayoutApi::class)
@Composable
fun MultiLayerWeldPathItem(
    multiPath: MultiLayerWeldPath,
    isSelected: Boolean,
    selectedPassIndex: Int, // -1 for Base Path
    selectedRefPointType: MultiLayerWeldViewModel.RefPointType,
    onSelect: () -> Unit,
    onSelectPass: (Int) -> Unit,
    onProcessBase: () -> Unit,
    onProcessPass: (Int) -> Unit,
    onRename: () -> Unit,
    onSelectPointBase: (Int) -> Unit,
    onDeletePointBase: (Int) -> Unit,
    onToggleEnabledBase: () -> Unit,
    onToggleEnabledPass: (Int) -> Unit,
    index: Int,
    totalCount: Int,
    onMove: (Int, Int) -> Unit,
    onSelectRefPoint: (MultiLayerWeldViewModel.RefPointType) -> Unit,
    onDeleteRefPoint: (MultiLayerWeldViewModel.RefPointType) -> Unit,
    onUpdatePassOffset: (Int, String, String) -> Unit,
    modifier: Modifier = Modifier
) {
    var itemHeight by remember { mutableStateOf(0) }
    var dragOffset by remember { mutableStateOf(0f) }

    val currentIndex by rememberUpdatedState(index)
    val currentTotalCount by rememberUpdatedState(totalCount)

    Card(
        modifier = modifier
            .onSizeChanged { itemHeight = it.height }
            .offset { IntOffset(0, dragOffset.roundToInt()) }
            .zIndex(if (dragOffset != 0f) 1f else 0f)
            .fillMaxWidth()
            .padding(8.dp)
            .border(
                width = 2.dp,
                color = if (isSelected) Color(0xFF2196F3) else Color.Transparent,
                shape = RoundedCornerShape(8.dp)
            )
            .clickable { onSelect() },
        shape = RoundedCornerShape(8.dp),
        elevation = CardDefaults.cardElevation(defaultElevation = 4.dp)
    ) {
        Column(modifier = Modifier.padding(8.dp)) {
            // Title Row
            Row(
                modifier = Modifier.fillMaxWidth().padding(bottom = 8.dp),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically
            ) {
                Text(
                    text = multiPath.name,
                    fontSize = 18.sp,
                    fontWeight = FontWeight.Bold,
                    color = Color(0xFF333333),
                    modifier = Modifier.weight(1f).clickable { onRename() }
                )
                Icon(
                    imageVector = Icons.Filled.DragHandle,
                    contentDescription = "Reorder",
                    modifier = Modifier
                        .size(32.dp)
                        .padding(4.dp)
                        .pointerInput(Unit) {
                            detectVerticalDragGestures(
                                onDragEnd = { dragOffset = 0f },
                                onDragCancel = { dragOffset = 0f }
                            ) { change, dragAmount ->
                                change.consume()
                                dragOffset += dragAmount
                                if (itemHeight > 0) {
                                    if (dragOffset > itemHeight * 0.5f) {
                                        if (currentIndex < currentTotalCount - 1) {
                                            onMove(currentIndex, currentIndex + 1)
                                            dragOffset -= itemHeight
                                        }
                                    } else if (dragOffset < -itemHeight * 0.5f) {
                                        if (currentIndex > 0) {
                                            onMove(currentIndex, currentIndex - 1)
                                            dragOffset += itemHeight
                                        }
                                    }
                                }
                            }
                        }
                )
            }
            
            // Base Path Section
            Text("基准层 (第1道)", fontSize = 14.sp, fontWeight = FontWeight.Bold, color = Color.Gray)
            Card(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(vertical = 4.dp)
                    .border(
                        width = 1.dp,
                        color = if (isSelected && selectedPassIndex == -1) Color(0xFF2196F3) else Color.LightGray,
                        shape = RoundedCornerShape(4.dp)
                    )
                    .clickable { onSelect() }, // Select Base
                colors = CardDefaults.cardColors(
                    containerColor = if (multiPath.isBaseCompleted) Color(0xFFC8E6C9) else Color(0xFFFAFAFA)
                )
            ) {
                 Column(modifier = Modifier.padding(8.dp)) {
                    Row(
                        modifier = Modifier.fillMaxWidth(),
                        horizontalArrangement = Arrangement.SpaceBetween,
                        verticalAlignment = Alignment.CenterVertically
                    ) {
                        // Process Info
                        val process = multiPath.basePath.process
                         val processInfo = buildAnnotatedString {
                            withStyle(style = SpanStyle(color = Color(0xFF2196F3), fontWeight = FontWeight.Bold)) {
                                append(process.name)
                            }
                            append(" | ")
                            withStyle(style = SpanStyle(color = Color(0xFF2196F3), fontWeight = FontWeight.Bold)) {
                                append("${process.current}A")
                            }
                            append(" | ")
                            withStyle(style = SpanStyle(color = Color(0xFF2196F3), fontWeight = FontWeight.Bold)) {
                                append("${process.voltage}V")
                            }
                            append(" | ")
                            withStyle(style = SpanStyle(color = Color(0xFF2196F3), fontWeight = FontWeight.Bold)) {
                                append("${process.speed}mm/s")
                            }
                            append(" | ")
                            withStyle(style = SpanStyle(color = Color(0xFF2196F3), fontWeight = FontWeight.Bold)) {
                                append(process.oscillation.type)
                            }
                        }
                        Text(text = processInfo, fontSize = 12.sp, modifier = Modifier.weight(1f))
                        
                        Row {
                            if (multiPath.isBaseCompleted) {
                                Text(
                                    text = "已焊",
                                    fontSize = 11.sp,
                                    fontWeight = FontWeight.Bold,
                                    color = Color(0xFF2E7D32),
                                    modifier = Modifier.padding(end = 8.dp).align(Alignment.CenterVertically)
                                )
                            }
                            Button(
                                onClick = onProcessBase,
                                modifier = Modifier.height(28.dp),
                                contentPadding = PaddingValues(horizontal = 8.dp),
                                colors = ButtonDefaults.buttonColors(Color(0xFFFFC107))
                            ) { Text("工艺", fontSize = 10.sp) }
                            Spacer(modifier = Modifier.width(4.dp))
                            Button(
                                onClick = onToggleEnabledBase,
                                modifier = Modifier.height(28.dp),
                                contentPadding = PaddingValues(horizontal = 8.dp),
                                colors = ButtonDefaults.buttonColors(
                                    containerColor = if (multiPath.basePath.isEnabled) Color(0xFF4CAF50) else Color.Gray
                                )
                            ) { Text(if (multiPath.basePath.isEnabled) "焊接" else "跳过", fontSize = 10.sp) }
                        }
                    }
                    
                    // Points
                    if (multiPath.basePath.isEnabled) {
                        Spacer(modifier = Modifier.height(8.dp))
                        
                        // Points List
                        androidx.compose.foundation.layout.FlowRow(
                            modifier = Modifier.fillMaxWidth()
                        ) {
                            multiPath.basePath.points.forEachIndexed { pointIndex, point ->
                                Column(horizontalAlignment = Alignment.CenterHorizontally) {
                                    MultiLayerPointButton(
                                        point = point,
                                        isSelected = isSelected && selectedPassIndex == -1 && pointIndex == multiPath.basePath.selectedPointIndex && selectedRefPointType == MultiLayerWeldViewModel.RefPointType.NONE,
                                        onClick = { 
                                            onSelect() // Ensure base is selected
                                            onSelectPointBase(pointIndex) 
                                        }
                                    )
                                }
                            }
                        }

                        // Ref Points Row (Below Points)
                        Row(
                            modifier = Modifier
                                .fillMaxWidth()
                                .horizontalScroll(rememberScrollState())
                                .padding(vertical = 4.dp),
                            horizontalArrangement = Arrangement.spacedBy(4.dp)
                        ) {
                            // Start Ref Points
                            RefPointButton(
                                label = "X1",
                                isSet = multiPath.refPointX1 != null,
                                isSelected = isSelected && selectedPassIndex == -1 && selectedRefPointType == MultiLayerWeldViewModel.RefPointType.START_X,
                                onClick = { 
                                    onSelect()
                                    onSelectRefPoint(MultiLayerWeldViewModel.RefPointType.START_X) 
                                },
                                onDelete = { onDeleteRefPoint(MultiLayerWeldViewModel.RefPointType.START_X) }
                            )
                            RefPointButton(
                                label = "Z1",
                                isSet = multiPath.refPointZ1 != null,
                                isSelected = isSelected && selectedPassIndex == -1 && selectedRefPointType == MultiLayerWeldViewModel.RefPointType.START_Z,
                                onClick = { 
                                    onSelect()
                                    onSelectRefPoint(MultiLayerWeldViewModel.RefPointType.START_Z) 
                                },
                                onDelete = { onDeleteRefPoint(MultiLayerWeldViewModel.RefPointType.START_Z) }
                            )
                            
                            // Middle Ref Points
                            if (multiPath.basePath.points.any { it.type == WeldPointType.ARC_MIDDLE }) {
                                RefPointButton(
                                    label = "X中",
                                    isSet = multiPath.refPointXMiddle != null,
                                    isSelected = isSelected && selectedPassIndex == -1 && selectedRefPointType == MultiLayerWeldViewModel.RefPointType.MIDDLE_X,
                                    onClick = { 
                                        onSelect()
                                        onSelectRefPoint(MultiLayerWeldViewModel.RefPointType.MIDDLE_X) 
                                    },
                                    onDelete = { onDeleteRefPoint(MultiLayerWeldViewModel.RefPointType.MIDDLE_X) }
                                )
                                RefPointButton(
                                    label = "Z中",
                                    isSet = multiPath.refPointZMiddle != null,
                                    isSelected = isSelected && selectedPassIndex == -1 && selectedRefPointType == MultiLayerWeldViewModel.RefPointType.MIDDLE_Z,
                                    onClick = { 
                                        onSelect()
                                        onSelectRefPoint(MultiLayerWeldViewModel.RefPointType.MIDDLE_Z) 
                                    },
                                    onDelete = { onDeleteRefPoint(MultiLayerWeldViewModel.RefPointType.MIDDLE_Z) }
                                )
                            }

                            // End Ref Points
                            RefPointButton(
                                label = "X2",
                                isSet = multiPath.refPointXEnd != null,
                                isSelected = isSelected && selectedPassIndex == -1 && selectedRefPointType == MultiLayerWeldViewModel.RefPointType.END_X,
                                onClick = { 
                                    onSelect()
                                    onSelectRefPoint(MultiLayerWeldViewModel.RefPointType.END_X) 
                                },
                                onDelete = { onDeleteRefPoint(MultiLayerWeldViewModel.RefPointType.END_X) }
                            )
                            RefPointButton(
                                label = "Z2",
                                isSet = multiPath.refPointZEnd != null,
                                isSelected = isSelected && selectedPassIndex == -1 && selectedRefPointType == MultiLayerWeldViewModel.RefPointType.END_Z,
                                onClick = { 
                                    onSelect()
                                    onSelectRefPoint(MultiLayerWeldViewModel.RefPointType.END_Z) 
                                },
                                onDelete = { onDeleteRefPoint(MultiLayerWeldViewModel.RefPointType.END_Z) }
                            )
                        }
                    }
                 }
            }
            
            // Passes Section
            if (multiPath.passes.isNotEmpty()) {
                Spacer(modifier = Modifier.height(8.dp))
                Text("填充层", fontSize = 14.sp, fontWeight = FontWeight.Bold, color = Color.Gray)
                
                multiPath.passes.forEachIndexed { index, pass ->
                    val isPassSelected = isSelected && selectedPassIndex == index
                    Card(
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(vertical = 2.dp)
                            .border(
                                width = 1.dp,
                                color = if (isPassSelected) Color(0xFF2196F3) else Color.LightGray,
                                shape = RoundedCornerShape(4.dp)
                            )
                            .clickable { onSelectPass(index) },
                        colors = CardDefaults.cardColors(
                            containerColor = if (pass.isCompleted) Color(0xFFC8E6C9) else Color(0xFFFAFAFA)
                        )
                    ) {
                        Column(modifier = Modifier.padding(4.dp)) {
                            Row(
                                modifier = Modifier.fillMaxWidth(),
                                horizontalArrangement = Arrangement.SpaceBetween,
                                verticalAlignment = Alignment.CenterVertically
                            ) {
                                Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.weight(1f)) {
                                    Text(pass.name, fontWeight = FontWeight.Bold, fontSize = 14.sp)
                                    if (pass.isCompleted) {
                                        Spacer(modifier = Modifier.width(6.dp))
                                        Text(
                                            text = "已焊",
                                            fontSize = 11.sp,
                                            fontWeight = FontWeight.Bold,
                                            color = Color(0xFF2E7D32)
                                        )
                                    }
                                    Spacer(modifier = Modifier.width(8.dp))
                                    
                                    // Process Info
                                    val processInfo = buildAnnotatedString {
                                        withStyle(style = SpanStyle(color = Color(0xFF2196F3), fontWeight = FontWeight.Bold)) {
                                            append(pass.process.name)
                                        }
                                        append(" | ")
                                        withStyle(style = SpanStyle(color = Color(0xFF2196F3), fontWeight = FontWeight.Bold)) {
                                            append("${pass.process.current}A")
                                        }
                                        append(" | ")
                                        withStyle(style = SpanStyle(color = Color(0xFF2196F3), fontWeight = FontWeight.Bold)) {
                                            append("${pass.process.voltage}V")
                                        }
                                        append(" | ")
                                        withStyle(style = SpanStyle(color = Color(0xFF2196F3), fontWeight = FontWeight.Bold)) {
                                            append("${pass.process.speed}mm/s")
                                        }
                                        append(" | ")
                                        withStyle(style = SpanStyle(color = Color(0xFF2196F3), fontWeight = FontWeight.Bold)) {
                                            append(pass.process.oscillation.type)
                                        }
                                    }
                                    Text(text = processInfo, fontSize = 12.sp, modifier = Modifier.weight(1f))
                                }

                                Row {
                                     Button(
                                        onClick = { onProcessPass(index) },
                                        modifier = Modifier.height(28.dp),
                                        contentPadding = PaddingValues(horizontal = 8.dp),
                                        colors = ButtonDefaults.buttonColors(Color(0xFFFFC107))
                                    ) { Text("工艺", fontSize = 10.sp) }
                                    Spacer(modifier = Modifier.width(4.dp))
                                    Button(
                                        onClick = { onToggleEnabledPass(index) },
                                        modifier = Modifier.height(28.dp),
                                        contentPadding = PaddingValues(horizontal = 8.dp),
                                        colors = ButtonDefaults.buttonColors(
                                            containerColor = if (pass.isEnabled) Color(0xFF4CAF50) else Color.Gray
                                        )
                                    ) { Text(if (pass.isEnabled) "焊接" else "跳过", fontSize = 10.sp) }
                                }
                            }
                            
                            // Offset Inputs
                            if (pass.isEnabled) {
                                Row(
                                    modifier = Modifier.fillMaxWidth().padding(vertical = 2.dp),
                                    horizontalArrangement = Arrangement.spacedBy(4.dp)
                                ) {
                                    OffsetInput(label = "X", value = pass.valX, onValueChange = { onUpdatePassOffset(index, "valX", it.toString()) }, modifier = Modifier.weight(1f))
                                    OffsetInput(label = "Y左", value = pass.valYLeft, onValueChange = { onUpdatePassOffset(index, "valYLeft", it.toString()) }, modifier = Modifier.weight(1f))
                                    OffsetInput(label = "Y右", value = pass.valYRight, onValueChange = { onUpdatePassOffset(index, "valYRight", it.toString()) }, modifier = Modifier.weight(1f))
                                    OffsetInput(label = "Z", value = pass.valZ, onValueChange = { onUpdatePassOffset(index, "valZ", it.toString()) }, modifier = Modifier.weight(1f))
                                    OffsetInput(label = "R", value = pass.valR, onValueChange = { onUpdatePassOffset(index, "valR", it.toString()) }, modifier = Modifier.weight(1f))
                                }
                            }
                        }
                    }
                }
            }
        }
    }
}

@Composable
fun RefPointButton(
    label: String,
    isSet: Boolean,
    isSelected: Boolean,
    onClick: () -> Unit,
    onDelete: () -> Unit
) {
    Box {
        Column(
            modifier = Modifier.padding(1.dp),
            horizontalAlignment = Alignment.CenterHorizontally
        ) {
            Button(
                onClick = onClick,
                contentPadding = PaddingValues(horizontal = 8.dp, vertical = 4.dp),
                colors = ButtonDefaults.buttonColors(
                    containerColor = if (isSelected) Color(0xFFFFFFFF) else Color(0xFF9E9E9E),
                    contentColor = if (isSelected) Color(0xFF9E9E9E) else Color.White
                ),
                border = if (isSet) BorderStroke(2.dp, Color(0xFF4CAF50)) else null,
                shape = RoundedCornerShape(4.dp),
                elevation = ButtonDefaults.buttonElevation(defaultElevation = if (isSelected) 4.dp else 0.dp),
                modifier = Modifier.height(40.dp).width(60.dp)
            ) {
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.Center,
                    modifier = Modifier.fillMaxWidth()
                ) {
                    Text(label, fontSize = 14.sp)
                    if (isSet) {
                        Spacer(modifier = Modifier.width(4.dp))
                        Icon(
                            imageVector = Icons.Filled.Check,
                            contentDescription = "已采集",
                            modifier = Modifier.size(16.dp),
                            tint = if (isSelected) Color(0xFF4CAF50) else Color.White
                        )
                    }
                }
            }
        }
    }
}

@Composable
fun OffsetInput(
    label: String,
    value: Double,
    onValueChange: (Double) -> Unit,
    modifier: Modifier = Modifier
) {
    // Local state to handle typing (e.g., "1.") without immediate conversion/reset
    var text by remember { mutableStateOf(if (value == 0.0) "0" else value.toString().removeSuffix(".0")) }

    LaunchedEffect(value) {
        val currentD = text.toDoubleOrNull()
        if (currentD == null || kotlin.math.abs(currentD - value) > 0.0001) {
            text = if (value == 0.0) "0" else value.toString().removeSuffix(".0")
        }
    }

    Row(modifier = modifier, verticalAlignment = Alignment.CenterVertically) {
        Text(label, fontSize = 12.sp, color = Color.Gray, modifier = Modifier.padding(end = 4.dp))
        BasicTextField(
            value = text,
            onValueChange = { newText ->
                text = newText
                val d = newText.toDoubleOrNull()
                if (d != null) {
                    onValueChange(d)
                }
            },
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
            textStyle = androidx.compose.ui.text.TextStyle(fontSize = 12.sp, textAlign = TextAlign.Center, color = Color.Black),
            singleLine = true,
            modifier = Modifier
                .weight(1f)
                .height(28.dp)
                .border(1.dp, Color.LightGray, RoundedCornerShape(4.dp)),
            decorationBox = { innerTextField ->
                Box(
                    modifier = Modifier.fillMaxSize().padding(horizontal = 4.dp),
                    contentAlignment = Alignment.Center
                ) {
                    innerTextField()
                }
            }
        )
    }
}


@Composable
fun MultiLayerPointButton(
    point: WeldPoint,
    isSelected: Boolean,
    onClick: () -> Unit
) {
    val hasData = point.pose != null

    val color = when (point.type) {
        WeldPointType.START_SAFE, WeldPointType.END_SAFE -> Color(0xFF9E9E9E)
        WeldPointType.START, WeldPointType.END -> Color(0xFFF44336)
        WeldPointType.MIDDLE -> Color(0xFF2196F3)
        WeldPointType.ARC_MIDDLE -> Color(0xFFFF9800)
        WeldPointType.GROOVE_A_LOWER, WeldPointType.GROOVE_B_LOWER,
        WeldPointType.GROOVE_A_UPPER, WeldPointType.GROOVE_B_UPPER -> Color(0xFF00897B)
    }

    val name = point.type.displayName()

    Box {
        Column(
            modifier = Modifier.padding(1.dp),
            horizontalAlignment = Alignment.CenterHorizontally
        ) {
            Button(
                onClick = onClick,
                contentPadding = PaddingValues(horizontal = 8.dp, vertical = 4.dp),
                colors = ButtonDefaults.buttonColors(
                    containerColor = if (isSelected) Color(0xFFFFFFFF) else color,
                    contentColor = if (isSelected) color else Color.White
                ),
                border = if (hasData) BorderStroke(2.dp, Color(0xFF4CAF50)) else null,
                shape = RoundedCornerShape(4.dp),
                elevation = ButtonDefaults.buttonElevation(defaultElevation = if (isSelected) 4.dp else 0.dp),
                modifier = Modifier.height(40.dp).width(80.dp)
            ) {
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.Center,
                    modifier = Modifier.fillMaxWidth()
                ) {
                    Text(name, fontSize = 14.sp)
                    if (hasData) {
                        Spacer(modifier = Modifier.width(4.dp))
                        Icon(
                            imageVector = Icons.Filled.Check,
                            contentDescription = "已采集",
                            modifier = Modifier.size(16.dp),
                            tint = if (isSelected) Color(0xFF4CAF50) else Color.White
                        )
                    }
                }
            }
        }
    }
}
