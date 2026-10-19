package blob

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// 写入后用量要涨，删掉之后回到空。
func TestMemoryUsage(t *testing.T) {
	// 建一个空的内存对象表。
	m := NewMemory()
	// 拿出不会被取消的上下文。
	ctx := context.Background()
	// 没能按键覆盖写进一份字节就停住本用例。
	if err := m.Put(ctx, "a", []byte("hello")); err != nil {
		// 没能按键覆盖写进一份字节就停住本用例。
		t.Fatal(err)
	}
	// 合计已经存下的字节和个数。
	used, objects, err := m.Usage(ctx)
	// 出错或结果对不上就停住本用例。
	if err != nil || used != 5 || objects != 1 {
		// 结果和预期不符就停住。
		t.Fatalf("got %d %d %v", used, objects, err)
	}
}

// Open 交出的流接到标准件上：整包 200、续传 206、越界 416，没有的键报找不到。
func TestMemoryOpenServeContent(t *testing.T) {
	// 建一个空的内存对象表。
	m := NewMemory()
	// 拿出不会被取消的上下文。
	ctx := context.Background()
	// 准备一段已知内容，后面按偏移核对。
	body := []byte("apk-stream-body")
	// 写不进去就停住本用例。
	if err := m.Put(ctx, "k", body); err != nil {
		// 写入失败就停住本用例。
		t.Fatal(err)
	}
	// 没有的键必须报找不到，不能交出空流。
	if _, err := m.Open(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		// 没报找不到就停住本用例。
		t.Fatalf("missing %v", err)
	}
	// 逐条列出区间头、期望状态和期望正文。
	cases := []struct {
		rng  string // 请求带的区间头，空则整包
		code int    // 期望的响应状态
		want string // 期望的响应正文，越界时不看
	}{
		{"", http.StatusOK, string(body)},
		{"bytes=4-", http.StatusPartialContent, string(body[4:])},
		{"bytes=100-", http.StatusRequestedRangeNotSatisfiable, ""},
	}
	// 每条都新开一路流，互不影响偏移。
	for _, c := range cases {
		// 按键打开只读流。
		obj, err := m.Open(ctx, "k")
		// 打不开就停住本用例。
		if err != nil {
			// 打开失败就停住本用例。
			t.Fatal(err)
		}
		// 总长必须是写入的长度，标准件靠它算区间。
		if obj.Size() != int64(len(body)) {
			// 长度不对就停住本用例。
			t.Fatalf("size %d", obj.Size())
		}
		// 组一个下载请求。
		req := httptest.NewRequest(http.MethodGet, "/client.apk", nil)
		// 带了区间才设区间头。
		if c.rng != "" {
			// 设上这条的区间头。
			req.Header.Set("Range", c.rng)
		}
		// 用记录器接住响应。
		rec := httptest.NewRecorder()
		// 交给标准件处理整包、续传和越界。
		http.ServeContent(rec, req, "client.apk", time.Time{}, obj)
		// 用完关掉流。
		_ = obj.Close()
		// 读出响应正文。
		got, _ := io.ReadAll(rec.Body)
		// 状态不对就停住本用例。
		if rec.Code != c.code {
			// 状态和预期不符就停住。
			t.Fatalf("%q code %d", c.rng, rec.Code)
		}
		// 不是越界时正文也要对上。
		if c.code != http.StatusRequestedRangeNotSatisfiable && string(got) != c.want {
			// 正文和预期不符就停住。
			t.Fatalf("%q body %q", c.rng, got)
		}
	}
}
