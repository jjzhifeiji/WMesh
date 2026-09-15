package com.gbndt.shijiaoqi.robot

import com.gbndt.shijiaoqi.data.models.Pose
import java.nio.ByteBuffer
import java.nio.ByteOrder

/** 8083 二进制状态；帧头 `0x5A 0x5A`，长度 ≥104。 */
data class Status8083(
    val programState: Int,
    val alarmCode: Int,
    val alarmText: String,
    val joints: List<Double>,
    val pose: Pose,
    val progTotalLine: Int,
    val progCurLine: Int,
    val weldCurrent: Double,
    val weldVoltage: Double,
    val inputSignal: Int,
    val weldingBreakOff: Boolean,
    val arcBreakOff: Boolean,
    val extAxisPos: Double,
    val extAxisReady: Boolean,
) {
    companion object {
        const val HEADER0: Byte = 0x5A
        const val HEADER1: Byte = 0x5A
        const val MIN_LEN = 104

        fun parse(data: ByteArray, length: Int = data.size): Status8083? {
            if (length < MIN_LEN) return null
            if (data[0] != HEADER0 || data[1] != HEADER1) return null
            val alarmCode = data[6].toInt() and 0xFF
            val buf = ByteBuffer.wrap(data, 8, 96).order(ByteOrder.LITTLE_ENDIAN)
            val doubles = DoubleArray(12)
            for (i in 0 until 12) doubles[i] = buf.double
            val joints = doubles.slice(0..5)
            var inputSignal = 0
            if (length > 388) {
                val high = data[388].toInt() and 0xFF
                val low = data[387].toInt() and 0xFF
                inputSignal = (high shl 8) or low
            }
            var breakOff = false
            var arcOff = false
            if (length >= 427) {
                breakOff = (data[425].toInt() and 0xFF) == 1
                arcOff = (data[426].toInt() and 0xFF) == 1
            }
            var programState = 0
            var total = 0
            var cur = 0
            if (length > 177) {
                programState = data[5].toInt() and 0xFF
                total = data[176].toInt() and 0xFF
                cur = data[177].toInt() and 0xFF
            }
            var current = 0.0
            var voltage = 0.0
            if (length >= 387) {
                val ai0 = (data[383].toInt() and 0xFF) or ((data[384].toInt() and 0xFF) shl 8)
                val ai1 = (data[385].toInt() and 0xFF) or ((data[386].toInt() and 0xFF) shl 8)
                current = ai0 / 4095.0 * 500.0
                voltage = ai1 / 4095.0 * 50.0
            }
            var extPos = 0.0
            var extReady = false
            if (length >= 381) {
                val ext = ByteBuffer.wrap(data, 265, 29).order(ByteOrder.LITTLE_ENDIAN)
                extPos = ext.double
                ext.double
                ext.int
                extReady = (ext.get().toInt() and 0xFF) == 1
            }
            return Status8083(
                programState = programState,
                alarmCode = alarmCode,
                alarmText = alarmText(alarmCode),
                joints = joints,
                pose = Pose(doubles[6], doubles[7], doubles[8], doubles[9], doubles[10], doubles[11], extPos),
                progTotalLine = total,
                progCurLine = cur,
                weldCurrent = current,
                weldVoltage = voltage,
                inputSignal = inputSignal,
                weldingBreakOff = breakOff,
                arcBreakOff = arcOff,
                extAxisPos = extPos,
                extAxisReady = extReady,
            )
        }

        fun alarmText(code: Int): String = when (code) {
            0x00 -> "无故障"
            0x01 -> "驱动器故障"
            0x02 -> "超出软限位故障"
            0x03 -> "碰撞故障"
            0x04 -> "奇异位姿"
            0x05 -> "从站错误"
            0x06 -> "指令点错误"
            0x07 -> "IO错误"
            0x08 -> "夹爪错误"
            0x09 -> "文件错误"
            0x0a -> "参数错误"
            0x0b -> "扩展轴超出软限位错误"
            else -> "未知错误($code)"
        }
    }
}
