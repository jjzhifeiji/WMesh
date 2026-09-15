package com.gbndt.shijiaoqi.data.pouch

import com.gbndt.shijiaoqi.data.crypt.Wm2
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.UUID

class PouchIssueTest {
    @Test
    fun assetCodeMatchesGoShape() {
        assertEquals("GY-W-000001", AssetCode.format("process", AssetCode.ORIGIN_WAN, 1))
        assertEquals("GC-F01-000045", AssetCode.format("project", "F01", 45))
        assertEquals("GY-C0008-000012", AssetCode.format("process", "C0008", 12))
        assertReject(AssetCode.ERR_EXHAUSTED) { AssetCode.format("process", AssetCode.ORIGIN_WAN, 0) }
        assertTrue(AssetCode.matchKind("process", "GY-W-000001"))
        assertFalse(AssetCode.matchKind("project", "GY-W-000001"))
        assertEquals("F01", AssetCode.formatFactory(1))
        assertTrue(AssetCode.validFactoryOrigin("F01"))
        assertFalse(AssetCode.validFactoryOrigin("F00"))
        assertEquals("C0008", AssetCode.formatClient(8))
        assertTrue(AssetCode.validClientOrigin("C0008"))
    }

    @Test
    fun twoClientsDoNotCollideAndKindsSplit() {
        val a = logged("C0001")
        val b = logged("C0008")
        val pa = a.issuePersonal(Pouch.KIND_PROCESS, "焊A", """{"n":1}""".toByteArray())
        val pb = b.issuePersonal(Pouch.KIND_PROCESS, "焊B", """{"n":2}""".toByteArray())
        assertEquals("GY-C0001-000001", pa.code)
        assertEquals("GY-C0008-000001", pb.code)
        assertNotEquals(pa.code, pb.code)
        val ga = a.issuePersonal(Pouch.KIND_PROJECT, "工程A", """{"items":[]}""".toByteArray())
        assertEquals("GC-C0001-000001", ga.code)
        val pa2 = a.issuePersonal(Pouch.KIND_PROCESS, "焊A2", """{"n":3}""".toByteArray())
        assertEquals("GY-C0001-000002", pa2.code)
        assertEquals(Pouch.LEVEL_PERSONAL, pa.level)
        assertFalse(String(a.open(pa.id)).contains(pa.code))
    }

    @Test
    fun bindImportedDoesNotBumpAndConflictsRefuse() {
        val p = logged("C0008")
        val imported = UUID.randomUUID()
        p.bindCode(imported, "GY-W-000001")
        val local = p.issuePersonal(Pouch.KIND_PROCESS, "本机", """{"n":1}""".toByteArray())
        assertEquals("GY-C0008-000001", local.code)
        p.bindCode(imported, "GY-W-000001")
        assertReject(Pouch.ERR_CODE_CONFLICT) { p.bindCode(imported, "GY-W-000002") }
        assertReject(Pouch.ERR_CODE_CONFLICT) { p.bindCode(UUID.randomUUID(), "GY-W-000001") }
    }

    @Test
    fun missingOriginAndOverflowRefuse() {
        val p = Pouch()
        p.login(Wm2.randomKey(), UUID.randomUUID(), false)
        assertReject(Pouch.ERR_CODE_MISSING) {
            p.issuePersonal(Pouch.KIND_PROCESS, "无短码", """{"n":1}""".toByteArray())
        }
        p.setOrigin("C0008")
        p.seedSeq(Pouch.KIND_PROCESS, 1_000_000)
        assertReject(Pouch.ERR_CODE_EXHAUSTED) {
            p.issuePersonal(Pouch.KIND_PROCESS, "溢出", """{"n":1}""".toByteArray())
        }
        assertEquals(0, p.pendingFacts().size)
    }

    @Test
    fun personalStaysPersonalAndOtherPersonCannotOpen() {
        val key = Wm2.randomKey()
        val a = UUID.randomUUID()
        val p = Pouch()
        p.login(key, a, false)
        p.setOrigin("C0008")
        val issued = p.issuePersonal(Pouch.KIND_PROCESS, "我的", """{"p":1}""".toByteArray())
        assertEquals(Pouch.LEVEL_PERSONAL, issued.level)
        p.logout()
        p.login(key, UUID.randomUUID(), false)
        assertThrows(SecurityException::class.java) { p.open(issued.id) }
    }

