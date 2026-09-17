package com.gbndt.shijiaoqi.ui.preview

import com.gbndt.shijiaoqi.domain.shared.ProcessChoice
import com.gbndt.shijiaoqi.domain.shared.ProjectChoice
import com.gbndt.shijiaoqi.model.CapturedPoint
import com.gbndt.shijiaoqi.model.FactoryOffer
import com.gbndt.shijiaoqi.model.Oscillation
import com.gbndt.shijiaoqi.model.Pose
import com.gbndt.shijiaoqi.model.WeldPoint
import com.gbndt.shijiaoqi.model.WeldPointType
import com.gbndt.shijiaoqi.model.WeldProcess
import com.gbndt.shijiaoqi.model.multilayer.MultiLayerWeldPath
import com.gbndt.shijiaoqi.model.multilayer.WeldPassOffset
import com.gbndt.shijiaoqi.model.single.WeldPath
import com.gbndt.shijiaoqi.model.tbar.GapBand
import com.gbndt.shijiaoqi.ui.login.LoginUiState
import com.gbndt.shijiaoqi.ui.login.ScanStatus
import com.gbndt.shijiaoqi.ui.welding.MultiLayerUiState
import com.gbndt.shijiaoqi.ui.welding.SingleWeldUiState
import com.gbndt.shijiaoqi.ui.welding.TBarUiState
import com.gbndt.shijiaoqi.ui.welding.WeldShellUi
import com.gbndt.shijiaoqi.ui.welding.asUiPath
import java.util.UUID

/** Preview 用的袋内工程/工艺、焊道和登录样例。 */
object PadSamples {
    val projectId: UUID = UUID.fromString("11111111-1111-4111-8111-111111111111")
    val processId: UUID = UUID.fromString("22222222-2222-4222-8222-222222222222")

    val pose: Pose = Pose(120.5, -30.2, 450.0, 180.0, 0.0, 90.0)

    val offer: FactoryOffer = FactoryOffer(
        httpBase = "http://192.168.1.10:8080",
        factoryId = "factory-1",
        status = "active",
        belongs = true,
        clientId = "client-1",
        clientName = "一号厂",
    )

    val login: LoginUiState = LoginUiState(
        offers = listOf(offer),
        selected = offer,
        scan = ScanStatus.Ready,
        loginName = "operator",
        password = "******",
        remember = true,
    )

    val projects: List<ProjectChoice> = listOf(
        ProjectChoice(id = projectId, name = "厂端引用同步验收", revision = 3, active = true),
        ProjectChoice(id = UUID.fromString("33333333-3333-4333-8333-333333333333"), name = "舷侧分段", revision = 1, active = false),
    )

    val processes: List<ProcessChoice> = listOf(
        ProcessChoice(id = processId, name = "CO2 打底"),
        ProcessChoice(id = UUID.fromString("44444444-4444-4444-8444-444444444444"), name = "CO2 盖面"),
    )

    val captured: CapturedPoint = CapturedPoint(
        pose = pose,
        joints = listOf(10.0, -20.0, 30.0, 0.0, 45.0, 0.0),
    )

    val shell: WeldShellUi = WeldShellUi(
        currentProjectName = "厂端引用同步验收",
        pouchProjects = projects,
        pouchProcesses = processes,
    )

    fun weldPath(
        id: String = "path-1",
        name: String = "焊道1",
        extraPoints: List<WeldPoint> = emptyList(),
        gapBands: List<GapBand> = emptyList(),
    ): WeldPath = WeldPath(
        id = id,
        name = name,
        points = mutableListOf(
            WeldPoint(id = "$id-start", type = WeldPointType.START, pose = pose),
            WeldPoint(id = "$id-end", type = WeldPointType.END, pose = pose.copy(x = pose.x + 200)),
        ).also { it.addAll(1, extraPoints) },
        process = WeldProcess(name = "CO2 打底"),
        processId = processId.toString(),
        gapBands = gapBands,
    ).asUiPath()

    fun tBarPath(): WeldPath = weldPath(
        id = "tbar-1",
        name = "T排焊道1",
        extraPoints = listOf(
            WeldPoint(id = "tbar-al", type = WeldPointType.GROOVE_A_LOWER, pose = pose),
            WeldPoint(id = "tbar-bl", type = WeldPointType.GROOVE_B_LOWER, pose = pose.copy(y = pose.y + 8)),
            WeldPoint(id = "tbar-au", type = WeldPointType.GROOVE_A_UPPER, pose = pose.copy(z = pose.z + 12)),
            WeldPoint(id = "tbar-bu", type = WeldPointType.GROOVE_B_UPPER, pose = pose.copy(y = pose.y + 8, z = pose.z + 12)),
        ),
        gapBands = listOf(
            GapBand(minGap = 2.0, maxGap = 4.0, layer = 1, rootProcessId = processId.toString()),
        ),
    )

    fun multiPath(): MultiLayerWeldPath = MultiLayerWeldPath(
        id = "ml-1",
        name = "多层焊道1",
        basePath = weldPath(id = "ml-base", name = "底道"),
        passes = mutableListOf(
            WeldPassOffset(id = "pass-1", name = "第1层", processId = processId.toString()),
        ),
    ).asUiPath()

    val singleUi: SingleWeldUiState = SingleWeldUiState(
        weldPaths = listOf(weldPath(), weldPath(id = "path-2", name = "焊道2")),
        selectedWeldPathIndex = 0,
        weldingLength = 12.4,
        weldingDuration = 3661,
        shell = shell,
        savedCurrent = 170.0,
        savedVoltage = 20.0,
    )

    val multiUi: MultiLayerUiState = MultiLayerUiState(
        paths = listOf(multiPath()),
        selectedMultiLayerPathIndex = 0,
        selectedPassIndex = -1,
        weldingLength = 8.2,
        weldingDuration = 1800,
        shell = shell,
        savedCurrent = 180.0,
        savedVoltage = 22.0,
    )

    val tbarUi: TBarUiState = TBarUiState(
        weldPaths = listOf(tBarPath()),
        selectedWeldPathIndex = 0,
        weldingLength = 5.1,
        weldingDuration = 900,
        shell = shell,
        savedCurrent = 160.0,
        savedVoltage = 19.0,
    )

    val gapBands: List<GapBand> = tBarPath().gapBands

    val oscillation: Oscillation = Oscillation(frequency = 4.0, amplitude = 2.5)
}
