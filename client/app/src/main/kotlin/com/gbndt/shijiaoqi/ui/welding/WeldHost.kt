package com.gbndt.shijiaoqi.ui.welding

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
    suspend fun loadProcessFromPouch(id: UUID): com.gbndt.shijiaoqi.model.WeldProcess? = null
    fun saveProcessFromPouch(id: UUID?, process: com.gbndt.shijiaoqi.model.WeldProcess) {}
    fun createPouchProject(name: String) {}
    fun deletePouchProject(id: UUID) {}
    fun deleteProcessFromPouch(id: UUID) {}
}

/** 袋操作失败给操作工看的短句。 */
fun pouchUserMessage(e: Throwable): String = when (e.message) {
    "asset origin code is not assigned" -> "本机短号未就绪"
    "asset is not copyable" -> "保密工艺不能改"
    "forbidden" -> "不能删除当前工程或非个人级"
    "still referenced" -> "厂端工程还在用这份工艺"
    else -> e.message ?: "操作失败"
}
