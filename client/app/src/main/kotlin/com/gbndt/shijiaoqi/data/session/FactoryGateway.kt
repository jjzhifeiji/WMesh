package com.gbndt.shijiaoqi.data.session

import com.gbndt.shijiaoqi.data.pouch.TransitClosure
import java.util.UUID
import com.gbndt.shijiaoqi.model.FactoryOffer

/** 登录可见的一台 Client；焊机只有编号，不带解封钥。 */
data class PadDevice(
    val id: String, // Client 身份
    val name: String, // 设备显示名
    val deviceSerial: String, // 机械臂识别号
    val shortCode: String, // 本机短号
    val unwrapKey: ByteArray = ByteArray(0), // 焊机不持钥；兼容旧文件格式
)

/** 平板登录结果：人、人钥、策略和设备。 */
data class PadLoginResult(
    val token: String, // 登录令牌
    val unwrapKey: ByteArray, // 登录人解封钥；焊机不持钥
    val person: PersonMeta, // 当前登录人
    val policy: PolicyMeta, // 厂端客户端策略
    val roles: List<String> = emptyList(), // 厂端授予角色
    val devices: List<PadDevice> = emptyList(), // 本厂未作废设备
)

/** 目录里一份待拉资产，不含正文。 */
data class ClosureRef(
    val assetId: UUID, // 资产稳定身份
    val revision: Long, // 目录修订
    val digest: ByteArray?, // 整包摘要；空则拉取时再核
    val name: String, // 显示名
    val level: String, // factory / personal / platform
)

data class ClientInbox(
    val policy: PolicyMeta,
    val closures: List<ClosureRef>,
)

/** 厂端 HTTP：登录、目录、拉工艺/工程。不管机械臂。 */
interface FactoryGateway {
    fun loginPad(
        baseUrl: String,
        factoryId: String,
        loginName: String,
        password: String,
    ): PadLoginResult

    fun padInbox(baseUrl: String, factoryId: String, token: String): ClientInbox
    fun padPullClosure(baseUrl: String, factoryId: String, assetId: String, token: String): TransitClosure
    fun discover(baseUrl: String): List<FactoryOffer>
    fun changePassword(baseUrl: String, factoryId: String, token: String, password: String)
    fun getAsset(baseUrl: String, factoryId: String, token: String, assetId: String): RemoteAsset?
    fun createPadAsset(
        baseUrl: String,
        factoryId: String,
        token: String,
        kind: String,
        name: String,
        content: String,
        id: String,
        code: String,
        deps: List<com.gbndt.shijiaoqi.data.pouch.AssetDep>,
    ): RemoteAsset
    fun updateAssetContent(
        baseUrl: String,
        factoryId: String,
        token: String,
        assetId: String,
        expected: Long,
        content: String,
    ): RemoteAsset
    fun setAssetDeps(
        baseUrl: String,
        factoryId: String,
        token: String,
        assetId: String,
        expected: Long,
        deps: List<com.gbndt.shijiaoqi.data.pouch.AssetDep>,
    ): RemoteAsset
    /** 删厂端这份；没有当成已删。 */
    fun deleteAsset(baseUrl: String, factoryId: String, token: String, assetId: String)
}

/** 厂端资产元数据；不含正文。 */
data class RemoteAsset(
    val id: java.util.UUID,
    val kind: String,
    val level: String,
    val name: String,
    val code: String,
    val status: String,
    val copyable: Boolean,
    val revision: Long,
    val digest: ByteArray,
    val deps: List<com.gbndt.shijiaoqi.data.pouch.AssetDep> = emptyList(),
)

/** 登录或绑定被拒绝，code 是英文原因。 */
class LoginRejected(val code: String) : Exception(code)
