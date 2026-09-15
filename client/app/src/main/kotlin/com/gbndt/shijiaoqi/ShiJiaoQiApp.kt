package com.gbndt.shijiaoqi

import android.app.Application
import com.gbndt.shijiaoqi.data.session.BagSession
import com.gbndt.shijiaoqi.data.remote.HttpFactoryGateway
import com.gbndt.shijiaoqi.data.remote.MqttDownChannel
import com.gbndt.shijiaoqi.data.prefs.PrefsIdentity
import com.gbndt.shijiaoqi.data.db.RoomEnvelopeStore

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
