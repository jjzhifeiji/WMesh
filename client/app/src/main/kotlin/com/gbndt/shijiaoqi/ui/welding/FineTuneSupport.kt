package com.gbndt.shijiaoqi.ui.welding

import android.util.Log
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import com.gbndt.shijiaoqi.data.repository.RobotRepository
import com.gbndt.shijiaoqi.model.Oscillation
import com.gbndt.shijiaoqi.model.WeldProcess
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch
import java.util.Locale

class FineTuneSupport(
    scope: CoroutineScope,
    private val socketManager: RobotRepository,
    private val nextCommandId: () -> Int,
    private val toast: (String) -> Unit,
    private val currentProcess: () -> WeldProcess?,
    private val onOscillationChanged: (Oscillation) -> Unit
) {
    var isDialogVisible by mutableStateOf(false)

    var liveCurrent by mutableStateOf(0.0)
        private set
    var liveVoltage by mutableStateOf(0.0)
        private set

    var setCurrent by mutableStateOf(0.0)
        private set
    var setVoltage by mutableStateOf(0.0)
        private set

    var speedPercent by mutableStateOf(10)
        private set
    var offsetStepMm by mutableStateOf(1.0)
    var weaveOffsetX by mutableStateOf(0.0)
        private set
    var weaveOffsetY by mutableStateOf(0.0)
        private set
    var weaveOffsetZ by mutableStateOf(0.0)
        private set

    var amplitude by mutableStateOf(1.0)
        private set
    var frequency by mutableStateOf(5.0)
        private set
    var oscillationType by mutableStateOf("无摆动")
        private set

    init {
        scope.launch {
            socketManager.weldCurrent.collect { liveCurrent = it }
        }
        scope.launch {
            socketManager.weldVoltage.collect { liveVoltage = it }
        }
    }

    fun open() {
        val process = currentProcess()
        val osc = process?.oscillation ?: Oscillation()
        amplitude = osc.amplitude
        frequency = osc.frequency
        oscillationType = osc.type
        setCurrent = if (liveCurrent > 1.0) liveCurrent else (process?.current ?: 0.0)
        setVoltage = if (liveVoltage > 0.5) liveVoltage else (process?.voltage ?: 0.0)
        isDialogVisible = true
    }

    fun close() {
        isDialogVisible = false
    }

    fun resetOffsets() {
        weaveOffsetX = 0.0
        weaveOffsetY = 0.0
        weaveOffsetZ = 0.0
    }

    fun increaseAmplitude() = adjustWeave(amplitudeDelta = 1.0)

    fun decreaseAmplitude() = adjustWeave(amplitudeDelta = -1.0)

    fun increaseFrequency() = adjustWeave(frequencyDelta = 1.0)

    fun decreaseFrequency() = adjustWeave(frequencyDelta = -1.0)

    fun increaseSpeed() {
        speedPercent = (speedPercent + 5).coerceAtMost(100)
        sendCommand("SetSpeed($speedPercent)", 206)
        toast("SetSpeed($speedPercent)")
    }

    fun decreaseSpeed() {
        speedPercent = (speedPercent - 5).coerceAtLeast(1)
        sendCommand("SetSpeed($speedPercent)", 206)
        toast("SetSpeed($speedPercent)")
    }

    fun sendOffset(axis: Char, positive: Boolean) {
        val delta = if (positive) offsetStepMm else -offsetStepMm
        when (axis) {
            'X' -> weaveOffsetX += delta
            'Y' -> weaveOffsetY += delta
            'Z' -> weaveOffsetZ += delta
        }
        val cmd = String.format(
            Locale.US,
            "SetWeaveOffsetRT(%.1f,%.1f,%.1f,0.0,0.0,0.0)",
            weaveOffsetX, weaveOffsetY, weaveOffsetZ
        )
        sendCommand(cmd, 1368)
        toast(cmd)
    }

    fun increaseCurrent() = adjustCurrent(1.0)

    fun decreaseCurrent() = adjustCurrent(-1.0)

    fun increaseVoltage() = adjustVoltage(0.5)

    fun decreaseVoltage() = adjustVoltage(-0.5)

    private fun adjustCurrent(delta: Double) {
        setCurrent = (setCurrent + delta).coerceIn(0.0, 1000.0)
        currentProcess()?.current = setCurrent
        val currentStr = formatNumber(setCurrent)
        sendCommand("WeldingSetCurrent(0,$currentStr,0)", 201)
        toast("电流 ${currentStr}A")
    }

    private fun adjustVoltage(delta: Double) {
        setVoltage = (setVoltage + delta).coerceIn(0.0, 100.0)
        currentProcess()?.voltage = setVoltage
        val voltageStr = formatNumber(setVoltage)
        sendCommand("WeldingSetVoltage(0,$voltageStr,1)", 201)
        toast("电压 ${voltageStr}V")
    }

    private fun adjustWeave(amplitudeDelta: Double = 0.0, frequencyDelta: Double = 0.0) {
        val process = currentProcess()
        val base = process?.oscillation ?: Oscillation()
        val newAmp = (base.amplitude + amplitudeDelta).coerceAtLeast(1.0)
        val newFreq = (base.frequency + frequencyDelta).coerceAtLeast(1.0)
        if (newAmp == base.amplitude && newFreq == base.frequency) {
            toast("已到最小值")
            return
        }
        val osc = base.copy(amplitude = newAmp, frequency = newFreq)
        amplitude = osc.amplitude
        frequency = osc.frequency
        oscillationType = osc.type
        onOscillationChanged(osc)
        sendWeaveOnline(osc)
        toast(
            String.format(
                Locale.US,
                "摆幅 %.0f mm  频率 %.0f Hz",
                osc.amplitude,
                osc.frequency
            )
        )
    }

    private fun sendWeaveOnline(osc: Oscillation) {
        val typeCode = when (osc.type) {
            "三角波摆动" -> 0
            "直角L型三角波摆动" -> 1
            "圆形摆动-顺时针" -> 2
            "圆形摆动-逆时针" -> 3
            "正弦波摆动" -> 4
            "垂直L型正弦波摆动" -> 5
            "立焊三角摆动" -> 6
            else -> 0
        }
        val waitTimeCode = if (osc.waitTime == "不包括") 0 else 1
        val posWaitCode = if (osc.positionWait == "等待时间内位置继续移动") 0 else 1
        val cmd = "WeaveOnlineSetPara(3,$typeCode,${formatNumber(osc.frequency)},$waitTimeCode,${formatNumber(osc.amplitude)},${osc.leftStopTime.toInt()},${osc.rightStopTime.toInt()},${osc.callbackRatio.toInt().coerceIn(0, 100)},$posWaitCode)"
        sendCommand(cmd, 825)
    }

    private fun sendCommand(cmd: String, type: Int) {
        val id = nextCommandId()
        val msg = "/f/bIII${id}III${type}III${cmd.length}III${cmd}III/b/f"
        Log.d("FineTune", "Send: $msg")
        socketManager.sendControlCommand(msg)
    }

    private fun formatNumber(value: Double): String {
        return if (value == value.toLong().toDouble()) {
            value.toLong().toString()
        } else {
            String.format(Locale.US, "%.1f", value)
        }
    }
}
