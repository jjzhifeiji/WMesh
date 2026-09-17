package com.gbndt.shijiaoqi.domain.shared

import com.gbndt.shijiaoqi.model.Oscillation
import com.gbndt.shijiaoqi.model.WeldProcess
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.jsonObject
import java.util.UUID

/** 按 processId 出库解开工艺；合到焊道后给界面和 Lua。 */
fun interface ProcessSource {
    fun open(processId: UUID): WeldProcess?
}

data class ProcessChoice(
    val id: UUID,
    val name: String,
    val copyable: Boolean = true, // 可否查看/另存；否即保密
    val dirty: Boolean = false, // 本机已改未同步
    val level: String = "", // factory / personal / platform
)

data class ProjectChoice(
    val id: UUID,
    val name: String,
    val revision: Long,
    val active: Boolean,
    val level: String = "", // factory / personal / platform
)

/** 把工艺 JSON 解成参数对象；身份不在这份 JSON 里。 */
object ProcessJson {
    private val json = Json {
        ignoreUnknownKeys = true
        isLenient = true
        coerceInputValues = true
    }

    fun decode(bytes: ByteArray): WeldProcess {
        val obj = json.parseToJsonElement(bytes.decodeToString()).jsonObject
        return WeldProcess(
            name = obj.str("name", "默认工艺"),
            offsetX = obj.str("offsetX", "0"),
            offsetY = obj.str("offsetY", "0"),
            offsetZ = obj.str("offsetZ", "0"),
            current = obj.num("current", 170.0),
            voltage = obj.num("voltage", 20.0),
            speed = obj.num("speed", 10.0),
            startArcTime = obj.num("startArcTime", 400.0),
            endArcTime = obj.num("endArcTime", 400.0),
            startArcCurrent = obj.num("startArcCurrent", 180.0),
            endArcCurrent = obj.num("endArcCurrent", 160.0),
            startArcVoltage = obj.num("startArcVoltage", 20.0),
            endArcVoltage = obj.num("endArcVoltage", 20.0),
            oscillation = oscOf(obj["oscillation"]),
        )
    }

    fun encode(process: WeldProcess): ByteArray =
        json.encodeToString(WeldProcess.serializer(), process).encodeToByteArray()

    private fun oscOf(raw: JsonElement?): Oscillation {
        val obj = raw as? JsonObject ?: return Oscillation()
        return Oscillation(
            type = obj.str("type", "无摆动"),
            waitTime = obj.str("waitTime", "不包括"),
            positionWait = obj.str("positionWait", "等待时间内位置继续移动"),
            frequency = obj.num("frequency", 5.0),
            amplitude = obj.num("amplitude", 1.0),
            leftStopTime = obj.num("leftStopTime", 100.0),
            rightStopTime = obj.num("rightStopTime", 100.0),
            leftSideLength = obj.num("leftSideLength", 1.0),
            rightSideLength = obj.num("rightSideLength", 1.0),
            zeroTime = obj.num("zeroTime", 20.0),
            callbackRatio = obj.num("callbackRatio", 10.0),
            azimuth = obj.num("azimuth", 0.0),
            inclination = obj.num("inclination", 0.0),
        )
    }

    private fun JsonObject.str(key: String, def: String): String {
        val p = this[key] as? JsonPrimitive ?: return def
        return p.content.ifBlank { def }
    }

    private fun JsonObject.num(key: String, def: Double): Double {
        val p = this[key] as? JsonPrimitive ?: return def
        return p.content.toDoubleOrNull() ?: def
    }
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
    fun idsOf(path: com.gbndt.shijiaoqi.model.single.WeldPath): List<String> {
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
