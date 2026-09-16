package com.gbndt.shijiaoqi.domain.script

import com.gbndt.shijiaoqi.model.Oscillation
import com.gbndt.shijiaoqi.model.Pose
import com.gbndt.shijiaoqi.model.WeldPointType
import com.gbndt.shijiaoqi.model.WeldProcess
import com.gbndt.shijiaoqi.data.crypt.Wm2
import com.gbndt.shijiaoqi.data.pouch.AssetDep
import com.gbndt.shijiaoqi.data.pouch.ClosureMemberPlain
import com.gbndt.shijiaoqi.data.pouch.ClosureSnapshotPlain
import com.gbndt.shijiaoqi.data.pouch.Digest
import com.gbndt.shijiaoqi.data.pouch.Pouch
import com.gbndt.shijiaoqi.data.robot.protocol.FrPacket
import com.gbndt.shijiaoqi.data.robot.protocol.RobotCommands
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.UUID
import com.gbndt.shijiaoqi.domain.weld.Capture
import com.gbndt.shijiaoqi.data.pouch.PouchProcessSource
import com.gbndt.shijiaoqi.domain.weld.ProcessBind
import com.gbndt.shijiaoqi.domain.weld.ProcessJson
import com.gbndt.shijiaoqi.domain.weld.ProcessRef
import com.gbndt.shijiaoqi.domain.weld.ProcessSource
import com.gbndt.shijiaoqi.domain.weld.SingleLayerProject

class SingleLayerWeldTest {
    @Test
    fun emptyProcessIdAllowedAndPathIgnored() {
        val src = RecordingSource()
        val refs = listOf(
            ProcessRef("焊道1", "", true),
            ProcessRef("焊道2", "", true),
        )
        val out = ProcessBind.resolve(refs, src)
        assertTrue(out.missing.isEmpty())
        assertTrue(out.loaded.isEmpty())
        assertTrue(src.opened.isEmpty())
    }

    @Test
    fun missingMemberRefusedWithoutReadingPath() {
        val src = RecordingSource()
        val id = UUID.randomUUID()
        val out = ProcessBind.resolve(listOf(ProcessRef("焊道1", id.toString(), true)), src)
        assertEquals(listOf("焊道1: $id"), out.missing)
        assertEquals(listOf(id), src.opened)
        assertTrue(out.loaded.isEmpty())
    }

    @Test
    fun pouchOpenProcessById() {
        val (p, client) = loggedPouch()
        val body = """{"name":"角焊","current":180.0,"voltage":21.0,"speed":12.0}""".toByteArray()
        val proc = processMember("角焊", body)
        val snap = projectSnap(client, "工程", proc)
        p.cacheClosure(snap)
        p.activate(snap.assetId)
        val src = PouchProcessSource(p)
        val got = src.open(proc.id)!!
        assertEquals("角焊", got.name)
        assertEquals(180.0, got.current, 0.0)
        assertNull(src.open(snap.assetId))
        assertEquals(1, src.list().size)
        assertEquals(proc.id, src.list()[0].id)
    }

    @Test
    fun parseProjectUsesProcessId() {
        val id = UUID.randomUUID()
        val json = """[{"name":"w","processId":"$id","points":[]}]"""
        val paths = SingleLayerProject.parse(json.toByteArray())
        assertEquals(1, paths.size)
        assertEquals(id.toString(), paths[0].processId)
        val encoded = SingleLayerProject.encode(paths)
        val text = encoded.decodeToString()
        assertFalse(text.contains("\"processPath\""))
        assertFalse(text.contains("180.0"))
        val again = SingleLayerProject.parse(encoded)
        assertEquals(id.toString(), again[0].processId)
    }

