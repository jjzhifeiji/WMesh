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
	for _, k := range []string{"WMESH_WEB_DIR", "WMESH_OSS_ENDPOINT", "WMESH_SHUTDOWN_TIMEOUT", "WMESH_WAN_URL", "WMESH_WAN_MQTT_URL", "WMESH_WAN_CA_FILE", "WMESH_WAN_TLS", "WMESH_WAN_HTTPS_PORT", "WMESH_WAN_MQTTS_PORT"} {
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

// 镜像带了打包 CA：明文地址自动改成 TLS 端口；显式配 CA 或 WMESH_WAN_TLS=off 时照环境走。
func TestLoadPackTLS(t *testing.T) {
	// 现生成一套证书，取 CA 文件当作打包带进的 CA。
	ca, _, _ := testtls.New(t).WriteFiles(t)
	// 先记下原路径，测完还原。
	old := packCAFile
	// 测完换回镜像里的路径。
	t.Cleanup(func() { packCAFile = old })
	// 指向测试 CA。
	packCAFile = ca
	// 逐种环境列出来。
	cases := []struct {
		name    string // 场景
		env     map[string]string // 本例要设的环境变量
		wan    string // 期望的网页地址
		mqtt    string // 期望的 MQTT 地址
		packTLS bool   // 期望是否自动切
	}{
		{"compose 默认明文", map[string]string{"WMESH_WAN_URL": "http://154.82.81.16:52080", "WMESH_WAN_MQTT_URL": "tcp://154.82.81.16:52183"}, "https://154.82.81.16:52443", "ssl://154.82.81.16:52883", true},
		{"MQTT 留空", map[string]string{"WMESH_WAN_URL": "http://wan.example:52080"}, "https://wan.example:52443", "ssl://wan.example:52883", true},
		{"改过端口", map[string]string{"WMESH_WAN_URL": "http://wan.example:52080", "WMESH_WAN_HTTPS_PORT": "8443", "WMESH_WAN_MQTTS_PORT": "8883"}, "https://wan.example:8443", "ssl://wan.example:8883", true},
		{"已是 https", map[string]string{"WMESH_WAN_URL": "https://wan.example:9443", "WMESH_WAN_MQTT_URL": "ssl://wan.example:9883"}, "https://wan.example:9443", "ssl://wan.example:9883", true},
		{"显式关掉", map[string]string{"WMESH_WAN_URL": "http://wan.example:52080", "WMESH_WAN_MQTT_URL": "tcp://wan.example:52183", "WMESH_WAN_TLS": "off"}, "http://wan.example:52080", "tcp://wan.example:52183", false},
	}
	// 每种环境都按预期改。
	for _, tc := range cases {
		// 先清掉环境。
		cleanEnv(t)
		// 设本例的环境。
		for k, v := range tc.env {
			// 逐个写入。
			t.Setenv(k, v)
		}
		// 读配置。
		c, err := Load()
		// 读失败就停测。
		if err != nil {
			// 带上场景和原因。
			t.Fatalf("%s: load: %v", tc.name, err)
		}
		// 地址、开关和 CA 池都要对。
		if c.WANURL != tc.wan || c.WANMQTT != tc.mqtt || c.WANPackTLS != tc.packTLS || (c.WANRootCAs != nil) != tc.packTLS {
			// 带上场景和实际值。
			t.Fatalf("%s: got wan %q mqtt %q pack %v cas %v", tc.name, c.WANURL, c.WANMQTT, c.WANPackTLS, c.WANRootCAs != nil)
		}
	}
	// 显式配了 CA 以环境为准，不改地址。
	cleanEnv(t)
	// 明文地址加显式 CA。
	t.Setenv("WMESH_WAN_URL", "http://wan.example:52080")
	// 显式 CA 用同一份文件。
	t.Setenv("WMESH_WAN_CA_FILE", ca)
	// 读配置。
	c, err := Load()
	// 地址该原样、不算自动切。
	if err != nil || c.WANURL != "http://wan.example:52080" || c.WANPackTLS {
		// 带上实际值。
		t.Fatalf("explicit ca: err %v wan %q pack %v", err, c.WANURL, c.WANPackTLS)
	}
}
