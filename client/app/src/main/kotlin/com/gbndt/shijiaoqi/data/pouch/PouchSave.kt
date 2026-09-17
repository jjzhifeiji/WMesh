package com.gbndt.shijiaoqi.data.pouch

import com.gbndt.shijiaoqi.model.multilayer.MultiLayerWeldPath
import com.gbndt.shijiaoqi.model.single.WeldPath
import com.gbndt.shijiaoqi.model.WeldProcess
import com.gbndt.shijiaoqi.data.crypt.Wm2
import com.gbndt.shijiaoqi.data.session.BagSession
import java.util.UUID
import com.gbndt.shijiaoqi.domain.shared.ProcessJson

/** 焊道只写工程；可复制工艺另存个人级，保密工艺不改正文。 */
object PouchSave {
    fun persistProcess(bag: BagSession, processId: String, process: WeldProcess): String {
        if (processId.isBlank()) return processId
        val id = runCatching { UUID.fromString(processId.trim()) }.getOrNull() ?: return processId
        if (!bag.pouch.copyableOf(id)) return processId
        val body = ProcessJson.encode(process)
        return try {
            bag.ensurePersonalProcess(id, process.name, body).toString()
        } catch (_: Exception) {
            processId
        } finally {
            Wm2.zero(body)
        }
    }

    fun weldPath(bag: BagSession, path: WeldPath) {
        path.processId = persistProcess(bag, path.processId, path.process)
        path.extraProcesses.forEach { slot ->
            slot.processId = persistProcess(bag, slot.processId, slot.process)
        }
        path.gapBands = path.gapBands.map { band ->
            band.copy(
                rootProcessId = persistProcess(bag, band.rootProcessId, band.rootProcess),
                capProcessId = persistProcess(bag, band.capProcessId, band.capProcess),
            )
        }
        path.cornerGroupParams?.let { g ->
            if (g.processId.isNotBlank()) {
                path.cornerGroupParams = g.copy(processId = persistProcess(bag, g.processId, path.process))
            }
        }
    }

    fun multi(bag: BagSession, path: MultiLayerWeldPath) {
        weldPath(bag, path.basePath)
        path.passes.forEach { pass ->
            pass.processId = persistProcess(bag, pass.processId, pass.process)
        }
    }

    fun project(bag: BagSession, id: UUID, bytes: ByteArray) {
        try {
            bag.rewritePlain(id, bytes)
        } catch (_: Exception) {
        } finally {
            Wm2.zero(bytes)
        }
    }
}
