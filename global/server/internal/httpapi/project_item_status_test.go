// 工程焊道点了认不出的模版回 400，不当内部故障。
package httpapi

import (
	"fmt"
	"net/http"
	"testing"

	"wmesh/global/internal/platform/domain"
)

// 认不出的焊道模版是请求本身的问题，要回 400。
func TestProjectItemTemplateIsBadRequest(t *testing.T) {
	// 包一层也要认得出。
	err := fmt.Errorf("create project: %w", domain.ErrProjectItemTemplate)
	// 不是 400 就停。
	if got := statusOf(err); got != http.StatusBadRequest {
		// 带上实际状态停住。
		t.Fatalf("status %d, want 400", got)
	}
}
