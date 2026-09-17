package com.gbndt.shijiaoqi.data.remote

import com.gbndt.shijiaoqi.data.pouch.TransitClosure
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import java.io.OutputStreamWriter
import java.net.HttpURLConnection
import java.net.URL
import java.util.Base64
import com.gbndt.shijiaoqi.model.FactoryOffer
import java.util.UUID
import com.gbndt.shijiaoqi.data.session.ClientInbox
import com.gbndt.shijiaoqi.data.session.ClosureRef
import com.gbndt.shijiaoqi.data.session.FactoryGateway
import com.gbndt.shijiaoqi.data.session.LoginRejected
import com.gbndt.shijiaoqi.data.session.PadDevice
import com.gbndt.shijiaoqi.data.session.PadLoginResult
import com.gbndt.shijiaoqi.data.session.PersonMeta
import com.gbndt.shijiaoqi.data.session.PolicyMeta
import com.gbndt.shijiaoqi.data.session.RemoteAsset
import com.gbndt.shijiaoqi.data.log.HttpLog

class HttpFactoryGateway : FactoryGateway {
    private val json = Json { ignoreUnknownKeys = true }

    override fun padInbox(baseUrl: String, factoryId: String, token: String): ClientInbox {
        val (code, text) = get(baseUrl, "/v1/factories/$factoryId/pad/inbox", token)
        if (code !in 200..299) throw LoginRejected(parseError(code, text))
        return decodeInbox(text)
    }

    override fun padPullClosure(baseUrl: String, factoryId: String, assetId: String, token: String): TransitClosure {
        val (code, text) = get(baseUrl, "/v1/factories/$factoryId/pad/closures/$assetId", token)
        if (code !in 200..299) throw LoginRejected(parseError(code, text))
        return decodeTransit(text)
    }

    override fun loginPad(
        baseUrl: String,
        factoryId: String,
        loginName: String,
        password: String,
    ): PadLoginResult {
        val body = """{"loginName":${jsonStr(loginName)},"password":${jsonStr(password)}}"""
        val (code, text) = post(baseUrl, "/v1/factories/$factoryId/pad/login", body)
        if (code !in 200..299) throw LoginRejected(parseError(code, text))
        val dto = json.decodeFromString(PadLoginDto.serializer(), text)
        val key = decodeB64(dto.unwrapKey)
        if (key.size != 32) throw LoginRejected("invalid unwrap key")
        return PadLoginResult(
            token = dto.token,
            unwrapKey = key,
            person = PersonMeta(UUID.fromString(dto.account.id), dto.account.loginName, dto.account.displayName),
            policy = dto.policy.toMeta(),
            roles = dto.roles,
            devices = dto.devices.map { d ->
                PadDevice(
                    id = d.id,
                    name = d.name,
                    deviceSerial = d.deviceSerial,
                    shortCode = d.shortCode,
                )
            },
        )
    }

    override fun discover(baseUrl: String): List<FactoryOffer> {
        val (code, text) = get(baseUrl, "/v1/discover", token = "", connectMs = 400, readMs = 800)
        if (code !in 200..299) throw LoginRejected(parseError(code, text))
        val dto = json.decodeFromString(DiscoverDto.serializer(), text)
        val httpBase = dto.httpBase.ifBlank { baseUrl.trimEnd('/') }
        return dto.factories.map { f ->
            FactoryOffer(
                httpBase = httpBase,
                factoryId = f.factoryId,
                status = f.status,
                belongs = f.belongs,
                clientId = f.clientId,
                clientName = f.clientName,
            )
        }
    }

    override fun changePassword(baseUrl: String, factoryId: String, token: String, password: String) {
        val (code, text) = post(baseUrl, "/v1/factories/$factoryId/me/password", """{"password":${jsonStr(password)}}""", token)
        if (code !in 200..299) throw LoginRejected(parseError(code, text))
    }

    override fun getAsset(baseUrl: String, factoryId: String, token: String, assetId: String): RemoteAsset? {
        val (code, text) = get(baseUrl, "/v1/factories/$factoryId/assets/$assetId", token)
        if (code == 404) return null
        if (code !in 200..299) throw LoginRejected(parseError(code, text))
        return decodeAsset(text)
    }

    override fun createPadAsset(
        baseUrl: String,
        factoryId: String,
        token: String,
        kind: String,
        name: String,
        content: String,
        id: String,
        code: String,
        deps: List<com.gbndt.shijiaoqi.data.pouch.AssetDep>,
    ): RemoteAsset {
        val body = buildString {
            append("{\"kind\":${jsonStr(kind)},\"name\":${jsonStr(name)},\"content\":${jsonStr(content)}")
            if (id.isNotBlank()) append(",\"id\":${jsonStr(id)}")
            if (code.isNotBlank()) append(",\"code\":${jsonStr(code)}")
            append(",\"deps\":"); append(depsJson(deps)); append('}')
        }
        val (http, text) = post(baseUrl, "/v1/factories/$factoryId/pad/assets", body, token)
        if (http !in 200..299) throw LoginRejected(parseError(http, text))
        return decodeAsset(text)
    }

