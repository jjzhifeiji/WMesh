package com.gbndt.shijiaoqi.data.pouch

import com.gbndt.shijiaoqi.model.multilayer.MultiLayerWeldPath
import com.gbndt.shijiaoqi.model.single.WeldPath
import com.gbndt.shijiaoqi.model.WeldProcess
import com.gbndt.shijiaoqi.data.crypt.Wm2
import com.gbndt.shijiaoqi.data.session.BagSession
import java.util.UUID
import com.gbndt.shijiaoqi.domain.shared.ProcessJson

/** 把内存工艺/工程写回袋信封；失败则保持内存，不落明文文件。 */
object PouchSave {
    fun process(bag: BagSession, processId: String, process: WeldProcess) {
        if (processId.isBlank()) return
        val id = runCatching { UUID.fromString(processId.trim()) }.getOrNull() ?: return
        val body = ProcessJson.encode(process)
        try {
            bag.rewritePlain(id, body)
        } catch (_: Exception) {
        } finally {
            Wm2.zero(body)
        }
    }

    fun weldPath(bag: BagSession, path: WeldPath) {
        process(bag, path.processId, path.process)
        path.extraProcesses.forEach { process(bag, it.processId, it.process) }
    }

    fun multi(bag: BagSession, path: MultiLayerWeldPath) {
        weldPath(bag, path.basePath)
        path.passes.forEach { process(bag, it.processId, it.process) }
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
