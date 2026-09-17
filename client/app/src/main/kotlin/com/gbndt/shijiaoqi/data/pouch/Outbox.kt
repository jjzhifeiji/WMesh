package com.gbndt.shijiaoqi.data.pouch

import kotlinx.serialization.Serializable
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import java.util.Base64
import java.util.UUID

/** 本机新建的个人级；编号跟人看，身份仍是 UUID。 */
data class IssuedAsset(
    val id: UUID, // 稳定身份
    val kind: String, // process / project
    val code: String, // 只读编号，创建后不改
    val level: String, // 个人级
)

/** 待汇聚运行事实，尚未进厂库。 */
data class PendingFact(
    val id: UUID, // 事实身份
    val creatorId: UUID, // 创建人
)

/** 待发点云/图片记录，不含工艺正文。 */
data class PendingUploadRecord(
    val id: UUID, // 待发记录身份
    val kind: String, // point_cloud / image
    val digest: ByteArray, // 内容摘要
    val creatorId: UUID, // 创建人
    val clientId: UUID, // 当时绑定的 Client
)

internal data class HeldUpload(
    val rec: PendingUploadRecord,
    val content: ByteArray,
)

internal object LedgerCodec {
    private val json = Json { ignoreUnknownKeys = true }

    fun encode(
        origin: String,
        nextProcess: Long,
        nextProject: Long,
        codes: Map<UUID, String>,
        facts: List<PendingFact>,
        uploads: List<HeldUpload>,
        dirty: List<UUID> = emptyList(),
        copyable: Map<UUID, Boolean> = emptyMap(),
        synced: Map<UUID, ByteArray> = emptyMap(),
    ): String = json.encodeToString(
        LedgerDto(
            origin = origin,
            nextProcess = nextProcess,
            nextProject = nextProject,
            codes = codes.map { CodeDto(it.key.toString(), it.value) },
            facts = facts.map { FactDto(it.id.toString(), it.creatorId.toString()) },
            uploads = uploads.map {
                UploadDto(
                    id = it.rec.id.toString(),
                    kind = it.rec.kind,
                    digest = b64(it.rec.digest),
                    creatorId = it.rec.creatorId.toString(),
                    clientId = it.rec.clientId.toString(),
                    content = b64(it.content),
                )
            },
            dirty = dirty.map { it.toString() },
            copyable = copyable.map { FlagDto(it.key.toString(), it.value) },
            synced = synced.map { HashDto(it.key.toString(), b64(it.value)) },
        ),
    )

    fun decode(raw: String): LedgerState {
        if (raw.isBlank()) return LedgerState()
        val dto = json.decodeFromString(LedgerDto.serializer(), raw)
        val codes = LinkedHashMap<UUID, String>()
        dto.codes.forEach { codes[UUID.fromString(it.id)] = it.code }
        return LedgerState(
            origin = dto.origin,
            nextProcess = if (dto.nextProcess < 1) 1L else dto.nextProcess,
            nextProject = if (dto.nextProject < 1) 1L else dto.nextProject,
            codes = codes,
            facts = dto.facts.map { PendingFact(UUID.fromString(it.id), UUID.fromString(it.creatorId)) },
            uploads = dto.uploads.map {
                val content = unb64(it.content)
                HeldUpload(
                    PendingUploadRecord(
                        id = UUID.fromString(it.id),
                        kind = it.kind,
                        digest = unb64(it.digest),
                        creatorId = UUID.fromString(it.creatorId),
                        clientId = UUID.fromString(it.clientId),
                    ),
                    content,
                )
            },
            dirty = dto.dirty.mapNotNull { runCatching { UUID.fromString(it) }.getOrNull() },
            copyable = dto.copyable.associate { UUID.fromString(it.id) to it.copyable },
            synced = dto.synced.associate { UUID.fromString(it.id) to unb64(it.digest) },
        )
    }

    @Serializable
    private data class LedgerDto(
        val origin: String = "",
        val nextProcess: Long = 1,
        val nextProject: Long = 1,
        val codes: List<CodeDto> = emptyList(),
        val facts: List<FactDto> = emptyList(),
        val uploads: List<UploadDto> = emptyList(),
        val dirty: List<String> = emptyList(),
        val copyable: List<FlagDto> = emptyList(),
        val synced: List<HashDto> = emptyList(),
    )

    @Serializable
    private data class CodeDto(val id: String, val code: String)

    @Serializable
    private data class FactDto(val id: String, val creatorId: String)

    @Serializable
    private data class UploadDto(
        val id: String,
        val kind: String = "",
        val digest: String = "",
        val creatorId: String,
        val clientId: String,
        val content: String = "",
    )

    @Serializable
    private data class FlagDto(val id: String, val copyable: Boolean = true)

    @Serializable
    private data class HashDto(val id: String, val digest: String = "")

    private fun b64(b: ByteArray): String =
        if (b.isEmpty()) "" else Base64.getEncoder().encodeToString(b)

    private fun unb64(s: String): ByteArray =
        if (s.isBlank()) ByteArray(0) else Base64.getDecoder().decode(s)
}

internal data class LedgerState(
    val origin: String = "", // 本机短号前缀
    val nextProcess: Long = 1, // 下一工艺序号
    val nextProject: Long = 1, // 下一工程序号
    val codes: Map<UUID, String> = emptyMap(), // 已发出的只读编号
    val facts: List<PendingFact> = emptyList(), // 待汇聚事实
    val uploads: List<HeldUpload> = emptyList(), // 待发点云/图片
    val dirty: List<UUID> = emptyList(), // 本机已改未同步
    val copyable: Map<UUID, Boolean> = emptyMap(), // 可否另存
    val synced: Map<UUID, ByteArray> = emptyMap(), // 上次对齐的内容摘要
)
