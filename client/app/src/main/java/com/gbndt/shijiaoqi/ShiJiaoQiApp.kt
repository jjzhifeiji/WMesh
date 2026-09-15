package com.gbndt.shijiaoqi

import android.app.Application
import com.gbndt.shijiaoqi.platform.session.BagSession
import com.gbndt.shijiaoqi.platform.session.HttpFactoryGateway
import com.gbndt.shijiaoqi.platform.session.MqttDownChannel
import com.gbndt.shijiaoqi.platform.session.PrefsIdentity
import com.gbndt.shijiaoqi.platform.session.RoomEnvelopeStore

class ShiJiaoQiApp : Application() {
    lateinit var bag: BagSession
        private set

    override fun onCreate() {
        super.onCreate()
        val gateway = HttpFactoryGateway()
        bag = BagSession(
            serials = { serial },
            factory = gateway,
            identity = PrefsIdentity(this),
            store = RoomEnvelopeStore(this),
            down = MqttDownChannel(gateway),
        )
    }

    @Volatile
    var serial: String = ""

    override fun onTerminate() {
        bag.logout()
        super.onTerminate()
    }
}
