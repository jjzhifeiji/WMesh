package com.gbndt.shijiaoqi.domain.robot

/** 现网点动与探活报文；不生成焊接 Lua。 */
object RobotCommands {
    const val HANDSHAKE = "connect"
    const val TYPE_LUA = 201
    const val TYPE_SERVO_CART = 201
    const val ID_SERVO_CART = 18
    const val TYPE_EXT_JOG = 292
    const val TYPE_EXT_JOG_STOP = 240
    const val TYPE_EXT_SERVO = 296
    const val TYPE_MODE = 303
    const val TYPE_ENABLE = 302

    data class CartAxes(val x: Int, val y: Int, val rx: Int, val ry: Int)

    fun frame(id: Int, type: Int, payload: String): String = FrPacket.encode(id, type, payload)

    fun mapCartAxes(positionMode: String, joyFwd: Int, joyLeft: Int, rotFwd: Int, rotLeft: Int): CartAxes {
        return when (positionMode) {
            "前" -> CartAxes(joyFwd, joyLeft, -rotLeft, rotFwd)
            "左" -> CartAxes(-joyLeft, joyFwd, -rotFwd, -rotLeft)
            "后" -> CartAxes(-joyFwd, -joyLeft, rotLeft, -rotFwd)
            else -> CartAxes(joyLeft, -joyFwd, rotFwd, rotLeft)
        }
    }

    fun servoCart(
        positionMode: String,
        joyFwd: Int,
        joyLeft: Int,
        z: Int,
        rotFwd: Int,
        rotLeft: Int,
        rz: Int,
        speedS: Double,
        speedR: Double,
        ext1: Int,
    ): String {
        val a = mapCartAxes(positionMode, joyFwd, joyLeft, rotFwd, rotLeft)
        val cmd = "ServoCart(1,{${a.x},${a.y},$z,${a.rx},${a.ry},$rz},{$speedS,$speedS,$speedS,$speedR,$speedR,$speedR},{$ext1,0,0,0},0,0,0.2,0,0)"
        return frame(ID_SERVO_CART, TYPE_SERVO_CART, cmd)
    }

    fun extAxisStartJog(direction: Int): String =
        frame(1, TYPE_EXT_JOG, "ExtAxisStartJog(6,1,$direction,100,100,2000)")

    fun extAxisStopJog(): String = frame(1, TYPE_EXT_JOG_STOP, "StopExtAxisJog")

    fun extAxisServoOn(): String = frame(1, TYPE_EXT_SERVO, "ExtAxisServoOn(1,1)")

    fun modeManual(): String = frame(20, TYPE_MODE, "Mode(1)")

    fun modeAuto(): String = frame(20, TYPE_MODE, "Mode(0)")

    fun robotEnable(): String = frame(1, TYPE_ENABLE, "RobotEnable(1)")
}