    @Test
    fun processEnvelopeHidesPlaintext() {
        val p = Pouch()
        val who = UUID.randomUUID()
        val id = UUID.randomUUID()
        p.login(Wm2.randomKey(), who, false)
        val body = ProcessJson.encode(WeldProcess(name = "SECRET_PROCESS_LEAK", current = 180.0))
        p.putPlain(id, Pouch.LEVEL_FACTORY, "工艺", 1, null, body)
        Wm2.zero(body)
        p.diskSnapshot().forEach { env ->
            val s = String(env.blob, Charsets.ISO_8859_1)
            assertFalse(s.contains("SECRET_PROCESS_LEAK"))
            assertTrue(Wm2.isEnvelope(env.blob))
        }
        val again = ProcessJson.encode(WeldProcess(name = "AFTER_REWRITE", current = 190.0))
        p.putPlain(id, Pouch.LEVEL_FACTORY, "工艺", 1, null, again)
        Wm2.zero(again)
        p.diskSnapshot().forEach { env ->
            val s = String(env.blob, Charsets.ISO_8859_1)
            assertFalse(s.contains("AFTER_REWRITE"))
            assertTrue(Wm2.isEnvelope(env.blob))
        }
        p.logout()
        assertTrue(runCatching { p.open(id) }.isFailure)
    }

    @Test
    fun arcStartAfterStartMoveAndSimHasNoArc() {
        val pts = linePoints()
        val process = WeldProcess(current = 170.0, voltage = 20.0, speed = 10.0)
        val weld = SingleLayerLua.job(listOf(ScriptPath(pts, process)), welding = true, simulating = false).texts()
        val sim = SingleLayerLua.job(listOf(ScriptPath(pts, process)), welding = true, simulating = true).texts()
        assertEquals(SingleLayerLua.GLOBAL_SPEED, weld.first())
        val arc = weld.indexOfFirst { it.startsWith("ARCStart") }
        assertTrue(arc > 0)
        val lastMoveBeforeArc = weld.subList(0, arc).indexOfLast { it.startsWith("MoveL(") }
        assertTrue(lastMoveBeforeArc >= 0)
        assertTrue(lastMoveBeforeArc < arc)
        assertFalse(sim.any { it.startsWith("ARCStart") || it.startsWith("ARCEnd") })
        assertTrue(weld.any { it.startsWith("ARCEnd") })
        assertTrue(weld.any { it.startsWith("WeldingSetProcessParam") })
    }

    @Test
    fun extrasReuseGeometry() {
        val pts = linePoints()
        val main = WeldProcess(name = "主", current = 170.0, speed = 10.0)
        val extra = WeldProcess(name = "附加", current = 200.0, speed = 8.0)
        val lines = SingleLayerLua.job(
            listOf(ScriptPath(pts, main, extras = listOf(extra))),
            welding = true,
            simulating = false,
        ).texts()
        val params = lines.filter { it.startsWith("WeldingSetProcessParam") }
        assertEquals(2, params.size)
        assertTrue(params[0].contains("170.0"))
        assertTrue(params[1].contains("200.0"))
        val moves = lines.filter { it.startsWith("MoveL(") }
        assertEquals(8, moves.size)
        val coords = { s: String -> s.substringAfter("MoveL(").substringBefore(",1,0,100,100") }
        assertEquals(coords(moves[0]), coords(moves[4]))
        assertEquals(coords(moves[1]), coords(moves[5]))
    }

    @Test
    fun batchAndPauseResumeStopFrames() {
        val plan = WeldRun.batch(11, 12, "SetSpeed(10)\r\n")
        val f105 = FrPacket.decode(plan.fileName)!!
        val f106 = FrPacket.decode(plan.body)!!
        assertEquals(WeldRun.TYPE_FILENAME, f105.type)
        assertEquals(WeldRun.LUA_NAME, f105.payload)
        assertEquals(WeldRun.TYPE_BODY, f106.type)
        assertEquals("SetSpeed(10)\r\n", f106.payload)
        assertEquals(50L, WeldRun.AFTER_FILENAME_MS)
        assertEquals(500L, WeldRun.AFTER_BODY_ACK_MS)
        assertEquals(100L, WeldRun.AFTER_MODE_MS)
        assertTrue(plan.modeAuto.contains("Mode(0)"))
        assertEquals("Start", FrPacket.decode(plan.start)!!.payload)
        assertEquals("Pause", FrPacket.decode(WeldRun.pause())!!.payload)
        assertEquals("RESUME", FrPacket.decode(WeldRun.resume())!!.payload)
        assertEquals("STOP", FrPacket.decode(WeldRun.stop())!!.payload)
        val pause = WeldRun.pauseSeq()
        assertEquals(2, pause.size)
        assertTrue(pause[1].contains("Mode(1)"))
        val resumeBreak = WeldRun.resumeSeq(true)
        assertEquals(RobotCommands.modeAuto(), resumeBreak[0])
        assertTrue(resumeBreak[1].contains("WeldingStartReWeldAfterBreakOff()"))
        assertEquals("RESUME", FrPacket.decode(resumeBreak[2])!!.payload)
        val stopBreak = WeldRun.stopSeq(true)
        assertEquals("STOP", FrPacket.decode(stopBreak[0])!!.payload)
        assertTrue(stopBreak[2].contains("WeldingAbortWeldAfterBreakOff()"))
    }

