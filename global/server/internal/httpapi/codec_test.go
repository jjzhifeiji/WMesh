// 正文上限、读完扔掉和放宽读时限：不用测试库，直接跑。
package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// 造一份带超长登录名的 JSON，长度正好是 size 字节。
func bigLoginJSON(size int) string {
	// 先算出外壳占的字节，余下全部填进登录名。
	head, tail := `{"loginName":"`, `"}`
	// 登录名填满剩下的字节，整份正好 size。
	return head + strings.Repeat("a", size-len(head)-len(tail)) + tail
}

// 组一条请求；known 为假时去掉长度，逼着走边读边数那条路。
func bodyRequest(t *testing.T, body string, known bool) *http.Request {
	// 标成辅助，失败行号落到调用它的测试。
	t.Helper()
	// 组好请求，长度先按正文自动填上。
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	// 不带长度时改成未知，模拟分块上传。
	if !known {
		// 长度未知，只能靠读到上限来拦。
		r.ContentLength = -1
	}
	return r
}

// 普通接口正文超一兆回 413，带不带长度都一样。
func TestDecodeJSONTooLarge(t *testing.T) {
	// 带长度走预检，不带长度走边读边数。
	for _, known := range []bool{true, false} {
		// 每种情况单独跑，失败能对上是哪条路。
		t.Run(fmt.Sprintf("known=%v", known), func(t *testing.T) {
			// 比上限多一个字节，刚好越线。
			r := bodyRequest(t, bigLoginJSON(maxJSONBody+1), known)
			// 接住解码结果，超限应报错。
			var req loginReq
			// 解码应失败，并交回超限错误。
			err := decodeJSON(r, &req)
			// 接住超限错误，确认种类对。
			var tooLarge *http.MaxBytesError
			// 不是超限错误就停测，别的错不算拦住。
			if !errors.As(err, &tooLarge) {
				// 错误种类不对，带上现场停测。
				t.Fatalf("err %v", err)
			}
			// 录下回包，看状态和错误句。
			rec := httptest.NewRecorder()
			// 走统一的坏请求出口，应改回 413。
			writeBadRequest(rec, err)
			// 状态或错误句不对就停测。
			if rec.Code != http.StatusRequestEntityTooLarge || !strings.Contains(rec.Body.String(), `"request body too large"`) {
				// 回包不对，带上现场停测。
				t.Fatalf("got %d %s", rec.Code, rec.Body.String())
			}
		})
	}
}

// 一兆以内照常解码，大入口放宽后两兆也能过。
func TestDecodeJSONWithinLimit(t *testing.T) {
	// 正好一兆，不越线。
	r := bodyRequest(t, bigLoginJSON(maxJSONBody), false)
	// 接住解码结果，后面核对长度。
	var req loginReq
	// 一兆以内应能解开。
	if err := decodeJSON(r, &req); err != nil {
		// 不该拒绝却拒绝了，就停测。
		t.Fatalf("1MiB: %v", err)
	}
	// 解出的登录名长度不对就停测。
	if len(req.LoginName) != maxJSONBody-len(`{"loginName":""}`) {
		// 解码结果残缺，带上长度停测。
		t.Fatalf("login len %d", len(req.LoginName))
	}
	// 两兆正文交给批量上限，应能过。
	big := bodyRequest(t, bigLoginJSON(2<<20), true)
	// 接住解码结果，只看是否报错。
	var bulk loginReq
	// 批量上限三十二兆，两兆不该被拦。
	if err := decodeJSONLimit(big, &bulk, maxBulkJSONBody); err != nil {
		// 放宽后仍被拦，就停测。
		t.Fatalf("2MiB bulk: %v", err)
	}
}

// countReader 数出被读走多少字节，用来看读到哪里就停。
type countReader struct {
	r io.Reader // 真正的正文
	n int       // 已读走的字节数
}

// 读一段并记下字节数。
func (c *countReader) Read(p []byte) (int, error) {
	// 从真正的正文读，读到多少记多少。
	n, err := c.r.Read(p)
	// 累计已读字节。
	c.n += n
	return n, err
}

// 上限以内读完扔掉；超上限读到上限加一就停；声明超长则一字节不读。
func TestDiscardBody(t *testing.T) {
	// 正文一千字节，上限两千，应全部读完。
	small := &countReader{r: strings.NewReader(strings.Repeat("x", 1000))}
	// 组请求，长度未知，靠读来判。
	r := httptest.NewRequest(http.MethodPost, "/", small)
	// 长度未知，逼着边读边数。
	r.ContentLength = -1
	// 读完扔掉，不进内存。
	discardBody(r, 2000)
	// 没读完就停测，客户端会收不到干净回包。
	if small.n != 1000 {
		// 读的字节数不对，带上现场停测。
		t.Fatalf("small read %d", small.n)
	}
	// 再读一次，应已到头。
	if n, err := r.Body.Read(make([]byte, 1)); n != 0 || err != io.EOF {
		// 正文还有剩，带上现场停测。
		t.Fatalf("after discard %d %v", n, err)
	}
	// 正文一万字节，上限一千，读到上限就该停。
	large := &countReader{r: strings.NewReader(strings.Repeat("x", 10000))}
	// 组请求，长度未知，靠读来判。
	r = httptest.NewRequest(http.MethodPost, "/", large)
	// 长度未知，逼着边读边数。
	r.ContentLength = -1
	// 读到上限就停，剩下的留给服务器断开。
	discardBody(r, 1000)
	// 多读了就停测，匿名者能让服务端白读。
	if large.n > 1001 {
		// 读的字节数超了上限，带上现场停测。
		t.Fatalf("large read %d", large.n)
	}
	// 声明长度已超上限，应一字节不读。
	declared := &countReader{r: strings.NewReader(strings.Repeat("x", 10000))}
	// 组请求，长度按正文自动带上。
	r = httptest.NewRequest(http.MethodPost, "/", declared)
	// 声明一万字节，比上限大。
	r.ContentLength = 10000
	// 声明超长直接放弃读。
	discardBody(r, 1000)
	// 读了就停测，白读浪费带宽。
	if declared.n != 0 {
		// 不该读却读了，带上现场停测。
		t.Fatalf("declared read %d", declared.n)
	}
}

