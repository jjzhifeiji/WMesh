package com.gbndt.shijiaoqi.ui.welding

import com.gbndt.shijiaoqi.domain.shared.ProcessChoice
import com.gbndt.shijiaoqi.domain.shared.ProjectChoice
import com.gbndt.shijiaoqi.model.RobotError
import com.gbndt.shijiaoqi.model.single.WeldPath

/** 开焊互斥态：空闲 / 焊接或模拟（可暂停）。 */
sealed interface WeldRunUi {
    data object Idle : WeldRunUi
    data class Active(val welding: Boolean, val paused: Boolean) : WeldRunUi
}

val WeldRunUi.busy: Boolean get() = this is WeldRunUi.Active
val WeldRunUi.isPaused: Boolean get() = (this as? WeldRunUi.Active)?.paused == true
val WeldRunUi.isWelding: Boolean get() = (this as? WeldRunUi.Active)?.welding == true
val WeldRunUi.isSimulating: Boolean get() = (this as? WeldRunUi.Active)?.welding == false

/** 外壳状态栏与袋列表。 */
data class WeldShellUi(
    val currentProjectName: String? = null, // 当前工程名
    val weldingBreakOffState: String = "正常", // 焊接中断态
    val weldArcState: String = "正常", // 电弧态
    val pouchProjects: List<ProjectChoice> = emptyList(), // 袋内工程
    val pouchProcesses: List<ProcessChoice> = emptyList(), // 袋内工艺
)

fun List<ProcessChoice>.copyableOf(processId: String): Boolean {
    if (processId.isBlank()) return true
    return firstOrNull { it.id.toString() == processId }?.copyable ?: true
}

fun processParamsCaption(name: String, current: Double, voltage: Double, speed: Double, osc: String, copyable: Boolean): String {
    if (!copyable) return "$name | 保密"
    return "$name | ${current}A | ${voltage}V | ${speed}mm/s | $osc"
}

/** 单层焊道屏状态。 */
data class SingleWeldUiState(
    val run: WeldRunUi = WeldRunUi.Idle, // 开焊态
    val weldPaths: List<WeldPath> = emptyList(), // 焊道
    val selectedWeldPathIndex: Int = 0, // 选中焊道
    val listScrollIndex: Int = 0, // 列表滚动
    val listScrollOffset: Int = 0, // 列表偏移
    val weldingLength: Double = 0.0, // 累计焊长米
    val weldingDuration: Long = 0L, // 累计焊时秒
    val shell: WeldShellUi = WeldShellUi(), // 外壳
    val isMissingProcessDialogVisible: Boolean = false,
    val missingProcessMessage: String = "",
    val isRenameDialogVisible: Boolean = false,
    val newWeldPathName: String = "",
    val isCurrentVoltageDialogVisible: Boolean = false,
    val inputCurrent: String = "",
    val inputVoltage: String = "",
    val savedCurrent: Double = 0.0,
    val savedVoltage: Double = 0.0,
    val isRobotErrorDialogVisible: Boolean = false,
    val currentRobotErrors: List<RobotError> = emptyList(),
    val isCornerParamsUnlocked: Boolean = false,
)

/** 多层焊道屏状态。 */
data class MultiLayerUiState(
    val run: WeldRunUi = WeldRunUi.Idle, // 开焊态
    val paths: List<com.gbndt.shijiaoqi.model.multilayer.MultiLayerWeldPath> = emptyList(), // 多层焊道
    val selectedMultiLayerPathIndex: Int = 0, // 选中焊道
    val selectedPassIndex: Int = -1, // 道次；底道 -1
    val listScrollIndex: Int = 0, // 列表滚动
    val listScrollOffset: Int = 0, // 列表偏移
    val weldingLength: Double = 0.0, // 累计焊长米
    val weldingDuration: Long = 0L, // 累计焊时秒
    val shell: WeldShellUi = WeldShellUi(), // 外壳
    val isMissingProcessDialogVisible: Boolean = false,
    val missingProcessMessage: String = "",
    val isRenameDialogVisible: Boolean = false,
    val newWeldPathName: String = "",
    val isCurrentVoltageDialogVisible: Boolean = false,
    val inputCurrent: String = "",
    val inputVoltage: String = "",
    val savedCurrent: Double = 0.0,
    val savedVoltage: Double = 0.0,
    val isRobotErrorDialogVisible: Boolean = false,
    val currentRobotErrors: List<RobotError> = emptyList(),
)

/** T 排焊道屏状态。 */
data class TBarUiState(
    val run: WeldRunUi = WeldRunUi.Idle, // 开焊态
    val weldPaths: List<WeldPath> = emptyList(), // 焊道
    val selectedWeldPathIndex: Int = 0, // 选中焊道
    val listScrollIndex: Int = 0, // 列表滚动
    val listScrollOffset: Int = 0, // 列表偏移
    val weldingLength: Double = 0.0, // 累计焊长米
    val weldingDuration: Long = 0L, // 累计焊时秒
    val shell: WeldShellUi = WeldShellUi(), // 外壳
    val isMissingProcessDialogVisible: Boolean = false,
    val missingProcessMessage: String = "",
    val isRenameDialogVisible: Boolean = false,
    val newWeldPathName: String = "",
    val isCurrentVoltageDialogVisible: Boolean = false,
    val inputCurrent: String = "",
    val inputVoltage: String = "",
    val savedCurrent: Double = 0.0,
    val savedVoltage: Double = 0.0,
    val isRobotErrorDialogVisible: Boolean = false,
    val currentRobotErrors: List<RobotError> = emptyList(),
)