    @Test
    fun joinLuaUsesCrlfAndNumberSkipsIk() {
        assertEquals("A\r\nB\r\n", WeldRun.joinLua(listOf("A", "B")))
        var next = 100
        val numbered = WeldRun.number(
            listOf(
                LuaLine("SetSpeed(10)", withId = true),
                LuaLine("GetInverseKinExaxis()", withId = false),
                LuaLine("MoveL()", withId = true, pathIndex = 0, pointIndex = 1),
            ),
        ) { next++ }
        assertEquals("SetSpeed(10)\r\nGetInverseKinExaxis()\r\nMoveL()\r\n", numbered.body)
        assertEquals(100, numbered.lineToId[1])
        assertEquals(101, numbered.lineToId[3])
        assertEquals(null, numbered.lineToId[2])
        assertEquals(Triple(0, 1, -1), numbered.idToPoint[101])
    }

    @Test
    fun resumeFromStopInsertsCurrentPose() {
        val pts = linePoints()
        val process = WeldProcess(current = 170.0, speed = 10.0)
        val stop = StopResume(
            pose = Pose(12.0, 0.0, 0.0, 0.0, 0.0, 0.0),
            joints = List(6) { 1.0 },
            pathIndex = 0,
            pointIndex = 1,
        )
        val lines = SingleLayerLua.job(
            listOf(ScriptPath(pts, process)),
            welding = true,
            simulating = false,
            resume = stop,
        ).texts()
        assertTrue(lines.any { it.startsWith("MoveL(") && it.contains("12.000") })
    }

    @Test
    fun lineFollowCountsLengthBetweenStartAndEnd() {
        val follow = WeldLineFollow()
        follow.load(
            WeldRun.NumberedLua(
                body = "",
                lineToId = mapOf(1 to 10, 2 to 11, 3 to 12),
                idToPoint = mapOf(
                    10 to Triple(0, 1, -1),
                    11 to Triple(0, 2, -1),
                    12 to Triple(0, 3, -1),
                ),
            ),
        )
        val types = listOf(WeldPointType.START_SAFE, WeldPointType.START, WeldPointType.END, WeldPointType.END_SAFE)
        val hits = follow.onProgLine(3, busy = true) { _, i -> types.getOrNull(i) }
        assertTrue(hits.any { it.startStats })
        assertTrue(hits.any { it.stopStats })
        assertTrue(hits.any { it.countLength })
    }

    @Test
    fun captureNeedsPoseAndJoints() {
        assertNull(Capture.snapshot(null, listOf(1.0)))
        assertNull(Capture.snapshot(Pose(1.0, 2.0, 3.0, 0.0, 0.0, 0.0), emptyList()))
        val snap = Capture.snapshot(Pose(1.0, 2.0, 3.0, 0.0, 0.0, 0.0), listOf(1.0, 2.0))
        assertNotNull(snap)
        assertEquals(1.0, snap!!.first.x, 0.0)
        assertEquals(2, snap.second.size)
    }

    @Test
    fun weaveAfterStartWhenOscillationOn() {
        val process = WeldProcess(
            oscillation = Oscillation(type = "三角波摆动"),
        )
        val lines = SingleLayerLua.pathLines(linePoints(), process, isWelding = true, simulating = false, speedMode = "1倍", toolIndex = 1).texts()
        val start = lines.indexOfFirst { it.startsWith("ARCStart") }
        val weave = lines.indexOfFirst { it.startsWith("WeaveStart") }
        assertTrue(lines.any { it.startsWith("WeaveSetPara") })
        assertTrue(weave > start)
        assertTrue(lines.any { it.startsWith("WeaveEnd") })
    }

