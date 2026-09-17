package com.gbndt.shijiaoqi.data.log

import android.util.Log
import com.gbndt.shijiaoqi.config.AppConfig
import java.io.File
import java.util.concurrent.atomic.AtomicBoolean
import kotlin.concurrent.thread

/** 本机 info/warn/error 落盘，debug 只 logcat；扫到厂网后直推 Alloy，不登录。 */
object PadLog {
    @Volatile
    internal var ring: LogRing? = null

    @Volatile
    internal var poster: (String, String) -> Int = LokiPush::post

    private val shipping = AtomicBoolean(false)
    private val hooked = AtomicBoolean(false)
    private val clock: () -> Long = { System.currentTimeMillis() }

    /** 进程启动装环形文件；可重复调用。 */
    fun start(filesDir: File) {
        ring = LogRing(File(filesDir, AppConfig.LOG_DIR))
        hookCrash()
    }

    /** 测试拆掉落盘，避免污染其它用例。 */
    internal fun reset() {
        ring = null
        poster = LokiPush::post
        shipping.set(false)
    }

    fun click(name: String, at: String? = null) = info("UI", "click $name", at)

    fun toast(msg: String, at: String? = null) = info("Toast", msg, at)

    fun warn(tag: String, msg: String, at: String? = null) {
        val where = at ?: origin()
        echo(tag, kind = "warn", msg, null, where)
        write("warn", tag, msg, null, where)
    }

    fun error(tag: String, msg: String, err: Throwable? = null, at: String? = null) {
        val where = at ?: origin()
        echo(tag, kind = "error", msg, err, where)
        write("error", tag, msg, err, where)
    }

    /** 节点关键动作也落盘上送，不含密钥。 */
    fun info(tag: String, msg: String, at: String? = null) {
        val where = at ?: origin()
        echo(tag, kind = "info", msg, null, where)
        write("info", tag, msg, null, where)
    }

    /** 厂 HTTP 通了就试推；机械臂网会快速失败，文件留下。 */
    fun tryShip(factoryHttpBase: String, factoryId: String, clientId: String) {
        if (!shipping.compareAndSet(false, true)) return
        thread(name = "pad-log-ship", isDaemon = true) {
            try {
                ship(factoryHttpBase, factoryId, clientId)
            } finally {
                shipping.set(false)
            }
        }
    }

    /** 同步推一批，测试用；2xx 才 ack。 */
    internal fun ship(factoryHttpBase: String, factoryId: String, clientId: String): Boolean {
        val store = ring ?: return false
        val url = LokiPush.urlFromFactory(factoryHttpBase) ?: return false
        val batch = store.take()
        if (batch.records.isEmpty()) {
            store.ack(batch)
            return true
        }
        val body = LokiPush.body(batch.records, factoryId, clientId) ?: return true
        val code = runCatching { poster(url, body) }.getOrDefault(0)
        if (code in 200..299) {
            store.ack(batch)
            return true
        }
        return false
    }

    /** 第一个业务栈帧：文件:行号 方法，便于对上代码。 */
    internal fun origin(): String {
        for (el in Throwable().stackTrace) {
            if (skip(el)) continue
            return format(el)
        }
        return "?"
    }

    private fun write(level: String, tag: String, msg: String, err: Throwable?, at: String) {
        val store = ring ?: return
        val detail = err?.let { "${it.javaClass.simpleName}: ${it.message ?: ""}".trim() }.orEmpty()
        val line = buildString {
            append(tag)
            append(' ')
            append(msg.take(4_000))
            if (detail.isNotEmpty()) {
                append(' ')
                append(detail.take(500))
            }
            append(" at ")
            append(at)
        }
        runCatching { store.append(LogRec(LokiPush.nsTimestamp(clock()), level, line)) }
    }

    private fun echo(tag: String, kind: String, msg: String, err: Throwable?, at: String) {
        val text = "$msg at $at"
        runCatching {
            when (kind) {
                "error" -> Log.e(tag, text, err)
                "warn" -> Log.w(tag, text)
                else -> Log.i(tag, text)
            }
        }
    }

    private fun skip(el: StackTraceElement): Boolean {
        val c = el.className
        if (c.startsWith("com.gbndt.shijiaoqi.data.log.")) {
            val simple = c.substringAfterLast('.').substringBefore('$')
            if (!simple.endsWith("Test")) return true
        }
        if (!c.startsWith("com.gbndt.shijiaoqi")) return true
        when (el.fileName) {
            "SharedComponents.kt", "StatusBar.kt" -> return true
        }
        val method = el.methodName.substringBefore('-').substringBefore('$')
        return method in skipMethods
    }

    private fun format(el: StackTraceElement): String {
        val file = el.fileName ?: el.className.substringAfterLast('.').substringBefore('$')
        val raw = el.methodName.substringBefore('-').substringBefore('$')
        val method = if (raw == "invoke" || raw == "invokeSuspend") {
            el.className.substringAfterLast('.').substringBefore('$')
        } else {
            raw
        }
        return "$file:${el.lineNumber} $method"
    }

    private fun hookCrash() {
        if (!hooked.compareAndSet(false, true)) return
        val prev = Thread.getDefaultUncaughtExceptionHandler()
        Thread.setDefaultUncaughtExceptionHandler { t, e ->
            runCatching { error("PadLog", "uncaught thread=${t.name}", e) }
            prev?.uncaughtException(t, e)
        }
    }

    private val skipMethods = setOf(
        "origin", "format", "skip", "write", "echo",
        "toast", "click", "info", "warn", "error",
        "req", "rsp", "rspBytes", "fail", "get", "post",
        "notify", "rememberLogSite",
        "ActionButton", "HoldButton", "StatusBarItem", "CustomStatusBar",
        "ModeCard", "ActionLine", "PointButton",
    )
}
