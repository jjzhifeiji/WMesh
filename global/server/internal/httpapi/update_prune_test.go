// 清镜像请求：点名须带 ref，全部须 all=true。
package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"wmesh/global/internal/platform/domain"
)

// 点名、全部和空请求该怎样被拒绝或收下。
func TestImagePruneRef(t *testing.T) {
	// 覆盖空体、点名和显式全部，防止误清。
	cases := []struct {
		name string // 这一组的名字，失败时能对上场景。
		body string // 请求 JSON，空体也要覆盖到。
		ref  string // 期望留下的镜像引用，空表示全部。
		err  error  // 期望的业务错误，空表示应该成功。
	}{
		{name: "empty", body: "", err: domain.ErrInvalidName},
		{name: "obj", body: "{}", err: domain.ErrInvalidName},
		{name: "blank", body: `{"ref":""}`, err: domain.ErrInvalidName},
		{name: "all", body: `{"all":true}`},
		{name: "one", body: `{"ref":"app:old"}`, ref: "app:old"},
		{name: "refWins", body: `{"ref":"app:old","all":true}`, ref: "app:old"},
	}
	// 逐组输入单独验，一组失败不掩盖其他组。
	for _, tc := range cases {
		// 这一组单独跑，失败能对上场景。
		t.Run(tc.name, func(t *testing.T) {
			// 组好请求，组失败就停测。
			r, err := http.NewRequest(http.MethodPost, "/", strings.NewReader(tc.body))
			// 这一步失败就不能继续，以免误判通过。
			if err != nil {
				// 这一步失败就不能继续，以免误判通过。
				t.Fatal(err)
			}
			// 解析点名或全部，空请求必须拒绝。
			got, err := imagePruneRef(r)
			// 这组预期会失败，成功反而是错。
			if tc.err != nil {
				// 失败种类不对就停测，避免放过别的错。
				if !errors.Is(err, tc.err) {
					// 错误种类和预期不一致，就停测。
					t.Fatalf("err %v", err)
				}
				return
			}
			// 这一步失败就不能继续，以免误判通过。
			if err != nil || got != tc.ref {
				// 结果和点名或全部的预期不一致。
				t.Fatalf("got %q %v", got, err)
			}
		})
	}
}