    override fun updateAssetContent(
        baseUrl: String,
        factoryId: String,
        token: String,
        assetId: String,
        expected: Long,
        content: String,
    ): RemoteAsset {
        val body = """{"expected":$expected,"content":${jsonStr(content)}}"""
        val (http, text) = post(baseUrl, "/v1/factories/$factoryId/assets/$assetId/content", body, token)
        if (http !in 200..299) throw LoginRejected(parseError(http, text))
        return decodeAsset(text)
    }

    override fun setAssetDeps(
        baseUrl: String,
        factoryId: String,
        token: String,
        assetId: String,
        expected: Long,
        deps: List<com.gbndt.shijiaoqi.data.pouch.AssetDep>,
    ): RemoteAsset {
        val body = """{"expected":$expected,"deps":${depsJson(deps)}}"""
        val (http, text) = post(baseUrl, "/v1/factories/$factoryId/assets/$assetId/deps", body, token)
        if (http !in 200..299) throw LoginRejected(parseError(http, text))
        return decodeAsset(text)
    }

    private fun decodeAsset(text: String): RemoteAsset {
        val dto = json.decodeFromString(AssetDto.serializer(), text)
        return RemoteAsset(
            id = UUID.fromString(dto.id),
            kind = dto.kind,
            level = dto.level,
            name = dto.name,
            code = dto.code,
            status = dto.status,
            copyable = dto.copyable,
            revision = dto.revision,
            digest = decodeB64(dto.digest),
            deps = dto.deps.map { d ->
                com.gbndt.shijiaoqi.data.pouch.AssetDep(UUID.fromString(d.id), d.revision, decodeB64(d.digest))
            },
        )
    }

    private fun depsJson(deps: List<com.gbndt.shijiaoqi.data.pouch.AssetDep>): String = buildString {
        append('[')
        deps.forEachIndexed { i, d ->
            if (i > 0) append(',')
            append("{\"id\":${jsonStr(d.id.toString())},\"revision\":${d.revision},\"digest\":${jsonStr(encodeB64(d.digest))}}")
        }
        append(']')
    }

    private fun decodeInbox(text: String): ClientInbox {
        val dto = json.decodeFromString(InboxDto.serializer(), text)
        return ClientInbox(
            policy = dto.policy.toMeta(),
            closures = dto.closures.map {
                ClosureRef(UUID.fromString(it.assetId), it.revision, decodeB64OrNull(it.digest), it.name, it.level)
            },
        )
    }

    private fun decodeTransit(text: String): TransitClosure {
        val dto = json.decodeFromString(TransitDto.serializer(), text)
        val members = dto.snapshot.members.map { m ->
            com.gbndt.shijiaoqi.data.pouch.TransitMember(
                id = UUID.fromString(m.id),
                level = m.level,
                name = m.name,
                revision = m.revision,
                ownerId = m.creatorId?.let(UUID::fromString),
                content = decodeB64(m.content),
                kind = m.kind,
                status = m.status,
                digest = decodeB64(m.digest),
                deps = m.deps.map { d ->
                    com.gbndt.shijiaoqi.data.pouch.AssetDep(
                        UUID.fromString(d.id),
                        d.revision,
                        decodeB64(d.digest),
                    )
                },
                code = m.code,
                copyable = m.copyable ?: (m.level != "platform"),
            )
        }
        return TransitClosure(
            wrap = decodeB64(dto.wrap),
            members = members,
            assetId = UUID.fromString(dto.snapshot.assetId),
            revision = dto.snapshot.revision,
            kind = dto.snapshot.kind,
            level = dto.snapshot.level,
            status = dto.snapshot.status,
            digest = decodeB64(dto.snapshot.digest),
            targetClientId = dto.snapshot.targetClientId?.let(UUID::fromString),
        )
    }

    private fun post(baseUrl: String, path: String, body: String, token: String = ""): Pair<Int, String> {
        HttpLog.req("POST", path, body)
        return try {
            val url = URL(baseUrl.trimEnd('/') + path)
            val conn = (url.openConnection() as HttpURLConnection).apply {
                requestMethod = "POST"
                connectTimeout = 15_000
                readTimeout = 15_000
                doOutput = true
                setRequestProperty("Content-Type", "application/json")
                if (token.isNotBlank()) {
                    setRequestProperty("Authorization", "Bearer $token")
                }
            }
            OutputStreamWriter(conn.outputStream, Charsets.UTF_8).use { it.write(body) }
            val text = (if (conn.responseCode in 200..299) conn.inputStream else conn.errorStream)
                ?.bufferedReader(Charsets.UTF_8)?.readText().orEmpty()
            val code = conn.responseCode
            conn.disconnect()
            HttpLog.rsp("POST", path, code, text)
            code to text
        } catch (e: Exception) {
            HttpLog.fail("POST", path, e.javaClass.simpleName)
            throw e
        }
    }

