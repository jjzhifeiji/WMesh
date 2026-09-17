package com.gbndt.shijiaoqi.data.repository

import com.gbndt.shijiaoqi.data.crypt.Wm2
import com.gbndt.shijiaoqi.data.pouch.Pouch
import com.gbndt.shijiaoqi.data.pouch.PouchProcessSource
import com.gbndt.shijiaoqi.data.pouch.PouchSave
import com.gbndt.shijiaoqi.data.session.BagSession
import com.gbndt.shijiaoqi.data.session.LoginRejected
import com.gbndt.shijiaoqi.domain.shared.ProcessChoice
import com.gbndt.shijiaoqi.domain.shared.ProcessSource
import com.gbndt.shijiaoqi.domain.shared.ProjectChoice
import com.gbndt.shijiaoqi.model.multilayer.MultiLayerWeldPath
import com.gbndt.shijiaoqi.model.single.WeldPath
import java.util.UUID
import javax.inject.Inject
import javax.inject.Singleton

/** 本机袋对界面的唯一出口：工艺出库解开后合到焊道，字节用完即抹。 */
@Singleton
class PouchRepository @Inject constructor(
    private val session: BagSession,
) {
    /** 从库取出工艺信封解开；调用方把对象合到焊道。 */
    fun processSource(): ProcessSource = PouchProcessSource(
        openBytes = { id ->
            runCatching { session.openProcess(id) }.getOrNull()
                ?: runCatching { session.open(id) }.getOrNull()
        },
        listFn = { session.pouch.listCachedProcesses().map { ProcessChoice(it.id, it.name) } },
    )

    fun listProcesses(): List<ProcessChoice> =
        session.pouch.listCachedProcesses().map { ProcessChoice(it.id, it.name) }

    /** 列本机已缓存工程，与厂端工程列表对齐。 */
    fun listProjects(): List<ProjectChoice> {
        val pouch = session.pouch
        val active = pouch.activeProject()
        return pouch.exportClosures()
            .filter { it.kind == Pouch.KIND_PROJECT }
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

    /** 作业前本地核本厂设备号；失败返回中文原因。 */
    fun factoryArmError(): String? {
        return try {
            session.ensureFactoryArm()
            null
        } catch (e: LoginRejected) {
            when (e.code) {
                "device serial is required" -> "读不到设备号"
                "device serial does not match" -> "设备号未在本厂登记"
                else -> e.code
            }
        }
    }

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
