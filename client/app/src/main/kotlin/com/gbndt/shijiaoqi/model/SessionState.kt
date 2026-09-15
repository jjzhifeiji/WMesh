package com.gbndt.shijiaoqi.model

/** 登录会话对外的全部可观察状态；不含钥、不含正文。 */
data class SessionState(
    /** 本厂账号是否已登录 */
    val loggedIn: Boolean = false,
    /** 读到的机械臂识别号是否与本机绑定一致 */
    val armMatched: Boolean = false,
    /** 登录人显示名 */
    val personName: String = "",
    /** 正在扫描或登录 */
    val busy: Boolean = false,
    /** 上一次失败的中文原因，成功后清空 */
    val error: String? = null,
)