    private fun get(baseUrl: String, path: String, token: String, connectMs: Int = 15_000, readMs: Int = 30_000): Pair<Int, String> {
        val quiet = HttpLog.isDiscover(path)
        if (!quiet) HttpLog.req("GET", path)
        return try {
            val url = URL(baseUrl.trimEnd('/') + path)
            val conn = (url.openConnection() as HttpURLConnection).apply {
                requestMethod = "GET"
                connectTimeout = connectMs
                readTimeout = readMs
                if (token.isNotBlank()) {
                    setRequestProperty("Authorization", "Bearer $token")
                }
            }
            val text = (if (conn.responseCode in 200..299) conn.inputStream else conn.errorStream)
                ?.bufferedReader(Charsets.UTF_8)?.readText().orEmpty()
            val code = conn.responseCode
            conn.disconnect()
            // 扫网段时大量 404/拒绝不记，命中厂服 2xx 才记回包。
            if (!quiet || code in 200..299) HttpLog.rsp("GET", path, code, text)
            code to text
        } catch (e: Exception) {
            HttpLog.fail("GET", path, e.javaClass.simpleName, quiet = quiet)
            throw e
        }
    }

    private fun decodeB64(s: String): ByteArray =
        if (s.isBlank()) ByteArray(0) else Base64.getDecoder().decode(s)

    private fun encodeB64(b: ByteArray): String =
        if (b.isEmpty()) "" else Base64.getEncoder().encodeToString(b)

    private fun decodeB64OrNull(s: String?): ByteArray? =
        if (s.isNullOrBlank()) null else Base64.getDecoder().decode(s)

    private fun parseError(code: Int, text: String): String {
        val err = runCatching { json.decodeFromString(ErrorDto.serializer(), text).error }.getOrNull()
        return err?.ifBlank { null } ?: "http $code"
    }

    private fun jsonStr(s: String): String = buildString {
        append('"')
        s.forEach { c ->
            when (c) {
                '\\' -> append("\\\\")
                '"' -> append("\\\"")
                '\n' -> append("\\n")
                '\r' -> append("\\r")
                else -> append(c)
            }
        }
        append('"')
    }

    @Serializable
    private data class DiscoverDto(
        val status: String = "",
        val httpBase: String = "",
        val factories: List<DiscoverFactoryDto> = emptyList(),
    )

    @Serializable
    private data class DiscoverFactoryDto(
        val factoryId: String = "",
        val status: String = "",
        val belongs: Boolean = false,
        val clientId: String = "",
        val clientName: String = "",
    )

    @Serializable
    private data class AssetDto(
        val id: String,
        val kind: String = "",
        val level: String = "",
        val name: String = "",
        val code: String = "",
        val status: String = "",
        val copyable: Boolean = true,
        val revision: Long = 0,
        val digest: String = "",
        val deps: List<DepDto> = emptyList(),
    )

    @Serializable
    private data class ErrorDto(val error: String = "")

    @Serializable
    private data class PadLoginDto(
        val token: String,
        val unwrapKey: String = "",
        val account: AccountDto,
        val policy: PolicyDto,
        val roles: List<String> = emptyList(),
        val devices: List<PadDeviceDto> = emptyList(),
    )

    @Serializable
    private data class PadDeviceDto(
        val id: String = "",
        val name: String = "",
        val deviceSerial: String = "",
        val shortCode: String = "",
    )

    @Serializable
    private data class AccountDto(val id: String, val loginName: String, val displayName: String)

    @Serializable
    private data class PolicyDto(
        val revision: Long = 0,
        val persistUnwrapKey: Boolean = false,
        val keyTtlSeconds: Long = 0,
        val encryptPouch: Boolean = true,
    ) {
        fun toMeta() = PolicyMeta(revision, persistUnwrapKey, keyTtlSeconds, encryptPouch)
    }

    @Serializable
    private data class InboxDto(
        val policy: PolicyDto,
        val closures: List<ClosureRefDto> = emptyList(),
    )

    @Serializable
    private data class ClosureRefDto(
        val assetId: String,
        val revision: Long = 0,
        val digest: String? = null,
        val name: String = "",
        val level: String = "",
    )

    @Serializable
    private data class TransitDto(
        val wrap: String = "",
        val snapshot: SnapshotDto,
    )

    @Serializable
    private data class SnapshotDto(
        val assetId: String,
        val revision: Long = 0,
        val kind: String = "project",
        val level: String = "",
        val status: String = "",
        val digest: String = "",
        val targetClientId: String? = null,
        val members: List<MemberDto> = emptyList(),
    )

    @Serializable
    private data class MemberDto(
        val id: String,
        val kind: String = "",
        val level: String = "",
        val name: String = "",
        val status: String = "",
        val revision: Long = 0,
        val content: String = "",
        val digest: String = "",
        val creatorId: String? = null,
        val deps: List<DepDto> = emptyList(),
        val code: String = "",
        val copyable: Boolean? = null,
    )

    @Serializable
    private data class DepDto(
        val id: String,
        val revision: Long = 0,
        val digest: String = "",
    )
}
