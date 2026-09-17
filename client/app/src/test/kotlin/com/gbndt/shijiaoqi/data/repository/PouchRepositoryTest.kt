package com.gbndt.shijiaoqi.data.repository

import com.gbndt.shijiaoqi.data.pouch.AssetDep
import com.gbndt.shijiaoqi.data.pouch.ClosureMemberPlain
import com.gbndt.shijiaoqi.data.pouch.ClosureSnapshotPlain
import com.gbndt.shijiaoqi.data.pouch.Digest
import com.gbndt.shijiaoqi.data.pouch.Pouch
import com.gbndt.shijiaoqi.data.session.BagSession
import com.gbndt.shijiaoqi.data.session.FakeFactory
import com.gbndt.shijiaoqi.data.session.MemoryEnvelopeStore
import com.gbndt.shijiaoqi.data.session.MemoryIdentityStore
import com.gbndt.shijiaoqi.data.session.PadDevice
import com.gbndt.shijiaoqi.data.session.SessionGate
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Test
import java.util.UUID

/** 袋出口：激活与打开工程走挂起，目录走 Flow。 */
class PouchRepositoryTest {
    @Test
    fun activateThenReadProjectPlain() = runBlocking {
        val cid = UUID.randomUUID()
        val bag = BagSession(
            { "ARM-1" },
            FakeFactory(devices = listOf(PadDevice(cid.toString(), "焊机", "ARM-1", "C0008"))),
            MemoryIdentityStore(),
            MemoryEnvelopeStore(),
        )
        bag.login("http://f", UUID.randomUUID().toString(), "op", "p")
        val body = """{"items":[]}""".toByteArray()
        val proc = ClosureMemberPlain(
            id = UUID.randomUUID(),
            kind = Pouch.KIND_PROCESS,
            level = Pouch.LEVEL_FACTORY,
            name = "工艺",
            status = Pouch.STATUS_AVAILABLE,
            revision = 1,
            content = """{"a":1}""".toByteArray(),
            digest = Digest.sum("""{"a":1}""".toByteArray()),
        )
        val rootId = UUID.randomUUID()
        val root = ClosureMemberPlain(
            id = rootId,
            kind = Pouch.KIND_PROJECT,
            level = Pouch.LEVEL_FACTORY,
            name = "工程",
            status = Pouch.STATUS_AVAILABLE,
            revision = 1,
            content = body,
            digest = Digest.sum(body),
            deps = listOf(AssetDep(proc.id, proc.revision, proc.digest)),
        )
        val members = listOf(root, proc)
        bag.cacheClosure(
            ClosureSnapshotPlain(
                kind = Pouch.KIND_PROJECT,
                assetId = rootId,
                revision = 1,
                level = Pouch.LEVEL_FACTORY,
                status = Pouch.STATUS_AVAILABLE,
                digest = Digest.closureSum(members.map { Digest.Member(it.id, it.revision, it.digest, it.content) }),
                targetClientId = cid,
                members = members,
            ),
        )
        val repo = PouchRepository(bag, SessionGate(), Dispatchers.Unconfined)
        repo.refresh()
        repo.activate(rootId)
        assertEquals(rootId, repo.activeProjectId())
        assertEquals(1, repo.projects.value.count { it.active && it.id == rootId })
        val got = repo.openProjectBytes(rootId)
        assertArrayEquals(body, got)
    }
}
