package com.gbndt.shijiaoqi.ui.welding

import androidx.compose.runtime.toMutableStateList
import com.gbndt.shijiaoqi.model.multilayer.MultiLayerWeldPath
import com.gbndt.shijiaoqi.model.single.WeldPath

/**
 * 领域层解出来的焊道用的是普通列表；进界面前换成 Compose 快照列表，
 * 点位增删改才会触发重组。只换容器，内容一个不动。
 */
fun WeldPath.asUiPath(): WeldPath = copy(
    points = points.toMutableStateList(),
    extraProcesses = extraProcesses.toMutableStateList(),
)

fun MultiLayerWeldPath.asUiPath(): MultiLayerWeldPath = copy(
    basePath = basePath.asUiPath(),
    passes = passes.toMutableStateList(),
)
