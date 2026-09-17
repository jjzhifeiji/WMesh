package com.gbndt.shijiaoqi.di

import javax.inject.Qualifier

/** 阻塞磁盘/网络用的调度器，对应 nowinandroid 的 IO。 */
@Qualifier
@Retention(AnnotationRetention.RUNTIME)
annotation class IoDispatcher

/** 进程级协程域：开机恢复等不跟界面走。 */
@Qualifier
@Retention(AnnotationRetention.RUNTIME)
annotation class ApplicationScope
