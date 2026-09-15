package com.gbndt.shijiaoqi.robot

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.nio.ByteBuffer
import java.nio.ByteOrder

class RobotAdapterTest {
    @Test
    fun frameRoundtripAndAckAndError() {
        val raw = FrPacket.encode(77, 101, "Start")
        assertEquals("/f/bIII77III101III5IIIStartIII/b/f", raw)
        val p = FrPacket.decode(raw)!!
        assertEquals(77, p.id)
        assertEquals(101, p.type)
        assertEquals("Start", p.payload)
        assertTrue(FrPacket.isSuccessAck("/f/bIII77III101III1III1III/b/f"))
        assertNull(FrPacket.errorCode("robot errcode:0"))
        assertNull(FrPacket.errorCode("robot errcode:18"))
        assertEquals(46, FrPacket.errorCode("robot errcode:46"))
        assertEquals(RobotCommands.HANDSHAKE, "connect")
    }

    @Test
    fun probeNeeds8080AndFresh8083() {
        val now = 10_000L
        assertFalse(RobotLink.connected(port8080 = false, port8083 = true, last8083At = now, now = now))
        assertFalse(RobotLink.connected(port8080 = true, port8083 = true, last8083At = now - 1001, now = now))
        assertTrue(RobotLink.connected(port8080 = true, port8083 = true, last8083At = now - 500, now = now))
        assertEquals(RobotLink.DOWN, RobotLink.status(true, true, now - 1001, now))
        assertEquals(RobotLink.UP, RobotLink.status(true, true, now, now))
        assertEquals(RobotLink.DOWN, RobotLink.status(true, false, now, now))
    }

    @Test
    fun parse8083Offsets() {
        val data = ByteArray(427)
        data[0] = 0x5A
        data[1] = 0x5A
        data[5] = 2
        data[6] = 0x03
        val cart = ByteBuffer.wrap(data, 8, 96).order(ByteOrder.LITTLE_ENDIAN)
        repeat(12) { i -> cart.putDouble((i + 1).toDouble()) }
        data[176] = 10
        data[177] = 3
        val ext = ByteBuffer.wrap(data, 265, 29).order(ByteOrder.LITTLE_ENDIAN)
        ext.putDouble(12.5)
        ext.putDouble(0.0)
        ext.putInt(0)
        ext.put(1)
        data[383] = 0xFF.toByte()
        data[384] = 0x0F
        data[387] = 1
        data[388] = 0
        data[425] = 1
        data[426] = 0
        val st = Status8083.parse(data)
        assertNotNull(st)
        assertEquals(2, st!!.programState)
        assertEquals("碰撞故障", st.alarmText)
        assertEquals(listOf(1.0, 2.0, 3.0, 4.0, 5.0, 6.0), st.joints)
        assertEquals(7.0, st.pose.x, 0.0)
        assertEquals(12.0, st.pose.rz, 0.0)
        assertEquals(12.5, st.extAxisPos, 0.0)
        assertTrue(st.extAxisReady)
        assertEquals(10, st.progTotalLine)
        assertEquals(3, st.progCurLine)
        assertEquals(500.0, st.weldCurrent, 0.01)
        assertTrue(st.weldingBreakOff)
        assertFalse(st.arcBreakOff)
        assertEquals(1, st.inputSignal)
        assertNull(Status8083.parse(ByteArray(50)))
        assertNull(Status8083.parse(ByteArray(104)))
    }

    @Test
    fun jogMatchesLiveTiming() {
        val right = RobotCommands.servoCart("右", 1, 0, 0, 0, 0, 0, 1.0, 0.2, 0)
        val cmd = "ServoCart(1,{0,-1,0,0,0,0},{1.0,1.0,1.0,0.2,0.2,0.2},{0,0,0,0},0,0,0.2,0,0)"
        assertEquals("/f/bIII18III201III${cmd.length}III${cmd}III/b/f", right)
        val front = RobotCommands.mapCartAxes("前", 1, 0, 0, 0)
        assertEquals(1, front.x)
        assertEquals(0, front.y)
        val jog = RobotCommands.extAxisStartJog(1)
        assertTrue(jog.contains("III292III"))
        assertTrue(jog.contains("ExtAxisStartJog(6,1,1,100,100,2000)"))
        val bus = MemoryRobotBus()
        bus.port8080 = true
        bus.push8083(now = 1000L)
        assertEquals(RobotLink.UP, bus.status(1000L))
        bus.sendJog(RobotCommands.servoCart("右", 0, 1, 0, 0, 0, 0, 1.0, 0.2, 0))
        assertTrue(bus.lastControl.contains("ServoCart"))
        assertTrue(bus.lastControl.startsWith("/f/bIII18III201III"))
    }

    private class MemoryRobotBus {
        var port8080 = false
        var port8083 = false
        var last8083 = 0L
        var lastControl = ""

        fun push8083(now: Long) {
            port8083 = true
            last8083 = now
        }

        fun status(now: Long) = RobotLink.status(port8080, port8083, last8083, now)

        fun sendJog(frame: String) {
            lastControl = frame
        }
    }
}
