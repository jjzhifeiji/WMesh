// 清镜像请求：点名须带 ref，全部须 all=true。
package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"wmesh/factory/internal/platform/domain"
)

// 核对点名清理和全部清理的入参规则。
func TestImagePruneRef(t *testing.T) {
	// 列出各组请求和期望，用来核对清理规则。
	cases := []struct {
		name string // 这一组样例的名字
		body string // 这一组清镜像请求的正文
		ref  string // 期望解析出的镜像引用
		err  error  // 期望的业务错误，空则应成功
	}{
		{name: "empty", body: "", err: domain.ErrInvalidName},
		{name: "obj", body: "{}", err: domain.ErrInvalidName},
		{name: "blank", body: `{"ref":""}`, err: domain.ErrInvalidName},
		{name: "all", body: `{"all":true}`},
		{name: "one", body: `{"ref":"app:old"}`, ref: "app:old"},
		{name: "refWins", body: `{"ref":"app:old","all":true}`, ref: "app:old"},
	}
	// 逐组样例核对，每组各自判断成败。
	for _, tc := range cases {
		// 用这一组样例单独核对清理规则。
		t.Run(tc.name, func(t *testing.T) {
			// 组出请求，方法或地址不合法则停测。
			r, err := http.NewRequest(http.MethodPost, "/", strings.NewReader(tc.body))
			// 请求组不起来则停测。
			if err != nil {
				// 与预期不符就停测，避免后面连环误判。
				t.Fatal(err)
			}
			// 按这条样例解析清理范围。
			got, err := imagePruneRef(r)
			// 应当成功且引用与样例一致，否则失败。
			if tc.err != nil {
				// 拒绝原因和预期不一致则本测失败。
				if !errors.Is(err, tc.err) {
					// 与预期不符就停测，避免后面连环误判。
					t.Fatalf("err %v", err)
				}
				return
			}
			// 这一步失败则停测，避免在坏结果上继续。
			if err != nil || got != tc.ref {
				// 与预期不符就停测，避免后面连环误判。
				t.Fatalf("got %q %v", got, err)
			}
		})
	}
}
