package com.gbndt.shijiaoqi.data.log

import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import java.io.File

/** 一条待推记录：标签在上送时补，这里只有级别、时间和正文。 */
@Serializable
data class LogRec(
    val ts: String, // Loki 纳秒时间戳
    val level: String, // info / warn / error
    val line: String, // 一行正文，不含密钥和工艺正文
)

/** 已切出、待推或失败留下的一批文件。 */
data class LogBatch(
    val files: List<File>, // 本次占用的 pend 文件
    val records: List<LogRec>, // 文件里解出的记录
)

/** 本机 warn/error 环形文件；满了丢最旧。不跟解封钥绑。 */
class LogRing(
    private val dir: File,
    private val maxTotalBytes: Long = 1_000_000L,
) {
    private val lock = Any()
    private val json = Json { ignoreUnknownKeys = true }

    /** 追加一行；超容先丢掉最旧的 pend。 */
    fun append(rec: LogRec) {
        synchronized(lock) {
            dir.mkdirs()
            dropOldestIfNeeded()
            current().appendText(json.encodeToString(LogRec.serializer(), rec) + "\n", Charsets.UTF_8)
        }
    }

    /** 把 current 切成 pend，连同未推成功的旧 pend 一起交出。 */
    fun take(): LogBatch {
        synchronized(lock) {
            dir.mkdirs()
            val cur = current()
            if (cur.length() > 0) {
                val pend = File(dir, "pend-${System.nanoTime()}.ndjson")
                if (!cur.renameTo(pend) && cur.exists()) {
                    pend.writeText(cur.readText(Charsets.UTF_8), Charsets.UTF_8)
                    cur.writeText("", Charsets.UTF_8)
                }
            }
            val files = pendFiles()
            val records = files.flatMap { parseFile(it) }
            return LogBatch(files, records)
        }
    }

    /** 只删这一批；推失败不要调用，下次 take 还会带上。 */
    fun ack(batch: LogBatch) {
        synchronized(lock) {
            batch.files.forEach { runCatching { it.delete() } }
        }
    }

    private fun current(): File = File(dir, "current.ndjson")

    private fun pendFiles(): List<File> =
        dir.listFiles { _, name -> name.startsWith("pend-") && name.endsWith(".ndjson") }
            ?.sortedBy { it.name }
            .orEmpty()

    private fun parseFile(file: File): List<LogRec> =
        file.readLines(Charsets.UTF_8).mapNotNull { line ->
            val t = line.trim()
            if (t.isEmpty()) null else runCatching { json.decodeFromString(LogRec.serializer(), t) }.getOrNull()
        }

    private fun dropOldestIfNeeded() {
        val cur = current()
        if (cur.length() > maxTotalBytes / 2 && cur.length() > 0) {
            val pend = File(dir, "pend-${System.nanoTime()}.ndjson")
            if (!cur.renameTo(pend)) return
        }
        var total = dir.listFiles()?.sumOf { it.length() } ?: 0L
        for (f in pendFiles()) {
            if (total <= maxTotalBytes) return
            val n = f.length()
            if (f.delete()) total -= n
        }
    }
}
