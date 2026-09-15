package com.gbndt.shijiaoqi.platform.session

import com.gbndt.shijiaoqi.platform.pouch.TransitClosure
import java.util.UUID

data class ClientLoginResult(
    val token: String,
    val unwrapKey: ByteArray,
    val person: PersonMeta,
    val policy: PolicyMeta,
    val mqttUrl: String = "",
    val signingPublicKey: ByteArray = ByteArray(0),
    val clientShortCode: String = "",
    val roles: List<String> = emptyList(),
)

data class ClosureRef(
    val assetId: UUID,
    val revision: Long,
    val digest: ByteArray?,
    val name: String,
    val level: String,
)

data class ClientInbox(
    val policy: PolicyMeta,
    val closures: List<ClosureRef>,
    val signingPublicKey: ByteArray,
)

interface FactoryGateway {
    fun registerDevice(baseUrl: String, factoryId: String, clientId: String, serial: String)
    fun loginOnClient(
        baseUrl: String,
        factoryId: String,
        clientId: String,
        serial: String,
        loginName: String,
        password: String,
    ): ClientLoginResult

    fun inbox(baseUrl: String, factoryId: String, clientId: String, token: String): ClientInbox
    fun pullClosure(baseUrl: String, factoryId: String, clientId: String, projectId: String, token: String): TransitClosure
}

class LoginRejected(val code: String) : Exception(code)
