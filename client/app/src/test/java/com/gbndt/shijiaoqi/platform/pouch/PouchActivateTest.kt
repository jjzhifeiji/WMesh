package com.gbndt.shijiaoqi.platform.pouch

import com.gbndt.shijiaoqi.platform.crypt.Wm2
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.UUID

class PouchActivateTest {
    @Test
    fun activateThenOpenProcessById() {
        val (p, client) = loggedPouch()
        val body = """{"current":180}""".toByteArray()
        val proc = processMember("工艺", body)
        val snap = projectSnap(client, "工程", proc)
        p.cacheClosure(snap)
        p.activate(snap.assetId)
        assertArrayEquals(body, p.openProcess(proc.id))
        assertReject(Pouch.ERR_NOT_FOUND) { p.openProcess(snap.assetId) }
    }

    @Test
    fun rejectsIncompleteMismatchCopyFullWeldAndOfflineCurrent() {
        val (p, client) = loggedPouch()
        val proc = processMember("工艺", """{"a":1}""".toByteArray())
        val good = projectSnap(client, "工程", proc)
        p.cacheClosure(good)

        val root = good.members.first()
        val missing = good.copy(
            members = listOf(root),
            digest = Digest.closureSum(listOf(Digest.Member(root.id, root.revision, root.digest, root.content))),
        )
        assertReject(Pouch.ERR_INCOMPLETE) { p.cacheClosure(missing) }

        val mismatch = projectSnap(client, "串版", processMember("工艺", """{"a":1}""".toByteArray()))
        val mismatchRoot = mismatch.members.first()
        val brokenRoot = mismatchRoot.copy(deps = listOf(mismatchRoot.deps.first().copy(revision = 99)))
        assertReject(Pouch.ERR_MISMATCH) { p.cacheClosure(mismatch.copy(members = listOf(brokenRoot) + mismatch.members.drop(1))) }

        val tamperedProc = processMember("工艺", """{"a":1}""".toByteArray())
        val tampered = projectSnap(client, "摘要", tamperedProc)
        val dirty = tampered.members.toMutableList()
        dirty[0] = dirty[0].copy(content = "tampered".toByteArray())
        assertReject(Pouch.ERR_INTEGRITY) { p.cacheClosure(tampered.copy(members = dirty)) }

        val copied = projectSnap(UUID.randomUUID(), "只拷", processMember("他机", """{"b":1}""".toByteArray()))
        assertReject(Pouch.ERR_FORBIDDEN) { p.cacheClosure(copied) }

        p.setPolicy(1, Pouch.SCOPE_ALL)
        val second = projectSnap(client, "第二份", processMember("工艺2", """{"c":1}""".toByteArray()))
        assertReject(Pouch.ERR_CACHE_FULL) { p.cacheClosure(second) }

        p.setPolicy(2, Pouch.SCOPE_ALL)
        p.cacheClosure(second)
        p.activate(good.assetId)
        p.setWelding(true)
        assertReject(Pouch.ERR_FORBIDDEN) { p.activate(second.assetId) }
        assertEquals(good.assetId, p.activeProject())
        p.setWelding(false)

        p.setPolicy(2, Pouch.SCOPE_CURRENT)
        p.setOnline(false)
        assertReject(Pouch.ERR_NOT_FOUND) { p.activate(UUID.randomUUID()) }
    }

    @Test
    fun currentScopeDropsOtherProjects() {
        val (p, client) = loggedPouch()
        p.setPolicy(2, Pouch.SCOPE_CURRENT)
        val aProc = processMember("A工艺", """{"a":1}""".toByteArray())
        val bProc = processMember("B工艺", """{"b":1}""".toByteArray())
        val a = projectSnap(client, "A", aProc)
        val b = projectSnap(client, "B", bProc)
        p.cacheClosure(a)
        p.cacheClosure(b)
        p.activate(b.assetId)
        assertFalse(p.hasClosure(a.assetId))
        assertThrows(NoSuchElementException::class.java) { p.open(aProc.id) }
        assertArrayEquals("""{"b":1}""".toByteArray(), p.openProcess(bProc.id))
    }

    @Test
    fun personalActivateFollowsOwner() {
        val unwrap = Wm2.randomKey()
        val who = UUID.randomUUID()
        val client = UUID.randomUUID()
        val p = Pouch()
        p.login(unwrap, who, false)
        p.bindClient(client)
        val proc = processMember("工艺", """{"p":1}""".toByteArray())
        val snap = projectSnap(client, "我的工程", proc)
        val root = snap.members.first().copy(level = Pouch.LEVEL_PERSONAL, ownerId = who)
        p.cacheClosure(snap.copy(level = Pouch.LEVEL_PERSONAL, members = listOf(root) + snap.members.drop(1)))
        p.logout()
        p.login(unwrap, UUID.randomUUID(), false)
        p.bindClient(client)
        assertReject(Pouch.ERR_FORBIDDEN) { p.activate(snap.assetId) }
    }

    @Test
    fun cacheTransitThenActivate() {
        val unwrap = Wm2.randomKey()
        val dek = Wm2.randomKey()
        val who = UUID.randomUUID()
        val factoryId = UUID.randomUUID()
        val client = UUID.randomUUID()
        val procBody = """{"current":180}""".toByteArray()
        val proc = processMember("工艺", procBody)
        val snap = projectSnap(client, "工程", proc)
        val fid = Pouch.uuidBytes(factoryId)
        val cid = Pouch.uuidBytes(client)
        val sealed = snap.members.map { m ->
            TransitMember(
                id = m.id,
                level = m.level,
                name = m.name,
                revision = m.revision,
                ownerId = m.ownerId,
                content = Wm2.seal(dek, m.content, Wm2.clientTransitAad(fid, cid, Pouch.uuidBytes(m.id), m.revision)),
                kind = m.kind,
                status = m.status,
                digest = m.digest,
                deps = m.deps,
            )
        }
        val wrap = Wm2.seal(unwrap, dek, Wm2.clientTransitDekAad(fid, cid))
        val p = Pouch()
        p.login(unwrap, who, false)
        p.bindClient(client)
        p.cacheTransit(
            factoryId,
            client,
            TransitClosure(
                wrap = wrap,
                members = sealed,
                assetId = snap.assetId,
                revision = snap.revision,
                kind = snap.kind,
                level = snap.level,
                status = snap.status,
                digest = snap.digest,
                targetClientId = client,
            ),
        )
        p.activate(snap.assetId)
        assertArrayEquals(procBody, p.openProcess(proc.id))
    }

    private fun assertReject(code: String, block: () -> Unit) {
        val e = assertThrows(PouchRejected::class.java, block)
        assertEquals(code, e.code)
    }

    private fun loggedPouch(): Pair<Pouch, UUID> {
        val p = Pouch()
        p.login(Wm2.randomKey(), UUID.randomUUID(), false)
        val client = UUID.randomUUID()
        p.bindClient(client)
        return p to client
    }

    private fun processMember(name: String, body: ByteArray): ClosureMemberPlain {
        return ClosureMemberPlain(
            id = UUID.randomUUID(),
            kind = Pouch.KIND_PROCESS,
            level = Pouch.LEVEL_FACTORY,
            name = name,
            status = Pouch.STATUS_AVAILABLE,
            revision = 1,
            content = body,
            digest = Digest.sum(body),
        )
    }

    private fun projectSnap(client: UUID, name: String, vararg procs: ClosureMemberPlain): ClosureSnapshotPlain {
        val deps = procs.map { AssetDep(it.id, it.revision, it.digest) }
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
