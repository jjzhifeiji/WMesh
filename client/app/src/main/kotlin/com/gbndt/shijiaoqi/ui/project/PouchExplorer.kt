package com.gbndt.shijiaoqi.ui.project

import com.gbndt.shijiaoqi.domain.shared.ProcessChoice
import com.gbndt.shijiaoqi.domain.shared.ProjectChoice
import java.util.UUID

/** 资源浏览器一行：文件夹或袋里的一份工艺/工程。 */
data class ExplorerItem(
    val key: String,
    val name: String,
    val isDirectory: Boolean = false,
    val isProject: Boolean = false,
    val isProcess: Boolean = false,
    val deletable: Boolean = false,
    val assetId: UUID? = null,
    val copyable: Boolean = true,
)

/** 袋里三级目录，对接厂端平台级 / 厂级 / 个人级。 */
object PouchFolders {
    const val PLATFORM = "平台级"
    const val FACTORY = "厂级"
    const val PERSONAL = "个人级"

    fun isPersonal(path: String): Boolean = path == PERSONAL

    fun isLocked(path: String): Boolean = path == PLATFORM || path == FACTORY

    fun folderOf(level: String): String = when (level) {
        "platform" -> PLATFORM
        "personal" -> PERSONAL
        else -> FACTORY
    }

    fun roots(): List<ExplorerItem> = listOf(
        ExplorerItem(key = PLATFORM, name = PLATFORM, isDirectory = true),
        ExplorerItem(key = FACTORY, name = FACTORY, isDirectory = true),
        ExplorerItem(key = PERSONAL, name = PERSONAL, isDirectory = true),
    )

    fun processes(all: List<ProcessChoice>, path: String): List<ExplorerItem> {
        if (path.isEmpty()) return roots()
        return all.filter { folderOf(it.level) == path }
            .sortedBy { it.name.ifBlank { it.id.toString() } }
            .map {
                ExplorerItem(
                    key = it.id.toString(),
                    name = it.name.ifBlank { it.id.toString() },
                    isProcess = true,
                    deletable = path == PERSONAL,
                    assetId = it.id,
                    copyable = it.copyable,
                )
            }
    }

    fun projects(all: List<ProjectChoice>, path: String): List<ExplorerItem> {
        if (path.isEmpty()) return roots()
        return all.filter { folderOf(it.level) == path }
            .sortedBy { it.name.ifBlank { it.id.toString() } }
            .map {
                ExplorerItem(
                    key = it.id.toString(),
                    name = it.name.ifBlank { it.id.toString() },
                    isProject = true,
                    deletable = path == PERSONAL && !it.active,
                    assetId = it.id,
                )
            }
    }
}