    @Test
    fun pendingAndPointCloudStayOnThisPouch() {
        val a = logged("C0001")
        a.bindClient(UUID.randomUUID())
        a.setRoles(listOf(Pouch.ROLE_OPERATOR))
        val cloud = "pcd-body".toByteArray()
        a.enqueueFact()
        val up = a.enqueueUpload(Pouch.KIND_POINT_CLOUD, cloud)
        assertEquals(1, a.pendingFacts().size)
        assertEquals(1, a.pendingUploads().size)
        assertEquals(Pouch.KIND_POINT_CLOUD, a.pendingUploads().single().kind)
        assertArrayEquals(Digest.sum(cloud), up.digest)
        assertArrayEquals(cloud, a.openPendingUpload(up.id))

        val b = logged("C0008")
        assertEquals(0, b.pendingFacts().size)
        assertEquals(0, b.pendingUploads().size)

        a.logout()
        assertEquals(1, a.pendingFacts().size)
        assertReject(Pouch.ERR_UNAUTHORIZED) { a.openPendingUpload(up.id) }
        a.login(Wm2.randomKey(), UUID.randomUUID(), false)
        assertArrayEquals(cloud, a.openPendingUpload(up.id))
    }

    @Test
    fun auditorCannotEnqueueUnknownKindRefused() {
        val p = logged("C0008")
        p.bindClient(UUID.randomUUID())
        p.setRoles(listOf("auditor"))
        assertReject(Pouch.ERR_FORBIDDEN) { p.enqueueFact() }
        p.setRoles(listOf(Pouch.ROLE_PROCESS_ENGINEER))
        assertReject(Pouch.ERR_FORBIDDEN) { p.enqueueUpload("video", byteArrayOf(1)) }
        val rec = p.enqueueUpload(Pouch.KIND_IMAGE, byteArrayOf(9, 9))
        assertEquals(Pouch.KIND_IMAGE, rec.kind)
        assertEquals(1, p.pendingUploads().size)
    }

    @Test
    fun cachedMemberCodeConflictRefuses() {
        val (p, client) = loggedWithClient("C0008")
        p.bindCode(UUID.randomUUID(), "GY-F01-000001")
        val body = """{"current":180}""".toByteArray()
        val proc = ClosureMemberPlain(
            id = UUID.randomUUID(),
            kind = Pouch.KIND_PROCESS,
            level = Pouch.LEVEL_FACTORY,
            name = "工艺",
            status = Pouch.STATUS_AVAILABLE,
            revision = 1,
            content = body,
            digest = Digest.sum(body),
            code = "GY-F01-000001",
        )
        val snap = projectSnap(client, "工程", proc)
        assertReject(Pouch.ERR_CODE_CONFLICT) { p.cacheClosure(snap) }
        assertFalse(p.hasClosure(snap.assetId))
    }

    @Test
    fun ledgerRoundTripKeepsSeqAndPending() {
        val p = logged("C0008")
        p.bindClient(UUID.randomUUID())
        p.setRoles(listOf(Pouch.ROLE_OPERATOR))
        val issued = p.issuePersonal(Pouch.KIND_PROCESS, "焊", """{"n":1}""".toByteArray())
        p.enqueueFact()
        val raw = p.exportLedger()
        val q = Pouch()
        q.restoreLedger(raw)
        assertEquals("C0008", q.origin())
        assertEquals(issued.code, q.codeOf(issued.id))
        assertEquals(1, q.pendingFacts().size)
        q.login(Wm2.randomKey(), UUID.randomUUID(), false)
        q.setOrigin("C0008")
        val next = q.issuePersonal(Pouch.KIND_PROCESS, "焊2", """{"n":2}""".toByteArray())
        assertEquals("GY-C0008-000002", next.code)
    }

    private fun logged(short: String): Pouch {
        val p = Pouch()
        p.login(Wm2.randomKey(), UUID.randomUUID(), false)
        p.setOrigin(short)
        return p
    }

    private fun loggedWithClient(short: String): Pair<Pouch, UUID> {
        val p = logged(short)
        val client = UUID.randomUUID()
        p.bindClient(client)
        return p to client
    }

    private fun assertReject(code: String, block: () -> Unit) {
        val e = assertThrows(PouchRejected::class.java, block)
        assertEquals(code, e.code)
    }

    private fun projectSnap(client: UUID, name: String, proc: ClosureMemberPlain): ClosureSnapshotPlain {
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
