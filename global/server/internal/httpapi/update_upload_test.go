// 软件上传超上限：表单和 JSON 两条路，带不带长度都回 413。
package httpapi

import (
	"bytes"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 造一份带四千字节包文件的表单正文。
func bigMultipart(t *testing.T) (string, []byte) {
	// 标成辅助，失败行号落到调用它的测试。
	t.Helper()
	// 正文先写进内存缓冲。
	var buf bytes.Buffer
	// 按表单格式写各字段。
	mw := multipart.NewWriter(&buf)
	// 种类和版本像真上传一样填上。
	fields := map[string]string{"kind": "factory_service", "version": "1", "versionName": "1.0.0"}
	// 逐个写上表单字段。
	for k, v := range fields {
		// 写字段失败就停测。
		if err := mw.WriteField(k, v); err != nil {
			// 这一步失败就不能继续，以免误判通过。
			t.Fatal(err)
		}
	}
	// 开一个文件字段放包字节。
	fw, err := mw.CreateFormFile("file", "pkg.tar")
	// 这一步失败就不能继续，以免误判通过。
	if err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	// 写四千字节，超过测试上限一千。
	if _, err := fw.Write(bytes.Repeat([]byte("p"), 4096)); err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	// 收尾写上结束边界。
	if err := mw.Close(); err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	// 交回表单类型和正文。
	return mw.FormDataContentType(), buf.Bytes()
}

// 表单和 JSON 正文超上限，都交回超限错误并回 413。
func TestReadSoftwareUploadTooLarge(t *testing.T) {
	// 先造好表单正文。
	formType, form := bigMultipart(t)
	// JSON 正文同样超过一千字节。
	jsonBody := []byte(`{"kind":"factory_service","version":1,"versionName":"1.0.0","content":"` + strings.Repeat("A", 4096) + `"}`)
	// 表单与 JSON 两条路都要覆盖。
	cases := []struct {
		name  string // 场景名
		ctype string // 请求类型
		body  []byte // 请求正文
	}{{"multipart", formType, form}, {"json", "application/json", jsonBody}}
	// 每条路各带长度、不带长度跑一遍。
	for _, tc := range cases {
		// 带长度走预检，不带长度走边读边数。
		for _, known := range []bool{true, false} {
			// 每种情况单独跑，失败能对上场景。
			t.Run(fmt.Sprintf("%s/known=%v", tc.name, known), func(t *testing.T) {
				// 组好请求，长度先按正文自动填上。
				r := httptest.NewRequest(http.MethodPost, "/v1/software", bytes.NewReader(tc.body))
				// 标明正文类型，决定走哪条路。
				r.Header.Set("Content-Type", tc.ctype)
				// 不带长度时改成未知，模拟分块上传。
				if !known {
					// 长度未知，只能靠读到上限来拦。
					r.ContentLength = -1
				}
				// 录下回包，看状态和错误句。
				rec := httptest.NewRecorder()
				// 上限一千字节，四千字节应被拦。
				_, _, _, _, err := readSoftwareUpload(rec, r, 1024)
				// 接住超限错误，确认种类对。
				var tooLarge *http.MaxBytesError
				// 不是超限错误就停测，别的错不算拦住。
				if !errors.As(err, &tooLarge) {
					// 错误种类不对，带上现场停测。
					t.Fatalf("err %v", err)
				}
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
}
