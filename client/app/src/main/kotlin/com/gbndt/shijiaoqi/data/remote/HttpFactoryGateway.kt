package com.gbndt.shijiaoqi.data.remote

import com.gbndt.shijiaoqi.data.pouch.TransitClosure
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import java.io.OutputStreamWriter
import java.net.HttpURLConnection
import java.net.URL
import java.net.URLEncoder
import java.util.Base64
import com.gbndt.shijiaoqi.model.FactoryOffer
import java.util.UUID
import com.gbndt.shijiaoqi.data.session.ClientInbox
import com.gbndt.shijiaoqi.data.session.ClientLoginResult
import com.gbndt.shijiaoqi.data.session.ClosureRef
import com.gbndt.shijiaoqi.data.session.FactoryGateway
import com.gbndt.shijiaoqi.data.session.LoginRejected
import com.gbndt.shijiaoqi.data.session.PadDevice
import com.gbndt.shijiaoqi.data.session.PadLoginResult
import com.gbndt.shijiaoqi.data.session.PersonMeta
import com.gbndt.shijiaoqi.data.session.PolicyMeta

class HttpFactoryGateway : FactoryGateway {
    private val json = Json { ignoreUnknownKeys = true }

    override fun registerDevice(baseUrl: String, factoryId: String, clientId: String, serial: String) {
        val code = post(baseUrl, "/v1/factories/$factoryId/clients/$clientId/device", """{"deviceSerial":${jsonStr(serial)}}""").first
        if (code !in 200..299) throw LoginRejected(parseError(code, lastBody))
    }

    override fun loginOnClient(
        baseUrl: String,
        factoryId: String,
        clientId: String,
        serial: String,
        loginName: String,
        password: String,
    ): ClientLoginResult {
        val body = """{"deviceSerial":${jsonStr(serial)},"loginName":${jsonStr(loginName)},"password":${jsonStr(password)}}"""
        val (code, text) = post(baseUrl, "/v1/factories/$factoryId/clients/$clientId/login", body)
        if (code !in 200..299) throw LoginRejected(parseError(code, text))
        val dto = json.decodeFromString(LoginDto.serializer(), text)
        val key = Base64.getDecoder().decode(dto.unwrapKey)
        if (key.size != 32) throw LoginRejected("invalid unwrap key")
        return ClientLoginResult(
            token = dto.token,
            unwrapKey = key,
            person = PersonMeta(UUID.fromString(dto.account.id), dto.account.loginName, dto.account.displayName),
            policy = PolicyMeta(
                revision = dto.policy.revision,
                maxCachedProjects = dto.policy.maxCachedProjects,
                cacheScope = dto.policy.cacheScope,
                persistUnwrapKey = dto.policy.persistUnwrapKey,
                keyTtlSeconds = dto.policy.keyTtlSeconds,
            ),
            mqttUrl = dto.mqttUrl,
            signingPublicKey = decodeB64(dto.signingPublicKey),
            clientShortCode = dto.clientShortCode,
            roles = dto.roles,
        )
    }

    override fun inbox(baseUrl: String, factoryId: String, clientId: String, token: String): ClientInbox {
        val (code, text) = get(baseUrl, "/v1/factories/$factoryId/clients/$clientId/inbox", token)
        if (code !in 200..299) throw LoginRejected(parseError(code, text))
        return decodeInbox(text)
    }

    override fun padInbox(baseUrl: String, factoryId: String, clientId: String, token: String): ClientInbox {
        val (code, text) = get(baseUrl, "/v1/factories/$factoryId/pad/clients/$clientId/inbox", token)
        if (code !in 200..299) throw LoginRejected(parseError(code, text))
        return decodeInbox(text)
    }

    override fun pullClosure(baseUrl: String, factoryId: String, clientId: String, projectId: String, token: String): TransitClosure {
        val (code, text) = get(baseUrl, "/v1/factories/$factoryId/clients/$clientId/closures/$projectId", token)
        if (code !in 200..299) throw LoginRejected(parseError(code, text))
        return decodeTransit(text)
    }

