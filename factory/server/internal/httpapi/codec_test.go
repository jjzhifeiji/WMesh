// 正文上限：普通接口一兆，批量入口放宽，超限回 413。
package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// countReader 数出被读走多少字节，核对读到上限就停。
type countReader struct {
	r io.Reader // 真正的正文来源
	n int64     // 已被读走的字节数
}

// Read 读一段并累计字节数。
func (c *countReader) Read(p []byte) (int, error) {
	// 从真正的正文里读一段。
	n, err := c.r.Read(p)
	// 累计这次读走的字节。
	c.n += int64(n)
	return n, err
}

// nameBody 拼出一段 JSON，name 字段长 n 个字节。
func nameBody(n int) string {
	// 字段值用同一个字母填满，总长约等于 n。
	return `{"name":"` + strings.Repeat("a", n) + `"}`
}

// jsonReq 组一个 POST 请求；sized 为假时不带声明长度。
func jsonReq(body string, sized bool) *http.Request {
	// 组出带正文的请求，长度由测试工具自动算好。
	r := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body))
	// 不带长度时模拟分块上送，只能靠读时限住。
	if !sized {
		// 长度未知，服务端读的时候才知道多大。
		r.ContentLength = -1
	}
	return r
}

// 普通接口超过一兆回 413，有无声明长度都一样。
func TestDecodeJSONTooLarge(t *testing.T) {
	// 带声明长度和不带声明长度两种都要验。
	for _, sized := range []bool{true, false} {
		// 承接解码结果，超限时不应被用到。
		var dst struct {
			Name string `json:"name"` // 超长字段
		}
		// 正文比一兆多一点，刚好越过上限。
		err := decodeJSON(jsonReq(nameBody(maxJSONBody+1), sized), &dst)
		// 超限必须报错，放行说明没限住。
		if err == nil {
			// 与预期不符就停测，避免后面连环误判。
			t.Fatalf("sized=%v: want error", sized)
		}
		// 录下回包，核对状态和错误句子。
		rec := httptest.NewRecorder()
		// 走和接口一样的出错回包。
		writeBadRequest(rec, err)
		// 先把回包解开，再核对错误句子。
		var body errorBody
		// 回包不是约定的错误对象则停测。
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			// 与预期不符就停测，避免后面连环误判。
			t.Fatalf("sized=%v: body %q: %v", sized, rec.Body.String(), err)
		}
		// 不是 413 或句子不对，前端就提示不了太大。
		if rec.Code != http.StatusRequestEntityTooLarge || body.Error != "request body too large" {
			// 与预期不符就停测，避免后面连环误判。
			t.Fatalf("sized=%v: %d %q", sized, rec.Code, body.Error)
		}
	}
}

// 一兆以内照常解码，批量上限能放过两兆。
func TestDecodeJSONWithinLimit(t *testing.T) {
	// 承接解码结果，核对字段长度。
	var small struct {
		Name string `json:"name"` // 普通长度字段
	}
	// 不带声明长度的一兆以内正文应能解开。
	if err := decodeJSON(jsonReq(nameBody(maxJSONBody-64), false), &small); err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("small: %v", err)
	}
	// 字段长度不对说明正文被截了。
	if len(small.Name) != maxJSONBody-64 {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("small len %d", len(small.Name))
	}
	// 承接批量解码结果，核对字段长度。
	var bulk struct {
		Name string `json:"name"` // 两兆长的字段
	}
	// 按批量上限解两兆正文，应当放行。
	if err := decodeJSONLimit(jsonReq(nameBody(2<<20), false), &bulk, maxBulkJSONBody); err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("bulk: %v", err)
	}
	// 字段长度不对说明批量正文被截了。
	if len(bulk.Name) != 2<<20 {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("bulk len %d", len(bulk.Name))
	}
}

// 扔正文时上限内读完，超限最多读到上限加一字节。
func TestDiscardBody(t *testing.T) {
	// 上限内的正文要能读完，后面再读就到头。
	in := &countReader{r: strings.NewReader(strings.Repeat("x", 1000))}
	// 组出不带长度的请求，正文由计数器供给。
	r := httptest.NewRequest(http.MethodPost, "/x", in)
	// 不带长度，逼着按上限一点点读。
	r.ContentLength = -1
	// 按一千零二十四字节上限读完扔掉。
	discardBody(r, 1024)
	// 读走的字节应等于正文全长。
	if in.n != 1000 {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("read %d", in.n)
	}
	// 再读一次，应当已经到头。
	if n, err := r.Body.Read(make([]byte, 8)); n != 0 || !errors.Is(err, io.EOF) {
		// 还能读出东西说明没读完。
		t.Fatalf("after discard %d %v", n, err)
	}
	// 超限的正文只读到上限加一就停。
	big := &countReader{r: strings.NewReader(strings.Repeat("x", 4096))}
	// 组出不带长度的超限请求。
	r = httptest.NewRequest(http.MethodPost, "/x", big)
	// 不带长度，只能靠读时限住。
	r.ContentLength = -1
	// 按一千零二十四字节上限读，超了就停。
	discardBody(r, 1024)
	// 读走超过上限加一说明没限住。
	if big.n > 1025 {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("over limit read %d", big.n)
	}
	// 声明长度已超上限，一字节都不该读。
	sized := &countReader{r: strings.NewReader(strings.Repeat("x", 4096))}
	// 组出超限请求，并声明真实长度。
	r = httptest.NewRequest(http.MethodPost, "/x", sized)
	// 声明长度比上限大，应当直接放弃。
	r.ContentLength = 4096
	// 声明超限时不读，留给服务器断开。
	discardBody(r, 1024)
	// 读了字节说明白读了一遍。
	if sized.n != 0 {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("sized read %d", sized.n)
	}
}
