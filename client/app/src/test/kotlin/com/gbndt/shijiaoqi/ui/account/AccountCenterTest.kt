package com.gbndt.shijiaoqi.ui.account

import org.junit.Assert.assertEquals
import org.junit.Test

class AccountCenterTest {
    @Test
    fun roleLabelsAreChinese() {
        assertEquals("操作员", roleLabel("operator"))
        assertEquals("工厂超管", roleLabel("factory_super_admin"))
        assertEquals("工艺工程师", roleLabel("process_engineer"))
        assertEquals("custom", roleLabel("custom"))
    }
}
