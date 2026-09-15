package com.gbndt.shijiaoqi.domain.weld

import com.gbndt.shijiaoqi.model.Oscillation
import com.gbndt.shijiaoqi.model.Pose
import com.gbndt.shijiaoqi.model.WeldPointType
import com.gbndt.shijiaoqi.model.WeldProcess
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.UUID
import com.gbndt.shijiaoqi.domain.script.ScriptPoint
import com.gbndt.shijiaoqi.domain.script.SingleLayerLua

class MultiLayerWeldTest {
    @Test
    fun interleaveAllPathsPerLayer() {
        val steps = MultiLayerRun.interleave(listOf(2, 1)) { _, _ -> true }
        assertEquals(
            listOf(
                MultiLayerRun.Step(0, -1),
                MultiLayerRun.Step(1, -1),
                MultiLayerRun.Step(0, 0),
                MultiLayerRun.Step(1, 0),
                MultiLayerRun.Step(0, 1),
            ),
            steps,
        )
    }

    @Test
    fun interleaveSkipsDisabled() {
        val steps = MultiLayerRun.interleave(listOf(1, 1)) { path, pass -> !(path == 0 && pass == -1) }
        assertEquals(
            listOf(MultiLayerRun.Step(1, -1), MultiLayerRun.Step(0, 0), MultiLayerRun.Step(1, 0)),
            steps,
        )
    }

    @Test
    fun simpleOffsetKeepsSafeAndShiftsStartEnd() {
        val p = Pose(10.0, 20.0, 30.0, 0.0, 0.0, 0.0)
        val safe = MultiLayerRun.simpleOffset(p, WeldPointType.START_SAFE, true, false, 5.0, 1.0, 2.0, 3.0)
        assertEquals(p, safe)
        val start = MultiLayerRun.simpleOffset(p, WeldPointType.START, true, false, 5.0, 1.0, 2.0, 3.0)
        assertEquals(15.0, start.x, 0.0)
        assertEquals(21.0, start.y, 0.0)
        assertEquals(33.0, start.z, 0.0)
        val end = MultiLayerRun.simpleOffset(p, WeldPointType.END, false, true, 5.0, 1.0, 2.0, 3.0)
        assertEquals(22.0, end.y, 0.0)
        val mid = MultiLayerRun.simpleOffset(p, WeldPointType.MIDDLE, false, false, 5.0, 1.0, 2.0, 3.0)
        assertEquals(20.0, mid.y, 0.0)
    }

    @Test
    fun weaveStartAfterStartMoveOnOffsetLayer() {
        fun pt(type: WeldPointType, x: Double) = ScriptPoint(type, Pose(x, 0.0, 0.0, 0.0, 0.0, 0.0))
        val base = listOf(
            pt(WeldPointType.START_SAFE, 0.0),
            pt(WeldPointType.START, 10.0),
            pt(WeldPointType.END, 20.0),
            pt(WeldPointType.END_SAFE, 30.0),
        )
        val offset = base.map {
            it.copy(
                pose = MultiLayerRun.simpleOffset(
                    it.pose, it.type,
                    it.type == WeldPointType.START,
                    it.type == WeldPointType.END,
                    5.0, 1.0, 2.0, 3.0,
                )
            )
        }
        val process = WeldProcess(oscillation = Oscillation(type = "三角波摆动"))
        val lines = SingleLayerLua.pathLines(offset, process, isWelding = true, simulating = false, speedMode = "1倍", toolIndex = 1)
        val startMove = lines.indexOfFirst { it.startsWith("MoveL(") && it.contains("15.000") }
        val arc = lines.indexOfFirst { it.startsWith("ARCStart") }
        val weave = lines.indexOfFirst { it.startsWith("WeaveStart") }
        assertTrue(startMove >= 0)
        assertTrue(arc > startMove)
        assertTrue(weave > arc)
    }

    @Test
    fun parseUsesProcessIdAndSkipsSingleLayer() {
        val baseId = UUID.randomUUID()
        val passId = UUID.randomUUID()
        val json = """
            [
              {"name":"single","processId":"$baseId"},
              {"kind":"multi","name":"多层","basePath":{"name":"基","processId":"$baseId"},"passes":[{"name":"第1道","processId":"$passId","valX":1.5}]}
            ]
        """.trimIndent()
        val paths = MultiLayerProject.parse(json.toByteArray())
        assertEquals(1, paths.size)
        assertEquals(baseId.toString(), paths[0].basePath.processId)
        assertEquals(passId.toString(), paths[0].passes[0].processId)
        assertEquals(1.5, paths[0].passes[0].valX, 0.0)
        assertFalse(MultiLayerProject.encode(paths).decodeToString().contains("\"processPath\""))
    }

    @Test
    fun missingPassProcessIdRefused() {
        val id = UUID.randomUUID()
        val src = ProcessSource { null }
        val out = ProcessBind.resolve(
            listOf(
                ProcessRef("基准", "", true),
                ProcessRef("第1道", id.toString(), true),
            ),
            src,
        )
        assertEquals(listOf("第1道: $id"), out.missing)
    }
}
