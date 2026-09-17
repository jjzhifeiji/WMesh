package com.gbndt.shijiaoqi.data.session

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class PolicyKvTest {
    @Test
    fun oldFiveSegmentsStayEncrypted() {
        val got = parsePolicyKv("3|2|all|false|0")
        assertEquals(3L, got!!.revision)
        assertTrue(got.encryptPouch)
    }

    @Test
    fun sixthSegmentTogglesEncrypt() {
        assertTrue(parsePolicyKv("1|2|all|false|0|true")!!.encryptPouch)
        assertFalse(parsePolicyKv("1|2|all|false|0|false")!!.encryptPouch)
        assertNull(parsePolicyKv("1|2"))
    }
}
