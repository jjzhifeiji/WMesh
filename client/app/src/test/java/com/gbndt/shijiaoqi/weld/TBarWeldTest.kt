package com.gbndt.shijiaoqi.weld

import com.gbndt.shijiaoqi.data.models.GapBand
import com.gbndt.shijiaoqi.data.models.Oscillation
import com.gbndt.shijiaoqi.data.models.Pose
import com.gbndt.shijiaoqi.data.models.WeldPointType
import com.gbndt.shijiaoqi.data.models.WeldProcess
import com.gbndt.shijiaoqi.utils.TBarPass
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.UUID

class TBarWeldTest {
    @Test
    fun matchOpenIntervalThenClosedThenNearest() {
        val a = GapBand(minGap = 3.0, maxGap = 4.0, layer = 1)
        val b = GapBand(minGap = 4.0, maxGap = 6.0, layer = 1)
        val layer2 = GapBand(minGap = 3.0, maxGap = 4.0, layer = 2)
        val bands = listOf(a, b, layer2)
        assertEquals(a, TBarRun.matchBand(3.5, bands, 1))
        assertEquals(b, TBarRun.matchBand(4.0, bands, 1))
        assertEquals(a, TBarRun.matchBand(4.0, listOf(a), 1))
        assertEquals(b, TBarRun.matchBand(10.0, bands, 1))
        assertEquals(layer2, TBarRun.matchBand(3.2, bands, 2))
        assertNull(TBarRun.matchBand(3.2, bands, 3))
    }

    @Test
    fun samplesSplitWhenGapCrossesBands() {
        val bands = listOf(
            GapBand(minGap = 3.0, maxGap = 4.0, layer = 1, rootProcessId = "r1", capProcessId = "c1"),
            GapBand(minGap = 4.0, maxGap = 6.0, layer = 1, rootProcessId = "r2", capProcessId = "c2"),
        )
        val segs = TBarRun.buildSegments(
            aLower = Pose(0.0, 0.0, 0.0, 0.0, 0.0, 0.0),
            bLower = Pose(3.2, 0.0, 0.0, 0.0, 0.0, 0.0),
            aUpper = Pose(0.0, 0.0, 10.0, 0.0, 0.0, 0.0),
            bUpper = Pose(5.5, 0.0, 10.0, 0.0, 0.0, 0.0),
            startPose = Pose(1.6, 0.0, 0.0, 0.0, 0.0, 0.0),
            endPose = Pose(2.75, 0.0, 10.0, 0.0, 0.0, 0.0),
            bands = bands,
            pass = TBarPass.ROOT,
            layer = 1,
        )
        assertEquals(2, segs.size)
        assertEquals("r1", segs[0].processId)
        assertEquals("r2", segs[1].processId)
        assertEquals("c1", TBarRun.buildSegments(
            Pose(0.0, 0.0, 0.0, 0.0, 0.0, 0.0),
            Pose(3.2, 0.0, 0.0, 0.0, 0.0, 0.0),
            Pose(0.0, 0.0, 10.0, 0.0, 0.0, 0.0),
            Pose(5.5, 0.0, 10.0, 0.0, 0.0, 0.0),
            Pose(1.6, 0.0, 0.0, 0.0, 0.0, 0.0),
            Pose(2.75, 0.0, 10.0, 0.0, 0.0, 0.0),
            bands,
            TBarPass.CAP,
            1,
        )[0].processId)
    }

    @Test
    fun parseUsesGapBandIdsAndSkipsSingleLayer() {
        val root = UUID.randomUUID()
        val cap = UUID.randomUUID()
        val json = """
            [{"name":"单层","processId":"$root","points":[]},
             {"name":"T","kind":"tbar",
              "gapBands":[{"minGap":3.0,"maxGap":4.0,"layer":1,
                "rootProcessId":"$root","capProcessId":"$cap"}],
              "points":[]}]
        """.trimIndent()
        val paths = TBarProject.parse(json.toByteArray())
        assertEquals(1, paths.size)
        assertEquals("T", paths[0].name)
        assertEquals(1, paths[0].gapBands.size)
        assertEquals(root.toString(), paths[0].gapBands[0].rootProcessId)
        assertEquals(cap.toString(), paths[0].gapBands[0].capProcessId)
        val encoded = TBarProject.encode(paths)
        val text = encoded.decodeToString()
        assertFalse(text.contains("\"processPath\""))
        assertTrue(text.contains("\"kind\":\"tbar\"") || text.contains(TBarRun.KIND))
        val again = TBarProject.parse(encoded)
        assertEquals(1, again.size)
        assertEquals(root.toString(), again[0].gapBands[0].rootProcessId)
    }

