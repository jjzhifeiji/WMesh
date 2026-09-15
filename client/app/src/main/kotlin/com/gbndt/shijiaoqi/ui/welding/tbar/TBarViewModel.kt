package com.gbndt.shijiaoqi.ui.welding.tbar

import android.speech.tts.TextToSpeech
import android.app.Application
import android.util.Log
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.launch
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.flow.collect
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.asSharedFlow
import com.gbndt.shijiaoqi.data.legacy.ProcessManager
import com.gbndt.shijiaoqi.data.legacy.ProjectManager
import com.gbndt.shijiaoqi.data.repository.PouchRepository
import com.gbndt.shijiaoqi.data.repository.UpdateRepository
import com.gbndt.shijiaoqi.data.repository.RobotRepository
import com.gbndt.shijiaoqi.data.repository.SessionRepository
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import com.gbndt.shijiaoqi.domain.robot.RobotCommands
import com.gbndt.shijiaoqi.model.FileSystemItem
import com.gbndt.shijiaoqi.model.GapBand
import com.gbndt.shijiaoqi.model.Pose
import com.gbndt.shijiaoqi.model.AppSettings
import com.gbndt.shijiaoqi.model.WeldPath
import com.gbndt.shijiaoqi.model.WeldPathProcessSlot
import com.gbndt.shijiaoqi.model.WeldPoint
import com.gbndt.shijiaoqi.model.WeldPointType
import com.gbndt.shijiaoqi.model.WeldProcess
import com.gbndt.shijiaoqi.ui.welding.FineTuneSupport
import com.gbndt.shijiaoqi.ui.welding.WeldViewModelInterface
import com.gbndt.shijiaoqi.domain.weld.TBarGeometry
import com.gbndt.shijiaoqi.domain.weld.TBarPass
import com.gbndt.shijiaoqi.model.isTBarCollectable
import com.gbndt.shijiaoqi.domain.weld.ProcessBind
import com.gbndt.shijiaoqi.domain.weld.ProcessChoice
import com.gbndt.shijiaoqi.domain.weld.ProcessJson
import com.gbndt.shijiaoqi.domain.weld.ProcessRef
import com.gbndt.shijiaoqi.domain.weld.ProjectChoice
import com.gbndt.shijiaoqi.domain.script.ScriptPoint
import com.gbndt.shijiaoqi.domain.script.TBarLua
import com.gbndt.shijiaoqi.domain.script.TBarPoint
import com.gbndt.shijiaoqi.domain.weld.TBarProject
import com.gbndt.shijiaoqi.domain.weld.TBarRun
import com.gbndt.shijiaoqi.domain.script.TBarScriptPath
import com.gbndt.shijiaoqi.domain.script.WeldRun
import java.util.UUID
import java.util.Locale
import java.io.File

import kotlin.math.sqrt
import kotlin.math.abs

import com.gbndt.shijiaoqi.model.UpdateInfo
import android.content.IntentFilter
import android.content.Intent
import android.app.DownloadManager
import android.content.BroadcastReceiver
import android.content.Context
import android.net.Uri
import com.gbndt.shijiaoqi.model.RefPoint


import com.gbndt.shijiaoqi.domain.weld.CoordinateUtils
import com.gbndt.shijiaoqi.domain.weld.CoordinateUtils.CoordinateSystem
import com.gbndt.shijiaoqi.domain.weld.Point3D
import com.gbndt.shijiaoqi.domain.weld.CoordinateUtils.toPoint3D
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlin.math.pow
import kotlin.math.sqrt

import kotlinx.coroutines.flow.first
import kotlinx.coroutines.withTimeoutOrNull
import kotlinx.coroutines.async
import com.gbndt.shijiaoqi.ui.welding.*

