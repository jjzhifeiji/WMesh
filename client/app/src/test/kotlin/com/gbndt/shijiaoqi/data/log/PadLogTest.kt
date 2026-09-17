package com.gbndt.shijiaoqi.data.log

import com.gbndt.shijiaoqi.config.AppConfig
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import kotlin.io.path.createTempDirectory

/** 本机环形文件与直推 Alloy：不登录、失败保留、2xx 才删。 */
class PadLogTest {
    @After
    fun tearDown() {
        PadLog.reset()
    }

    @Test
    fun alloyUrlUsesFactoryHost() {
        assertEquals(
            "http://10.0.0.8:${AppConfig.Factory.ALLOY_PORT}/loki/api/v1/push",
            LokiPush.urlFromFactory("http://10.0.0.8:52081"),
        )
        assertEquals(null, LokiPush.urlFromFactory("not-a-url"))
    }

    @Test
    fun bodyGroupsByLevelAndSkipsBlankIdentity() {
        val recs = listOf(
            LogRec("1", "error", "e1"),
            LogRec("2", "warn", "w1"),
            LogRec("3", "error", "e2"),
        )
        val json = LokiPush.body(recs, "", "")!!
        assertTrue(json.contains("\"job\":\"shijiaoqi\""))
        assertTrue(json.contains("\"level\":\"error\""))
        assertTrue(json.contains("\"level\":\"warn\""))
        assertFalse(json.contains("\"factory\""))
        assertFalse(json.contains("\"client\""))
        assertTrue(json.contains("e1"))
        assertTrue(json.contains("e2"))
        assertTrue(json.contains("w1"))
    }

    @Test
    fun shipAckOn2xxKeepsOn5xx() {
        val dir = createTempDirectory("pad-log").toFile()
        PadLog.start(dir)
        PadLog.info("T", "login ok")
        PadLog.error("T", "boom")
        PadLog.poster = { _, _ -> 500 }
        assertFalse(PadLog.ship("http://10.0.0.8:52081", "fac", "cli"))
        var posted = ""
        PadLog.poster = { url, body ->
            assertEquals("http://10.0.0.8:3500/loki/api/v1/push", url)
            posted = body
            204
        }
        assertTrue(PadLog.ship("http://10.0.0.8:52081", "fac", "cli"))
        assertTrue(posted.contains("login ok"))
        assertTrue(posted.contains("boom"))
        assertTrue(posted.contains("\"factory\":\"fac\""))
        assertTrue(posted.contains("\"client\":\"cli\""))
        PadLog.poster = { _, _ -> error("empty ship must not post") }
        assertTrue(PadLog.ship("http://10.0.0.8:52081", "fac", "cli"))
    }

    @Test
    fun clickWritesInfo() {
        val dir = createTempDirectory("pad-click").toFile()
        PadLog.start(dir)
        PadLog.click("scan")
        var posted = ""
        PadLog.poster = { _, body ->
            posted = body
            204
        }
        assertTrue(PadLog.ship("http://10.0.0.8:52081", "fac", "cli"))
        assertTrue(posted.contains("click scan"))
        assertTrue(posted.contains("\"level\":\"info\""))
        assertTrue(posted.contains("at PadLogTest.kt:"))
        assertTrue(posted.contains("clickWritesInfo"))
    }

    @Test
    fun toastWritesInfo() {
        val dir = createTempDirectory("pad-toast").toFile()
        PadLog.start(dir)
        PadLog.toast("下载完成，正在准备安装...")
        var posted = ""
        PadLog.poster = { _, body ->
            posted = body
            204
        }
        assertTrue(PadLog.ship("http://10.0.0.8:52081", "fac", "cli"))
        assertTrue(posted.contains("Toast 下载完成，正在准备安装..."))
        assertTrue(posted.contains("\"level\":\"info\""))
        assertTrue(posted.contains("at PadLogTest.kt:"))
        assertTrue(posted.contains("toastWritesInfo"))
    }

    @Test
    fun originPointsAtCaller() {
        val at = PadLog.origin()
        assertTrue(at.contains("PadLogTest.kt"))
        assertTrue(at.contains("originPointsAtCaller"))
    }

    @Test
    fun toastSkipsHelperName() {
        val dir = createTempDirectory("pad-toast-helper").toFile()
        PadLog.start(dir)
        toast("shown")
        var posted = ""
        PadLog.poster = { _, body ->
            posted = body
            204
        }
        assertTrue(PadLog.ship("http://10.0.0.8:52081", "fac", "cli"))
        assertTrue(posted.contains("toastSkipsHelperName"))
        assertTrue(posted.contains("at PadLogTest.kt:"))
    }

    private fun toast(msg: String) {
        PadLog.toast(msg)
    }

    @Test
    fun ringDropsOldestWhenFull() {
        val dir = createTempDirectory("pad-ring").toFile()
        val ring = LogRing(dir, maxTotalBytes = 80)
        repeat(20) { i ->
            ring.append(LogRec(i.toString(), "error", "x".repeat(20)))
        }
        val total = dir.listFiles()?.sumOf { it.length() } ?: 0
        assertTrue(total <= 80 + 60)
    }
}
