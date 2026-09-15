package com.gbndt.shijiaoqi.ui.login

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
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

/** 登录屏：扫厂服务、选一个、登录；忙与错误以会话为准。 */
@HiltViewModel
class LoginViewModel @Inject constructor(
    private val session: SessionRepository,
) : ViewModel() {

    private val local = MutableStateFlow(LoginUiState())

    val uiState: StateFlow<LoginUiState> = combine(local, session.state) { own, sess ->
        own.copy(busy = sess.busy, error = sess.error)
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), LoginUiState())

    /** 扫本网厂服务；失败时按空结果处理，原因由会话给。 */
    fun scan() {
        viewModelScope.launch {
            val hits = runCatching { session.findFactories() }.getOrDefault(emptyList())
            local.update {
                it.copy(offers = hits, selected = hits.firstOrNull(), scanned = true)
            }
        }
    }

    fun select(offer: FactoryOffer) = local.update { it.copy(selected = offer) }

    /** 登录成功与否都反映在会话状态里，这里不另存。 */
    fun login(loginName: String, password: String) {
        val hit = local.value.selected ?: return
        viewModelScope.launch {
            runCatching { session.login(hit.httpBase, hit.factoryId, loginName, password) }
        }
    }
}
