package com.gbndt.shijiaoqi.di

import android.content.Context
import com.gbndt.shijiaoqi.config.AppConfig
import com.gbndt.shijiaoqi.data.db.RoomEnvelopeStore
import com.gbndt.shijiaoqi.data.prefs.DeviceSettingsStore
import com.gbndt.shijiaoqi.data.remote.AndroidLanMulticast
import com.gbndt.shijiaoqi.data.remote.HttpFactoryGateway
import com.gbndt.shijiaoqi.data.remote.LanMulticast
import com.gbndt.shijiaoqi.data.session.BagSession
import com.gbndt.shijiaoqi.data.session.DeviceSerialHolder
import com.gbndt.shijiaoqi.data.session.DeviceSerialReader
import com.gbndt.shijiaoqi.data.session.EnvelopeStore
import com.gbndt.shijiaoqi.data.session.FactoryGateway
import com.gbndt.shijiaoqi.data.session.FileUnwrapKeyStore
import com.gbndt.shijiaoqi.data.session.IdentityStore
import com.gbndt.shijiaoqi.data.session.SessionVault
import com.gbndt.shijiaoqi.data.session.UnwrapKeyStore
import com.gbndt.shijiaoqi.data.log.PadLog
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.android.qualifiers.ApplicationContext
import dagger.hilt.components.SingletonComponent
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import java.io.File
import javax.inject.Singleton

/** 单模块工程的唯一装配处：谁实现谁、谁是单例，都在这里说清。 */
@Module
@InstallIn(SingletonComponent::class)
object AppModule {

    /** 阻塞 IO 用的调度器；测试里换成单线程的即可。 */
    @Provides
    @Singleton
    @IoDispatcher
    fun provideIoDispatcher(): CoroutineDispatcher = Dispatchers.IO

    @Provides
    @Singleton
    @ApplicationScope
    fun provideApplicationScope(@IoDispatcher io: CoroutineDispatcher): CoroutineScope =
        CoroutineScope(SupervisorJob() + io)

    @Provides
    @Singleton
    fun provideFactoryGateway(): FactoryGateway = HttpFactoryGateway()

    @Provides
    @Singleton
    fun provideIdentityStore(settings: DeviceSettingsStore): IdentityStore = settings

    @Provides
    @Singleton
    fun provideSessionVault(settings: DeviceSettingsStore): SessionVault = settings

    @Provides
    @Singleton
    fun provideEnvelopeStore(@ApplicationContext context: Context): EnvelopeStore = RoomEnvelopeStore(context)

    @Provides
    @Singleton
    fun provideUnwrapKeyStore(@ApplicationContext context: Context): UnwrapKeyStore =
        FileUnwrapKeyStore(File(context.applicationContext.filesDir, AppConfig.UNWRAP_KEY_FILE))

    @Provides
    @Singleton
    fun provideDeviceSerialReader(holder: DeviceSerialHolder): DeviceSerialReader = holder

    @Provides
    @Singleton
    fun provideLanMulticast(impl: AndroidLanMulticast): LanMulticast = impl

    @Provides
    @Singleton
    fun provideBagSession(
        serials: DeviceSerialReader,
        factory: FactoryGateway,
        identity: IdentityStore,
        store: EnvelopeStore,
        multicast: LanMulticast,
        vault: SessionVault,
        keys: UnwrapKeyStore,
    ): BagSession = BagSession(
        serials = serials,
        factory = factory,
        identity = identity,
        store = store,
        multicast = multicast,
        vault = vault,
        keys = keys,
        onFactoryNet = { base, factoryId -> PadLog.tryShip(base, factoryId, identity.clientId) },
        restoreOnStart = false,
    )
}
