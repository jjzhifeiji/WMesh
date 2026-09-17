package com.gbndt.shijiaoqi.data.log

import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import kotlin.io.path.createTempDirectory

/** 厂 HTTP 日志：打码密钥正文，扫描超时不落盘。 */
class HttpLogTest {
    @After
    fun tearDown() {
        PadLog.reset()
    }

    @Test
    fun redactStripsSecretsAndKeepsMeta() {
        val raw = """{"loginName":"a","password":"secret","token":"t1","unwrapKey":"k","content":"blob","wrap":"w","roles":["welder"]}"""
        val out = HttpLog.redact(raw)
        assertFalse(out.contains("secret"))
        assertFalse(out.contains("\"t1\""))
        assertFalse(out.contains("\"k\""))
        assertFalse(out.contains("blob"))
        assertTrue(out.contains("\"password\":\"***\""))
        assertTrue(out.contains("\"token\":\"***\""))
        assertTrue(out.contains("\"unwrapKey\":\"***\""))
        assertTrue(out.contains("\"content\":\"***\""))
        assertTrue(out.contains("\"wrap\":\"***\""))
        assertTrue(out.contains("\"loginName\":\"a\""))
        assertTrue(out.contains("welder"))
    }

    @Test
    fun pathOfDropsQuery() {
        assertEquals(
            "http://10.0.0.8:52081/update.json",
            HttpLog.pathOf("http://10.0.0.8:52081/update.json?token=abc"),
        )
        assertTrue(HttpLog.isDiscover("/v1/discover"))
        assertFalse(HttpLog.isDiscover("/v1/factories/x/pad/login"))
    }

    @Test
    fun reqRspWriteRedactedBody() {
        val dir = createTempDirectory("http-log").toFile()
        PadLog.start(dir)
        HttpLog.req("POST", "/v1/factories/f/pad/login", """{"loginName":"a","password":"p"}""")
        HttpLog.rsp("POST", "/v1/factories/f/pad/login", 200, """{"token":"t","unwrapKey":"k"}""")
        var posted = ""
        PadLog.poster = { _, body ->
            posted = body
            204
        }
        assertTrue(PadLog.ship("http://10.0.0.8:52081", "fac", "cli"))
        assertTrue(posted.contains("req POST /v1/factories/f/pad/login"))
        assertTrue(posted.contains("password"))
        assertTrue(posted.contains("***"))
        assertTrue(posted.contains("rsp POST /v1/factories/f/pad/login code=200"))
        assertTrue(posted.contains("at HttpLogTest.kt:"))
        assertTrue(posted.contains("reqRspWriteRedactedBody"))
        assertFalse(posted.contains("\\\"p\\\""))
        assertFalse(posted.contains("\\\"t\\\""))
        assertFalse(posted.contains("\\\"k\\\""))
    }

    @Test
    fun discoverFailStaysQuiet() {
        val dir = createTempDirectory("http-quiet").toFile()
        PadLog.start(dir)
        HttpLog.fail("GET", "/v1/discover", "SocketTimeoutException", quiet = true)
        PadLog.poster = { _, _ -> error("quiet discover must not ship") }
        assertTrue(PadLog.ship("http://10.0.0.8:52081", "fac", "cli"))
    }
}
