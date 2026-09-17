package com.gbndt.shijiaoqi.model

/** 厂服给出的客户端新包；确认后才下载安装。 */
data class UpdateInfo(
    val versionCode: Int, // 单调整数，大于本机才提示
    val versionName: String, // 给人看的版本名
    val digest: ByteArray, // 包文件 SHA-256
    val description: String = "", // 弹窗说明
)
