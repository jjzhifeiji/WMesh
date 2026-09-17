package com.gbndt.shijiaoqi.ui.login

import com.gbndt.shijiaoqi.model.FactoryOffer

/** 厂服务扫描所处阶段。 */
enum class ScanStatus {
    Ready,
    Scanning,
    Empty,
    Unavailable,
}

/** 登录界面的完整形状；界面只读它，不自己攒状态。 */
data class LoginUiState(
    /** 本网扫到的厂服务 */
    val offers: List<FactoryOffer> = emptyList(),
    /** 当前选中的厂服务 */
    val selected: FactoryOffer? = null,
    /** 扫描阶段：可以扫描 / 扫描中 / 未发现 / 不可用 */
    val scan: ScanStatus = ScanStatus.Ready,
    /** 正在提交登录，此时不能再扫 */
    val loggingIn: Boolean = false,
    /** 失败原因，中文 */
    val error: String? = null,
    /** 登录名，勾选记住时预填 */
    val loginName: String = "",
    /** 密码，勾选记住时预填 */
    val password: String = "",
    /** 是否记住账号密码 */
    val remember: Boolean = false,
) {
    /** 未在扫描、未在登录时可以再扫。 */
    val canScan: Boolean get() = scan != ScanStatus.Scanning && !loggingIn

    /** 选中可用厂服务且当前既没在扫也没在登录。 */
    val canLogin: Boolean get() = canScan && selected?.usable() == true

    /** 给人看的扫描状态。 */
    val scanCaption: String get() = when {
        loggingIn -> "不可用"
        scan == ScanStatus.Scanning -> "扫描中"
        scan == ScanStatus.Empty -> "未发现厂服务"
        scan == ScanStatus.Unavailable -> "不可用"
        else -> "可以扫描"
    }
}
