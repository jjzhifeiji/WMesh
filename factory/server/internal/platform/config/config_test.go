package config

import (
	"os"
	"path/filepath"
	"testing"

	"wmesh/factory/internal/platform/testtls"
)

// 清掉会影响校验的环境，只留本例要测的 WAN 地址和 CA。
func cleanEnv(t *testing.T) {
	// 失败行指到调用用例，不指到夹具。
	t.Helper()
	// 逐个置空，本机环境里设过的不干扰断言。
	for _, k := range []string{"WMESH_WEB_DIR", "WMESH_OSS_ENDPOINT", "WMESH_SHUTDOWN_TIMEOUT", "WMESH_WAN_URL", "WMESH_WAN_MQTT_URL", "WMESH_WAN_CA_FILE"} {
		// 置空并在测完还原。
		t.Setenv(k, "")
	}
}

// 配了合法 CA 就读成池；没配时池为空，用系统根证书。
func TestLoadWANCA(t *testing.T) {
	// 先清掉环境。
	cleanEnv(t)
	// 没配 CA 时读配置。
	c, err := Load()
	// 默认配置读失败就停测。
	if err != nil {
		// 停测并带上原因。
		t.Fatalf("load: %v", err)
	}
	// 没配 CA 就不该有池。
	if c.WANRootCAs != nil {
		// 凭空多出了 CA 池。
		t.Fatal("root cas without ca file")
	}
	// 现生成一套证书，取 CA 文件。
	ca, _, _ := testtls.New(t).WriteFiles(t)
	// 指向 CA 文件。
	t.Setenv("WMESH_WAN_CA_FILE", ca)
	// 再读一次配置。
	c, err = Load()
	// 合法 CA 读失败就停测。
	if err != nil {
		// 停测并带上原因。
		t.Fatalf("load with ca: %v", err)
	}
	// 配了 CA 就该有池。
	if c.WANRootCAs == nil {
		// CA 没读进来。
		t.Fatal("no root cas")
	}
}

// CA 文件不存在或不是 PEM 都要拒绝启动。
func TestLoadRejectsBadWANCA(t *testing.T) {
	// 先清掉环境。
	cleanEnv(t)
	// 一个不是 PEM 的文件。
	junk := filepath.Join(t.TempDir(), "junk.pem")
	// 写进一段不是证书的内容。
	if err := os.WriteFile(junk, []byte("not a certificate"), 0o600); err != nil {
		// 写不进去就停测。
		t.Fatal(err)
	}
	// 逐个坏路径试。
	for _, path := range []string{filepath.Join(t.TempDir(), "missing.pem"), junk} {
		// 指向坏的 CA 文件。
		t.Setenv("WMESH_WAN_CA_FILE", path)
		// 坏 CA 不该被放过。
		if _, err := Load(); err == nil {
			// 坏 CA 读出来了。
			t.Fatalf("bad ca %s accepted", path)
		}
	}
}

// https 配明文 MQTT 要拒绝；https 配 ssl、留空，http 配 tcp 都放行。
func TestLoadRejectsMixedWANScheme(t *testing.T) {
	// 逐种地址组合列出来。
	cases := []struct {
		wan  string // WAN 网页地址
		mqtt string // WAN MQTT 地址
		ok   bool   // 是否该放行
	}{
		{"https://wan.example:52443", "tcp://wan.example:52183", false},
		{"https://wan.example:52443", "mqtt://wan.example:52183", false},
		{"https://wan.example:52443", "ws://wan.example:52183", false},
		{"HTTPS://wan.example:52443", "TCP://wan.example:52183", false},
		{"https://wan.example:52443", "ssl://wan.example:52883", true},
		{"https://wan.example:52443", "", true},
		{"http://wan.example:52080", "tcp://wan.example:52183", true},
	}
	// 每种组合都按预期判。
	for _, tc := range cases {
		// 先清掉环境。
		cleanEnv(t)
		// 设 WAN 网页地址。
		t.Setenv("WMESH_WAN_URL", tc.wan)
		// 设 WAN MQTT 地址。
		t.Setenv("WMESH_WAN_MQTT_URL", tc.mqtt)
		// 读配置看判得对不对。
		_, err := Load()
		// 该放行的被拒，或该拒的放行了，都停测。
		if (err == nil) != tc.ok {
			// 带上组合和结果。
			t.Fatalf("wan %q mqtt %q: err %v, want ok=%v", tc.wan, tc.mqtt, err, tc.ok)
		}
	}
}
