package com.gbndt.shijiaoqi.data.robot.link

import com.gbndt.shijiaoqi.config.AppConfig
import com.gbndt.shijiaoqi.data.remote.LanLink
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.net.Inet4Address
import java.net.InetAddress

class RobotLanTest {
    @Test
    fun plantWifiIsNotRobotSubnet() {
        val plant = LanLink(inet("192.168.123.54"), 24)
        assertFalse(onRobotSubnet(listOf(plant), AppConfig.Robot.IP))
        assertFalse(sameSubnet(inet("192.168.123.54"), 24, inet(AppConfig.Robot.IP)))
    }

    @Test
    fun robotWifiIsRobotSubnet() {
        val arm = LanLink(inet("192.168.57.10"), 24)
        assertTrue(onRobotSubnet(listOf(arm), AppConfig.Robot.IP))
        assertTrue(sameSubnet(inet("192.168.57.10"), 24, inet(AppConfig.Robot.IP)))
    }

    private fun inet(ip: String) = InetAddress.getByName(ip) as Inet4Address
}
