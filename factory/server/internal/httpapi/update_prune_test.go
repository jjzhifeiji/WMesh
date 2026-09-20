// 清镜像请求：点名须带 ref，全部须 all=true。
package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"wmesh/factory/internal/platform/domain"
)

func TestImagePruneRef(t *testing.T) {
	cases := []struct {
		name string
		body string
		ref  string
		err  error
	}{
		{name: "empty", body: "", err: domain.ErrInvalidName},
		{name: "obj", body: "{}", err: domain.ErrInvalidName},
		{name: "blank", body: `{"ref":""}`, err: domain.ErrInvalidName},
		{name: "all", body: `{"all":true}`},
		{name: "one", body: `{"ref":"app:old"}`, ref: "app:old"},
		{name: "refWins", body: `{"ref":"app:old","all":true}`, ref: "app:old"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, err := http.NewRequest(http.MethodPost, "/", strings.NewReader(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			got, err := imagePruneRef(r)
			if tc.err != nil {
				if !errors.Is(err, tc.err) {
					t.Fatalf("err %v", err)
				}
				return
			}
			if err != nil || got != tc.ref {
				t.Fatalf("got %q %v", got, err)
			}
		})
	}
}
