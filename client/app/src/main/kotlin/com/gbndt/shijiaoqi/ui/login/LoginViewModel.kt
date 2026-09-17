package com.gbndt.shijiaoqi.ui.login

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.gbndt.shijiaoqi.data.prefs.DeviceSettingsStore
import com.gbndt.shijiaoqi.model.FactoryOffer
import com.gbndt.shijiaoqi.data.repository.SessionRepository
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import javax.inject.Inject

/** 登录屏：扫厂服务、选一个、登录；扫描状态与登录忙闲分开。 */
@HiltViewModel
class LoginViewModel @Inject constructor(
    private val session: SessionRepository,
    private val settings: DeviceSettingsStore,
) : ViewModel() {

    private val local = MutableStateFlow(initialLogin())

    val uiState: StateFlow<LoginUiState> = combine(local, session.state) { own, sess ->
        val scanning = own.scan == ScanStatus.Scanning
        own.copy(
            loggingIn = sess.busy && !scanning,
            error = if (scanning || own.scan == ScanStatus.Empty) null else sess.error,
        )
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), local.value)

    /** 扫本网厂服务；进行中不再开一轮。 */
    fun scan() {
        if (local.value.scan == ScanStatus.Scanning) return
        local.update { it.copy(scan = ScanStatus.Scanning) }
        viewModelScope.launch {
            val result = runCatching { session.findFactories() }
            local.update { cur ->
                result.fold(
                    onSuccess = { hits ->
                        val offers = loginOffers(hits)
                        cur.copy(
                            offers = offers,
                            selected = pick(offers, cur.selected),
                            scan = if (offers.isEmpty()) ScanStatus.Empty else ScanStatus.Ready,
                        )
                    },
                    onFailure = {
                        cur.copy(offers = emptyList(), selected = null, scan = ScanStatus.Unavailable)
                    },
                )
            }
        }
    }

    fun select(offer: FactoryOffer) = local.update { it.copy(selected = offer) }

    fun setLoginName(value: String) = local.update { it.copy(loginName = value) }

    fun setPassword(value: String) = local.update { it.copy(password = value) }

    /** 取消勾选立刻清掉已存账号密码。 */
    fun setRemember(value: Boolean) {
        local.update { it.copy(remember = value) }
        if (!value) settings.saveRememberedLogin(false, "", "")
    }

    /** 登录成功才按勾选落盘账号密码。 */
    fun login() {
        val cur = local.value
        val hit = cur.selected ?: return
        if (!hit.usable()) return
        val name = cur.loginName
        val password = cur.password
        if (name.isBlank() || password.isBlank()) return
        viewModelScope.launch {
            val result = runCatching { session.login(hit.httpBase, hit.factoryId, name, password) }
            if (result.isSuccess) {
                val (on, n, p) = storedLogin(local.value.remember, name, password)
                settings.saveRememberedLogin(on, n, p)
            }
        }
    }

    private fun initialLogin(): LoginUiState {
        val remembered = settings.loadRememberedLogin() ?: return LoginUiState()
        return LoginUiState(
            loginName = remembered.first,
            password = remembered.second,
            remember = true,
        )
    }

    private fun pick(hits: List<FactoryOffer>, current: FactoryOffer?): FactoryOffer? {
        current?.let { old -> hits.firstOrNull { it.factoryId == old.factoryId && it.httpBase == old.httpBase } }?.let { return it }
        return hits.firstOrNull()
    }
}

/** 登录只列出能登的厂，注销/停用不进下拉。 */
internal fun loginOffers(hits: List<FactoryOffer>): List<FactoryOffer> = hits.filter { it.usable() }

/** 不记住则落盘为空，避免下次预填。 */
internal fun storedLogin(remember: Boolean, name: String, password: String): Triple<Boolean, String, String> =
    if (remember) Triple(true, name.trim(), password) else Triple(false, "", "")
