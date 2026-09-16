package com.gbndt.shijiaoqi.domain.single

import com.gbndt.shijiaoqi.model.Pose
import com.gbndt.shijiaoqi.model.WeldPointType
import com.gbndt.shijiaoqi.model.WeldProcess
import com.gbndt.shijiaoqi.domain.shared.ProcessBind
import com.gbndt.shijiaoqi.domain.shared.ProcessRef
import com.gbndt.shijiaoqi.domain.shared.ProcessSource
import com.gbndt.shijiaoqi.geom.Vec3
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.UUID

class CornerWeldTest {
    @Test
    fun eachLayerIsStartSafeStartEndEndSafe() {
        val layers = CornerRun.generateLayers(sampleSpec(layerCount = 3))
        assertEquals(3, layers.size)
        layers.forEach { layer ->
            assertEquals(CornerRun.POINT_ORDER, layer.points.map { it.type })
        }
    }

    @Test
    fun lengthShrinksAndStopsWhenNonPositive() {
        val layers = CornerRun.generateLayers(
            sampleSpec(layerCount = 10, initialLength = 5.0, lengthReduction = 2.0),
        )
        assertEquals(listOf(5.0, 3.0, 1.0), layers.map { it.length })
        assertEquals(5.0, CornerRun.layerLength(15.0, 2.0, 5), 0.0)
    }

    @Test
    fun startEndSpanEqualsLayerLengthOnRightAngleWalls() {
        val layers = CornerRun.generateLayers(sampleSpec(layerCount = 2, initialLength = 15.0, lengthReduction = 2.0))
        layers.forEach { layer ->
            assertEquals(layer.length, layer.start.distanceTo(layer.end), 1e-6)
        }
        assertTrue(layers[1].start.z > layers[0].start.z)
        assertEquals(2.0, layers[1].start.z - layers[0].start.z, 1e-6)
    }

    @Test
    fun intersectionIsMidpointOfCommonPerpendicular() {
        val hit = CornerRun.intersection(
            Vec3(0.0, 0.0, 0.0),
            Vec3(1.0, 0.0, 0.0),
            Vec3(0.0, 1.0, 1.0),
            Vec3(0.0, 2.0, 1.0),
        )!!
        assertEquals(0.0, hit.x, 1e-9)
        assertEquals(0.0, hit.y, 1e-9)
        assertEquals(0.5, hit.z, 1e-9)
        assertNull(
            CornerRun.intersection(
                Vec3(0.0, 0.0, 0.0),
                Vec3(1.0, 0.0, 0.0),
                Vec3(0.0, 1.0, 0.0),
                Vec3(1.0, 1.0, 0.0),
            )
        )
    }

    @Test
    fun generatedPathsWriteProcessIdNotPath() {
        val id = UUID.randomUUID().toString()
        val paths = CornerWeldGenerator.generateCornerPaths(
            baseProcess = WeldProcess(name = "角焊"),
            processId = id,
            cornerPose = Pose(0.0, 0.0, 0.0, 0.0, 0.0, 0.0),
            safeStartPose = Pose(1.0, 0.0, 10.0, 0.0, 0.0, 0.0),
            safeEndPose = Pose(1.0, 0.0, 20.0, 0.0, 0.0, 0.0),
            vecAIn = Vec3(1.0, 0.0, 0.0),
            vecBIn = Vec3(0.0, 0.0, 1.0),
            initialLength = 15.0,
            layerCount = 2,
            upwardOffset = 2.0,
            lengthReduction = 2.0,
            refPathAId = "a",
            refPathBId = "b",
        )
        assertEquals(2, paths.size)
        paths.forEach { path ->
            assertEquals(id, path.processId)
            assertEquals(id, path.cornerGroupParams!!.processId)
            assertEquals(CornerRun.POINT_ORDER, path.points.map { it.type })
        }
        assertTrue(paths[0].cornerGroupParams!!.isMaster)
        assertTrue(!paths[1].cornerGroupParams!!.isMaster)
    }

    @Test
    fun parseUsesCornerProcessId() {
        val id = UUID.randomUUID()
        val json = """
            [{"name":"c","processId":"","points":[],
              "cornerGroupParams":{"groupId":"g","refPathAId":"a","refPathBId":"b",
              "layerCount":2,"initialLength":15.0,"upwardOffset":2.0,"lengthReduction":2.0,
              "processId":"$id"}}]
        """.trimIndent()
        val paths = SingleLayerProject.parse(json.toByteArray())
        assertEquals(1, paths.size)
        assertEquals(id.toString(), paths[0].processId)
        assertEquals(id.toString(), paths[0].cornerGroupParams!!.processId)
        assertFalse(SingleLayerProject.encode(paths).decodeToString().contains("\"processPath\""))
    }

    @Test
    fun emptyProcessIdAllowedMissingMemberRefused() {
        val src = ProcessSource { null }
        val empty = ProcessBind.resolve(listOf(ProcessRef("包角", "", true)), src)
        assertTrue(empty.missing.isEmpty())
        val id = UUID.randomUUID()
        val missing = ProcessBind.resolve(listOf(ProcessRef("包角", id.toString(), true)), src)
        assertEquals(listOf("包角: $id"), missing.missing)
    }

    private fun sampleSpec(
        layerCount: Int = 3,
        initialLength: Double = 15.0,
        lengthReduction: Double = 2.0,
        upwardOffset: Double = 2.0,
    ) = CornerSpec(
        cornerPose = Pose(0.0, 0.0, 0.0, 10.0, 20.0, 30.0),
        safeStartPose = Pose(5.0, 0.0, 10.0, 0.0, 0.0, 0.0),
        safeEndPose = Pose(5.0, 0.0, 20.0, 0.0, 0.0, 0.0),
        weldOrientation = Pose(0.0, 0.0, 0.0, 1.0, 2.0, 3.0),
        vecA = Vec3(1.0, 0.0, 0.0),
        vecB = Vec3(0.0, 0.0, 1.0),
        initialLength = initialLength,
        layerCount = layerCount,
        upwardOffset = upwardOffset,
        lengthReduction = lengthReduction,
    )
}
