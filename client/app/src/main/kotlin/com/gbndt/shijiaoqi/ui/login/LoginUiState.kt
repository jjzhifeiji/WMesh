package com.gbndt.shijiaoqi.ui.login

import com.gbndt.shijiaoqi.data.remote.FactoryOffer

/** 登录界面的完整形状；界面只读它，不自己攒状态。 */
data class LoginUiState(
    /** 本网扫到的厂服务 */
    val offers: List<FactoryOffer> = emptyList(),
    /** 当前选中的厂服务 */
    val selected: FactoryOffer? = null,
    /** 已经扫过一轮，用来区分“还没扫”和“扫了没有” */
    val scanned: Boolean = false,
    /** 扫描或登录进行中 */
    val busy: Boolean = false,
    /** 失败原因，中文 */
    val error: String? = null,
)
