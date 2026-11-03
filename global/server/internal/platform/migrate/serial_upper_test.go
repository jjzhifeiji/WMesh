// 识别号收大写迁移：存量大小写撞号的后来者清空，其余收成大写，之后按大写判重。
package migrate_test

import (
	"testing"
)

// 0031 的旧库里有大小写不同的同号，迁到最新后只留最早那台的号且是大写。
func TestClientSerialUpperMigration(t *testing.T) {
	// 打开一个测完就删的临时库。
	db := openTemp(t)
	// 先迁到收大写之前，模拟已有数据的旧库。
	mustUp(t, db, embeddedFirst(t, 31))
	// 旧库已在 0031。
	wantVersion(t, db, 31)
	// 最早登记的小写号，迁移后应留号并收成大写。
	mustExec(t, db, `INSERT INTO clients (id, name, short_code, device_serial, created_at)
		VALUES ('00000000-0000-0000-0000-000000000001', 'a', 'C0001', 'fa:ke:00:00:00:01', now() - interval '2 hour')`)
	// 后登记的大写同号，迁移后应被清空。
	mustExec(t, db, `INSERT INTO clients (id, name, short_code, device_serial, created_at)
		VALUES ('00000000-0000-0000-0000-000000000002', 'b', 'C0002', 'FA:KE:00:00:00:01', now() - interval '1 hour')`)
	// 不撞号的小写号只收大写。
	mustExec(t, db, `INSERT INTO clients (id, name, short_code, device_serial, created_at)
		VALUES ('00000000-0000-0000-0000-000000000003', 'c', 'C0003', 'arm-x', now())`)
	// 迁到最新。
	mustUp(t, db, embeddedFirst(t, len(embeddedNames(t))))
	// 最早那台留号且是大写。
	if n := countRows(t, db, `SELECT COUNT(*) FROM clients WHERE id = '00000000-0000-0000-0000-000000000001' AND device_serial = 'FA:KE:00:00:00:01'`); n != 1 {
		// 报出最早那台没留住大写号。
		t.Fatalf("first serial kept upper: %d", n)
	}
	// 后来的撞号那台被清空。
	if n := countRows(t, db, `SELECT COUNT(*) FROM clients WHERE id = '00000000-0000-0000-0000-000000000002' AND device_serial = ''`); n != 1 {
		// 报出撞号的后来者没被清空。
		t.Fatalf("duplicate cleared: %d", n)
	}
	// 不撞号的收成大写。
	if n := countRows(t, db, `SELECT COUNT(*) FROM clients WHERE device_serial = 'ARM-X'`); n != 1 {
		// 报出没收成大写。
		t.Fatalf("upper: %d", n)
	}
	// 迁移后再写小写同号要撞唯一索引。
	if err := db.Exec(`UPDATE clients SET device_serial = 'arm-x' WHERE id = '00000000-0000-0000-0000-000000000002'`).Error; err == nil {
		// 报出大小写不同的同号仍能写进。
		t.Fatal("case-insensitive unique index missing")
	}
}
