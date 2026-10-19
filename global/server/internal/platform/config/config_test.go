package config

import (
	"crypto/tls"
	"path/filepath"
	"testing"

	"wmesh/global/internal/platform/testtls"
)

// 清掉会影响校验的环境，只留本例要测的证书项。
func cleanEnv(t *testing.T) {
	// 失败行指到调用用例，不指到夹具。
	t.Helper()
	// 逐个置空，本机环境里设过的不干扰断言。
	for _, k := range []string{"WMESH_ADMIN_LOGIN", "WMESH_ADMIN_PASSWORD", "WMESH_ADMIN_RESET", "WMESH_WEB_DIR", "WMESH_OSS_ENDPOINT", "WMESH_SHUTDOWN_TIMEOUT", "WMESH_TLS_CERT_FILE", "WMESH_TLS_KEY_FILE"} {
		// 置空并在测完还原。
		t.Setenv(k, "")
	}
}

// 证书和私钥都不配时只开明文，不报错。
func TestLoadWithoutTLS(t *testing.T) {
	// 先清掉环境，只看默认。
	cleanEnv(t)
	// 读配置，什么都没配也该成功。
	c, err := Load()
	// 默认配置读失败就停测。
	if err != nil {
		// 停测并带上原因。
		t.Fatalf("load: %v", err)
	}
	// 没配证书就不该有 TLS。
	if c.TLS != nil {
		// TLS 被意外打开了。
		t.Fatal("tls enabled without cert")
	}
	// 默认 TLS 端口要是约定的 8443 和 8883。
	if c.HTTPSAddr != ":8443" || c.MQTTSAddr != ":8883" {
		// 默认端口不对。
		t.Fatalf("tls addrs %q %q", c.HTTPSAddr, c.MQTTSAddr)
	}
}

// 证书和私钥配对时加载成 TLS，并记下到期时间。
func TestLoadWithTLS(t *testing.T) {
	// 先清掉环境，只看证书项。
	cleanEnv(t)
	// 现生成一套证书。
	pki := testtls.New(t)
	// 写成文件，配置按路径读。
	_, cert, key := pki.WriteFiles(t)
	// 指向证书文件。
	t.Setenv("WMESH_TLS_CERT_FILE", cert)
	// 指向私钥文件。
	t.Setenv("WMESH_TLS_KEY_FILE", key)
	// 读配置，证书齐全应成功。
	c, err := Load()
	// 读失败就停测。
	if err != nil {
		// 停测并带上原因。
		t.Fatalf("load: %v", err)
	}
	// 证书齐全就该有 TLS，且至少 TLS1.2。
	if c.TLS == nil || c.TLS.MinVersion != tls.VersionTLS12 || len(c.TLS.Certificates) != 1 {
		// TLS 没按预期装好。
		t.Fatalf("tls %+v", c.TLS)
	}
	// 到期时间要和证书一致。
	if !c.TLSNotAfter.Equal(pki.Leaf.NotAfter) {
		// 到期时间记错了。
		t.Fatalf("not after %v want %v", c.TLSNotAfter, pki.Leaf.NotAfter)
	}
}

// 只配一项、文件不存在、证书和私钥不配对，都要拒绝启动。
func TestLoadRejectsBadTLS(t *testing.T) {
	// 两套证书，拿来拼不配对的组合。
	a, b := testtls.New(t), testtls.New(t)
	// 写出第一套。
	_, certA, keyA := a.WriteFiles(t)
	// 写出第二套。
	_, _, keyB := b.WriteFiles(t)
	// 一个不存在的路径。
	missing := filepath.Join(t.TempDir(), "nope.pem")
	// 逐种坏配置列出来。
	cases := []struct {
		name string // 子用例的名字
		cert string // 证书文件路径
		key  string // 私钥文件路径
	}{
		{"cert only", certA, ""},
		{"key only", "", keyA},
		{"missing cert", missing, keyA},
		{"missing key", certA, missing},
		{"mismatched pair", certA, keyB},
	}
	// 每种都要报错。
	for _, tc := range cases {
		// 子用例单独设环境。
		t.Run(tc.name, func(t *testing.T) {
			// 先清掉环境。
			cleanEnv(t)
			// 设证书路径。
			t.Setenv("WMESH_TLS_CERT_FILE", tc.cert)
			// 设私钥路径。
			t.Setenv("WMESH_TLS_KEY_FILE", tc.key)
			// 坏配置读出来不该成功。
			if _, err := Load(); err == nil {
				// 坏证书配置被放过了。
				t.Fatal("bad tls accepted")
			}
		})
	}
}
