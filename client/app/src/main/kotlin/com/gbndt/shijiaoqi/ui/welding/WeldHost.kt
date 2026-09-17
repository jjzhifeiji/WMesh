package com.gbndt.shijiaoqi.ui.welding

import com.gbndt.shijiaoqi.domain.shared.ProcessChoice
import com.gbndt.shijiaoqi.domain.shared.ProjectChoice
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.StateFlow
import java.util.UUID

/** 手柄录点与开焊：Activity 只把键转给当前在屏的这一份。 */
interface WeldPad {
    fun collectData()
    fun sendMoveLCommand()
    fun startSimulation()
    fun startArcWelding()
    fun stopWelding(force: Boolean = false)
}

/** 三个模式共用的外壳：袋里工程/工艺、状态栏焊态、一次性提示。 */
interface WeldShellHost {
    val toastEvent: SharedFlow<String>
    val shellUi: StateFlow<WeldShellUi>
    /** 从袋里把当前工程拉到焊道屏。 */
    suspend fun syncFromPouch()
    fun refreshPouchLists()
    fun activatePouchProject(id: UUID)
    fun bindProcessFromPouch(processId: UUID?)
    /** 取消挑工艺；多层没有附加工艺槽，默认什么都不做。 */
    fun cancelAddProcessVariant() {}
}
