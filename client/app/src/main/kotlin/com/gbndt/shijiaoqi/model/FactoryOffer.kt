package com.gbndt.shijiaoqi.model

/** 局域网里发现的一个厂服务。 */
data class FactoryOffer(
    val httpBase: String,
    val factoryId: String,
    val status: String,
    val belongs: Boolean,
    val clientId: String,
    val clientName: String,
) {
    fun usable(): Boolean = status.isBlank() || status == "active"

    fun label(): String {
        val host = httpBase.removePrefix("https://").removePrefix("http://")
        val name = clientName.ifBlank { "厂服务" }
        val text = "$name · $host"
        return if (usable()) text else "$text（不可用）"
    }
}