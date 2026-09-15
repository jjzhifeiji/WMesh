package com.gbndt.shijiaoqi.data.repository

import com.gbndt.shijiaoqi.data.crypt.Wm2
import com.gbndt.shijiaoqi.data.pouch.Pouch
import com.gbndt.shijiaoqi.data.pouch.PouchProcessSource
import com.gbndt.shijiaoqi.data.pouch.PouchSave
import com.gbndt.shijiaoqi.data.session.BagSession
import com.gbndt.shijiaoqi.domain.weld.ProcessChoice
import com.gbndt.shijiaoqi.domain.weld.ProcessSource
import com.gbndt.shijiaoqi.domain.weld.ProjectChoice
import com.gbndt.shijiaoqi.model.MultiLayerWeldPath
import com.gbndt.shijiaoqi.model.WeldPath
import java.util.UUID
import javax.inject.Inject
import javax.inject.Singleton

/** 本机袋对界面的唯一出口：明文只在回调里活着，出了这层就被抹掉。 */
@Singleton
class PouchRepository @Inject constructor(
    private val session: BagSession,
) {
    /** 按 processId 取工艺参数；界面不碰袋本身。 */
    fun processSource(): ProcessSource = PouchProcessSource(session.pouch)

    fun listProcesses(): List<ProcessChoice> = PouchProcessSource(session.pouch).list()

    /** 只列发到本机的工程，顺带标出当前激活的那一份。 */
    fun listProjects(): List<ProjectChoice> {
        val pouch = session.pouch
        val active = pouch.activeProject()
        val self = pouch.boundClient() ?: return emptyList()
        return pouch.exportClosures()
            .filter { it.kind == Pouch.KIND_PROJECT && it.targetClientId == self }
            .map { ProjectChoice(it.assetId, it.name, it.revision, it.assetId == active) }
    }

    fun activeProjectId(): UUID? = session.pouch.activeProject()

    fun projectName(id: UUID): String? =
        session.pouch.exportClosures().firstOrNull { it.assetId == id }?.name

    /** 打开工程正文：明文只在 block 里可见，返回前一定抹掉。 */
    fun <T> withProjectPlain(id: UUID, block: (ByteArray) -> T): T? {
        val bytes = try {
            session.open(id)
        } catch (_: Exception) {
            return null
        }
        return try {
            block(bytes)
        } finally {
            Wm2.zero(bytes)
        }
    }

    fun activate(id: UUID) = session.activate(id)

    /** 焊接中不许切换缓存，靠这个标记挡住。 */
    fun setWelding(on: Boolean) {
        runCatching { session.setWelding(on) }
    }

    /** 本机新建工艺：发个人级编号并入袋，明文用完抹掉。 */
    fun issueProcess(name: String, plain: ByteArray) {
        try {
            session.issuePersonal(Pouch.KIND_PROCESS, name, plain)
        } finally {
            Wm2.zero(plain)
        }
    }

    fun saveWeldPath(path: WeldPath) = PouchSave.weldPath(session, path)

    fun saveMultiLayerPath(path: MultiLayerWeldPath) = PouchSave.multi(session, path)

    fun saveProject(id: UUID, plain: ByteArray) = PouchSave.project(session, id, plain)
}
