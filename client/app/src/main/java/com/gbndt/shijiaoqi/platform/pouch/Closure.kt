package com.gbndt.shijiaoqi.platform.pouch

import kotlinx.serialization.Serializable
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import java.util.Base64
import java.util.UUID

class PouchRejected(val code: String) : Exception(code)

data class AssetDep(
    val id: UUID,
    val revision: Long,
    val digest: ByteArray,
)

data class ClosureMemberPlain(
    val id: UUID,
    val kind: String,
    val level: String,
    val name: String,
    val status: String,
    val revision: Long,
    val content: ByteArray,
    val digest: ByteArray,
    val deps: List<AssetDep> = emptyList(),
    val ownerId: UUID? = null,
    val code: String = "",
)

data class ClosureSnapshotPlain(
    val kind: String,
    val assetId: UUID,
    val revision: Long,
    val level: String,
    val status: String,
    val digest: ByteArray,
    val targetClientId: UUID?,
    val members: List<ClosureMemberPlain>,
)

data class CachedMember(
    val id: UUID,
    val kind: String,
    val level: String,
    val name: String,
    val status: String,
    val revision: Long,
    val digest: ByteArray,
    val deps: List<AssetDep> = emptyList(),
    val ownerId: UUID? = null,
    val code: String = "",
)

data class CachedClosure(
    val assetId: UUID,
    val revision: Long,
    val kind: String,
    val level: String,
    val status: String,
    val name: String,
    val digest: ByteArray,
    val targetClientId: UUID?,
    val members: List<CachedMember>,
)

internal object ClosureCodec {
    private val json = Json { ignoreUnknownKeys = true }

    fun encode(list: List<CachedClosure>): String =
        json.encodeToString(list.map { it.toDto() })

    fun decode(raw: String): List<CachedClosure> {
        if (raw.isBlank()) return emptyList()
        return json.decodeFromString<List<ClosureMetaDto>>(raw).map { it.toModel() }
    }

    @Serializable
    private data class DepDto(val id: String, val revision: Long = 0, val digest: String = "")

    @Serializable
    private data class MemberMetaDto(
        val id: String,
        val kind: String = "",
        val level: String = "",
        val name: String = "",
        val status: String = "",
        val revision: Long = 0,
        val digest: String = "",
        val deps: List<DepDto> = emptyList(),
        val ownerId: String? = null,
        val code: String = "",
    )

    @Serializable
    private data class ClosureMetaDto(
        val assetId: String,
        val revision: Long = 0,
        val kind: String = "",
        val level: String = "",
        val status: String = "",
        val name: String = "",
        val digest: String = "",
        val targetClientId: String? = null,
        val members: List<MemberMetaDto> = emptyList(),
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
