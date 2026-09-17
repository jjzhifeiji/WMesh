package com.gbndt.shijiaoqi.model.single

import com.gbndt.shijiaoqi.model.WeldPoint
import com.gbndt.shijiaoqi.model.WeldProcess
import com.gbndt.shijiaoqi.model.tbar.GapBand
import java.util.UUID
import kotlinx.serialization.Serializable

/** 包角组参数；只写 processId。 */
@Serializable
data class CornerGroupParams(
    val groupId: String,
    val refPathAId: String,
    val refPathBId: String,
    val layerCount: Int,
    val initialLength: Double,
    val upwardOffset: Double,
    val lengthReduction: Double,
    val isMaster: Boolean = false, // 主道代表整组改参数
    val torchRx: Double? = null,
    val torchRy: Double? = null,
    val torchRz: Double? = null,
    val processId: String = "",
)
/** 焊道上的附加工艺槽。 */
data class WeldPathProcessSlot(
    val id: String,
    var process: WeldProcess,
    var processId: String = "",
    var isEnabled: Boolean = true
)

/** 单层/T 排共用的焊道正文；包角和间隙带可空。 */
data class WeldPath(
    val id: String,
    var name: String,
    val points: MutableList<WeldPoint>,
    var process: WeldProcess,
    var processId: String = "",
    var selectedPointIndex: Int = 0,
    var isEnabled: Boolean = true,
    var cornerGroupParams: CornerGroupParams? = null,
    val extraProcesses: MutableList<WeldPathProcessSlot> = mutableListOf(),
    var gapBands: List<GapBand> = emptyList(),
)

@Serializable
data class WeldPathProcessSlotSurrogate(
    val id: String,
    val processId: String = "",
    val isEnabled: Boolean = true
)

@Serializable
data class WeldPathSurrogate(
    val id: String = "",
    val name: String = "",
    val points: List<WeldPoint> = emptyList(),
    val processId: String = "",
    val selectedPointIndex: Int = 0,
    val isEnabled: Boolean = true,
    val cornerGroupParams: CornerGroupParams? = null,
    val extraProcesses: List<WeldPathProcessSlotSurrogate> = emptyList(),
    val gapBands: List<GapBand> = emptyList(),
    val kind: String = "",
    val templateId: String = "",
)

fun WeldPath.toSurrogate() = WeldPathSurrogate(
    id = id.ifBlank { UUID.randomUUID().toString() },
    name = name,
    points = points,
    processId = processId,
    selectedPointIndex = selectedPointIndex,
    isEnabled = isEnabled,
    cornerGroupParams = cornerGroupParams,
    extraProcesses = extraProcesses.map {
        WeldPathProcessSlotSurrogate(id = it.id, processId = it.processId, isEnabled = it.isEnabled)
    },
    gapBands = gapBands,
)

/** 反序列化只带 processId；参数出库后再合进来。 */
fun WeldPathSurrogate.toWeldPath(): WeldPath {
    val stateList = mutableListOf<WeldPoint>()
    stateList.addAll(points)
    val process = WeldProcess()
    var validIndex = selectedPointIndex
    if (validIndex < 0 || (stateList.isNotEmpty() && validIndex >= stateList.size)) {
        validIndex = 0
    }
    if (stateList.isEmpty()) {
        validIndex = -1
    }
    val extras = mutableListOf<WeldPathProcessSlot>()
    extraProcesses.forEach { slot ->
        extras.add(
            WeldPathProcessSlot(
                id = slot.id.ifBlank { UUID.randomUUID().toString() },
                process = WeldProcess(),
                processId = slot.processId,
                isEnabled = slot.isEnabled
            )
        )
    }
    val group = cornerGroupParams
    return WeldPath(
        id = id.ifBlank { UUID.randomUUID().toString() },
        name = name,
        points = stateList,
        process = process,
        processId = processId.ifBlank { group?.processId.orEmpty() },
        selectedPointIndex = validIndex,
        isEnabled = isEnabled,
        cornerGroupParams = group,
        extraProcesses = extras,
        gapBands = gapBands,
    )
}
