package com.gbndt.shijiaoqi.domain.weld

import com.gbndt.shijiaoqi.model.WeldProcess
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import java.util.UUID

/** 按稳定身份从内存取工艺参数；不解文件路径。 */
fun interface ProcessSource {
    fun open(processId: UUID): WeldProcess?
}

data class ProcessChoice(
    val id: UUID,
    val name: String,
)

data class ProjectChoice(
    val id: UUID,
    val name: String,
    val revision: Long,
    val active: Boolean,
)

/** 把工艺 JSON 解成参数对象；身份不在这份 JSON 里。 */
object ProcessJson {
    private val json = Json {
        ignoreUnknownKeys = true
        isLenient = true
        coerceInputValues = true
    }

    fun decode(bytes: ByteArray): WeldProcess =
        json.decodeFromString(WeldProcess.serializer(), bytes.decodeToString())

    fun encode(process: WeldProcess): ByteArray =
        json.encodeToString(WeldProcess.serializer(), process).encodeToByteArray()
}

/** 焊道上的工艺引用；空 processId 表示未选。 */
data class ProcessRef(
    val label: String,
    val processId: String,
    val enabled: Boolean = true,
)

/** 按 processId 解工艺；空引用放行，袋内没有则拒。 */
object ProcessBind {
    data class Outcome(
        val missing: List<String>,
        val loaded: Map<UUID, WeldProcess>,
    )

    fun resolve(refs: List<ProcessRef>, source: ProcessSource): Outcome {
        val missing = mutableListOf<String>()
        val loaded = LinkedHashMap<UUID, WeldProcess>()
        for (ref in refs) {
            if (!ref.enabled || ref.processId.isBlank()) continue
            val id = try {
                UUID.fromString(ref.processId.trim())
            } catch (_: Exception) {
                missing.add("${ref.label}: ${ref.processId}")
                continue
            }
            if (id in loaded) continue
            val process = source.open(id)
            if (process == null) {
                missing.add("${ref.label}: ${ref.processId}")
            } else {
                loaded[id] = process
            }
        }
        return Outcome(missing, loaded)
    }
}

/** 工程正文里非空工艺引用；空引用不进 deps。 */
object ProjectRefs {
    fun idsOf(path: com.gbndt.shijiaoqi.model.WeldPath): List<String> {
        val out = ArrayList<String>()
        add(out, path.processId)
        path.extraProcesses.forEach { add(out, it.processId) }
        path.gapBands.forEach {
            add(out, it.rootProcessId)
            add(out, it.capProcessId)
        }
        path.cornerGroupParams?.processId?.let { add(out, it) }
        return out
    }

    fun missing(ids: List<String>, allowed: Set<UUID>): List<String> {
        val bad = ArrayList<String>()
        for (raw in ids) {
            val id = try {
                UUID.fromString(raw.trim())
            } catch (_: Exception) {
                bad.add(raw)
                continue
            }
            if (id !in allowed) bad.add(raw)
        }
        return bad
    }

    private fun add(out: MutableList<String>, raw: String) {
        if (raw.isNotBlank()) out.add(raw)
    }
}
