package com.gbndt.shijiaoqi.data.session

import javax.inject.Inject
import javax.inject.Singleton

/** 机械臂识别号只活在内存：连上臂读到就写进来，进程死即失。 */
@Singleton
class DeviceSerialHolder @Inject constructor() : DeviceSerialReader {
    @Volatile
    private var serial: String = ""

    override fun read(): String = serial

    fun set(value: String) {
        serial = value
    }
}