    override fun padPullClosure(baseUrl: String, factoryId: String, clientId: String, projectId: String, token: String): TransitClosure {
        val (code, text) = get(baseUrl, "/v1/factories/$factoryId/pad/clients/$clientId/closures/$projectId", token)
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
        return PadLoginResult(
            token = dto.token,
            person = PersonMeta(UUID.fromString(dto.account.id), dto.account.loginName, dto.account.displayName),
            policy = PolicyMeta(
                revision = dto.policy.revision,
                maxCachedProjects = dto.policy.maxCachedProjects,
                cacheScope = dto.policy.cacheScope,
                persistUnwrapKey = dto.policy.persistUnwrapKey,
                keyTtlSeconds = dto.policy.keyTtlSeconds,
            ),
            mqttUrl = dto.mqttUrl,
            signingPublicKey = decodeB64(dto.signingPublicKey),
            roles = dto.roles,
            devices = dto.devices.map { d ->
                PadDevice(
                    id = d.id,
                    name = d.name,
                    deviceSerial = d.deviceSerial,
                    shortCode = d.shortCode,
                    unwrapKey = decodeB64(d.unwrapKey),
                )
            },
        )
    }

    override fun discover(baseUrl: String, serial: String): List<FactoryOffer> {
        val path = if (serial.isBlank()) "/v1/discover" else {
            val q = URLEncoder.encode(serial, Charsets.UTF_8.name())
            "/v1/discover?deviceSerial=$q"
        }
        val (code, text) = get(baseUrl, path, token = "", connectMs = 800, readMs = 1500)
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

    private fun decodeInbox(text: String): ClientInbox {
        val dto = json.decodeFromString(InboxDto.serializer(), text)
        return ClientInbox(
            policy = dto.policy.toMeta(),
            closures = dto.closures.map {
                ClosureRef(UUID.fromString(it.assetId), it.revision, decodeB64OrNull(it.digest), it.name, it.level)
            },
            signingPublicKey = decodeB64(dto.signingPublicKey),
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

    private var lastBody = ""

    private fun post(baseUrl: String, path: String, body: String): Pair<Int, String> {
        val url = URL(baseUrl.trimEnd('/') + path)
        val conn = (url.openConnection() as HttpURLConnection).apply {
            requestMethod = "POST"
            connectTimeout = 15_000
            readTimeout = 15_000
            doOutput = true
            setRequestProperty("Content-Type", "application/json")
        }
        OutputStreamWriter(conn.outputStream, Charsets.UTF_8).use { it.write(body) }
        val text = (if (conn.responseCode in 200..299) conn.inputStream else conn.errorStream)
            ?.bufferedReader(Charsets.UTF_8)?.readText().orEmpty()
        lastBody = text
        val code = conn.responseCode
        conn.disconnect()
        return code to text
    }

    private fun get(baseUrl: String, path: String, token: String, connectMs: Int = 15_000, readMs: Int = 30_000): Pair<Int, String> {
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
        return code to text
    }

    private fun decodeB64(s: String): ByteArray =
        if (s.isBlank()) ByteArray(0) else Base64.getDecoder().decode(s)

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
    private data class ErrorDto(val error: String = "")

    @Serializable
    private data class PadLoginDto(
        val token: String,
        val account: AccountDto,
        val policy: PolicyDto,
        val mqttUrl: String = "",
        val signingPublicKey: String = "",
        val roles: List<String> = emptyList(),
        val devices: List<PadDeviceDto> = emptyList(),
    )

    @Serializable
    private data class PadDeviceDto(
        val id: String = "",
        val name: String = "",
        val deviceSerial: String = "",
        val shortCode: String = "",
        val unwrapKey: String = "",
    )

    @Serializable
    private data class LoginDto(
        val token: String,
        val unwrapKey: String,
        val account: AccountDto,
        val policy: PolicyDto,
        val mqttUrl: String = "",
        val signingPublicKey: String = "",
        val clientShortCode: String = "",
        val roles: List<String> = emptyList(),
    )

    @Serializable
    private data class AccountDto(val id: String, val loginName: String, val displayName: String)

    @Serializable
    private data class PolicyDto(
        val revision: Long = 0,
        val maxCachedProjects: Int = 2,
        val cacheScope: String = "all",
        val persistUnwrapKey: Boolean = false,
        val keyTtlSeconds: Long = 0,
    ) {
        fun toMeta() = PolicyMeta(revision, maxCachedProjects, cacheScope, persistUnwrapKey, keyTtlSeconds)
    }

    @Serializable
    private data class InboxDto(
        val policy: PolicyDto,
        val closures: List<ClosureRefDto> = emptyList(),
        val signingPublicKey: String = "",
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
        val wrap: String,
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
    )

    @Serializable
    private data class DepDto(
        val id: String,
        val revision: Long = 0,
        val digest: String = "",
    )
}
