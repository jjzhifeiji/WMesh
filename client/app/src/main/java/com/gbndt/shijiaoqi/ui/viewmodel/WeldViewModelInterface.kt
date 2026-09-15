package com.gbndt.shijiaoqi.ui.viewmodel

import com.gbndt.shijiaoqi.data.models.Pose
import androidx.compose.runtime.snapshots.SnapshotStateList

interface WeldViewModelInterface {
    // Tool Dialogs
    var isToolListDialogVisible: Boolean
    val toolCoordinates: SnapshotStateList<Pose?>
    val toolRemarks: SnapshotStateList<String>
    var toolCoordinateSystem: String
    fun selectTool(index: Int)
    fun openToolEdit(index: Int)
    
    var isToolEditDialogVisible: Boolean
    var editingToolPose: Pose
    var editingToolRemark: String
    var editingToolIndex: Int
    fun cancelToolEdit()
    fun saveToolEdit(pose: Pose, remark: String)

    // Position Dialog
    var isPositionDialogVisible: Boolean
    var positionMode: String
    fun setPosition(mode: String)

    // Speed Dialog
    var isSpeedDialogVisible: Boolean
    var speedMode: String
    fun setSpeed(mode: String)

    // Install Position Dialog
    var isInstallPosDialogVisible: Boolean
    var installPos: Int
    fun updateInstallPos(pos: Int)

    // Status Bar
    var connectionStatus: String
    var alarmStatus: String
    var isControllerActive: Boolean
    var operationPosition: Pose
    fun toggleControllerActive()
    fun stopControllerActive()
    
    // Project Info
    var currentProjectName: String?
    
    // Project Explorer Interface
    var projectCurrentPath: String
    val projectItems: SnapshotStateList<com.gbndt.shijiaoqi.data.models.FileSystemItem>
    fun navigateProject(item: com.gbndt.shijiaoqi.data.models.FileSystemItem)
    fun navigateProjectBack()
    fun refreshProjectExplorer()
    fun createProjectFolder(name: String)
    fun openProject(path: String)
    fun deleteProjectItem(item: com.gbndt.shijiaoqi.data.models.FileSystemItem)
    fun createProject(name: String)

    // Process Explorer Interface
    var processCurrentPath: String
    val processItems: SnapshotStateList<com.gbndt.shijiaoqi.data.models.FileSystemItem>
    fun navigateProcess(item: com.gbndt.shijiaoqi.data.models.FileSystemItem)
    fun navigateProcessBack()
    fun refreshProcessExplorer()
    fun createProcessFolder(name: String)
    fun createProcess(name: String, process: com.gbndt.shijiaoqi.data.models.WeldProcess)
    fun updateProcess(item: com.gbndt.shijiaoqi.data.models.FileSystemItem, process: com.gbndt.shijiaoqi.data.models.WeldProcess)
    fun deleteProcessItem(item: com.gbndt.shijiaoqi.data.models.FileSystemItem)
    fun importProcess(uri: android.net.Uri)
    fun importProcessZip(uri: android.net.Uri)
    fun updateStandardProcessLibrary(url: String)
    fun exportProcess(item: com.gbndt.shijiaoqi.data.models.FileSystemItem): java.io.File?
    fun exportProcessZip(item: com.gbndt.shijiaoqi.data.models.FileSystemItem): java.io.File?
    fun selectProcess(item: com.gbndt.shijiaoqi.data.models.FileSystemItem)
    fun loadProcess(path: String): com.gbndt.shijiaoqi.data.models.WeldProcess?
    fun saveProcess(process: com.gbndt.shijiaoqi.data.models.WeldProcess)

    // Registration
    var isRegistered: Boolean
    var machineCode: String
    fun checkRegistration()
    fun register(code: String): Boolean

    var extAxisPos: Double
    var extAxisReady: Boolean
    var isExtAxisEnabled: Boolean // 外部轴切换开关
    fun toggleExtAxisEnabled()

    // Update
    var isUpdateDialogVisible: Boolean
    var updateInfo: com.gbndt.shijiaoqi.data.manager.UpdateInfo?
    fun startUpdateDownload()
    fun checkForUpdate()

    // Control
    fun sendManualCommand(type: Int, command: String)

    // Stats & Actions
    var weldingLength: Double
    var weldingDuration: Long
    var weldingBreakOffState: String
    var weldArcState: String
    fun clearStats()
    fun resetAllError()
    fun reconnect()

    // Welding Operations
    fun startSimulation()
    fun startArcWelding()
    fun pauseWelding()
    fun continueWelding()
    fun stopWelding(force: Boolean = false)

    // Robot Error Dialog
    var isRobotErrorDialogVisible: Boolean
    val currentRobotErrors: SnapshotStateList<com.gbndt.shijiaoqi.data.models.RobotError>

    // Function to show toast
    fun showToast(message: String)
}
