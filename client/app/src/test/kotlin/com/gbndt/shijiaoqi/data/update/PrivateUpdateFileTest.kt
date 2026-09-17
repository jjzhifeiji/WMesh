package com.gbndt.shijiaoqi.data.update

import com.gbndt.shijiaoqi.config.AppConfig
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File
import kotlin.io.path.createTempDirectory

class PrivateUpdateFileTest {
    @Test
    fun staysUnderFilesDir() {
        val root = createTempDirectory("wmesh-files").toFile()
        val dest = privateUpdateFile(root, "ShiJiaoQi_v51.apk")
        assertEquals(File(File(root, AppConfig.UPDATE_DIR), "ShiJiaoQi_v51.apk"), dest)
        assertTrue(dest.canonicalPath.startsWith(root.canonicalPath + File.separator))
    }

    @Test
    fun stripsParentPath() {
        val root = createTempDirectory("wmesh-files").toFile()
        val dest = privateUpdateFile(root, "../escape.apk")
        assertEquals("escape.apk", dest.name)
        assertEquals(AppConfig.UPDATE_DIR, dest.parentFile?.name)
        assertTrue(dest.canonicalPath.startsWith(root.canonicalPath + File.separator))
    }
}
