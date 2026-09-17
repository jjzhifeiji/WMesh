package com.gbndt.shijiaoqi.data.session

import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import javax.inject.Inject
import javax.inject.Singleton

/** 袋与会话同一把锁：内存袋无并发保护，读写都从这扇门过。 */
@Singleton
class SessionGate @Inject constructor() {
    private val lock = Mutex()

    suspend fun <T> withLock(block: suspend () -> T): T = lock.withLock { block() }
}
