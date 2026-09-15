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
    fun label(): String {
        val host = httpBase.removePrefix("https://").removePrefix("http://")
        val name = clientName.ifBlank { "厂服务" }
        return "$name · $host"
    }
}

/** 扫局域网厂服务：UDP 探询加 HTTP 探活，多台都留下拉。 */
