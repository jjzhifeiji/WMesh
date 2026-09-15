import kotlin.math.*

class Vector3(val x: Double, val y: Double, val z: Double) {
    operator fun minus(other: Vector3) = Vector3(x - other.x, y - other.y, z - other.z)
    operator fun plus(other: Vector3) = Vector3(x + other.x, y + other.y, z + other.z)
    operator fun times(scalar: Double) = Vector3(x * scalar, y * scalar, z * scalar)
    fun cross(other: Vector3) = Vector3(
        y * other.z - z * other.y,
        z * other.x - x * other.z,
        x * other.y - y * other.x
    )
    fun dot(other: Vector3) = x * other.x + y * other.y + z * other.z
    fun length() = sqrt(x * x + y * y + z * z)
    fun normalize(): Vector3 {
        val len = length()
        return if (len > 0) this * (1.0 / len) else this
    }
}

fun test(angleC: Double, angle2: Double, angle3: Double) {
    val center = Vector3(0.0, 0.0, 0.0)
    val v1 = Vector3(100.0, 0.0, 0.0)
    
    val rad2 = Math.toRadians(angle2)
    val v2 = Vector3(100.0 * cos(rad2), 100.0 * sin(rad2), 0.0)
    
    val rad3 = Math.toRadians(angle3)
    val v3 = Vector3(100.0 * cos(rad3), 100.0 * sin(rad3), 0.0)
    
    val radC = Math.toRadians(angleC)
    val vC = Vector3(100.0 * cos(radC), 100.0 * sin(radC), 0.0)

    val v12 = v2 - v1
    val v23 = v3 - v2
    var normal = v12.cross(v23).normalize()
    
    val cp1 = (v1 - center).normalize()
    val cp2 = (v2 - center).normalize()
    val cp3 = (v3 - center).normalize()
    val cpC = (vC - center).normalize()
    
    val xAxis = cp1
    val yAxis = normal.cross(cp1).normalize()

    val xC = cpC.dot(xAxis)
    val yC = cpC.dot(yAxis)
    val xM = cp2.dot(xAxis)
    val yM = cp2.dot(yAxis)

    val cross2D = xC * yM - yC * xM
    println("Angle2=$angle2, Angle3=$angle3, AngleC=$angleC | cross2D = $cross2D -> ${if(cross2D > 0) "BEFORE" else "AFTER"}")
}

test(45.0, 90.0, 180.0) // Before mid
test(135.0, 90.0, 180.0) // After mid
test(10.0, 20.0, 40.0) // Before mid
test(30.0, 20.0, 40.0) // After mid
test(300.0, 175.0, 350.0) // After mid
test(100.0, 175.0, 350.0) // Before mid