    @Test
    fun emptyBandIdAllowedMissingMemberRefused() {
        val src = ProcessSource { null }
        val empty = ProcessBind.resolve(listOf(ProcessRef("打底", "", true)), src)
        assertTrue(empty.missing.isEmpty())
        val id = UUID.randomUUID()
        val missing = ProcessBind.resolve(listOf(ProcessRef("打底", id.toString(), true)), src)
        assertEquals(listOf("打底: $id"), missing.missing)
    }

    @Test
    fun rootThenReturnThenCapAndOnlineWeaveOnSegmentSwitch() {
        val root1 = UUID.randomUUID().toString()
        val root2 = UUID.randomUUID().toString()
        val cap1 = UUID.randomUUID().toString()
        val cap2 = UUID.randomUUID().toString()
        val osc = Oscillation(type = "三角波摆动")
        val processes = mapOf(
            root1 to WeldProcess(name = "r1", speed = 10.0, oscillation = osc),
            root2 to WeldProcess(name = "r2", speed = 12.0, oscillation = osc),
            cap1 to WeldProcess(name = "c1", speed = 8.0, oscillation = osc),
            cap2 to WeldProcess(name = "c2", speed = 9.0, oscillation = osc),
        )
        val bands = listOf(
            GapBand(minGap = 3.0, maxGap = 4.0, layer = 1, rootProcessId = root1, capProcessId = cap1),
            GapBand(minGap = 4.0, maxGap = 6.0, layer = 1, rootProcessId = root2, capProcessId = cap2),
        )
        val path = samplePath(bands)
        val weld = TBarLua.job(listOf(path), processes, welding = true, simulating = false)
        val sim = TBarLua.job(listOf(path), processes, welding = true, simulating = true)
        assertEquals(TBarLua.GLOBAL_SPEED, weld.first())
        val arcStarts = weld.withIndex().filter { it.value.startsWith("ARCStart") }.map { it.index }
        assertEquals(2, arcStarts.size)
        val firstMove = weld.indexOfFirst { it.startsWith("MoveL(") || it.startsWith("j1,j2") }
        assertTrue(firstMove >= 0)
        assertTrue(arcStarts[0] > firstMove)
        assertTrue(weld.any { it.startsWith("WeaveOnlineSetPara") })
        val returnBeforeCap = weld.subList(arcStarts[0], arcStarts[1]).any { it.contains("GetInverseKinExaxis") }
        assertTrue(returnBeforeCap)
        assertTrue(sim.none { it.startsWith("ARCStart") || it.startsWith("ARCEnd") })
        val safe = weld.filter { it.startsWith("MoveL(") && it.contains("0.000,0.000,0.000,0.000,0.000,0.000") }
        assertTrue(safe.size >= 2)
    }

    private fun samplePath(bands: List<GapBand>): TBarScriptPath {
        fun p(x: Double, z: Double) = Pose(x, 0.0, z, 0.0, 0.0, 0.0)
        return TBarScriptPath(
            startSafe = ScriptPoint(WeldPointType.START_SAFE, p(0.0, -10.0), listOf(0.0, 0.0, 0.0, 0.0, 0.0, 0.0)),
            endSafe = ScriptPoint(WeldPointType.END_SAFE, p(0.0, 20.0), listOf(0.0, 0.0, 0.0, 0.0, 0.0, 0.0)),
            aLower = p(0.0, 0.0),
            bLower = p(3.2, 0.0),
            aUpper = p(0.0, 10.0),
            bUpper = p(5.5, 10.0),
            startPose = p(1.6, 0.0),
            endPose = p(2.75, 10.0),
            bands = bands,
        )
    }
}
