package com.gbndt.shijiaoqi.domain.shared

import com.gbndt.shijiaoqi.model.WeldProcess
import org.junit.Assert.assertEquals
import org.junit.Test

/** 厂端模版把数字写成 JSON 数字，平板偏移字段是字符串。 */
class ProcessJsonTest {
    @Test
    fun factoryNumericJsonOpens() {
        val body = """
            {"name":"平台测试工艺 1","offsetX":0,"offsetY":1.5,"offsetZ":2,
             "current":200,"voltage":24,"speed":6,
             "startArcTime":200,"startArcCurrent":175,"startArcVoltage":20.5,
             "endArcTime":800,"endArcCurrent":200,"endArcVoltage":22,
             "oscillation":{"type":"正弦波摆动","positionWait":"等待时间内位置静止",
               "frequency":2,"amplitude":4,"leftStopTime":300,"rightStopTime":300,
               "leftSideLength":2,"rightSideLength":2,"zeroTime":10,"callbackRatio":5,
               "azimuth":0,"inclination":0}}
        """.trimIndent().toByteArray()
        val got = ProcessJson.decode(body)
        assertEquals("平台测试工艺 1", got.name)
        assertEquals("0", got.offsetX)
        assertEquals("1.5", got.offsetY)
        assertEquals(200.0, got.current, 0.0)
        assertEquals("正弦波摆动", got.oscillation.type)
        assertEquals(2.0, got.oscillation.frequency, 0.0)
    }

    @Test
    fun encodeRoundTrip() {
        val src = WeldProcess(name = "本机", offsetX = "3", current = 180.0)
        val got = ProcessJson.decode(ProcessJson.encode(src))
        assertEquals("本机", got.name)
        assertEquals("3", got.offsetX)
        assertEquals(180.0, got.current, 0.0)
    }
}
