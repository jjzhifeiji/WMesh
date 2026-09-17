package com.gbndt.shijiaoqi.data.pouch

import com.gbndt.shijiaoqi.data.crypt.Wm2
import com.gbndt.shijiaoqi.domain.shared.ProcessChoice
import com.gbndt.shijiaoqi.domain.shared.ProcessJson
import com.gbndt.shijiaoqi.domain.shared.ProcessSource
import com.gbndt.shijiaoqi.model.WeldProcess
import java.util.UUID

/** 出库解开工艺信封，解码成参数对象后抹掉明文。 */
class PouchProcessSource(
    private val openBytes: (UUID) -> ByteArray?,
    private val listFn: () -> List<ProcessChoice> = { emptyList() },
) : ProcessSource {
    constructor(pouch: Pouch) : this(openFrom(pouch), listFrom(pouch))

    companion object {
        private fun openFrom(pouch: Pouch): (UUID) -> ByteArray? = { id ->
            runCatching { pouch.openProcess(id) }.getOrNull()
                ?: runCatching { pouch.open(id) }.getOrNull()
        }

        private fun listFrom(pouch: Pouch): () -> List<ProcessChoice> = {
            pouch.listCachedProcesses().map {
                ProcessChoice(it.id, it.name, it.copyable, pouch.isDirty(it.id), it.level)
            }
        }
    }

    override fun open(processId: UUID): WeldProcess? {
        val bytes = openBytes(processId) ?: return null
        return try {
            ProcessJson.decode(bytes)
        } catch (_: Exception) {
            null
        } finally {
            Wm2.zero(bytes)
        }
    }

    fun list(): List<ProcessChoice> = listFn()
}