    @Test
    fun processOffsetMoveLUsesFlag3NotBaked() {
        val process = WeldProcess(offsetX = "2")
        val lines = SingleLayerLua.pathLines(linePoints(), process, true, false, "1倍", 1, false).texts()
        val start = lines.first { it.startsWith("MoveL(") && it.contains(",10.000,") }
        assertTrue(start.contains("3,0.000,-2.000,0.000"))
        assertTrue(start.contains("10.000,0.000,0.000"))
        assertFalse(lines.any { it.contains("GetInverseKinExaxis") })
    }

    @Test
    fun extAxisOffsetBakesAndUsesIk() {
        val process = WeldProcess(offsetX = "2")
        val lines = SingleLayerLua.pathLines(linePoints(), process, true, false, "1倍", 1, true).texts()
        assertTrue(lines.any { it.startsWith("ExtAxisMoveJ") })
        val ik = lines.first { it.contains("GetInverseKinExaxis") }
        assertTrue(ik.contains("{10.000,2.000,0.000,"))
        assertTrue(lines.any { it.startsWith("MoveL(j1,j2,j3,j4,j5,j6,") })
    }

    @Test
    fun extrasRecomputeOwnOffset() {
        val pts = linePoints()
        val main = WeldProcess(name = "主", current = 170.0, speed = 10.0)
        val extra = WeldProcess(name = "附加", current = 200.0, speed = 8.0, offsetX = "2")
        val lines = SingleLayerLua.job(listOf(ScriptPath(pts, main, extras = listOf(extra))), welding = true, simulating = false).texts()
        val moves = lines.filter { it.startsWith("MoveL(") }
        assertEquals(8, moves.size)
        assertTrue(moves[1].contains("0,0,0,0,0,0,0"))
        assertTrue(moves[5].contains("3,0.000,-2.000,0.000"))
    }

    private class RecordingSource : ProcessSource {
        val opened = mutableListOf<UUID>()
        override fun open(processId: UUID): WeldProcess? {
            opened.add(processId)
            return null
        }
    }

    private fun linePoints(): List<ScriptPoint> {
        fun p(x: Double) = Pose(x, 0.0, 0.0, 0.0, 0.0, 0.0)
        return listOf(
            ScriptPoint(WeldPointType.START_SAFE, p(0.0)),
            ScriptPoint(WeldPointType.START, p(10.0)),
            ScriptPoint(WeldPointType.END, p(20.0)),
            ScriptPoint(WeldPointType.END_SAFE, p(30.0)),
        )
    }

    private fun loggedPouch(): Pair<Pouch, UUID> {
        val p = Pouch()
        p.login(Wm2.randomKey(), UUID.randomUUID(), false)
        val client = UUID.randomUUID()
        p.bindClient(client)
        return p to client
    }

    private fun processMember(name: String, body: ByteArray): ClosureMemberPlain =
        ClosureMemberPlain(
            id = UUID.randomUUID(),
            kind = Pouch.KIND_PROCESS,
            level = Pouch.LEVEL_FACTORY,
            name = name,
            status = Pouch.STATUS_AVAILABLE,
            revision = 1,
            content = body,
            digest = Digest.sum(body),
        )

    private fun projectSnap(client: UUID, name: String, vararg procs: ClosureMemberPlain): ClosureSnapshotPlain {
        val deps = procs.map { AssetDep(it.id, it.revision, it.digest) }
        val body = """[{"name":"w","processId":"${procs.first().id}"}]""".toByteArray()
        val rootId = UUID.randomUUID()
        val root = ClosureMemberPlain(
            id = rootId,
            kind = Pouch.KIND_PROJECT,
            level = Pouch.LEVEL_FACTORY,
            name = name,
            status = Pouch.STATUS_AVAILABLE,
            revision = 1,
            content = body,
            digest = Digest.sum(body),
            deps = deps,
        )
        val members = listOf(root) + procs
        val pack = Digest.closureSum(members.map { Digest.Member(it.id, it.revision, it.digest, it.content) })
        return ClosureSnapshotPlain(
            kind = Pouch.KIND_PROJECT,
            assetId = rootId,
            revision = 1,
            level = Pouch.LEVEL_FACTORY,
            status = Pouch.STATUS_AVAILABLE,
            digest = pack,
            targetClientId = client,
            members = members,
        )
    }
}
