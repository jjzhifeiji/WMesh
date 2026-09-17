package com.gbndt.shijiaoqi.ui.project

import com.gbndt.shijiaoqi.domain.shared.ProcessChoice
import com.gbndt.shijiaoqi.domain.shared.ProjectChoice
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.UUID

/** 袋里工艺/工程按平台级、厂级、个人级分夹。 */
class PouchExplorerTest {
    @Test
    fun processesGroupByLevelAndOnlyPersonalDeletable() {
        val platform = ProcessChoice(UUID.randomUUID(), "平台", copyable = false, level = "platform")
        val factory = ProcessChoice(UUID.randomUUID(), "厂", copyable = true, level = "factory")
        val personal = ProcessChoice(UUID.randomUUID(), "我的", copyable = true, level = "personal")
        val all = listOf(platform, factory, personal)

        val roots = PouchFolders.processes(all, "")
        assertEquals(listOf("平台级", "厂级", "个人级"), roots.map { it.name })
        assertTrue(roots.all { it.isDirectory && !it.deletable })

        val locked = PouchFolders.processes(all, PouchFolders.FACTORY)
        assertEquals(listOf("厂"), locked.map { it.name })
        assertFalse(locked.single().deletable)

        val mine = PouchFolders.processes(all, PouchFolders.PERSONAL)
        assertEquals(listOf("我的"), mine.map { it.name })
        assertTrue(mine.single().deletable)
        assertTrue(mine.single().isProcess)
    }

    @Test
    fun activePersonalProjectCannotDelete() {
        val active = ProjectChoice(UUID.randomUUID(), "当前", 1, active = true, level = "personal")
        val other = ProjectChoice(UUID.randomUUID(), "备用", 1, active = false, level = "personal")
        val items = PouchFolders.projects(listOf(active, other), PouchFolders.PERSONAL)
        assertFalse(items.first { it.name == "当前" }.deletable)
        assertTrue(items.first { it.name == "备用" }.deletable)
    }
}
