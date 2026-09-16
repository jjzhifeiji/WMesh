package com.gbndt.shijiaoqi.domain.multilayer

import com.gbndt.shijiaoqi.model.multilayer.MultiLayerWeldPath
import com.gbndt.shijiaoqi.model.Oscillation
import com.gbndt.shijiaoqi.model.Pose
import com.gbndt.shijiaoqi.model.RefPoint
import com.gbndt.shijiaoqi.model.multilayer.WeldPassOffset
import com.gbndt.shijiaoqi.model.single.WeldPath
import com.gbndt.shijiaoqi.model.WeldPoint
import com.gbndt.shijiaoqi.model.WeldPointType
import com.gbndt.shijiaoqi.model.WeldProcess
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.UUID
import com.gbndt.shijiaoqi.domain.multilayer.MultiLayerLua
import com.gbndt.shijiaoqi.domain.shared.ProcessBind
import com.gbndt.shijiaoqi.domain.shared.ProcessRef
import com.gbndt.shijiaoqi.domain.shared.ProcessSource
import com.gbndt.shijiaoqi.domain.shared.ScriptPoint
import com.gbndt.shijiaoqi.domain.single.SingleLayerLua
import com.gbndt.shijiaoqi.domain.shared.texts

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
        val lines = SingleLayerLua.pathLines(offset, process, isWelding = true, simulating = false, speedMode = "1倍", toolIndex = 1).texts()
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

    @Test
    fun generateSimpleOffsetWithoutRefs() {
        val pose = Pose(10.0, 20.0, 30.0, 0.0, 0.0, 0.0)
        fun pt(type: WeldPointType) = WeldPoint(UUID.randomUUID().toString(), type, pose = pose, jointAngles = List(6) { 0.0 })
        val base = WeldPath(
            id = "b",
            name = "base",
            points = mutableListOf(
                pt(WeldPointType.START_SAFE),
                pt(WeldPointType.START),
                pt(WeldPointType.END),
                pt(WeldPointType.END_SAFE),
            ),
            process = WeldProcess(),
        )
        val pass = WeldPassOffset(valX = 5.0, valYLeft = 1.0, valYRight = 2.0, valZ = 3.0)
        val mp = MultiLayerWeldPath(id = "m", name = "m", basePath = base, passes = mutableListOf(pass))
        val pts = MultiLayerPass.generate(base, pass, mp)!!
        assertEquals(4, pts.size)
        assertEquals(pose, pts[0].pose)
        assertEquals(15.0, pts[1].pose.x, 0.0)
        assertEquals(21.0, pts[1].pose.y, 0.0)
        assertEquals(33.0, pts[1].pose.z, 0.0)
        assertEquals(22.0, pts[2].pose.y, 0.0)
        assertEquals(pose, pts[3].pose)
        assertTrue(pts[1].offsets.isNullOrEmpty())
    }

    @Test
    fun generateWithRefsKeepsOriginAndLuaFlag3() {
        val start = Pose(0.0, 0.0, 0.0, 0.0, 0.0, 0.0)
        val end = Pose(100.0, 0.0, 0.0, 0.0, 0.0, 0.0)
        fun pt(type: WeldPointType, pose: Pose) =
            WeldPoint(UUID.randomUUID().toString(), type, pose = pose, jointAngles = List(6) { 0.0 })
        fun ref(pose: Pose) = RefPoint(pose, List(6) { 0.0 })
        val base = WeldPath(
            id = "b",
            name = "base",
            points = mutableListOf(
                pt(WeldPointType.START_SAFE, start),
                pt(WeldPointType.START, start),
                pt(WeldPointType.END, end),
                pt(WeldPointType.END_SAFE, end),
            ),
            process = WeldProcess(),
        )
        val pass = WeldPassOffset(valX = 5.0, valYLeft = 1.0, valYRight = 2.0, valZ = 3.0)
        val mp = MultiLayerWeldPath(
            id = "m",
            name = "m",
            basePath = base,
            passes = mutableListOf(pass),
            refPointX1 = ref(Pose(0.0, 10.0, 0.0, 0.0, 0.0, 0.0)),
            refPointZ1 = ref(Pose(0.0, 0.0, 10.0, 0.0, 0.0, 0.0)),
            refPointXEnd = ref(Pose(100.0, 10.0, 0.0, 0.0, 0.0, 0.0)),
            refPointZEnd = ref(Pose(100.0, 0.0, 10.0, 0.0, 0.0, 0.0)),
        )
        val pts = MultiLayerPass.generate(base, pass, mp)!!
        assertEquals(start, pts[1].pose)
        assertEquals(6, pts[1].offsets!!.size)
        assertTrue(pts[1].offsets!!.any { kotlin.math.abs(it) >= 1e-5 })
        assertTrue(pts[1].world() != pts[1].pose)
        val lines = MultiLayerLua.job(listOf(mp), welding = true, simulating = false)!!.texts()
        val flagged = lines.filter { it.startsWith("MoveL(") && it.contains("3,") }
        assertTrue(flagged.isNotEmpty())
        assertTrue(flagged.any { it.contains("0.000,0.000,0.000,0.000,0.000,0.000,0.000,0.000,0.000") })
        assertFalse(lines.any { it.contains("GetInverseKinExaxis") })
    }

    @Test
    fun luaInterleavesBaseThenPass() {
        fun path(name: String): MultiLayerWeldPath {
            val p = Pose(1.0, 2.0, 3.0, 0.0, 0.0, 0.0)
            fun pt(t: WeldPointType) = WeldPoint(UUID.randomUUID().toString(), t, pose = p, jointAngles = List(6) { 0.0 })
            val base = WeldPath(
                id = name,
                name = name,
                points = mutableListOf(
                    pt(WeldPointType.START_SAFE),
                    pt(WeldPointType.START),
                    pt(WeldPointType.END),
                    pt(WeldPointType.END_SAFE),
                ),
                process = WeldProcess(current = 100.0),
            )
            val pass = WeldPassOffset(name = "p1", valX = 1.0, process = WeldProcess(current = 200.0))
            return MultiLayerWeldPath(id = name, name = name, basePath = base, passes = mutableListOf(pass))
        }
        val lines = MultiLayerLua.job(listOf(path("a"), path("b")), welding = true, simulating = false)!!.texts()
        val params = lines.filter { it.startsWith("WeldingSetProcessParam") }
        assertEquals(4, params.size)
        assertTrue(params[0].contains(",100.0,"))
        assertTrue(params[1].contains(",100.0,"))
        assertTrue(params[2].contains(",200.0,"))
        assertTrue(params[3].contains(",200.0,"))
        assertEquals("SetSpeed(10)", lines.first())
    }

    @Test
    fun jobNullWhenPoseMissing() {
        val base = WeldPath(
            id = "b",
            name = "b",
            points = mutableListOf(WeldPoint(UUID.randomUUID().toString(), WeldPointType.START_SAFE)),
            process = WeldProcess(),
        )
        val mp = MultiLayerWeldPath(id = "m", name = "m", basePath = base, passes = mutableListOf())
        assertNull(MultiLayerLua.job(listOf(mp), welding = true, simulating = false))
    }
}
