package com.gbndt.shijiaoqi.ui.login

import com.gbndt.shijiaoqi.model.FactoryOffer
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class LoginUiStateTest {
    private val active = FactoryOffer("http://10.0.0.8:52081", "fac", "active", false, "", "一厂")
    private val down = FactoryOffer("http://10.0.0.9:52081", "fac2", "disabled", false, "", "二厂")

    @Test
    fun captionsFollowScanPhase() {
        assertEquals("可以扫描", LoginUiState().scanCaption)
        assertEquals("扫描中", LoginUiState(scan = ScanStatus.Scanning).scanCaption)
        assertEquals("未发现厂服务", LoginUiState(scan = ScanStatus.Empty).scanCaption)
        assertEquals("不可用", LoginUiState(scan = ScanStatus.Unavailable).scanCaption)
        assertEquals("不可用", LoginUiState(scan = ScanStatus.Ready, loggingIn = true).scanCaption)
    }

    @Test
    fun scanAndLoginGates() {
        val ready = LoginUiState(offers = listOf(active), selected = active)
        assertTrue(ready.canScan)
        assertTrue(ready.canLogin)

        assertFalse(ready.copy(scan = ScanStatus.Scanning).canScan)
        assertFalse(ready.copy(loggingIn = true).canScan)
        assertFalse(LoginUiState(offers = listOf(down), selected = down).canLogin)
        assertFalse(LoginUiState(scan = ScanStatus.Empty).canLogin)
    }

    @Test
    fun disabledFactoryMarkedUnavailable() {
        assertTrue(active.usable())
        assertFalse(down.usable())
        assertEquals("二厂 · 10.0.0.9:52081（不可用）", down.label())
    }

    @Test
    fun loginListDropsUnusableDuplicates() {
        val retired = down.copy(factoryId = "fac3", status = "retired")
        assertEquals(listOf(active), loginOffers(listOf(down, active, retired)))
        assertTrue(loginOffers(listOf(down, retired)).isEmpty())
    }

    @Test
    fun storedLoginClearsWhenNotRemembered() {
        assertEquals(Triple(true, "op", "secret"), storedLogin(true, " op ", "secret"))
        assertEquals(Triple(false, "", ""), storedLogin(false, "op", "secret"))
    }
}
