package com.gbndt.shijiaoqi.ui.components

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.gestures.detectDragGestures
import androidx.compose.foundation.gestures.detectTransformGestures
import androidx.compose.foundation.layout.*
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Remove
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import com.gbndt.shijiaoqi.data.models.WeldPath
import com.gbndt.shijiaoqi.data.models.WeldPointType
import kotlin.math.cos
import kotlin.math.sin

data class LineSegment3D(val p1: FloatArray, val p2: FloatArray, val isWeld: Boolean)

@Composable
fun Path3DViewerDialog(
    weldPaths: List<WeldPath>,
    onDismiss: () -> Unit
) {
    Dialog(
        onDismissRequest = onDismiss,
        properties = DialogProperties(usePlatformDefaultWidth = false)
    ) {
        Surface(
            modifier = Modifier.fillMaxSize(0.9f).padding(16.dp),
            shape = MaterialTheme.shapes.medium,
            color = Color(0xFF1E1E1E) // 深色背景，突出线框
        ) {
            Column(modifier = Modifier.fillMaxSize()) {
                Row(
                    modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp),
                    horizontalArrangement = Arrangement.SpaceBetween,
                    verticalAlignment = Alignment.CenterVertically
                ) {
                    Text(
                        text = "3D 路径预览 (红色: 焊接, 黄色: 空走 | 单指拖拽旋转视角)", 
                        color = Color.White,
                        style = MaterialTheme.typography.titleMedium
                    )
                    IconButton(onClick = onDismiss) {
                        Icon(Icons.Default.Close, contentDescription = "关闭", tint = Color.White)
                    }
                }

                Box(modifier = Modifier.weight(1f).fillMaxWidth()) {
                    Path3DCanvas(weldPaths)
                }
            }
        }
    }
}

