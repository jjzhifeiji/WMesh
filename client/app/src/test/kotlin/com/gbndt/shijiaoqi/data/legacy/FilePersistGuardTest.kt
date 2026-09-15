package com.gbndt.shijiaoqi.data.legacy

import com.gbndt.shijiaoqi.model.WeldProcess
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.nio.file.Files

/** 工艺/工程不得再写 JSON 文件。 */
class FilePersistGuardTest {
    @Test
    fun processManagerNeverWritesJson() {
        val dir = Files.createTempDirectory("wmesh-proc").toFile()
        val pm = ProcessManager()
        pm.saveProcess(dir.absolutePath, WeldProcess(name = "leak", current = 180.0))
        assertNull(pm.loadProcess("leak.json"))
        assertFalse(pm.checkProcessExists("leak.json"))
        assertTrue(pm.listContents("").isEmpty())
        assertFalse(dir.walkTopDown().any { it.isFile && it.extension == "json" })
        assertFalse(pm.zipFileOrFolder("leak.json", dir.resolve("out.zip")))
        assertFalse(dir.resolve("out.zip").exists())
    }

    @Test
    fun projectManagerNeverWritesProjectJson() {
        val pm = ProjectManager()
        pm.saveStandardProject("secret", emptyList())
        pm.saveMultiLayerProject("secret", emptyList())
        pm.saveLicense("plain-license")
        assertTrue(pm.loadStandardProject("secret").isEmpty())
        assertTrue(pm.loadMultiLayerProject("secret").isEmpty())
        assertFalse(pm.hasLicense())
        assertTrue(pm.loadLicense().isEmpty())
    }
}
