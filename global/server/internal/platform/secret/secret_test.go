// 口令派生限并发后，多路同时校验结果仍然正确。
package secret

import (
	"sync"
	"testing"
)

// 十六路同时校验，对的都过、错的都拒，排队不报错。
func TestVerifyPasswordConcurrent(t *testing.T) {
	// 先生成一份哈希，后面都拿它校验。
	hash, err := HashPassword("wan-secret")
	// 这一步失败就不能继续，以免误判通过。
	if err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	// 等所有协程跑完再核对。
	var wg sync.WaitGroup
	// 每路的结果各占一格，不用加锁。
	got := make([]bool, 16)
	// 起十六路，超过四个名额，必须排队。
	for i := range got {
		// 每路各自校验，单双号分别用对错口令。
		wg.Go(func() {
			// 双号用对的口令，单号用错的。
			pass := "wan-secret"
			// 单号换成错口令，应被拒。
			if i%2 == 1 {
				// 错口令，校验应失败。
				pass = "bad"
			}
			// 记下这一路的校验结果。
			got[i] = VerifyPassword(hash, pass)
		})
	}
	// 等所有协程跑完。
	wg.Wait()
	// 逐路核对，对的过、错的拒。
	for i, ok := range got {
		// 结果和口令对错不符就停测。
		if ok != (i%2 == 0) {
			// 并发下结果错乱，带上路号停测。
			t.Fatalf("worker %d got %v", i, ok)
		}
	}
}
