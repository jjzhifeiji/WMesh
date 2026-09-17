/** 本机袋只四张表：配置、工艺、工程、设备。密钥不进库。 */
package com.gbndt.shijiaoqi.data.db

import androidx.room.Dao
import androidx.room.Database
import androidx.room.Entity
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.PrimaryKey
import androidx.room.Query
import androidx.room.RoomDatabase
import androidx.room.Transaction

/** 配置：登录人、厂策、令牌、激活工程、发号台账；单行。 */
@Entity(tableName = "config")
data class ConfigRow(
    @PrimaryKey val id: Int = 1, // 单行主键，固定 1
    val personId: String = "", // 人员稳定身份
    val loginName: String = "", // 厂内登录名
    val displayName: String = "", // 显示名
    val roles: String = "", // 厂端授予角色，逗号分隔
    val revision: Long = 0, // 策略修订
    val persistUnwrapKey: Boolean = false, // 解封钥可否落盘
    val keyTtlSeconds: Long = 0, // 登录时效秒；0 表示直到退出
    val encryptPouch: Boolean = true, // 本机库是否 SQLCipher
    val token: String = "", // 登录令牌
    val expiresAtMillis: Long = 0, // 到期毫秒；0 表示直到退出
    val loggedInAtMillis: Long = 0, // 本次登录时间
    val activeProjectId: String? = null, // 当前激活工程；空为未激活
    val activeRevision: Long = 0, // 激活时修订
    val ledger: String = "", // 发号与待汇聚，不含工艺明文
)

/** 工艺信封密文；明文永不进这列。 */
@Entity(tableName = "processes")
data class ProcessRow(
    @PrimaryKey val id: String, // 工艺稳定身份
    val level: String, // factory / personal / platform
    val ownerId: String?, // 个人级创建人；其余为空
    val name: String, // 显示名，不当身份
    val revision: Long, // 当前修订
    val blob: ByteArray, // 信封密文，工艺明文永不进这列
)

/** 工程明文；焊道只写 processId，不含工艺正文。 */
@Entity(tableName = "projects")
data class ProjectRow(
    @PrimaryKey val id: String, // 工程稳定身份
    val revision: Long, // 当前修订
    val kind: String, // 固定 project
    val level: String, // factory / personal / platform
    val status: String, // 送达时状态，与源相同
    val name: String, // 显示名
    val digest: ByteArray, // 工程正文 SHA-256
    val members: String, // 元数据 JSON，不含工艺正文
    val blob: ByteArray, // 工程明文 JSON；工艺用 processId 引用
)

/** 本账号可见的 Client；不含解封钥。 */
@Entity(tableName = "devices")
data class DeviceRow(
    @PrimaryKey val id: String, // Client 身份
    val name: String, // 设备显示名
    val deviceSerial: String, // 机械臂识别号
    val shortCode: String, // 本机短号
)

/** 四张表的读写；整表替换避免半份工程。 */
@Dao
abstract class PouchDao {
    @Query("SELECT * FROM config WHERE id = 1")
    abstract fun getConfig(): ConfigRow?

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    abstract fun upsertConfig(row: ConfigRow)

    @Query("SELECT * FROM processes")
    abstract fun allProcesses(): List<ProcessRow>

    @Query("SELECT * FROM processes WHERE id = :id")
    abstract fun getProcess(id: String): ProcessRow?

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    abstract fun upsertProcess(row: ProcessRow)

    @Query("DELETE FROM processes WHERE id = :id")
    abstract fun deleteProcess(id: String)

    @Query("DELETE FROM processes")
    abstract fun clearProcesses()

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    abstract fun insertProcesses(rows: List<ProcessRow>)

    @Query("SELECT * FROM projects")
    abstract fun allProjects(): List<ProjectRow>

    @Query("SELECT * FROM projects WHERE id = :id")
    abstract fun getProject(id: String): ProjectRow?

    @Query("DELETE FROM projects")
    abstract fun clearProjects()

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    abstract fun insertProjects(rows: List<ProjectRow>)

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    abstract fun upsertProject(row: ProjectRow)

    @Query("DELETE FROM projects WHERE id = :id")
    abstract fun deleteProject(id: String)

    @Query("DELETE FROM devices")
    abstract fun clearDevices()

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    abstract fun insertDevices(rows: List<DeviceRow>)

    @Query("SELECT * FROM devices")
    abstract fun allDevices(): List<DeviceRow>

    /** 工艺表整表换成袋内当前份。 */
    @Transaction
    open fun replaceProcesses(rows: List<ProcessRow>) {
        clearProcesses()
        if (rows.isNotEmpty()) insertProcesses(rows)
    }

    /** 工程表整表换成袋内当前份。 */
    @Transaction
    open fun replaceProjects(rows: List<ProjectRow>) {
        clearProjects()
        if (rows.isNotEmpty()) insertProjects(rows)
    }

    /** 整表换成本次登录可见的设备。 */
    @Transaction
    open fun replaceDevices(rows: List<DeviceRow>) {
        clearDevices()
        if (rows.isNotEmpty()) insertDevices(rows)
    }
}

/** 本机袋库：四张表，不含密钥、不含工艺明文。 */
@Database(
    entities = [ConfigRow::class, ProcessRow::class, ProjectRow::class, DeviceRow::class],
    version = 5,
    exportSchema = false,
)
abstract class PouchDatabase : RoomDatabase() {
    abstract fun dao(): PouchDao
}
