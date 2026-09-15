package com.gbndt.shijiaoqi.model

import kotlinx.serialization.Serializable

/** 发布通道给出的版本信息。 */
@Serializable
data class UpdateInfo(
    val versionCode: Int,
    val versionName: String,
    val downloadUrl: String,
    val description: String
)
