package com.gbndt.shijiaoqi.model.multilayer

import com.gbndt.shijiaoqi.model.RefPoint
import com.gbndt.shijiaoqi.model.WeldProcess
import com.gbndt.shijiaoqi.model.single.WeldPath
import com.gbndt.shijiaoqi.model.single.WeldPathSurrogate
import com.gbndt.shijiaoqi.model.single.toSurrogate
import com.gbndt.shijiaoqi.model.single.toWeldPath
import java.util.UUID
import kotlinx.serialization.Serializable
import kotlinx.serialization.Transient

/** 多层一道的层偏移；process 不进 JSON。 */
@Serializable
data class WeldPassOffset(
    val id: String = "",
    var name: String = "",
    var valX: Double = 0.0,
    var valYLeft: Double = 0.0,
    var valYRight: Double = 0.0,
    var valZ: Double = 0.0,
    var valR: Double = 0.0,
    var processId: String = "",
    @Transient var process: WeldProcess = WeldProcess(),
    var isEnabled: Boolean = true,
    var isCompleted: Boolean = false
)
/** 多层焊道：底道加各层偏移。 */
data class MultiLayerWeldPath(
    val id: String,
    var name: String,
    var basePath: WeldPath,
    val passes: MutableList<WeldPassOffset>,
    var refPointX1: RefPoint? = null,
    var refPointZ1: RefPoint? = null,
    var refPointXMiddle: RefPoint? = null,
    var refPointZMiddle: RefPoint? = null,
    var refPointXEnd: RefPoint? = null,
    var refPointZEnd: RefPoint? = null,
    var isBaseCompleted: Boolean = false
)

@Serializable
data class MultiLayerWeldPathSurrogate(
    val id: String = "",
    val name: String = "",
    val basePath: WeldPathSurrogate = WeldPathSurrogate(),
    val passes: List<WeldPassOffset> = emptyList(),
    val refPointX1: RefPoint? = null,
    val refPointZ1: RefPoint? = null,
    val refPointXMiddle: RefPoint? = null,
    val refPointZMiddle: RefPoint? = null,
    val refPointXEnd: RefPoint? = null,
    val refPointZEnd: RefPoint? = null,
    val isBaseCompleted: Boolean = false
)

fun MultiLayerWeldPath.toSurrogate() = MultiLayerWeldPathSurrogate(
    id, name, basePath.toSurrogate(), passes.toList(), refPointX1, refPointZ1, refPointXMiddle, refPointZMiddle, refPointXEnd, refPointZEnd, isBaseCompleted
)

fun MultiLayerWeldPathSurrogate.toMultiLayerWeldPath(): MultiLayerWeldPath {
    val passList = mutableListOf<WeldPassOffset>()
    passList.addAll(passes)
    return MultiLayerWeldPath(
        id = id.ifBlank { UUID.randomUUID().toString() },
        name = name,
        basePath = basePath.toWeldPath(),
        passes = passList,
        refPointX1 = refPointX1,
        refPointZ1 = refPointZ1,
        refPointXMiddle = refPointXMiddle,
        refPointZMiddle = refPointZMiddle,
        refPointXEnd = refPointXEnd,
        refPointZEnd = refPointZEnd,
        isBaseCompleted = isBaseCompleted,
    )
}
