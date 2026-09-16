package com.gbndt.shijiaoqi.data.pouch

import com.gbndt.shijiaoqi.data.crypt.Wm2
import com.gbndt.shijiaoqi.domain.shared.ProjectRefs
import com.gbndt.shijiaoqi.domain.single.SingleLayerProject
import com.gbndt.shijiaoqi.domain.tbar.TBarProject
import com.gbndt.shijiaoqi.domain.tbar.TBarRun
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.UUID

/** C0 矩阵里本机袋能判定的允许/拒绝。 */
class ClientMatrixTest {
    @Test
    fun c0_2_15_parseUsesProcessId() {
        val id = UUID.randomUUID()
        val json = """[{"name":"w","processId":"$id","points":[]}]"""
        val paths = SingleLayerProject.parse(json.toByteArray())
        assertEquals(id.toString(), paths[0].processId)
        assertEquals(listOf(id.toString()), ProjectRefs.idsOf(paths[0]))
        assertFalse(SingleLayerProject.encode(paths).decodeToString().contains("\"processPath\""))
    }

    @Test
    fun c0_11_emptyIdNotInRefs() {
        val json = """[{"name":"w","processId":"","points":[]}]"""
        val paths = SingleLayerProject.parse(json.toByteArray())
        assertTrue(ProjectRefs.idsOf(paths[0]).isEmpty())
        assertTrue(ProjectRefs.missing(emptyList(), setOf(UUID.randomUUID())).isEmpty())
    }

    @Test
    fun c0_10_14_nonEmptyIdMustBeInDeps() {
        val inDep = UUID.randomUUID()
        val stray = UUID.randomUUID()
        val missing = ProjectRefs.missing(listOf(inDep.toString(), stray.toString()), setOf(inDep))
        assertEquals(listOf(stray.toString()), missing)
    }

    @Test
    fun c0_13_singleLayerGapBandsNotTBar() {
        val json = """[{"name":"单层","processId":"${UUID.randomUUID()}","gapBands":[{"minGap":3.0,"maxGap":4.0,"layer":1}],"points":[]}]"""
        assertTrue(TBarProject.parse(json.toByteArray()).isEmpty())
        val tbar = """[{"name":"T","kind":"${TBarRun.KIND}","gapBands":[{"minGap":3.0,"maxGap":4.0,"layer":1}],"points":[]}]"""
        assertEquals(1, TBarProject.parse(tbar.toByteArray()).size)
    }

    @Test
    fun c0_8_9_8d_loginRequiredAndDefaultKeyNotOnDisk() {
        val p = Pouch()
        val id = UUID.randomUUID()
        val who = UUID.randomUUID()
        val plain = """{"n":1}""".toByteArray()
        assertTrue(runCatching { p.putPlain(id, Pouch.LEVEL_FACTORY, "厂级", 1, null, plain) }.isFailure)
        p.login(Wm2.randomKey(), who, false)
        p.putPlain(id, Pouch.LEVEL_FACTORY, "厂级", 1, null, plain)
        p.logout()
        assertFalse(p.hasUnwrapKey())
        assertTrue(runCatching { p.open(id) }.isFailure)
        p.diskSnapshot().forEach { env ->
            assertFalse(String(env.blob, Charsets.ISO_8859_1).contains("""{"n":1}"""))
            assertTrue(Wm2.isEnvelope(env.blob))
        }
    }

    @Test
    fun c0_22_23_copyWithoutBindAndWeldLock() {
        val unwrap = Wm2.randomKey()
        val who = UUID.randomUUID()
        val mine = UUID.randomUUID()
        val other = UUID.randomUUID()
        val body = """{"a":1}""".toByteArray()
        val proc = member(Pouch.KIND_PROCESS, "工艺", body)
        val snap = project(mine, "工程", proc)
        val p = Pouch()
        p.login(unwrap, who, false)
        p.bindClient(mine)
        p.cacheClosure(snap)
        p.activate(snap.assetId)
        p.setWelding(true)
        val secondProc = member(Pouch.KIND_PROCESS, "工艺2", """{"b":1}""".toByteArray())
        val second = project(mine, "第二", secondProc)
        p.setPolicy(2, Pouch.SCOPE_ALL)
        p.cacheClosure(second)
        val e = runCatching { p.activate(second.assetId) }.exceptionOrNull() as PouchRejected
        assertEquals(Pouch.ERR_FORBIDDEN, e.code)
        p.setWelding(false)

        val copy = Pouch()
        copy.login(unwrap, who, false)
        val copied = runCatching { copy.cacheClosure(project(other, "只拷", member(Pouch.KIND_PROCESS, "他", body))) }
        assertEquals(Pouch.ERR_FORBIDDEN, (copied.exceptionOrNull() as PouchRejected).code)
    }

    @Test
    fun c0_8c_currentOfflineMissingRefused() {
        val p = Pouch()
        p.login(Wm2.randomKey(), UUID.randomUUID(), false)
        p.bindClient(UUID.randomUUID())
        p.setPolicy(2, Pouch.SCOPE_CURRENT)
        p.setOnline(false)
        val e = runCatching { p.activate(UUID.randomUUID()) }.exceptionOrNull() as PouchRejected
        assertEquals(Pouch.ERR_NOT_FOUND, e.code)
    }

    @Test
    fun c0_25_issueStaysPersonal() {
        val p = Pouch()
        p.login(Wm2.randomKey(), UUID.randomUUID(), false)
        p.setOrigin("C0008")
        val issued = p.issuePersonal(Pouch.KIND_PROCESS, "我的", """{"n":1}""".toByteArray())
        assertEquals(Pouch.LEVEL_PERSONAL, issued.level)
        assertFalse(issued.level == Pouch.LEVEL_PLATFORM)
    }

    private fun member(kind: String, name: String, body: ByteArray) = ClosureMemberPlain(
        id = UUID.randomUUID(),
        kind = kind,
        level = Pouch.LEVEL_FACTORY,
        name = name,
        status = Pouch.STATUS_AVAILABLE,
        revision = 1,
        content = body,
        digest = Digest.sum(body),
    )

    private fun project(client: UUID, name: String, proc: ClosureMemberPlain): ClosureSnapshotPlain {
        val body = """{"items":[]}""".toByteArray()
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
            deps = listOf(AssetDep(proc.id, proc.revision, proc.digest)),
        )
        val members = listOf(root, proc)
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
