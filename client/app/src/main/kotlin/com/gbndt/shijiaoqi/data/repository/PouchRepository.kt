package com.gbndt.shijiaoqi.data.repository

import com.gbndt.shijiaoqi.data.crypt.Wm2
import com.gbndt.shijiaoqi.data.pouch.Pouch
import com.gbndt.shijiaoqi.data.pouch.PouchProcessSource
import com.gbndt.shijiaoqi.data.pouch.PouchSave
import com.gbndt.shijiaoqi.data.session.BagSession
import com.gbndt.shijiaoqi.data.session.LoginRejected
import com.gbndt.shijiaoqi.data.session.SessionGate
import com.gbndt.shijiaoqi.di.ApplicationScope
import com.gbndt.shijiaoqi.di.IoDispatcher
import com.gbndt.shijiaoqi.domain.shared.ProcessBind
import com.gbndt.shijiaoqi.domain.shared.ProcessChoice
import com.gbndt.shijiaoqi.domain.shared.ProcessJson
import com.gbndt.shijiaoqi.domain.shared.ProcessRef
import com.gbndt.shijiaoqi.domain.shared.ProcessSource
import com.gbndt.shijiaoqi.domain.shared.ProjectChoice
import com.gbndt.shijiaoqi.model.WeldProcess
import com.gbndt.shijiaoqi.model.multilayer.MultiLayerWeldPath
import com.gbndt.shijiaoqi.model.single.WeldPath
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import java.util.UUID
import javax.inject.Inject
import javax.inject.Singleton

/** 本机袋对界面的唯一出口：读走 Flow，写走挂起，线程在 IO 收敛。 */
@Singleton
class PouchRepository @Inject constructor(
    private val session: BagSession,
    private val gate: SessionGate,
    @param:IoDispatcher private val io: CoroutineDispatcher,
    @param:ApplicationScope private val scope: CoroutineScope,
) {
    private val flushMutex = Mutex()
    private var flushJob: Job? = null
    private val _projects = MutableStateFlow<List<ProjectChoice>>(emptyList())
    private val _processes = MutableStateFlow<List<ProcessChoice>>(emptyList())

    /** 袋内工程列表，落盘/激活后推送。 */
    val projects: StateFlow<List<ProjectChoice>> = _projects.asStateFlow()

    /** 袋内工艺列表，落盘后推送。 */
    val processes: StateFlow<List<ProcessChoice>> = _processes.asStateFlow()

    fun activeProjectId(): UUID? = _projects.value.firstOrNull { it.active }?.id

    fun projectName(id: UUID): String? = _projects.value.firstOrNull { it.id == id }?.name

    /** 重新快照内存袋，不碰库。 */
    suspend fun refresh() = ioLocked(emitCatalog = true) { }

    /** 打开工程正文副本；调用方用完必须抹掉。 */
    suspend fun openProjectBytes(id: UUID): ByteArray? = ioLocked(emitCatalog = false) {
        try {
            session.open(id)
        } catch (_: Exception) {
            null
        }
    }

    /** 出库解开一条工艺；没有则空。 */
    suspend fun openProcess(id: UUID): WeldProcess? = ioLocked(emitCatalog = false) {
        processSourceLocked().open(id)
    }

    /** 按焊道引用批量出库；只在 IO 上解信封。 */
    suspend fun resolveProcesses(refs: List<ProcessRef>): ProcessBind.Outcome =
        ioLocked(emitCatalog = false) { ProcessBind.resolve(refs, processSourceLocked()) }

    /** 激活工程并落库。 */
    suspend fun activate(id: UUID) = ioLocked { session.activate(id) }

    /** 作业前本地核本厂设备号；失败返回中文原因。 */
    suspend fun factoryArmError(): String? = ioLocked(emitCatalog = false) {
        try {
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
    suspend fun setWelding(on: Boolean) = ioLocked(emitCatalog = false) {
        runCatching { session.setWelding(on) }
        Unit
    }

    /** 本机新建工艺：发个人级编号并入袋，明文用完抹掉。 */
    suspend fun issueProcess(name: String, plain: ByteArray) = ioLocked {
        try {
            session.issuePersonal(Pouch.KIND_PROCESS, name, plain)
        } finally {
            Wm2.zero(plain)
        }
    }

    /** 可复制工艺改正文：厂级/平台级另存个人级；保密拒绝。 */
    suspend fun saveProcess(id: UUID?, process: WeldProcess): UUID = ioLocked {
        val body = ProcessJson.encode(process)
        try {
            if (id == null) {
                session.issuePersonal(Pouch.KIND_PROCESS, process.name, body).id
            } else {
                session.ensurePersonalProcess(id, process.name, body)
            }
        } finally {
            Wm2.zero(body)
        }
    }

    suspend fun saveWeldPath(path: WeldPath) = ioLocked { PouchSave.weldPath(session, path) }

    suspend fun saveWeldPaths(paths: List<WeldPath>) = ioLocked {
        paths.forEach { PouchSave.weldPath(session, it) }
    }

    suspend fun saveMultiLayerPath(path: MultiLayerWeldPath) = ioLocked { PouchSave.multi(session, path) }

    suspend fun saveMultiLayerPaths(paths: List<MultiLayerWeldPath>) = ioLocked {
        paths.forEach { PouchSave.multi(session, it) }
    }

    suspend fun saveProject(id: UUID, plain: ByteArray) = ioLocked { PouchSave.project(session, id, plain) }

    private suspend fun <T> ioLocked(emitCatalog: Boolean = true, block: () -> T): T =
        gate.withLock {
            withContext(io) {
                val result = block()
                if (emitCatalog) {
                    emitCatalog()
                    scheduleFlush()
                }
                result
            }
        }

    private fun emitCatalog() {
        val pouch = session.pouch
        val active = pouch.activeProject()
        _projects.value = pouch.exportClosures()
            .filter { it.kind == Pouch.KIND_PROJECT }
            .map { ProjectChoice(it.assetId, it.name, it.revision, it.assetId == active) }
        _processes.value = pouch.listCachedProcesses().map {
            ProcessChoice(it.id, it.name, it.copyable, pouch.isDirty(it.id), it.level)
        }
    }

    private fun scheduleFlush() {
        flushJob?.cancel()
        flushJob = scope.launch {
            delay(750)
            flushMutex.withLock { runCatching { session.flushDirty() } }
        }
    }

    private fun processSourceLocked(): ProcessSource = PouchProcessSource(
        openBytes = { id ->
            runCatching { session.openProcess(id) }.getOrNull()
                ?: runCatching { session.open(id) }.getOrNull()
        },
        listFn = { _processes.value },
    )
}
