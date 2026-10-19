// 只给测试用的口子：缩短等锁上限，免得测试等满五分钟。
package migrate

// SetLockWaitSeconds 把等锁上限临时改成 s 秒，交回恢复原值的函数。
func SetLockWaitSeconds(s uint64) func() {
	// 记下原值，测完恢复。
	old := lockWaitSeconds
	// 换成测试要的上限。
	lockWaitSeconds = s
	// 交回恢复原值的函数。
	return func() {
		// 恢复原来的上限。
		lockWaitSeconds = old
	}
}
