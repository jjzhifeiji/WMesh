package com.gbndt.shijiaoqi.data.models

import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.snapshots.SnapshotStateList
import kotlinx.serialization.Serializable
import kotlinx.serialization.Transient

@Serializable
enum class WeldPointType {
    START_SAFE,
    START,
    MIDDLE,
    ARC_MIDDLE,
    END,
    END_SAFE,
    GROOVE_A_LOWER,
    GROOVE_B_LOWER,
    GROOVE_A_UPPER,
    GROOVE_B_UPPER
}

fun WeldPointType.isGroovePoint(): Boolean {
    return this == WeldPointType.GROOVE_A_LOWER ||
        this == WeldPointType.GROOVE_B_LOWER ||
        this == WeldPointType.GROOVE_A_UPPER ||
        this == WeldPointType.GROOVE_B_UPPER
}

fun WeldPointType.isTBarCollectable(): Boolean {
    return this == WeldPointType.START_SAFE ||
        this == WeldPointType.END_SAFE ||
        isGroovePoint()
}

fun WeldPointType.displayName(): String {
    return when (this) {
        WeldPointType.START_SAFE -> "起安"
        WeldPointType.START -> "起点"
        WeldPointType.MIDDLE -> "中间"
        WeldPointType.ARC_MIDDLE -> "圆中"
        WeldPointType.END -> "终点"
        WeldPointType.END_SAFE -> "终安"
        WeldPointType.GROOVE_A_LOWER -> "A下"
        WeldPointType.GROOVE_B_LOWER -> "B下"
        WeldPointType.GROOVE_A_UPPER -> "A上"
        WeldPointType.GROOVE_B_UPPER -> "B上"
    }
}

@Serializable
data class Pose(
    val x: Double,
    val y: Double,
    val z: Double,
    val rx: Double,
    val ry: Double,
    val rz: Double,
    val ext1: Double = 0.0
)

@Serializable
data class WeldPoint(
    val id: String,
    val type: WeldPointType,
    var pose: Pose? = null,
    var jointAngles: List<Double>? = null,
    var executionOffsets: List<Double>? = null, // Stores [offX, offY, offZ, offRx, offRy, offRz] for execution
    var refPointX: RefPoint? = null // Reference point to define X direction
)

@Serializable
data class Oscillation(
    var type: String = "无摆动",
    var waitTime: String = "不包括",
    var positionWait: String = "等待时间内位置继续移动",
    var frequency: Double = 5.0,
    var amplitude: Double = 1.0,
    var leftStopTime: Double = 100.0,
    var rightStopTime: Double = 100.0,
    var leftSideLength: Double = 1.0,
    var rightSideLength: Double = 1.0,
    var zeroTime: Double = 20.0,
    var callbackRatio: Double = 10.0,
    var azimuth: Double = 0.0,
    var inclination: Double = 0.0
)

@Serializable
data class CapturedPoint(
    val pose: Pose,
    val joints: List<Double>
)

@Serializable
data class RobotTestSettings(
    val startPoint: CapturedPoint? = null,
    val endPoint: CapturedPoint? = null,
    val oscillation: Oscillation = Oscillation()
)

@Serializable
data class WeldProcess(
    var name: String = "默认工艺",
    var offsetX: String = "0",
    var offsetY: String = "0",
    var offsetZ: String = "0",
    var current: Double = 170.0,
    var voltage: Double = 20.0,
    var speed: Double = 10.0,
    var startArcTime: Double = 400.0,
    var endArcTime: Double = 400.0,
    var startArcCurrent: Double = 180.0,
    var endArcCurrent: Double = 160.0,
    var startArcVoltage: Double = 20.0,
    var endArcVoltage: Double = 20.0,
    var oscillation: Oscillation = Oscillation()
)

