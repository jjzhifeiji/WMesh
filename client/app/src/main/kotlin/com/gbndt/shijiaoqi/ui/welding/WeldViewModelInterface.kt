package com.gbndt.shijiaoqi.ui.welding

import com.gbndt.shijiaoqi.model.Pose
import com.gbndt.shijiaoqi.domain.weld.ProcessChoice
import com.gbndt.shijiaoqi.domain.weld.ProjectChoice
import androidx.compose.runtime.snapshots.SnapshotStateList
import kotlinx.coroutines.flow.SharedFlow
import java.util.UUID

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
    val projectItems: SnapshotStateList<com.gbndt.shijiaoqi.model.FileSystemItem>
    fun navigateProject(item: com.gbndt.shijiaoqi.model.FileSystemItem)
    fun navigateProjectBack()
    fun refreshProjectExplorer()
    fun createProjectFolder(name: String)
    fun openProject(path: String)
    fun deleteProjectItem(item: com.gbndt.shijiaoqi.model.FileSystemItem)
    fun createProject(name: String)

    // Process Explorer Interface
    var processCurrentPath: String
    val processItems: SnapshotStateList<com.gbndt.shijiaoqi.model.FileSystemItem>
    fun navigateProcess(item: com.gbndt.shijiaoqi.model.FileSystemItem)
    fun navigateProcessBack()
    fun refreshProcessExplorer()
    fun createProcessFolder(name: String)
    fun createProcess(name: String, process: com.gbndt.shijiaoqi.model.WeldProcess)
    fun updateProcess(item: com.gbndt.shijiaoqi.model.FileSystemItem, process: com.gbndt.shijiaoqi.model.WeldProcess)
    fun deleteProcessItem(item: com.gbndt.shijiaoqi.model.FileSystemItem)
    fun importProcess(uri: android.net.Uri)
    fun importProcessZip(uri: android.net.Uri)
    fun updateStandardProcessLibrary(url: String)
    fun exportProcess(item: com.gbndt.shijiaoqi.model.FileSystemItem): java.io.File?
    fun exportProcessZip(item: com.gbndt.shijiaoqi.model.FileSystemItem): java.io.File?
    fun selectProcess(item: com.gbndt.shijiaoqi.model.FileSystemItem)
    fun loadProcess(path: String): com.gbndt.shijiaoqi.model.WeldProcess?
    fun saveProcess(process: com.gbndt.shijiaoqi.model.WeldProcess)

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
    var updateInfo: com.gbndt.shijiaoqi.data.update.UpdateInfo?
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
    val currentRobotErrors: SnapshotStateList<com.gbndt.shijiaoqi.model.RobotError>

    // Function to show toast
    fun showToast(message: String)

    // 手柄：Activity 收键，写进当前在屏的焊接 ViewModel
    var joyX1: Float
    var joyY1: Float
    var joyX2: Float
    var joyY2: Float
    var btnUp: Boolean
    var btnDown: Boolean
    var btnLeft: Boolean
    var btnRight: Boolean
    var btnL1: Boolean
    var btnL2: Boolean
    var btnR2: Boolean
    fun collectData()
    fun sendMoveLCommand()

    // 本机袋：工程/工艺列表与激活，三个模式共用一套外壳
    val pouchProjects: SnapshotStateList<ProjectChoice>
    val pouchProcesses: SnapshotStateList<ProcessChoice>
    fun refreshPouchLists()
    fun activatePouchProject(id: UUID)
    fun bindProcessFromPouch(processId: UUID?)
    /** 取消挑工艺；多层没有附加工艺槽，默认什么都不做 */
    fun cancelAddProcessVariant() {}
    fun syncFromPouch()

    /** 一次性提示，界面收了就弹 */
    val toastEvent: SharedFlow<String>
}
