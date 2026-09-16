package com.gbndt.shijiaoqi.ui.teach

import com.gbndt.shijiaoqi.data.robot.protocol.RobotLink
import com.gbndt.shijiaoqi.model.Pose

/** 示教屏状态：连接、工具、点动开关。手柄轴不进这里。 */
data class TeachUiState(
    val connectionStatus: String = RobotLink.DOWN, // 连接文案
    val alarmStatus: String = "无报警", // 报警
    val pose: Pose = Pose(0.0, 0.0, 0.0, 0.0, 0.0, 0.0), // 当前位姿
    val extAxisPos: Double = 0.0, // 外轴位置
    val extAxisReady: Boolean = false, // 外轴伺服
    val machineCode: String = "", // 机械臂识别号
    val isControllerActive: Boolean = false, // 点动中
    val toolCoordinateSystem: String = "工具1", // 当前工具
    val toolCoordinates: List<Pose?> = List(14) { null }, // 14 套工具位姿
    val toolRemarks: List<String> = List(14) { "" }, // 工具备注
    val isToolListDialogVisible: Boolean = false,
    val isToolEditDialogVisible: Boolean = false,
    val editingToolIndex: Int = -1,
    val editingToolPose: Pose = Pose(0.0, 0.0, 0.0, 0.0, 0.0, 0.0),
    val editingToolRemark: String = "",
    val positionMode: String = "右",
    val isPositionDialogVisible: Boolean = false,
    val speedMode: String = "1倍",
    val isSpeedDialogVisible: Boolean = false,
    val installPos: Int = 0,
    val isInstallPosDialogVisible: Boolean = false,
    val isExtAxisEnabled: Boolean = true,
)