/** T 排：只写与单层不同的地方，其余走公共实现。 */
@HiltViewModel
class TBarViewModel @Inject constructor(
    application: Application,
    session: SessionRepository,
    socketManager: RobotRepository,
    pouch: PouchRepository,
    updateManager: UpdateRepository,
) : WeldingViewModel(application, session, socketManager, pouch, updateManager) {

    private var boundProcesses = emptyMap<UUID, WeldProcess>()

    override fun sendMoveLCommand() {
        val currentPath = currentActiveWeldPath ?: return
        
        val point = currentPath.points.getOrNull(currentPath.selectedPointIndex) ?: return
        
        var targetPose = point.pose
        val targetJoints = point.jointAngles

        if (targetPose != null) {
            val toolIndex = try {
                toolCoordinateSystem.removePrefix("工具").toInt()
            } catch (e: Exception) { 1 }

            val finalJoints = if (targetJoints != null && targetJoints.size >= 6) targetJoints else List(6) { 0.0 }

            val pos2 = listOf(
                finalJoints[0], finalJoints[1], finalJoints[2], finalJoints[3], finalJoints[4], finalJoints[5],
                targetPose.x, targetPose.y, targetPose.z, targetPose.rx, targetPose.ry, targetPose.rz
            ).joinToString(",") { String.format(Locale.US, "%.3f", it) }
            val ext1Str = String.format(Locale.US, "%.3f", targetPose.ext1)
            
            if (isExtAxisEnabled) {
                val extAxisCmd = "ExtAxisMoveJ(1,$ext1Str,0.000,0.000,0.000,100,0)"
                val msgExt = "/f/bIII${66}1III201III${extAxisCmd.length}III${extAxisCmd}III/b/f"
                socketManager.sendControlCommand(msgExt)
                Thread.sleep(50)
            }

            val cmd2 = "MoveL($pos2,$toolIndex,0,100,100,100,-1,0,$ext1Str,0.000,0.000,0.000,0,0,0,0,0,0,0,0,100,0)"
            val msg2 = "/f/bIII${66}2III201III${cmd2.length}III${cmd2}III/b/f"
            Log.d("MoveLCommand", "Standard MoveL: $msg2")
            Log.e("sendMoveLCommand", "msg2: $msg2")

            socketManager.sendControlCommand(msg2)
        }
    }

    override fun refreshProjectExplorer() {
        projectItems.clear()
        projectItems.addAll(projectManager.listContents(projectCurrentPath, "tbar"))
    }
    
    // 导航到工程目录

    override fun createProjectFolder(name: String) {
        if (projectManager.createFolder(projectCurrentPath, name, "tbar")) {
            refreshProjectExplorer()
        }
    }

    // 创建新工程

    override fun createNewProjectInCurrentPath(name: String) {
        if (projectManager.createProject(projectCurrentPath, name, "tbar")) {
            val fullPath = if (projectCurrentPath.isEmpty()) name else "$projectCurrentPath/$name"
            currentProjectName = fullPath
            
            // Reset to default state
            weldPaths.clear()
            
            addWeldPath() // Adds a default single layer path with empty points
            saveCurrentProject()
            refreshProjectExplorer()
        }
    }

    // Helper to get current active path (Single or Multi Base)

    protected override fun refsOf(paths: List<WeldPath>): List<ProcessRef> {
        val out = mutableListOf<ProcessRef>()
        paths.forEach { path ->
            path.gapBands.forEach { band ->
                val label = TBarRun.bandLabel(band)
                out.add(ProcessRef("${path.name} $label 打底", band.rootProcessId, path.isEnabled))
                out.add(ProcessRef("${path.name} $label 盖面", band.capProcessId, path.isEnabled))
            }
        }
        return out
    }

    protected override fun bindPouchProcesses(paths: List<WeldPath>): Boolean {
        val outcome = ProcessBind.resolve(refsOf(paths), processSource())
        boundProcesses = outcome.loaded
        if (outcome.missing.isNotEmpty()) {
            missingProcessMessage = "以下焊道的工艺不在当前闭包：\n" + outcome.missing.joinToString("\n")
            isMissingProcessDialogVisible = true
            return false
        }
        return true
    }

    override fun syncFromPouch() {
        refreshPouchLists()
        val id = pouch.activeProjectId() ?: return
        pouch.withProjectPlain(id) { bytes ->
            val loaded = try {
                TBarProject.parse(bytes)
            } catch (e: Exception) {
                Log.e("TBarViewModel", "active project parse failed", e)
                emptyList()
            }
            weldPaths.clear()
            weldPaths.addAll(loaded.map { it.asUiPath() })
            if (weldPaths.isEmpty()) addWeldPath()
            selectedWeldPathIndex = 0
            pouchProjectId = id
            currentProjectName = pouch.projectName(id)
            bindPouchProcesses(weldPaths)
        }
        refreshPouchLists()
    }

    fun currentGapBands(): List<GapBand> =
        (currentActiveWeldPath?.gapBands ?: emptyList()).sortedWith(compareBy({ it.layer }, { it.minGap }))

    fun bindGapBandProcess(bandIndex: Int, pass: TBarPass, processId: UUID?) {
        val path = currentActiveWeldPath ?: return
        val bands = path.gapBands.toMutableList()
        if (bandIndex !in bands.indices) return
        val idStr = processId?.toString().orEmpty()
        if (processId != null && processSource().open(processId) == null) {
            missingProcessMessage = "闭包里没有这条工艺"
            isMissingProcessDialogVisible = true
            return
        }
        val old = bands[bandIndex]
        bands[bandIndex] = if (pass == TBarPass.ROOT) {
            old.copy(rootProcessId = idStr)
        } else {
            old.copy(capProcessId = idStr)
        }
        weldPaths[selectedWeldPathIndex] = path.copy(gapBands = bands)
        saveCurrentProject()
    }

    private fun validateProcessFiles(paths: List<WeldPath>) {
        bindPouchProcesses(paths)
    }

    override fun saveCurrentProject() {
        val id = pouchProjectId ?: return
        weldPaths.forEach { pouch.saveWeldPath(it) }
        pouch.saveProject(id, TBarProject.encode(weldPaths.toList()))
    }

    override fun copyCurrentProject(newName: String) {
        val currentPath = currentProjectName ?: return
        // 复制到当前所在目录（或者根目录？）
        // 简单起见，复制到同级目录
        val parentPath = File(currentPath).parent?.replace("\\", "/") ?: ""
        if (projectManager.copyProject(currentPath, parentPath, newName, "tbar")) {
            refreshProjectExplorer()
        }
    }

    override fun deleteProjectItem(item: FileSystemItem) {
        if (projectManager.deleteItem(item.path, "tbar")) {
            if (currentProjectName == item.path) {
                currentProjectName = null
                weldPaths.clear()
                addWeldPath()
            }
            refreshProjectExplorer()
        }
    }
    
    // --- Process Explorer Methods ---

    override fun addWeldPath() {
        val id = UUID.randomUUID().toString()
        val name = "焊道 ${weldPaths.size + 1}"
        val points = mutableStateListOf(
            WeldPoint(UUID.randomUUID().toString(), WeldPointType.START_SAFE),
            WeldPoint(UUID.randomUUID().toString(), WeldPointType.GROOVE_A_LOWER),
            WeldPoint(UUID.randomUUID().toString(), WeldPointType.GROOVE_B_LOWER),
            WeldPoint(UUID.randomUUID().toString(), WeldPointType.GROOVE_A_UPPER),
            WeldPoint(UUID.randomUUID().toString(), WeldPointType.GROOVE_B_UPPER),
            WeldPoint(UUID.randomUUID().toString(), WeldPointType.START),
            WeldPoint(UUID.randomUUID().toString(), WeldPointType.END),
            WeldPoint(UUID.randomUUID().toString(), WeldPointType.END_SAFE)
        )
        val process = WeldProcess()
        val weldPath = WeldPath(id, name, points, process, gapBands = weldPaths.firstOrNull()?.gapBands.orEmpty())
        weldPaths.add(weldPath.asUiPath())
        selectedWeldPathIndex = weldPaths.lastIndex
        saveCurrentProject()

        // Scroll to new item
        viewModelScope.launch {
            _scrollToIndexEvent.emit(weldPaths.lastIndex)
        }
    }

    override fun generateCornerWelds(
        refPathAIndex: Int,
        refPathBIndex: Int,
        layerCount: Int,
        initialLength: Double,
        upwardOffset: Double,
        lengthReduction: Double,
        updateGroupId: String?,
        torchRx: Double?,
        torchRy: Double?,
        torchRz: Double?,
        process: WeldProcess?,
        processId: String?) {
        val refPathA = weldPaths.getOrNull(refPathAIndex) ?: return
        val refPathB = weldPaths.getOrNull(refPathBIndex) ?: return
        val baseProcess = process ?: refPathA.process
        val baseProcessId = processId?.takeIf { it.isNotEmpty() } ?: refPathA.processId

        val cornerPose: Pose
        
        val ptsA = refPathA.points.filter { it.type == WeldPointType.START || it.type == WeldPointType.MIDDLE || it.type == WeldPointType.END }
        val ptsB = refPathB.points.filter { it.type == WeldPointType.START || it.type == WeldPointType.MIDDLE || it.type == WeldPointType.END }
        
        if (ptsA.size < 2 || ptsB.size < 2) {
            viewModelScope.launch { _toastEvent.emit("参考焊道缺少足够的点，无法计算交点！") }
            return
        }
        
        // 找到两焊道中距离最近的两个点
        var minD = Double.MAX_VALUE
        var bestI = 0
        var bestJ = 0
        for (i in ptsA.indices) {
            for (j in ptsB.indices) {
                val pA = ptsA[i].pose?.toPoint3D() ?: continue
                val pB = ptsB[j].pose?.toPoint3D() ?: continue
                val d = pA.distanceTo(pB)
                if (d < minD) {
                    minD = d
                    bestI = i
                    bestJ = j
                }
            }
        }
        
        // 使用这两个最近点和它们相邻的点构造线段进行求交
        val pA1 = ptsA[bestI].pose!!.toPoint3D()
        val pA2 = if (bestI == 0) ptsA[1].pose!!.toPoint3D() else ptsA[bestI - 1].pose!!.toPoint3D()
        
        val pB1 = ptsB[bestJ].pose!!.toPoint3D()
        val pB2 = if (bestJ == 0) ptsB[1].pose!!.toPoint3D() else ptsB[bestJ - 1].pose!!.toPoint3D()
        
        val intersection = com.gbndt.shijiaoqi.domain.weld.CornerWeldGenerator.calculate3DIntersection(pA1, pA2, pB1, pB2)
        
        if (intersection == null) {
            viewModelScope.launch { _toastEvent.emit("无法计算交点，两条焊道平行或距离异常") }
            return
        }
        
        // 自动交点模式下，姿态继承自参考焊道B（立焊）的最近点，坐标用求出的交点
        cornerPose = ptsB[bestJ].pose!!.copy(x = intersection.x, y = intersection.y, z = intersection.z)

        val cornerPoint3D = cornerPose.toPoint3D()
        val vecA = getPathDirectionVector(refPathA, cornerPoint3D) ?: return
        val vecB = getPathDirectionVector(refPathB, cornerPoint3D) ?: return

        // 尝试获取安全起点和安全终点 (因为我们已经规定参考焊道B为立焊，所以直接提取B的安全点)
        val safeStartPose = refPathB.points.firstOrNull { it.type == WeldPointType.START_SAFE }?.pose
        val safeEndPose = refPathB.points.firstOrNull { it.type == WeldPointType.START_SAFE }?.pose

        val weldOrientation = if (torchRx != null && torchRy != null && torchRz != null) {
            Pose(0.0, 0.0, 0.0, torchRx, torchRy, torchRz)
        } else {
            null
        }

        val newPaths = com.gbndt.shijiaoqi.domain.weld.CornerWeldGenerator.generateCornerPaths(
            baseProcess = baseProcess,
            processId = baseProcessId,
            cornerPose = cornerPose,
            safeStartPose = safeStartPose,
            safeEndPose = safeEndPose,
            weldOrientation = weldOrientation,
            vecAIn = vecA,
            vecBIn = vecB,
            layerCount = layerCount,
            initialLength = initialLength,
            upwardOffset = upwardOffset,
            lengthReduction = lengthReduction,
            refPathAId = refPathA.id,
            refPathBId = refPathB.id
        )

        viewModelScope.launch {
            _toastEvent.emit("正在通过逆运动学计算关节角度，请稍候...")
            
            for (path in newPaths) {
                for (pt in path.points) {
                    if (pt.pose != null && pt.jointAngles == null) {
                        val joints = socketManager.getInverseKin(pt.pose!!)
                        if (joints != null && joints.size >= 6) {
                            pt.jointAngles = joints
                        }
                    }
                }
            }

            // Delete the old group if updating
            if (updateGroupId != null) {
                weldPaths.removeAll { it.cornerGroupParams?.groupId == updateGroupId }
            }

            // Add the generated paths to the end
            newPaths.forEachIndexed { i, p ->
                weldPaths.add(p.copy(name = "包角焊道 ${weldPaths.size + 1} (层 ${i + 1})").asUiPath())
            }

            saveCurrentProject()
            _toastEvent.emit("成功生成 $layerCount 层包角工艺")
            _scrollToIndexEvent.emit(weldPaths.lastIndex)
        }
    }

    // --- Duplicates Removed ---

    fun snapToCollectablePoint(weldPathIndex: Int) {
        val path = weldPaths.getOrNull(weldPathIndex) ?: return
        val current = path.points.getOrNull(path.selectedPointIndex)
        if (current != null && current.type.isTBarCollectable()) return
        val idx = path.points.indexOfFirst { it.type.isTBarCollectable() }
        if (idx >= 0 && idx != path.selectedPointIndex) {
            weldPaths[weldPathIndex] = path.copy(selectedPointIndex = idx)
        }
    }

    override fun selectPoint(weldPathIndex: Int, pointIndex: Int) {
        if (weldPathIndex < weldPaths.size && pointIndex < weldPaths[weldPathIndex].points.size) {
            val path = weldPaths[weldPathIndex]
            val point = path.points.getOrNull(pointIndex) ?: return
            if (!point.type.isTBarCollectable()) return

            // 切换到当前点击的焊道
            if (selectedWeldPathIndex != weldPathIndex) {
                selectedWeldPathIndex = weldPathIndex
            }

            if (path.selectedPointIndex != pointIndex) {
                weldPaths[weldPathIndex] = path.copy(selectedPointIndex = pointIndex)
            }
        }
    }

    fun tBarGapText(path: WeldPath): String {
        val aL = path.points.firstOrNull { it.type == WeldPointType.GROOVE_A_LOWER }?.pose
        val bL = path.points.firstOrNull { it.type == WeldPointType.GROOVE_B_LOWER }?.pose
        val aU = path.points.firstOrNull { it.type == WeldPointType.GROOVE_A_UPPER }?.pose
        val bU = path.points.firstOrNull { it.type == WeldPointType.GROOVE_B_UPPER }?.pose
        if (aL == null || bL == null || aU == null || bU == null) return "坡口点未采齐"
        val g0 = TBarGeometry.gapAt(aL, aU, bL, bU, 0.0)
        val g1 = TBarGeometry.gapAt(aL, aU, bL, bU, 1.0)
        val bands = path.gapBands
        val f0 = TBarRun.matchBand(g0, bands, 1)?.let { TBarRun.bandLabel(it) } ?: "无匹配"
        val f1 = TBarRun.matchBand(g1, bands, 1)?.let { TBarRun.bandLabel(it) } ?: "无匹配"
        return "起点间隙 ${"%.1f".format(g0)} mm($f0)  终点间隙 ${"%.1f".format(g1)} mm($f1)  先打底后盖面"
    }

    fun resolveTBarProcessFolder(): String = ""

    private fun rebuildTBarComputedPoints(path: WeldPath) {
        val startSafe = path.points.firstOrNull { it.type == WeldPointType.START_SAFE }?.pose
        val endSafe = path.points.firstOrNull { it.type == WeldPointType.END_SAFE }?.pose
        val aL = path.points.firstOrNull { it.type == WeldPointType.GROOVE_A_LOWER }?.pose
        val bL = path.points.firstOrNull { it.type == WeldPointType.GROOVE_B_LOWER }?.pose
        val aU = path.points.firstOrNull { it.type == WeldPointType.GROOVE_A_UPPER }?.pose
        val bU = path.points.firstOrNull { it.type == WeldPointType.GROOVE_B_UPPER }?.pose
        val computed = try {
            if (startSafe != null && endSafe != null && aL != null && bL != null && aU != null && bU != null) {
                TBarGeometry.buildWeldPoses(aL, bL, aU, bU, startSafe, endSafe)
            } else {
                null
            }
        } catch (t: Throwable) {
            Log.e("TBarViewModel", "计算起点终点失败", t)
            viewModelScope.launch { _toastEvent.emit("起点终点计算失败") }
            null
        }
        val startIdx = path.points.indices.firstOrNull { path.points[it].type == WeldPointType.START }
        val endIdx = path.points.indices.firstOrNull { path.points[it].type == WeldPointType.END }
        if (startIdx != null) {
            val point = path.points[startIdx]
            path.points[startIdx] = point.copy(pose = computed?.first, jointAngles = null)
        }
        if (endIdx != null) {
            val point = path.points[endIdx]
            path.points[endIdx] = point.copy(pose = computed?.second, jointAngles = null)
        }
    }

    override fun collectData() {
        try {
            collectDataInternal()
        } catch (t: Throwable) {
            Log.e("TBarViewModel", "采集失败", t)
            viewModelScope.launch { _toastEvent.emit("采集失败: ${t.message ?: t.javaClass.simpleName}") }
        }
    }

    private fun collectDataInternal() {
        if (connectionStatus == "未连接") {
            viewModelScope.launch {
                _toastEvent.emit("设备未连接")
            }
            return
        }

        val currentWeldPath = currentActiveWeldPath ?: return
        val pointIndex = currentWeldPath.selectedPointIndex
        if (pointIndex < 0 || pointIndex >= currentWeldPath.points.size) return

        val currentPoint = currentWeldPath.points[pointIndex]
        if (!currentPoint.type.isTBarCollectable()) {
            viewModelScope.launch { _toastEvent.emit("起点终点由坡口点计算，请采集起安、A下、B下、A上、B上、终安") }
            return
        }
        
        // Use real data if available, otherwise simulation data
        val currentPose = socketManager.robotPose.value
        val currentJoints = socketManager.robotJoints.value
        
        val updatedPoint = if (currentPose != null && currentJoints.isNotEmpty()) {
            currentPoint.copy(
                pose = currentPose,
                jointAngles = currentJoints
            )
        } else {
            // Fallback to simulation data if no connection
            currentPoint.copy(
                pose = Pose(100.0, 200.0, 300.0, 0.0, 0.0, 0.0),
                jointAngles = listOf(0.0, 0.0, 0.0, 0.0, 0.0, 0.0)
            )
        }
        
        currentWeldPath.points[pointIndex] = updatedPoint
        rebuildTBarComputedPoints(currentWeldPath)
        
        val nextPointIndex = (pointIndex + 1 until currentWeldPath.points.size).firstOrNull {
            currentWeldPath.points[it].type.isTBarCollectable()
        } ?: pointIndex
        
        if (nextPointIndex != pointIndex) {
            updateCurrentActiveWeldPath(currentWeldPath.copy(selectedPointIndex = nextPointIndex))
        } else {
            updateCurrentActiveWeldPath(currentWeldPath)
        }
    }

    // 新增：采集 X 方向参考点

    override fun clearPointData() {
        val currentWeldPath = currentActiveWeldPath ?: return
        val pointIndex = currentWeldPath.selectedPointIndex
        if (pointIndex < 0 || pointIndex >= currentWeldPath.points.size) return

        val currentPoint = currentWeldPath.points[pointIndex]
        if (!currentPoint.type.isTBarCollectable()) return

        // Clear Data
        val updatedPoint = currentPoint.copy(
            pose = null,
            jointAngles = null
        )
        currentWeldPath.points[pointIndex] = updatedPoint
        rebuildTBarComputedPoints(currentWeldPath)
        
        updateCurrentActiveWeldPath(currentWeldPath)
    }

    protected override fun appendCommandsForPath(
        weldPath: WeldPath, 
        builder: BatchCommandBuilder, 
        pathIndexForMap: Int, 
        toolIndex: Int,
        startIndex: Int,
        isResumeFromStop: Boolean,
        extraProcessIndex: Int) {
        val points = weldPath.points
        val process = weldPath.process

        // Calculate speed multiplier based on mode
        val multiplier = if (isSimulating) {
            when (speedMode) {
                "3倍" -> 3
                "5倍" -> 5
                else -> 1
            }
        } else {
            1
        }
        val finalSpeed = (process.speed * multiplier).toInt()
         
         // --- 1. Welding Process Parameters Command ---
         val paramCmd = "WeldingSetProcessParam(2,${process.startArcCurrent},${process.startArcVoltage},${process.startArcTime},${process.current},${process.voltage},${process.endArcCurrent},${process.endArcVoltage},${process.endArcTime})"
         val paramId = globalCommandId++
         builder.appendCmd(paramCmd, paramId)

          // --- 2. Oscillation Parameters Command ---
          val oscType = process.oscillation.type
          if (oscType != "无摆动") {
              val typeCode = when (oscType) {
                  "三角波摆动" -> 0
                  "直角L型三角波摆动" -> 1
                  "圆形摆动-顺时针" -> 2
                  "圆形摆动-逆时针" -> 3
                  "正弦波摆动" -> 4
                  "垂直L型正弦波摆动" -> 5
                  "立焊三角摆动" -> 6
                  else -> 0 // Default
              }
              
              val waitTimeCode = if (process.oscillation.waitTime == "不包括") 0 else 1
              val posWaitCode = if (process.oscillation.positionWait == "等待时间内位置继续移动") 0 else 1
              
              val weaveCmd = "WeaveSetPara(3,$typeCode,${process.oscillation.frequency},$waitTimeCode,${process.oscillation.amplitude},${process.oscillation.leftSideLength},${process.oscillation.rightSideLength},${process.oscillation.zeroTime},${process.oscillation.leftStopTime},${process.oscillation.rightStopTime},${process.oscillation.callbackRatio},$posWaitCode,${process.oscillation.azimuth},${process.oscillation.inclination})"
              val weaveId = globalCommandId++
              builder.appendCmd(weaveCmd, weaveId)
          }
          
          var pointIndex = startIndex
          var hasStartedArc = false
          
          if (isResumeFromStop && stopPointPose != null && stopPointJoints != null && stopPointJoints!!.size >= 6) {
              val vPose = stopPointPose!!
              val vJoints = stopPointJoints!!
              
              val pos2 = listOf(
                  vJoints[0], vJoints[1], vJoints[2], vJoints[3], vJoints[4], vJoints[5],
                  vPose.x, vPose.y, vPose.z, vPose.rx, vPose.ry, vPose.rz
              ).joinToString(",") { String.format(Locale.US, "%.3f", it) }
              
              val moveSpeed = 100 // 空走回到断点
              val ext1Str = String.format(Locale.US, "%.3f", vPose.ext1)
              
              if (isExtAxisEnabled) {
                  val extAxisCmd = "ExtAxisMoveJ(1,$ext1Str,0.000,0.000,0.000,$moveSpeed,-1)"
                  val extAxisId = globalCommandId++
                  builder.appendCmd(extAxisCmd, extAxisId)
              }

              val cmd2 = "MoveL($pos2,$toolIndex,0,100,100,$moveSpeed,-1,0,$ext1Str,0.000,0.000,0.000,0,0,0,0,0,0,0,0,100,0)"
              
              val id = globalCommandId++
              commandIdMap[id] = Triple(pathIndexForMap, stopPointIndex, extraProcessIndex)
              builder.appendCmd(cmd2, id)
              
              val startPointIndex = points.indexOfFirst { it.type == WeldPointType.START }
              
              if (stopPointIndex > startPointIndex && startPointIndex != -1) {
                  // Robot was already welding when it stopped. Resume welding from here.
                  if (isWelding) {
                      val arcStartCmd = "ARCStart(0,2,10000)"
                      val arcStartId = globalCommandId++
                      builder.appendCmd(arcStartCmd, arcStartId)
                  }
                  if (oscType != "无摆动") {
                      val weaveStartCmd = "WeaveStart(3)"
                      val weaveStartId = globalCommandId++
                      builder.appendCmd(weaveStartCmd, weaveStartId)
                  }
                  hasStartedArc = true
              }
              
              // Continue to the point we were moving towards when stopped
              pointIndex = stopPointIndex
              
              // NEW LOGIC: Arc Resume
              // If stopped during an arc, we dynamically calculate a new intermediate point
              // from the current physical position to properly resume the arc.
              var isArcResume = false
              var arcStartIndex = -1
              
              // Protect pointIndex from being negative when accessing points
              val safeIndex = if (pointIndex >= 0) pointIndex else 0
              
              android.util.Log.e("ArcDebug", "stopPointIndex: $stopPointIndex, pointIndex: $pointIndex, safeIndex: $safeIndex")
              
              // Find which segment of the arc we are in based on safeIndex (the point we are heading towards)
              if (safeIndex > 0 && safeIndex < points.size && points[safeIndex].type == WeldPointType.END && points[safeIndex - 1].type == WeldPointType.ARC_MIDDLE) {
                  // Heading towards END. The arc is START -> ARC_MIDDLE -> END
                  arcStartIndex = safeIndex - 2
                  isArcResume = true
                  android.util.Log.e("ArcDebug", "isArcResume: TRUE (Heading towards END)")
              } else if (safeIndex > 0 && safeIndex < points.size && points[safeIndex].type == WeldPointType.ARC_MIDDLE && points[safeIndex - 1].type == WeldPointType.START) {
                  // Heading towards ARC_MIDDLE. The arc is START -> ARC_MIDDLE -> END
                  arcStartIndex = safeIndex - 1
                  isArcResume = true
                  android.util.Log.e("ArcDebug", "isArcResume: TRUE (Heading towards ARC_MIDDLE)")
              } else if (safeIndex < points.size - 2 && points[safeIndex].type == WeldPointType.START && points[safeIndex + 1].type == WeldPointType.ARC_MIDDLE && points[safeIndex + 2].type == WeldPointType.END) {
                  // Heading towards START. We haven't even reached the start of the arc yet!
                  isArcResume = false
                  android.util.Log.e("ArcDebug", "isArcResume: FALSE (Heading towards START)")
              } else {
                  // EXTENDED LOGIC:
                  if (safeIndex < points.size && points[safeIndex].type == WeldPointType.ARC_MIDDLE && safeIndex > 0 && points[safeIndex - 1].type == WeldPointType.START) {
                      arcStartIndex = safeIndex - 1
                      isArcResume = true
                      android.util.Log.e("ArcDebug", "isArcResume: TRUE (Standing ON ARC_MIDDLE)")
                  } else if (safeIndex < points.size && points[safeIndex].type == WeldPointType.END && safeIndex > 1 && points[safeIndex - 1].type == WeldPointType.ARC_MIDDLE) {
                      arcStartIndex = safeIndex - 2
                      isArcResume = true
                      android.util.Log.e("ArcDebug", "isArcResume: TRUE (Standing ON END)")
                  } else {
                      android.util.Log.e("ArcDebug", "isArcResume: FALSE (Not in Arc Segment)")
                  }
              }
              
              if (isArcResume && arcStartIndex >= 0 && arcStartIndex + 2 < points.size) {
                  val pStartPt = points[arcStartIndex]
                  val pMidPt = points[arcStartIndex + 1]
                  val pEndPt = points[arcStartIndex + 2]
                  
                  val pStart = pStartPt.pose
                  val pMid = pMidPt.pose
                  val pEnd = pEndPt.pose
                  val jEnd = pEndPt.jointAngles ?: listOf(0.0, 0.0, 0.0, 0.0, 0.0, 0.0)
                  
                  if (pStart != null && pMid != null && pEnd != null) {
                      // 1. Calculate the new midpoint in Physical Space because stopPointPose is physical
                      val (startOffX, startOffY, startOffZ) = calculateOffset(pStartPt, points, arcStartIndex, process)
                      val physStart = pStart.copy(x = pStart.x + startOffX, y = pStart.y + startOffY, z = pStart.z + startOffZ)
                      
                      val (midOff, endOff) = calculateArcOffset(pMidPt, pEndPt, points, arcStartIndex + 1, process)
                      val physMid = pMid.copy(x = pMid.x + midOff.first, y = pMid.y + midOff.second, z = pMid.z + midOff.third)
                      val physEnd = pEnd.copy(x = pEnd.x + endOff.first, y = pEnd.y + endOff.second, z = pEnd.z + endOff.third)
                      
                      val newMidPose = calculateNewArcMidPoint(physStart, physMid, physEnd, stopPointPose!!)
                      
                      android.util.Log.e("ArcDebug", "========= SingleLayer Arc Debug =========")
                      android.util.Log.e("ArcDebug", "Original Start (Physical): ${physStart.x}, ${physStart.y}, ${physStart.z}")
                      android.util.Log.e("ArcDebug", "Original Mid (Physical):   ${physMid.x}, ${physMid.y}, ${physMid.z}")
                      android.util.Log.e("ArcDebug", "Original End (Physical):   ${physEnd.x}, ${physEnd.y}, ${physEnd.z}")
                      android.util.Log.e("ArcDebug", "Stop Point (Current):      ${stopPointPose!!.x}, ${stopPointPose!!.y}, ${stopPointPose!!.z}")
                      android.util.Log.e("ArcDebug", "New Mid Point calculated:  ${newMidPose.x}, ${newMidPose.y}, ${newMidPose.z}")
                      android.util.Log.e("ArcDebug", "=========================================")

                      val newMidJoints = interpolateJoints(stopPointJoints!!, jEnd)
                      
                      // 2. For the midpoint, pass the Physical Cartesian Pose and interpolated joints
                      //    And disable offset for Midpoint (Enable=0) since it is already physical.
                      val midPosStr = listOf(
                          newMidJoints[0], newMidJoints[1], newMidJoints[2], newMidJoints[3], newMidJoints[4], newMidJoints[5],
                          newMidPose.x, newMidPose.y, newMidPose.z, newMidPose.rx, newMidPose.ry, newMidPose.rz
                      ).joinToString(",") { String.format(Locale.US, "%.3f", it) }
                      
                      // 3. For the endpoint, pass the ORIGINAL Virtual Pose and ORIGINAL Joint Angles
                      //    And let the controller apply the offset (Enable=1) natively!
                      val endPosStr = listOf(
                          jEnd[0], jEnd[1], jEnd[2], jEnd[3], jEnd[4], jEnd[5],
                          pEnd.x, pEnd.y, pEnd.z, pEnd.rx, pEnd.ry, pEnd.rz
                      ).joinToString(",") { String.format(Locale.US, "%.3f", it) }
                      
                      // CRITICAL FIX: Even if there is no offset, we MUST force Enable=1 (1,0,0,0,0,0,0) for the midpoint.
                      // Why? Because newMidPose (Cartesian) and newMidJoints (Interpolated) do NOT mathematically match perfectly.
                      // If Enable=0, the controller strictly verifies Joints vs Cartesian and throws "Instruction Point Error".
                      // If Enable=1, the controller runs Inverse Kinematics (IK) to recalculate the exact joints for the Cartesian point, masking our interpolation error!
                      val midOffsetStr = "1,0.000,0.000,0.000,0.000,0.000,0.000"
                      val endOffsetStr = if (endOff.first == 0.0 && endOff.second == 0.0 && endOff.third == 0.0) {
                          "0,0,0,0,0,0,0"
                      } else {
                          "3,${String.format(Locale.US, "%.3f", endOff.first)},${String.format(Locale.US, "%.3f", endOff.second)},${String.format(Locale.US, "%.3f", endOff.third)},0,0,0"
                      }
                      
                      val moveC = "MoveC($midPosStr,$toolIndex,0,100,100,0,0,0,0,$midOffsetStr,$endPosStr,$toolIndex,0,100,100,0,0,0,0,$endOffsetStr,$finalSpeed,-1)"
                      
                      val arcId = globalCommandId++
                      commandIdMap[arcId] = Triple(pathIndexForMap, arcStartIndex + 2, extraProcessIndex)
                      builder.appendCmd(moveC, arcId)
                      
                      if (pEndPt.type == WeldPointType.END) {
                          if (isWelding) {
                              val arcEndCmd = "ARCEnd(0,2,10000)"
                              val arcEndId = globalCommandId++
                              builder.appendCmd(arcEndCmd, arcEndId)
                          }
                          if (oscType != "无摆动") {
                              val weaveEndCmd = "WeaveEnd(0)"
                              val weaveEndId = globalCommandId++
                              builder.appendCmd(weaveEndCmd, weaveEndId)
                          }
                      }
                      
                      // Skip all arc points, go to the point AFTER the arc END
                      pointIndex = arcStartIndex + 3
                  }
              }
          } else {
              // Not an arc resume, just normal resume point
              pointIndex = if (stopPointIndex >= 0) stopPointIndex else 0
          }
          
          while (pointIndex >= 0 && pointIndex < points.size) {
            val point = points[pointIndex]
            val pose = point.pose
            val joints = point.jointAngles
            
            if (pose != null) {
                val jointsStr = if (joints != null && joints.size >= 6) {
                    joints.take(6).joinToString(",") { String.format(Locale.US, "%.3f", it) }
                } else "0,0,0,0,0,0"

                // Check for MoveC (ARC_MIDDLE)
                if (point.type == WeldPointType.ARC_MIDDLE && pointIndex + 1 < points.size) {
                    val nextPoint = points[pointIndex + 1]
                    val nextPose = nextPoint.pose
                    val nextJoints = nextPoint.jointAngles
                    
                    if (nextPose != null) {
                        val nextJointsStr = if (nextJoints != null && nextJoints.size >= 6) {
                            nextJoints.take(6).joinToString(",") { String.format(Locale.US, "%.3f", it) }
                        } else "0,0,0,0,0,0"

                        // Prepare Mid Point Data
                        val midPosStr = "${jointsStr},${String.format(Locale.US, "%.3f", pose.x)},${String.format(Locale.US, "%.3f", pose.y)},${String.format(Locale.US, "%.3f", pose.z)},${String.format(Locale.US, "%.3f", pose.rx)},${String.format(Locale.US, "%.3f", pose.ry)},${String.format(Locale.US, "%.3f", pose.rz)}"
                        
                        // Prepare End Point Data
                        val endPosStr = "${nextJointsStr},${String.format(Locale.US, "%.3f", nextPose.x)},${String.format(Locale.US, "%.3f", nextPose.y)},${String.format(Locale.US, "%.3f", nextPose.z)},${String.format(Locale.US, "%.3f", nextPose.rx)},${String.format(Locale.US, "%.3f", nextPose.ry)},${String.format(Locale.US, "%.3f", nextPose.rz)}"
                        
                        // Calculate Arc Offsets
                        val (midOff, endOff) = calculateArcOffset(point, nextPoint, points, pointIndex, process)
                        
                        val midOffsetStr = if (midOff.first == 0.0 && midOff.second == 0.0 && midOff.third == 0.0) {
                            "0,0,0,0,0,0,0"
                        } else {
                            "3,${String.format(Locale.US, "%.3f", midOff.first)},${String.format(Locale.US, "%.3f", midOff.second)},${String.format(Locale.US, "%.3f", midOff.third)},0,0,0"
                        }
                        
                        val endOffsetStr = if (endOff.first == 0.0 && endOff.second == 0.0 && endOff.third == 0.0) {
                            "0,0,0,0,0,0,0"
                        } else {
                            "3,${String.format(Locale.US, "%.3f", endOff.first)},${String.format(Locale.US, "%.3f", endOff.second)},${String.format(Locale.US, "%.3f", endOff.third)},0,0,0"
                        }
                        
                        val ext1Str = String.format(Locale.US, "%.3f", nextPose.ext1)
                        if (isExtAxisEnabled) {
                            val extAxisCmd = "ExtAxisMoveJ(1,$ext1Str,0.000,0.000,0.000,$finalSpeed,-1)"
                            val extAxisId = globalCommandId++
                            builder.appendCmd(extAxisCmd, extAxisId)
                        }

                        val moveC = "MoveC($midPosStr,$toolIndex,0,100,100,0,0,0,0,$midOffsetStr,$endPosStr,$toolIndex,0,100,100,0,0,0,0,$endOffsetStr,$finalSpeed,-1)"
                        
                        val id = globalCommandId++
                        // Map to the END point of the arc
                        commandIdMap[id] = Triple(pathIndexForMap, pointIndex + 1, extraProcessIndex)
                        
                        builder.appendCmd(moveC, id)
                        
                        if (pointIndex == startIndex && oscType != "无摆动" && !hasStartedArc) {
                            val weaveStartCmd = "WeaveStart(3)"
                            val weaveStartId = globalCommandId++
                            builder.appendCmd(weaveStartCmd, weaveStartId)
                        }

                        // Logic for End of Arc if it's the actual END point
                        if (nextPoint.type == WeldPointType.END) {
                            if (isWelding) {
                                // Arc End
                                val arcEndCmd = "ARCEnd(0,2,10000)"
                                val arcEndId = globalCommandId++
                                builder.appendCmd(arcEndCmd, arcEndId)
                            }
                            
                            if (oscType != "无摆动") {
                                // Weave End
                                val weaveEndCmd = "WeaveEnd(0)"
                                val weaveEndId = globalCommandId++
                                builder.appendCmd(weaveEndCmd, weaveEndId)
                            }
                        }
                        
                        // Skip next point as it is consumed by MoveC
                        pointIndex += 2
                        continue
                    }
                }
                
                // Normal MoveL Logic
                // Calculate Offsets
                val (offX, offY, offZ) = calculateOffset(point, points, pointIndex, process)
                val hasOffset = abs(offX) >= 1e-5 || abs(offY) >= 1e-5 || abs(offZ) >= 1e-5
                
                // 空走（起安、起点、终安）ovl=100，焊接段用工艺速度
                val moveSpeed = if (point.type == WeldPointType.START_SAFE || point.type == WeldPointType.END_SAFE || point.type == WeldPointType.START) 100 else finalSpeed
                
                val ext1Str = String.format(Locale.US, "%.3f", pose.ext1)

                // 1. Insert ExtAxisMoveJ
                if (isExtAxisEnabled) {
                    val extAxisCmd = "ExtAxisMoveJ(1,$ext1Str,0.000,0.000,0.000,$moveSpeed,-1)"
                    val extAxisId = globalCommandId++
                    builder.appendCmd(extAxisCmd, extAxisId)
                }

                val id = globalCommandId++
                commandIdMap[id] = Triple(pathIndexForMap, pointIndex, extraProcessIndex)

                // 第七轴开启时，MoveL 的 offset_flag≠0 会在导轨到位后做笛卡尔逆解，终点报 112。
                // GetInverseKinRef 是六轴逆解，终点笛卡尔在扩展轴下会与关节对不上（74）。
                // 改用协议 6.3.28 GetInverseKinExaxis，把扩展轴位置一起代入再发 offset_flag=0 的 MoveL。
                if (hasOffset && isExtAxisEnabled && joints != null && joints.size >= 6) {
                    val nx = String.format(Locale.US, "%.3f", pose.x + offX)
                    val ny = String.format(Locale.US, "%.3f", pose.y + offY)
                    val nz = String.format(Locale.US, "%.3f", pose.z + offZ)
                    val rx = String.format(Locale.US, "%.3f", pose.rx)
                    val ry = String.format(Locale.US, "%.3f", pose.ry)
                    val rz = String.format(Locale.US, "%.3f", pose.rz)
                    val ikCmd = "j1,j2,j3,j4,j5,j6=GetInverseKinExaxis(0,{$nx,$ny,$nz,$rx,$ry,$rz},{$ext1Str,0.000,0.000,0.000},$toolIndex,0)"
                    builder.appendCmd(ikCmd)
                    val cmd2 = "MoveL(j1,j2,j3,j4,j5,j6,$nx,$ny,$nz,$rx,$ry,$rz,$toolIndex,0,100,100,$moveSpeed,-1,0,$ext1Str,0.000,0.000,0.000,0,0,0,0,0,0,0,0,100,0)"
                    builder.appendCmd(cmd2, id)
                } else {
                    val pos2 = "${jointsStr},${String.format(Locale.US, "%.3f", pose.x)},${String.format(Locale.US, "%.3f", pose.y)},${String.format(Locale.US, "%.3f", pose.z)},${String.format(Locale.US, "%.3f", pose.rx)},${String.format(Locale.US, "%.3f", pose.ry)},${String.format(Locale.US, "%.3f", pose.rz)}"
                    // 无第七轴时 offset_flag=3（基座标系）可用；有第七轴无偏移则 flag=0。
                    val userParams = if (!hasOffset) {
                        "0,0,0,0,0,0,0"
                    } else {
                        "3,${String.format(Locale.US, "%.3f", offX)},${String.format(Locale.US, "%.3f", offY)},${String.format(Locale.US, "%.3f", offZ)},0,0,0"
                    }
                    val cmd2 = "MoveL($pos2,$toolIndex,0,100,100,$moveSpeed,-1,0,$ext1Str,0.000,0.000,0.000,0,$userParams,100,0)"
                    builder.appendCmd(cmd2, id)
                }

                // --- INSERT Start/End Logic Here ---
                if (point.type == WeldPointType.START) {
                    if (!hasStartedArc) {
                        if (isWelding) {
                            // Arc Start
                            val arcStartCmd = "ARCStart(0,2,10000)"
                            val arcStartId = globalCommandId++
                            builder.appendCmd(arcStartCmd, arcStartId)
                        }
                        
                        if (oscType != "无摆动") {
                            // Weave Start
                            val weaveStartCmd = "WeaveStart(3)"
                            val weaveStartId = globalCommandId++
                            builder.appendCmd(weaveStartCmd, weaveStartId)
                        }
                        hasStartedArc = true
                    }
                } else {
                    // If it is the first point of the path (but not START), send WeaveStart to apply parameters
                    // This ensures parameters take effect for continuous welding segments
                    // But exclude START_SAFE and END_SAFE points
                    if (pointIndex == startIndex && oscType != "无摆动" && point.type != WeldPointType.START_SAFE && point.type != WeldPointType.END_SAFE && !hasStartedArc) {
                        val weaveStartCmd = "WeaveStart(3)"
                        val weaveStartId = globalCommandId++
                        builder.appendCmd(weaveStartCmd, weaveStartId)
                    }

                    if (point.type == WeldPointType.END) {
                        if (isWelding) {
                            // Arc End
                            val arcEndCmd = "ARCEnd(0,2,10000)"
                            val arcEndId = globalCommandId++
                            builder.appendCmd(arcEndCmd, arcEndId)
                        }
                        
                        if (oscType != "无摆动") {
                            // Weave End
                            val weaveEndCmd = "WeaveEnd(0)"
                            val weaveEndId = globalCommandId++
                            builder.appendCmd(weaveEndCmd, weaveEndId)
                        }
                    }
                }
            }
            pointIndex++
        }
    }

    private fun formatNum(value: Double): String {
        return if (value == value.toLong().toDouble()) value.toLong().toString()
        else String.format(Locale.US, "%.1f", value)
    }

    private fun weaveSetCmd(process: WeldProcess): String? {
        val osc = process.oscillation
        if (osc.type == "无摆动") return null
        val typeCode = when (osc.type) {
            "三角波摆动" -> 0
            "直角L型三角波摆动" -> 1
            "圆形摆动-顺时针" -> 2
            "圆形摆动-逆时针" -> 3
            "正弦波摆动" -> 4
            "垂直L型正弦波摆动" -> 5
            "立焊三角摆动" -> 6
            else -> 0
        }
        val waitTimeCode = if (osc.waitTime == "不包括") 0 else 1
        val posWaitCode = if (osc.positionWait == "等待时间内位置继续移动") 0 else 1
        return "WeaveSetPara(3,$typeCode,${osc.frequency},$waitTimeCode,${osc.amplitude},${osc.leftSideLength},${osc.rightSideLength},${osc.zeroTime},${osc.leftStopTime},${osc.rightStopTime},${osc.callbackRatio},$posWaitCode,${osc.azimuth},${osc.inclination})"
    }

    private fun weaveOnlineCmd(process: WeldProcess): String? {
        val osc = process.oscillation
        if (osc.type == "无摆动") return null
        val typeCode = when (osc.type) {
            "三角波摆动" -> 0
            "直角L型三角波摆动" -> 1
            "圆形摆动-顺时针" -> 2
            "圆形摆动-逆时针" -> 3
            "正弦波摆动" -> 4
            "垂直L型正弦波摆动" -> 5
            "立焊三角摆动" -> 6
            else -> 0
        }
        val waitTimeCode = if (osc.waitTime == "不包括") 0 else 1
        val posWaitCode = if (osc.positionWait == "等待时间内位置继续移动") 0 else 1
        return "WeaveOnlineSetPara(3,$typeCode,${formatNum(osc.frequency)},$waitTimeCode,${formatNum(osc.amplitude)},${osc.leftStopTime.toInt()},${osc.rightStopTime.toInt()},${osc.callbackRatio.toInt().coerceIn(0, 100)},$posWaitCode)"
    }

    private fun appendIkMoveL(builder: BatchCommandBuilder, pose: Pose, toolIndex: Int, speed: Int, cmdId: Int) {
        val nx = String.format(Locale.US, "%.3f", pose.x)
        val ny = String.format(Locale.US, "%.3f", pose.y)
        val nz = String.format(Locale.US, "%.3f", pose.z)
        val rx = String.format(Locale.US, "%.3f", pose.rx)
        val ry = String.format(Locale.US, "%.3f", pose.ry)
        val rz = String.format(Locale.US, "%.3f", pose.rz)
        val ext1Str = String.format(Locale.US, "%.3f", pose.ext1)
        if (isExtAxisEnabled) {
            builder.appendCmd("ExtAxisMoveJ(1,$ext1Str,0.000,0.000,0.000,$speed,-1)", globalCommandId++)
        }
        builder.appendCmd("j1,j2,j3,j4,j5,j6=GetInverseKinExaxis(0,{$nx,$ny,$nz,$rx,$ry,$rz},{$ext1Str,0.000,0.000,0.000},$toolIndex,0)")
        builder.appendCmd("MoveL(j1,j2,j3,j4,j5,j6,$nx,$ny,$nz,$rx,$ry,$rz,$toolIndex,0,100,100,$speed,-1,0,$ext1Str,0.000,0.000,0.000,0,0,0,0,0,0,0,0,100,0)", cmdId)
    }

    private fun appendTaughtMoveL(builder: BatchCommandBuilder, point: WeldPoint, toolIndex: Int, speed: Int, cmdId: Int) {
        val pose = point.pose ?: return
        val joints = point.jointAngles
        val jointsStr = if (joints != null && joints.size >= 6) {
            joints.take(6).joinToString(",") { String.format(Locale.US, "%.3f", it) }
        } else {
            appendIkMoveL(builder, pose, toolIndex, speed, cmdId)
            return
        }
        val ext1Str = String.format(Locale.US, "%.3f", pose.ext1)
        if (isExtAxisEnabled) {
            builder.appendCmd("ExtAxisMoveJ(1,$ext1Str,0.000,0.000,0.000,$speed,-1)", globalCommandId++)
        }
        val pos = "${jointsStr},${String.format(Locale.US, "%.3f", pose.x)},${String.format(Locale.US, "%.3f", pose.y)},${String.format(Locale.US, "%.3f", pose.z)},${String.format(Locale.US, "%.3f", pose.rx)},${String.format(Locale.US, "%.3f", pose.ry)},${String.format(Locale.US, "%.3f", pose.rz)}"
        builder.appendCmd("MoveL($pos,$toolIndex,0,100,100,$speed,-1,0,$ext1Str,0.000,0.000,0.000,0,0,0,0,0,0,0,0,100,0)", cmdId)
    }

    private fun applyProcessParams(builder: BatchCommandBuilder, process: WeldProcess, onlineWeave: Boolean) {
        val paramCmd = "WeldingSetProcessParam(2,${process.startArcCurrent},${process.startArcVoltage},${process.startArcTime},${process.current},${process.voltage},${process.endArcCurrent},${process.endArcVoltage},${process.endArcTime})"
        builder.appendCmd(paramCmd, globalCommandId++)
        builder.appendCmd("WeldingSetCurrent(0,${formatNum(process.current)},0)", globalCommandId++)
        builder.appendCmd("WeldingSetVoltage(0,${formatNum(process.voltage)},1)", globalCommandId++)
        val weave = if (onlineWeave) weaveOnlineCmd(process) else weaveSetCmd(process)
        if (weave != null) builder.appendCmd(weave, globalCommandId++)
    }

    private fun appendTBarCommandsForPath(
        weldPath: WeldPath,
        builder: BatchCommandBuilder,
        pathIndexForMap: Int,
        toolIndex: Int
    ) {
        val startSafe = weldPath.points.firstOrNull { it.type == WeldPointType.START_SAFE }
            ?: throw IllegalStateException("缺少起安点")
        val endSafe = weldPath.points.firstOrNull { it.type == WeldPointType.END_SAFE }
            ?: throw IllegalStateException("缺少终安点")
        val start = weldPath.points.firstOrNull { it.type == WeldPointType.START }
            ?: throw IllegalStateException("起点未计算")
        val end = weldPath.points.firstOrNull { it.type == WeldPointType.END }
            ?: throw IllegalStateException("终点未计算")
        if (startSafe.pose == null) throw IllegalStateException("请先采集起安")
        if (endSafe.pose == null) throw IllegalStateException("请先采集终安")
        val aL = weldPath.points.firstOrNull { it.type == WeldPointType.GROOVE_A_LOWER }?.pose
            ?: throw IllegalStateException("请先采集A下")
        val bL = weldPath.points.firstOrNull { it.type == WeldPointType.GROOVE_B_LOWER }?.pose
            ?: throw IllegalStateException("请先采集B下")
        val aU = weldPath.points.firstOrNull { it.type == WeldPointType.GROOVE_A_UPPER }?.pose
            ?: throw IllegalStateException("请先采集A上")
        val bU = weldPath.points.firstOrNull { it.type == WeldPointType.GROOVE_B_UPPER }?.pose
            ?: throw IllegalStateException("请先采集B上")
        val startPose = start.pose ?: throw IllegalStateException("起点未计算，请采齐坡口点和安全点")
        val endPose = end.pose ?: throw IllegalStateException("终点未计算，请采齐坡口点和安全点")
        if (weldPath.gapBands.none { it.layer == 1 }) {
            throw IllegalStateException("当前工程没有 1H 间隙带")
        }
        val startSafeIdx = weldPath.points.indexOfFirst { it.type == WeldPointType.START_SAFE }
        val startIdx = weldPath.points.indexOfFirst { it.type == WeldPointType.START }
        val endIdx = weldPath.points.indexOfFirst { it.type == WeldPointType.END }
        val endSafeIdx = weldPath.points.indexOfFirst { it.type == WeldPointType.END_SAFE }
        fun scriptPt(pt: WeldPoint) = ScriptPoint(pt.type, pt.pose!!, pt.jointAngles.orEmpty())
        val lines = TBarLua.job(
            listOf(
                TBarScriptPath(
                    startSafe = scriptPt(startSafe),
                    endSafe = scriptPt(endSafe),
                    aLower = aL,
                    bLower = bL,
                    aUpper = aU,
                    bUpper = bU,
                    startPose = startPose,
                    endPose = endPose,
                    bands = weldPath.gapBands,
                    enabled = true,
                )
            ),
            boundProcesses.mapKeys { it.key.toString() },
            welding = isWelding,
            simulating = isSimulating,
            speedMode = speedMode,
            toolIndex = toolIndex,
            extAxis = isExtAxisEnabled,
        )
        // 点位回填按老项目：只有 MoveL 挂可映射的指令号，求逆解那行不挂号
        lines.forEach { line ->
            if (!line.withId) {
                builder.appendCmd(line.text)
                return@forEach
            }
            val id = globalCommandId++
            val pointIdx = when (line.point) {
                TBarPoint.START_SAFE -> startSafeIdx
                TBarPoint.START -> startIdx
                TBarPoint.END -> endIdx
                TBarPoint.END_SAFE -> endSafeIdx
                null -> null
            }
            if (pointIdx != null) commandIdMap[id] = Triple(pathIndexForMap, pointIdx, -1)
            builder.appendCmd(line.text, id)
        }
    }

    // --- Batch MoveL Command ---

    override fun sendBatchMoveLCommands(resumePathIndex: Int, resumePointIndex: Int, isResumeFromStop: Boolean) {
        stopControllerActive()
        if (weldPaths.isEmpty()) return

        if (!bindPouchProcesses(weldPaths)) return

        val toolIndex = try {
            toolCoordinateSystem.removePrefix("工具").toInt()
        } catch (e: Exception) { 1 }

        executionQueue.clear()
        commandIdMap.clear()
        commandLineMap.clear()

        val batchBuilder = BatchCommandBuilder("All Paths")

        weldPaths.forEachIndexed { pathIndex, weldPath ->
            if (!weldPath.isEnabled) return@forEachIndexed
            if (resumePathIndex != -1 && pathIndex < resumePathIndex) return@forEachIndexed
            try {
                appendTBarCommandsForPath(weldPath, batchBuilder, pathIndex, toolIndex)
            } catch (e: Exception) {
                missingProcessMessage = "焊道 ${weldPath.name}: ${e.message}"
                isMissingProcessDialogVisible = true
                return
            }
        }

        batchBuilder.flush()
        startBatchExecution()
    }

    override fun startSimulation() {
        if (isWelding || isSimulating) return
        isSimulating = true
        programHasStarted = false
        fineTune.resetOffsets()
        
        // Reset Tracking
        lastReachedPoint = null
        lastReachedPointIndex = -1
        lastReachedPathIndex = -1
        
        // Clear Stop Point records to ensure it starts from the beginning
        stopPointPose = null
        stopPointJoints = null
        stopPointPathIndex = -1
        stopPointIndex = -1
        markPouchWelding(true)
        
        sendBatchMoveLCommands()
    }

    override fun startArcWelding() {
        if (isWelding || isSimulating) return
        isWelding = true
        isWeldingStatsActive = false // Ensure stats are OFF initially
        programHasStarted = false
        fineTune.resetOffsets()
        
        // Reset Tracking
        lastReachedPoint = null
        lastReachedPointIndex = -1
        lastReachedPathIndex = -1
        
        // Clear Stop Point records to ensure it starts from the beginning
        stopPointPose = null
        stopPointJoints = null
        stopPointPathIndex = -1
        stopPointIndex = -1
        
        // Initialize Start Point (assume first non-safe point is where we start, or wait for feedback)
        // We wait for feedback to be safe, but we can try to find the logical start.
        // Actually, the feedback logic handles "lastReachedPoint == null" by just setting it.
        // So the first segment (Start -> Next) will be calculated when Next is reached.
        // But what about distance 0 (Start -> Start)? Handled correctly (0 length).

        // Start Timer Job but DO NOT increment yet
        // The increment will happen only when isWeldingStatsActive becomes true
        weldingTimerJob?.cancel()
        weldingTimerJob = viewModelScope.launch {
            var timerRunning = false
            var lastTick = 0L
            while (isActive && isWelding) {
                delay(50) // Check frequently for responsiveness
                if (isWeldingStatsActive) {
                    if (!timerRunning) {
                         timerRunning = true
                         lastTick = System.currentTimeMillis()
                    }
                    val now = System.currentTimeMillis()
                    if (now - lastTick >= 1000) {
                        weldingDuration += 1
                        lastTick += 1000 // Keep alignment to avoid drift
                    }
                } else {
                    timerRunning = false
                }
            }
        }

        markPouchWelding(true)
        sendBatchMoveLCommands()
    }


    // --- Pause/Resume Logic ---

    protected override fun saveAppSettings() {
        val index = try {
            toolCoordinateSystem.removePrefix("工具").toInt() - 1
        } catch (e: Exception) { 0 }
        
        val settings = AppSettings(
            selectedToolIndex = index,
            toolCoordinates = toolCoordinates.toList(),
            toolRemarks = toolRemarks.toList(),
            positionMode = positionMode,
            speedMode = speedMode,
            lastOpenedProjectPath = projectManager.loadAppSettings().lastOpenedProjectPath,
            totalWeldingLength = weldingLength,
            totalWeldingDuration = weldingDuration,
            isRegistered = isRegistered,
            lastConnectionTime = lastConnectionTime,
            installPos = installPos,
            weldingCurrent = savedCurrent,
            weldingVoltage = savedVoltage,
            isExtAxisEnabled = isExtAxisEnabled
        )
        projectManager.saveAppSettings(settings)
    }

    // --- Update Methods ---

    override fun onRunFinished() = Unit

    /** T 排工程独立目录，不自动打开单层最近工程。 */
    override fun onInitialLoad() {
        if (weldPaths.isEmpty()) addWeldPath()
    }
}
