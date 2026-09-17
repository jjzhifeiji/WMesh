package com.gbndt.shijiaoqi.ui.component

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.*
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.LocalHapticFeedback
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.hapticfeedback.HapticFeedbackType
import com.gbndt.shijiaoqi.data.log.PadLog

/** 组合时记下真正界面位置，避免通用按钮只显示组件文件。 */
@Composable
fun rememberLogSite(): String = remember { PadLog.origin() }

@Composable
fun SectionHeader(title: String) {
    Text(
        text = title,
        fontWeight = FontWeight.Bold,
        fontSize = 16.sp,
        color = MaterialTheme.colorScheme.primary,
        modifier = Modifier.padding(vertical = 4.dp)
    )
}

@OptIn(ExperimentalFoundationApi::class)
@Composable
fun ActionButton(
    text: String,
    onClick: () -> Unit,
    onLongClick: (() -> Unit)? = null,
    modifier: Modifier = Modifier,
    containerColor: Color = MaterialTheme.colorScheme.primary,
    border: BorderStroke? = null,
    enabled: Boolean = true,
    height: Dp = 60.dp
) {
    val haptic = LocalHapticFeedback.current
    val at = rememberLogSite()
    
    Button(
        onClick = {
            if (onLongClick == null) {
                PadLog.click(text, at)
                onClick()
            }
        },
        modifier = modifier.height(height),
        colors = ButtonDefaults.buttonColors(containerColor = containerColor),
        shape = RoundedCornerShape(8.dp),
        border = border,
        contentPadding = PaddingValues(horizontal = 4.dp),
        enabled = enabled
    ) {
        // Use Box to handle long click if provided, since Button only supports onClick
        if (onLongClick != null) {
            Box(
                modifier = Modifier
                    .fillMaxSize()
                    .combinedClickable(
                        onClick = {
                            PadLog.click(text, at)
                            onClick()
                        },
                        onLongClick = {
                            haptic.performHapticFeedback(HapticFeedbackType.LongPress)
                            PadLog.info("UI", "longclick $text", at)
                            onLongClick()
                        }
                    ),
                contentAlignment = Alignment.Center
            ) {
                Text(text, fontSize = 13.sp, fontWeight = FontWeight.Bold, maxLines = 1)
            }
        } else {
            Text(text, fontSize = 13.sp, fontWeight = FontWeight.Bold, maxLines = 1)
        }
    }
}

@Composable
fun TwoColumnGrid(
    items: List<@Composable (Modifier) -> Unit>,
    spacing: Dp = 2.dp
) {
    Column(verticalArrangement = Arrangement.spacedBy(spacing)) {
        items.chunked(2).forEach { rowItems ->
            Row(horizontalArrangement = Arrangement.spacedBy(spacing)) {
                rowItems.forEach { item ->
                    item(Modifier.weight(1f))
                }
                if (rowItems.size == 1) {
                    Spacer(modifier = Modifier.weight(1f))
                }
            }
        }
    }
}

@Composable
fun HoldButton(
    text: String,
    onPress: () -> Unit,
    onRelease: () -> Unit,
    modifier: Modifier = Modifier,
    containerColor: Color = MaterialTheme.colorScheme.primary
) {
    val haptic = LocalHapticFeedback.current
    val at = rememberLogSite()
    Box(
        modifier = modifier
            .height(50.dp)
            .background(containerColor, RoundedCornerShape(8.dp))
            .pointerInput(Unit) {
                var isLongPressed = false
                detectTapGestures(
                    onLongPress = {
                        isLongPressed = true
                        haptic.performHapticFeedback(HapticFeedbackType.LongPress)
                        PadLog.info("UI", "press $text", at)
                        onPress()
                    },
                    onPress = {
                        isLongPressed = false
                        try {
                            tryAwaitRelease()
                        } finally {
                            if (isLongPressed) {
                                PadLog.info("UI", "release $text", at)
                                onRelease()
                            }
                        }
                    }
                )
            },
        contentAlignment = Alignment.Center
    ) {
        Text(text, color = Color.White, fontWeight = FontWeight.Bold, fontSize = 14.sp)
    }
}
