package com.gbndt.shijiaoqi.data.pouch

import kotlinx.serialization.Serializable
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import java.util.Base64
import java.util.UUID

/** 袋内拒绝：默认拒绝，调用方按 code 处理。 */
class PouchRejected(val code: String) : Exception(code)

/** 工程钉死的一条工艺依赖：身份、修订和当时摘要。 */
data class AssetDep(
    val id: UUID, // 被依赖工艺稳定身份
    val revision: Long, // 钉死的工艺修订
    val digest: ByteArray, // 当时该修订的 SHA-256
)

/** 闭包里一条资产的明文快照，仅内存使用。 */
data class ClosureMemberPlain(
    val id: UUID, // 稳定身份
    val kind: String, // process / project
    val level: String, // platform / factory / personal
    val name: String, // 显示名
    val status: String, // 送达时状态
    val revision: Long, // 钉死修订
    val content: ByteArray, // 明文正文，用完即清
    val digest: ByteArray, // 内容 SHA-256
    val deps: List<AssetDep> = emptyList(), // 工艺必须空
    val ownerId: UUID? = null, // 个人级创建人；其余为空
    val code: String = "", // 只读编号，跟身份走
)

/** 一份工程的完整明文快照，不是新身份。 */
data class ClosureSnapshotPlain(
    val kind: String, // 固定 project
    val assetId: UUID, // 根工程身份
    val revision: Long, // 根修订
    val level: String, // 与源相同
    val status: String, // 与源相同
    val digest: ByteArray, // 整包 SHA-256
    val targetClientId: UUID?, // 历史字段，工程不再发给某台设备
    val members: List<ClosureMemberPlain>, // 根在前，其余按 deps 顺序
)

/** 已缓存工程成员元数据；正文在信封密文里。 */
data class CachedMember(
    val id: UUID, // 稳定身份
    val kind: String, // process / project
    val level: String, // platform / factory / personal
    val name: String, // 显示名
    val status: String, // 送达时状态
    val revision: Long, // 钉死修订
    val digest: ByteArray, // 内容 SHA-256
    val deps: List<AssetDep> = emptyList(), // 工艺必须空
    val ownerId: UUID? = null, // 个人级创建人；其余为空
    val code: String = "", // 只读编号，跟身份走
)

/** 已缓存工程闭包元数据，不含工艺正文。 */
data class CachedClosure(
    val assetId: UUID, // 工程稳定身份
    val revision: Long, // 下发修订
    val kind: String, // 固定 project
    val level: String, // factory / personal / platform
    val status: String, // 送达时状态，与源相同
    val name: String, // 显示名
    val digest: ByteArray, // 整包 SHA-256
    val targetClientId: UUID?, // 历史字段，工程不再发给某台设备
    val members: List<CachedMember>, // 根在前，其余按 deps 顺序
)

/** 工程元数据编解码；不带正文。 */
internal object ClosureCodec {
    private val json = Json { ignoreUnknownKeys = true }

    /** 把工程元数据编成 JSON，不含正文。 */
    fun encode(list: List<CachedClosure>): String =
        json.encodeToString(list.map { it.toDto() })

    /** 从 JSON 还原工程元数据；空串当没有缓存。 */
    fun decode(raw: String): List<CachedClosure> {
        if (raw.isBlank()) return emptyList()
        return json.decodeFromString<List<ClosureMetaDto>>(raw).map { it.toModel() }
    }

    @Serializable
    private data class DepDto(
        val id: String, // 被依赖工艺身份
        val revision: Long = 0, // 钉死修订
        val digest: String = "", // 当时摘要，Base64
    )

    @Serializable
    private data class MemberMetaDto(
        val id: String, // 成员资产身份
        val kind: String = "", // process / project
        val level: String = "", // platform / factory / personal
        val name: String = "", // 显示名
        val status: String = "", // 送达时状态
        val revision: Long = 0, // 钉死修订
        val digest: String = "", // 内容摘要，Base64
        val deps: List<DepDto> = emptyList(), // 工艺必须空
        val ownerId: String? = null, // 个人级创建人；其余为空
        val code: String = "", // 只读编号，跟身份走
    )

    @Serializable
    private data class ClosureMetaDto(
        val assetId: String, // 工程稳定身份
        val revision: Long = 0, // 下发修订
        val kind: String = "", // 固定 project
        val level: String = "", // factory / personal / platform
        val status: String = "", // 送达时状态
        val name: String = "", // 显示名
        val digest: String = "", // 整包摘要，Base64
        val targetClientId: String? = null, // 这份包发给哪台 Client
        val members: List<MemberMetaDto> = emptyList(), // 根在前
    )

    private fun CachedClosure.toDto() = ClosureMetaDto(
        assetId = assetId.toString(),
        revision = revision,
        kind = kind,
        level = level,
        status = status,
        name = name,
        digest = b64(digest),
        targetClientId = targetClientId?.toString(),
        members = members.map {
            MemberMetaDto(
                id = it.id.toString(),
                kind = it.kind,
                level = it.level,
                name = it.name,
                status = it.status,
                revision = it.revision,
                digest = b64(it.digest),
                deps = it.deps.map { d -> DepDto(d.id.toString(), d.revision, b64(d.digest)) },
                ownerId = it.ownerId?.toString(),
                code = it.code,
            )
        },
    )

    private fun ClosureMetaDto.toModel() = CachedClosure(
        assetId = UUID.fromString(assetId),
        revision = revision,
        kind = kind,
        level = level,
        status = status,
        name = name,
        digest = unb64(digest),
        targetClientId = targetClientId?.let(UUID::fromString),
        members = members.map {
            CachedMember(
                id = UUID.fromString(it.id),
                kind = it.kind,
                level = it.level,
                name = it.name,
                status = it.status,
                revision = it.revision,
                digest = unb64(it.digest),
                deps = it.deps.map { d -> AssetDep(UUID.fromString(d.id), d.revision, unb64(d.digest)) },
                ownerId = it.ownerId?.let(UUID::fromString),
                code = it.code,
            )
        },
    )

    private fun b64(b: ByteArray): String =
        if (b.isEmpty()) "" else Base64.getEncoder().encodeToString(b)

    private fun unb64(s: String): ByteArray =
        if (s.isBlank()) ByteArray(0) else Base64.getDecoder().decode(s)
}
