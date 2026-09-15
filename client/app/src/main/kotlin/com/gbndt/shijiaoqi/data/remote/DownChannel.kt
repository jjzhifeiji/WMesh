package com.gbndt.shijiaoqi.data.remote

import com.gbndt.shijiaoqi.data.pouch.Pouch
import com.gbndt.shijiaoqi.data.pouch.PouchRejected
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import org.eclipse.paho.client.mqttv3.IMqttDeliveryToken
import org.eclipse.paho.client.mqttv3.MqttCallback
import org.eclipse.paho.client.mqttv3.MqttClient
import org.eclipse.paho.client.mqttv3.MqttConnectOptions
import org.eclipse.paho.client.mqttv3.MqttMessage
import org.eclipse.paho.client.mqttv3.persist.MemoryPersistence
import java.net.URI
import java.util.UUID
import java.util.concurrent.atomic.AtomicBoolean
import com.gbndt.shijiaoqi.data.session.DownIntent
import com.gbndt.shijiaoqi.data.session.DownIntents
import com.gbndt.shijiaoqi.data.session.EnvelopeStore
import com.gbndt.shijiaoqi.data.session.FactoryGateway
import com.gbndt.shijiaoqi.data.session.PolicyMeta
import com.gbndt.shijiaoqi.data.session.persistPouch

data class LiveChannel(
    val baseUrl: String,
    val factoryId: UUID,
    val clientId: UUID,
    val token: String,
    val mqttUrl: String,
    val signingPub: ByteArray,
    val pouch: Pouch,
    val store: EnvelopeStore,
)

interface DownChannel {
    fun start(live: LiveChannel)
    fun stop()
}

class NoopDownChannel : DownChannel {
    override fun start(live: LiveChannel) {}
    override fun stop() {}
}

/** HTTPS 对账加 MQTT 订自己的 down；正文只走 HTTPS。 */
object ChannelSync {
    private val json = Json { ignoreUnknownKeys = true }

    fun pullInbox(factory: FactoryGateway, live: LiveChannel) {
        val box = factory.inbox(live.baseUrl, live.factoryId.toString(), live.clientId.toString(), live.token)
        applyPolicyIfNewer(live, box.policy)
        for (ref in box.closures) {
            pullIfNewer(factory, live, ref.assetId, ref.revision)
        }
    }

    fun onDown(factory: FactoryGateway, live: LiveChannel, raw: String, ack: (String) -> Unit) {
        if (DownIntents.hasBody(raw)) return
        val dto = runCatching { json.decodeFromString(IntentDto.serializer(), raw) }.getOrNull() ?: return
        val intent = dto.toIntent()
        if (live.signingPub.isNotEmpty() && !DownIntents.verify(live.signingPub, live.factoryId, live.clientId, intent)) {
            return
        }
        when (intent.typ) {
            DownIntents.TYP_POLICY -> {
                val held = live.store.loadPolicy()?.revision ?: 0L
                if (!DownIntents.shouldApply(held, intent.revision)) return
                applyPolicyIfNewer(
                    live,
                    PolicyMeta(intent.revision, intent.maxCachedProjects.coerceAtLeast(1), intent.cacheScope.ifBlank { "all" }, intent.persistUnwrapKey, intent.keyTtlSeconds),
                )
                ack(DownIntents.ackJson(null, intent.revision))
            }
            DownIntents.TYP_CLOSURE -> {
                val id = intent.assetId ?: return
                pullIfNewer(factory, live, UUID.fromString(id), intent.revision)
                ack(DownIntents.ackJson(id, intent.revision))
            }
        }
    }

    private fun applyPolicyIfNewer(live: LiveChannel, policy: PolicyMeta) {
        val held = live.store.loadPolicy()?.revision ?: 0L
        if (!DownIntents.shouldApply(held, policy.revision)) return
        live.store.savePolicy(policy)
        live.pouch.setPolicy(policy.maxCachedProjects, policy.cacheScope)
        persistPouch(live.store, live.pouch)
    }

    private fun pullIfNewer(factory: FactoryGateway, live: LiveChannel, projectId: UUID, revision: Long) {
        if (live.pouch.hasClosure(projectId) && !DownIntents.shouldApply(live.pouch.revisionOf(projectId), revision)) return
        val t = factory.pullClosure(live.baseUrl, live.factoryId.toString(), live.clientId.toString(), projectId.toString(), live.token)
        try {
            live.pouch.cacheTransit(live.factoryId, live.clientId, t)
            persistPouch(live.store, live.pouch)
        } catch (_: PouchRejected) {
            return
        }
    }

    fun mqttUrlOf(http: String, explicit: String): String {
        if (explicit.isNotBlank()) return explicit
        val u = runCatching { URI(http) }.getOrNull() ?: return ""
        val host = u.host ?: return ""
        return "tcp://$host:1884"
    }

    @Serializable
    private data class IntentDto(
        val typ: String,
        val revision: Long = 0,
        val assetId: String? = null,
        val digest: String? = null,
        val maxCachedProjects: Int = 0,
        val cacheScope: String = "",
        val persistUnwrapKey: Boolean = false,
        val keyTtlSeconds: Long = 0,
        val sig: String = "",
    ) {
        fun toIntent() = DownIntent(
            typ = typ,
            revision = revision,
            assetId = assetId,
            digest = digest?.takeIf { it.isNotBlank() }?.let { java.util.Base64.getDecoder().decode(it) },
            maxCachedProjects = maxCachedProjects,
            cacheScope = cacheScope,
            persistUnwrapKey = persistUnwrapKey,
            keyTtlSeconds = keyTtlSeconds,
            sig = if (sig.isBlank()) ByteArray(0) else java.util.Base64.getDecoder().decode(sig),
        )
    }
}

class MqttDownChannel(
    private val factory: FactoryGateway,
) : DownChannel {
    private val running = AtomicBoolean(false)
    @Volatile private var client: MqttClient? = null
    @Volatile private var worker: Thread? = null

    override fun start(live: LiveChannel) {
        stop()
        running.set(true)
        worker = Thread({
            runCatching { ChannelSync.pullInbox(factory, live) }
            val url = ChannelSync.mqttUrlOf(live.baseUrl, live.mqttUrl)
            if (url.isBlank() || !running.get()) return@Thread
            val down = "factory/${live.factoryId}/client/${live.clientId}/down"
            val up = "factory/${live.factoryId}/client/${live.clientId}/up"
            val mqtt = MqttClient(url, live.clientId.toString(), MemoryPersistence())
            mqtt.setCallback(object : MqttCallback {
                override fun connectionLost(cause: Throwable?) {}
                override fun messageArrived(topic: String?, message: MqttMessage?) {
                    val raw = message?.payload?.toString(Charsets.UTF_8) ?: return
                    ChannelSync.onDown(factory, live, raw) { ack ->
                        runCatching { mqtt.publish(up, MqttMessage(ack.toByteArray()).apply { qos = 1 }) }
                    }
                }
                override fun deliveryComplete(token: IMqttDeliveryToken?) {}
            })
            val opts = MqttConnectOptions().apply {
                userName = live.factoryId.toString()
                password = live.token.toCharArray()
                isAutomaticReconnect = true
                isCleanSession = false
                connectionTimeout = 10
            }
            runCatching {
                mqtt.connect(opts)
                mqtt.subscribe(down, 1)
                client = mqtt
            }
        }, "wmesh-mqtt").also { it.isDaemon = true; it.start() }
    }

    override fun stop() {
        running.set(false)
        runCatching { client?.disconnect() }
        runCatching { client?.close() }
        client = null
        worker = null
    }
}
