package com.gbndt.shijiaoqi.data.pouch

import com.gbndt.shijiaoqi.data.crypt.Wm2
import com.gbndt.shijiaoqi.domain.weld.ProcessChoice
import com.gbndt.shijiaoqi.domain.weld.ProcessJson
import com.gbndt.shijiaoqi.domain.weld.ProcessSource
import com.gbndt.shijiaoqi.model.WeldProcess
import java.util.UUID

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