// 读时限两百毫秒；放宽后的入口能读完慢正文，没放宽的读到超时。
func TestExtendRead(t *testing.T) {
	// 收集各入口读正文的结果，测试端据此判断。
	results := make(chan error, 2)
	// 新建路由，一个放宽一个不放宽。
	mux := http.NewServeMux()
	// 放宽读时限后再读正文。
	mux.HandleFunc("POST /ext", func(w http.ResponseWriter, r *http.Request) {
		// 放宽到两秒，慢正文能读完。
		extendRead(w, 2*time.Second)
		// 读完全部正文，记下有没有出错。
		_, err := io.ReadAll(r.Body)
		// 把结果交给测试端。
		results <- err
	})
	// 不放宽，直接读正文。
	mux.HandleFunc("POST /plain", func(_ http.ResponseWriter, r *http.Request) {
		// 读完全部正文，应碰到读时限。
		_, err := io.ReadAll(r.Body)
		// 把结果交给测试端。
		results <- err
	})
	// 在本机随便挑个端口听。
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	// 这一步失败就不能继续，以免误判通过。
	if err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	// 套上 Wrap，验证能穿透包装层改到底层连接。
	srv := &http.Server{Handler: Wrap(slog.New(slog.DiscardHandler), mux), ReadTimeout: 200 * time.Millisecond}
	// 后台起服务，测完关掉。
	go func() {
		// 关闭后返回的错误不用管。
		_ = srv.Serve(ln)
	}()
	// 测完关掉服务，避免端口占着。
	t.Cleanup(func() {
		// 关掉监听和连接。
		_ = srv.Close()
	})
	// 放宽的应读完，不放宽的应超时。
	cases := []struct {
		path string // 入口路径
		ok   bool   // 期望能读完
	}{{"/ext", true}, {"/plain", false}}
	// 两个入口各慢传一次，分别看结果。
	for _, tc := range cases {
		// 直接拨号，手写请求才能控制慢传节奏。
		conn, err := net.Dial("tcp", ln.Addr().String())
		// 这一步失败就不能继续，以免误判通过。
		if err != nil {
			// 这一步失败就不能继续，以免误判通过。
			t.Fatal(err)
		}
		// 先发请求头，声明六字节正文。
		_, _ = fmt.Fprintf(conn, "POST %s HTTP/1.1\r\nHost: x\r\nContent-Length: 6\r\n\r\n", tc.path)
		// 六百毫秒内分六次慢慢写正文。
		for range 6 {
			// 每次隔一百毫秒，总长超过读时限。
			time.Sleep(100 * time.Millisecond)
			// 写一个字节；对端已断开时出错不用管。
			_, _ = conn.Write([]byte("x"))
		}
		// 等这个入口交回读的结果。
		select {
		// 读的结果到了，按期望核对。
		case err := <-results:
			// 结果与期望不符就停测。
			if (err == nil) != tc.ok {
				// 放宽没生效或不放宽也读完了。
				t.Fatalf("%s read err %v", tc.path, err)
			}
		// 迟迟没有结果，说明入口没跑到。
		case <-time.After(5 * time.Second):
			// 入口没跑到，带上路径停测。
			t.Fatalf("%s no result", tc.path)
		}
		// 关掉这条连接，下一轮重新拨。
		_ = conn.Close()
	}
}

// 解码结果仍保留未知字段拒绝，上限改动不影响原约定。
func TestDecodeJSONUnknownField(t *testing.T) {
	// 带一个未知字段的小正文。
	r := bodyRequest(t, `{"loginName":"w","x":1}`, true)
	// 接住解码结果，应被拒绝。
	var req loginReq
	// 未知字段应被拒绝。
	err := decodeJSON(r, &req)
	// 没报错或误报成超限都算错。
	var tooLarge *http.MaxBytesError
	// 没拒绝或种类错了就停测。
	if err == nil || errors.As(err, &tooLarge) {
		// 未知字段没被正确拒绝，就停测。
		t.Fatalf("err %v", err)
	}
	// 坏包仍回 400，不被当成超限。
	rec := httptest.NewRecorder()
	// 走统一的坏请求出口。
	writeBadRequest(rec, err)
	// 解出错误体，核对状态。
	var body errorBody
	// 回包不是合法 JSON 就停测。
	if rec.Code != http.StatusBadRequest || json.Unmarshal(rec.Body.Bytes(), &body) != nil {
		// 回包不对，带上现场停测。
		t.Fatalf("got %d %s", rec.Code, rec.Body.String())
	}
}
