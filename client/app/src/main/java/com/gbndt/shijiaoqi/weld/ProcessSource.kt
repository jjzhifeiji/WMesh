package com.gbndt.shijiaoqi.weld

import com.gbndt.shijiaoqi.data.models.WeldProcess
import com.gbndt.shijiaoqi.platform.crypt.Wm2
import com.gbndt.shijiaoqi.platform.pouch.CachedMember
import com.gbndt.shijiaoqi.platform.pouch.Pouch
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
}

/** 只从当前激活闭包的工艺成员解参数，用完清明文。 */
class PouchProcessSource(private val pouch: Pouch) : ProcessSource {
    override fun open(processId: UUID): WeldProcess? {
        val bytes = try {
            pouch.openProcess(processId)
        } catch (_: Exception) {
            return null
        }
        return try {
            ProcessJson.decode(bytes)
        } catch (_: Exception) {
            null
        } finally {
            Wm2.zero(bytes)
        }
    }

    fun list(): List<ProcessChoice> =
        pouch.listProcessMembers().map { ProcessChoice(it.id, it.name) }

    fun members(): List<CachedMember> = pouch.listProcessMembers()
}

/** 焊道上的工艺引用；空 processId 表示未选。 */
data class ProcessRef(
    val label: String,
    val processId: String,
    val enabled: Boolean = true,
)

/** 按 processId 解工艺；空引用放行，袋内没有则拒。不读 processPath。 */
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