@Serializable
data class CornerGroupParams(
    val groupId: String,
    val refPathAId: String,
    val refPathBId: String,
    val layerCount: Int,
    val initialLength: Double,
    val upwardOffset: Double,
    val lengthReduction: Double,
    val isMaster: Boolean = false, // If true, this path represents the whole group in the UI for updates
    val torchRx: Double? = null,
    val torchRy: Double? = null,
    val torchRz: Double? = null,
    val processId: String = "",
)

data class WeldPathProcessSlot(
    val id: String,
    var process: WeldProcess,
    var processId: String = "",
    var isEnabled: Boolean = true
)

@Serializable
data class GapBand(
    val minGap: Double = 0.0,
    val maxGap: Double = 0.0,
    val layer: Int = 1,
    val rootProcessId: String = "",
    val capProcessId: String = "",
)

data class WeldPath(
    val id: String,
    var name: String,
    val points: SnapshotStateList<WeldPoint>,
    var process: WeldProcess,
    var processId: String = "",
    var selectedPointIndex: Int = 0,
    var isEnabled: Boolean = true,
    var cornerGroupParams: CornerGroupParams? = null,
    val extraProcesses: SnapshotStateList<WeldPathProcessSlot> = mutableStateListOf(),
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

@Serializable
data class RefPoint(
    val pose: Pose,
    val jointAngles: List<Double>
)

data class MultiLayerWeldPath(
    val id: String,
    var name: String,
    var basePath: WeldPath, // The base path (Layer 1)
    val passes: SnapshotStateList<WeldPassOffset>, // Subsequent layers (Layer 2..N)
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
    val passList = mutableStateListOf<WeldPassOffset>()
    passList.addAll(passes)
    return MultiLayerWeldPath(
        id = id.ifBlank { java.util.UUID.randomUUID().toString() },
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

@Serializable
data class FileSystemItem(
    val name: String,
    val path: String, // Relative path from root
    val isDirectory: Boolean,
    val isProject: Boolean = false, // True if this folder is a valid project (contains project_data.json)
    val isMultiLayerProject: Boolean = false, // True if this folder contains multi-layer data
    val isProcess: Boolean = false  // True if this is a process file
)

@Serializable
data class AppSettings(
    val selectedToolIndex: Int = 0,
    val toolCoordinates: List<Pose?> = List(14) { null },
    val toolRemarks: List<String> = List(14) { "" },
    val positionMode: String = "前",
    val speedMode: String = "1倍",
    val lastOpenedProjectPath: String? = null,
    val totalWeldingLength: Double = 0.0,
    val totalWeldingDuration: Long = 0L,
    val isRegistered: Boolean = false,
    val lastConnectionTime: Long = 0L,
    val installPos: Int = 0, // 0-平装, 1-侧装, 2-挂装
    val weldingCurrent: Double = 0.0,
    val weldingVoltage: Double = 0.0,
    val isExtAxisEnabled: Boolean = true
)

fun WeldPath.toSurrogate() = WeldPathSurrogate(
    id = id.ifBlank { java.util.UUID.randomUUID().toString() },
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

fun WeldPathSurrogate.toWeldPath(): WeldPath {
    val stateList = mutableStateListOf<WeldPoint>()
    stateList.addAll(points)
    val process = WeldProcess()
    var validIndex = selectedPointIndex
    if (validIndex < 0 || (stateList.isNotEmpty() && validIndex >= stateList.size)) {
        validIndex = 0
    }
    if (stateList.isEmpty()) {
        validIndex = -1
    }
    val extras = mutableStateListOf<WeldPathProcessSlot>()
    extraProcesses.forEach { slot ->
        extras.add(
            WeldPathProcessSlot(
                id = slot.id.ifBlank { java.util.UUID.randomUUID().toString() },
                process = WeldProcess(),
                processId = slot.processId,
                isEnabled = slot.isEnabled
            )
        )
    }
    val group = cornerGroupParams
    return WeldPath(
        id = id.ifBlank { java.util.UUID.randomUUID().toString() },
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
