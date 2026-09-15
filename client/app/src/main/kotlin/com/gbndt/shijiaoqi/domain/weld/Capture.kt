package com.gbndt.shijiaoqi.domain.weld

import com.gbndt.shijiaoqi.model.Pose

/** 从 8083 位姿采点；没有实时数据则不成点。 */
object Capture {
    fun snapshot(pose: Pose?, joints: List<Double>): Pair<Pose, List<Double>>? {
        if (pose == null || joints.isEmpty()) return null
        return pose to joints
    }
}