@Composable
fun Path3DCanvas(weldPaths: List<WeldPath>) {
    var pitch by remember { mutableStateOf(-0.5f) } // 默认向下俯视一点
    var yaw by remember { mutableStateOf(0.5f) }   // 默认侧视一点
    var scale by remember { mutableStateOf(1f) }
    var offsetX by remember { mutableStateOf(0f) }
    var offsetY by remember { mutableStateOf(0f) }

    // 解析路径为 3D 线段
    val segments = remember(weldPaths) {
        val list = mutableListOf<LineSegment3D>()
        var lastPoint: FloatArray? = null

        for (path in weldPaths) {
            if (!path.isEnabled) continue
            val pts = path.points.filter { it.pose != null }
            if (pts.isEmpty()) continue

            // 1. 与上一条焊道的连接 (空走)
            val firstPt = pts.first()
            if (lastPoint != null) {
                list.add(LineSegment3D(
                    lastPoint!!,
                    floatArrayOf(firstPt.pose!!.x.toFloat(), firstPt.pose!!.y.toFloat(), firstPt.pose!!.z.toFloat()),
                    false
                ))
            }

            // 2. 焊道内部点位连接
            for (i in 0 until pts.size - 1) {
                val pt1 = pts[i]
                val pt2 = pts[i+1]
                
                // 判断是否为实际焊接段 (起点到中间点，中间点到中间点/终点)
                val isWeld = (pt1.type == WeldPointType.START || pt1.type == WeldPointType.MIDDLE || pt1.type == WeldPointType.ARC_MIDDLE) &&
                             (pt2.type == WeldPointType.MIDDLE || pt2.type == WeldPointType.ARC_MIDDLE || pt2.type == WeldPointType.END)

                list.add(LineSegment3D(
                    floatArrayOf(pt1.pose!!.x.toFloat(), pt1.pose!!.y.toFloat(), pt1.pose!!.z.toFloat()),
                    floatArrayOf(pt2.pose!!.x.toFloat(), pt2.pose!!.y.toFloat(), pt2.pose!!.z.toFloat()),
                    isWeld
                ))
            }

            // 记录最后一个点
            val lastPt = pts.last()
            lastPoint = floatArrayOf(lastPt.pose!!.x.toFloat(), lastPt.pose!!.y.toFloat(), lastPt.pose!!.z.toFloat())
        }
        list
    }

    // 计算包围盒和中心点
    val bounds = remember(segments) {
        var minX = Float.MAX_VALUE; var maxX = -Float.MAX_VALUE
        var minY = Float.MAX_VALUE; var maxY = -Float.MAX_VALUE
        var minZ = Float.MAX_VALUE; var maxZ = -Float.MAX_VALUE

        segments.forEach { seg ->
            arrayOf(seg.p1, seg.p2).forEach { p ->
                if (p[0] < minX) minX = p[0]
                if (p[0] > maxX) maxX = p[0]
                if (p[1] < minY) minY = p[1]
                if (p[1] > maxY) maxY = p[1]
                if (p[2] < minZ) minZ = p[2]
                if (p[2] > maxZ) maxZ = p[2]
            }
        }
        
        if (minX == Float.MAX_VALUE) {
            // 没有有效点
            floatArrayOf(0f, 0f, 0f, 100f)
        } else {
            val cx = (minX + maxX) / 2f
            val cy = (minY + maxY) / 2f
            val cz = (minZ + maxZ) / 2f
            val maxSpan = maxOf(maxX - minX, maxY - minY, maxZ - minZ).coerceAtLeast(10f)
            floatArrayOf(cx, cy, cz, maxSpan)
        }
    }

    val centerX = bounds[0]
    val centerY = bounds[1]
    val centerZ = bounds[2]
    val maxSpan = bounds[3]

    Box(modifier = Modifier.fillMaxSize()) {
        Canvas(modifier = Modifier
            .fillMaxSize()
            .pointerInput(Unit) {
                detectTransformGestures { _, pan, zoom, _ ->
                    scale *= zoom
                    offsetX += pan.x
                    offsetY += pan.y
                }
            }
            .pointerInput(Unit) {
                detectDragGestures { change, dragAmount ->
                    change.consume()
                    // 拖拽旋转
                    yaw += dragAmount.x * 0.01f
                    pitch += dragAmount.y * 0.01f
                }
            }
        ) {
            val w = size.width
            val h = size.height

            // 投影函数
            fun project(p: FloatArray): Offset {
                // 平移到原点
                val x = p[0] - centerX
                val y = p[1] - centerY
                val z = p[2] - centerZ

                // 绕 Y 轴旋转 (Yaw)
                val cosY = cos(yaw)
                val sinY = sin(yaw)
                val x1 = x * cosY - z * sinY
                val z1 = x * sinY + z * cosY

                // 绕 X 轴旋转 (Pitch)
                val cosX = cos(pitch)
                val sinX = sin(pitch)
                val y1 = y * cosX - z1 * sinX
                // val z2 = y * sinX + z1 * cosX // Z-buffer 暂不需要

                // 正交投影缩放
                val baseScale = minOf(w, h) / (maxSpan * 1.5f)
                val finalScale = baseScale * scale

                // 映射到屏幕中心
                val screenX = x1 * finalScale + w / 2f + offsetX
                val screenY = -y1 * finalScale + h / 2f + offsetY // Y轴反转

                return Offset(screenX, screenY)
            }

            // 绘制所有线段
            segments.forEach { seg ->
                val p1 = project(seg.p1)
                val p2 = project(seg.p2)

                drawLine(
                    color = if (seg.isWeld) Color.Red else Color.Yellow,
                    start = p1,
                    end = p2,
                    strokeWidth = if (seg.isWeld) 4f else 2f
                )
            }
            
            // 可以选择绘制原点坐标系 (辅助)
            val origin = project(floatArrayOf(centerX, centerY, centerZ))
            drawCircle(color = Color.White, radius = 4f, center = origin)
        }

        // 浮动控制按钮
        Column(
            modifier = Modifier.align(Alignment.BottomEnd).padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(8.dp)
        ) {
            FloatingActionButton(onClick = { scale *= 1.2f }, containerColor = MaterialTheme.colorScheme.primary) {
                Icon(Icons.Default.Add, "放大")
            }
            FloatingActionButton(onClick = { scale /= 1.2f }, containerColor = MaterialTheme.colorScheme.primary) {
                Icon(Icons.Default.Remove, "缩小")
            }
            FloatingActionButton(onClick = {
                scale = 1f
                offsetX = 0f
                offsetY = 0f
                pitch = -0.5f
                yaw = 0.5f
            }, containerColor = MaterialTheme.colorScheme.secondary) {
                Icon(Icons.Default.Refresh, "复位")
            }
        }
    }
}
