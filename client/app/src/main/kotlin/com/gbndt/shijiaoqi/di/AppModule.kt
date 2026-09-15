package com.gbndt.shijiaoqi.di

import android.content.Context
import com.gbndt.shijiaoqi.data.db.RoomEnvelopeStore
import com.gbndt.shijiaoqi.data.prefs.PrefsIdentity
import com.gbndt.shijiaoqi.data.remote.DownChannel
import com.gbndt.shijiaoqi.data.remote.HttpFactoryGateway
import com.gbndt.shijiaoqi.data.remote.MqttDownChannel
import com.gbndt.shijiaoqi.data.session.BagSession
import com.gbndt.shijiaoqi.data.session.DeviceSerialHolder
import com.gbndt.shijiaoqi.data.session.DeviceSerialReader
import com.gbndt.shijiaoqi.data.session.EnvelopeStore
import com.gbndt.shijiaoqi.data.session.FactoryGateway
import com.gbndt.shijiaoqi.data.session.IdentityStore
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.android.qualifiers.ApplicationContext
import dagger.hilt.components.SingletonComponent
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.Dispatchers
import javax.inject.Singleton

/** 单模块工程的唯一装配处：谁实现谁、谁是单例，都在这里说清。 */
@Module
@InstallIn(SingletonComponent::class)
object AppModule {

    /** 阻塞 IO 用的调度器；测试里换成单线程的即可。 */
    @Provides
    @Singleton
    fun provideIoDispatcher(): CoroutineDispatcher = Dispatchers.IO

    @Provides
    @Singleton
    fun provideFactoryGateway(): FactoryGateway = HttpFactoryGateway()

    @Provides
    @Singleton
    fun provideIdentityStore(@ApplicationContext context: Context): IdentityStore = PrefsIdentity(context)

    @Provides
    @Singleton
    fun provideEnvelopeStore(@ApplicationContext context: Context): EnvelopeStore = RoomEnvelopeStore(context)

    /** 控制面只走 MQTT，正文不进这条通道。 */
    @Provides
    @Singleton
    fun provideDownChannel(factory: FactoryGateway): DownChannel = MqttDownChannel(factory)

    @Provides
    @Singleton
    fun provideDeviceSerialReader(holder: DeviceSerialHolder): DeviceSerialReader = holder

    @Provides
    @Singleton
    fun provideBagSession(
        serials: DeviceSerialReader,
        factory: FactoryGateway,
        identity: IdentityStore,
        store: EnvelopeStore,
        down: DownChannel,
    ): BagSession = BagSession(
        serials = serials,
        factory = factory,
        identity = identity,
        store = store,
        down = down,
    )
}
