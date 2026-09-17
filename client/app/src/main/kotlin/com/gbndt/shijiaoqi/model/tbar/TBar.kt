package com.gbndt.shijiaoqi.model.tbar

import com.gbndt.shijiaoqi.model.WeldPointType
import com.gbndt.shijiaoqi.model.WeldProcess
import kotlinx.serialization.Serializable
import kotlinx.serialization.Transient

/** T 排间隙带；JSON 只留工艺 Id，参数对象出库后合进来。 */
@Serializable
data class GapBand(
    val minGap: Double = 0.0,
    val maxGap: Double = 0.0,
    val layer: Int = 1,
    val rootProcessId: String = "",
    val capProcessId: String = "",
    @Transient var rootProcess: WeldProcess = WeldProcess(),
    @Transient var capProcess: WeldProcess = WeldProcess(),
)
/** 坡口四点。 */
fun WeldPointType.isGroovePoint(): Boolean {
    return this == WeldPointType.GROOVE_A_LOWER ||
        this == WeldPointType.GROOVE_B_LOWER ||
        this == WeldPointType.GROOVE_A_UPPER ||
        this == WeldPointType.GROOVE_B_UPPER
}

/** T 排可采点：安全点加坡口。 */
fun WeldPointType.isTBarCollectable(): Boolean {
    return this == WeldPointType.START_SAFE ||
        this == WeldPointType.END_SAFE ||
        isGroovePoint()
}
