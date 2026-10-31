// Package config 集中读取厂内进程的环境变量并做最基本校验；不含业务规则，也不连库。
package config

import (
	"crypto/x509"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
)

// Config 是厂内进程一次启动用到的全部外部配置。
type Config struct {
	HTTPAddr        string         // 监听地址
	DSN             string         // 维护库连接串；按工厂身份从这里建/开各厂库，账号需 CREATEDB
	WebDir          string         // 已构建管理端目录；空表示只提供 API
	WANURL          string         // WAN HTTP 根地址，厂出站认领；空则不能认领
	WANMQTT         string         // WAN MQTT 地址，空则按 HTTP 主机拼端口
	WANCAFile       string         // 校验 WAN 证书的 CA 文件；空则用系统根证书
	WANRootCAs      *x509.CertPool // 从 WANCAFile 读出的 CA 池；nil 用系统根证书
	ClientMQTTAddr  string         // 本厂 Client MQTT 监听；空则 :1884
	ClientMQTTURL   string         // 回给平板的 MQTT URL；空则按请求主机拼端口
	ClientMQTTPort  string         // 登录回包 MQTT 端口；空则 52184
	OSS             OSS            // 厂内对象存储；点云/图片本体落这里，不进 WAN
	ShutdownTimeout time.Duration  // 优雅退出最长等待
	DockerUpdate    bool           // 超管确认后由帮手 docker load 换本容器
	DockerHost      string         // Docker 套接字，默认 unix:///var/run/docker.sock
	DockerUpdateDir string         // 与帮手共用的 tar 目录
	DemoData        bool           // 只开发版打开：放开写入演示焊次；生产默认关，免得假数据记到真人名下
}

// OSS 是 S3 兼容对象存储的接入参数；Endpoint 为空表示本厂暂未接 OSS。
type OSS struct {
	Endpoint  string // S3 端点，如 http://oss:9000
	Bucket    string // 本厂默认桶
	AccessKey string // 访问密钥；只在进程内存里，不进日志
	SecretKey string // 访问密钥密码；只在进程内存里，不进日志
}

// Enabled 表示配置齐全、可以接对象存储。
func (o OSS) Enabled() bool { return o.Endpoint != "" }

// 本地开发默认连 server/docker-compose.yml 起的测试库；生产必须显式给 WMESH_DSN。
const devDSN = "postgres://wmesh:wmesh@127.0.0.1:55433/postgres?sslmode=disable"

// Load 从 WMESH_* 环境变量装配配置；缺必填项直接报错，不带隐含默认密码上线。
func Load() (Config, error) {
	// 先按环境把配置装进来，缺的稍后补。
	c := Config{
		HTTPAddr:       envOr("WMESH_HTTP_ADDR", ":8081"),
		DSN:            envOr("WMESH_DSN", devDSN),
		WebDir:         strings.TrimSpace(os.Getenv("WMESH_WEB_DIR")),
		WANURL:         strings.TrimSpace(os.Getenv("WMESH_WAN_URL")),
		WANMQTT:        strings.TrimSpace(os.Getenv("WMESH_WAN_MQTT_URL")),
		WANCAFile:      strings.TrimSpace(os.Getenv("WMESH_WAN_CA_FILE")),
		ClientMQTTAddr: envOr("WMESH_CLIENT_MQTT_ADDR", ":1884"),
		ClientMQTTURL:  strings.TrimSpace(os.Getenv("WMESH_CLIENT_MQTT_URL")),
		ClientMQTTPort: envOr("WMESH_CLIENT_MQTT_PORT", "52184"),
		OSS: OSS{
			Endpoint:  strings.TrimRight(strings.TrimSpace(os.Getenv("WMESH_OSS_ENDPOINT")), "/"),
			Bucket:    strings.TrimSpace(os.Getenv("WMESH_OSS_BUCKET")),
			AccessKey: os.Getenv("WMESH_OSS_ACCESS_KEY"),
			SecretKey: os.Getenv("WMESH_OSS_SECRET_KEY"),
		},
		ShutdownTimeout: 10 * time.Second,
		DockerUpdate:    envTruthy("WMESH_DOCKER_UPDATE"),
		DockerHost:      envOr("WMESH_DOCKER_HOST", "unix:///var/run/docker.sock"),
		DockerUpdateDir: envOr("WMESH_DOCKER_UPDATE_DIR", "/var/lib/wmesh/update"),
		DemoData:        envTruthy("WMESH_DEMO_DATA"),
	}
	// 环境指定了退出等待就解析成一段时间。
	if v := os.Getenv("WMESH_SHUTDOWN_TIMEOUT"); v != "" {
		// 把退出等待解析成一段时间。
		d, err := time.ParseDuration(v)
		// 没能把退出等待解析成一段时间就停，避免带着残缺继续。
		if err != nil {
			// 退出等待时间不合法，不能启动。
			return Config{}, fmt.Errorf("WMESH_SHUTDOWN_TIMEOUT: %w", err)
		}
		// 环境里的退出等待合法就改用它。
		c.ShutdownTimeout = d
	}
	// 配了 CA 就当场读成池，读不出不带着坏配置去连 WAN。
	caErr := c.loadWANCA()
	// 交回半套配置直接拒绝，和 CA 错误一起交回。
	return c, errors.Join(c.validate(), caErr)
}

// loadWANCA 把 WMESH_WAN_CA_FILE 读成 CA 池；没配就留空，走系统根证书。
func (c *Config) loadWANCA() error {
	// 没配 CA 文件就用系统根证书，公网证书也能校验。
	if c.WANCAFile == "" {
		return nil
	}
	// 读 CA 文件，读不到就拒绝启动。
	raw, err := os.ReadFile(c.WANCAFile)
	// 文件读不到说明挂载或路径写错了。
	if err != nil {
		// 带上原因交回，方便对照挂载路径。
		return fmt.Errorf("WMESH_WAN_CA_FILE: %w", err)
	}
	// 新建空池，只信这份 CA，不混系统根证书。
	pool := x509.NewCertPool()
	// 一张 PEM 证书都解不出就不是 CA 文件。
	if !pool.AppendCertsFromPEM(raw) {
		// 带上路径交回，方便查是不是拷错了文件。
		return fmt.Errorf("WMESH_WAN_CA_FILE %q has no PEM certificate", c.WANCAFile)
	}
	// 读好的池交给出站通道校验 WAN 证书。
	c.WANRootCAs = pool
	return nil
}

// validate 半配置直接拒绝，避免带空密码上线。
func (c Config) validate() error {
	// 先记下失败，重试完再决定交不交出去。
	var errs []error
	// 配了页面目录才检查它是不是文件夹。
	if c.WebDir != "" {
		// 出错或条件不够就停下，避免半对的结果往下用。
		if st, err := os.Stat(c.WebDir); err != nil || !st.IsDir() {
			// 把这一段接进结果，顺序要保持住。
			errs = append(errs, fmt.Errorf("WMESH_WEB_DIR %q is not a directory", c.WebDir))
		}
	}
	// 接了 OSS 就必须给全桶和密钥，半配置比不配更难排查。
	if c.OSS.Enabled() && (c.OSS.Bucket == "" || c.OSS.AccessKey == "" || c.OSS.SecretKey == "") {
		// 把这一段接进结果，顺序要保持住。
		errs = append(errs, errors.New("WMESH_OSS_BUCKET, WMESH_OSS_ACCESS_KEY and WMESH_OSS_SECRET_KEY are required with WMESH_OSS_ENDPOINT"))
	}
	// HTTPS 配明文 MQTT 时，抓到的连接签名能拿去 HTTPS 领租约，直接拒绝。
	if schemeOf(c.WANURL) == "https" && plainMQTT(c.WANMQTT) {
		// 记下混配，提示改成 ssl:// 或留空自动拼。
		errs = append(errs, fmt.Errorf("WMESH_WAN_MQTT_URL %q is plaintext while WMESH_WAN_URL is https; use ssl:// or leave it empty", c.WANMQTT))
	}
	// 交回把多条错误并成一条再交回去的结果。
	return errors.Join(errs...)
}

// schemeOf 取地址的协议并转小写；解析不了给空。
func schemeOf(raw string) string {
	// 解析地址，只要协议部分。
	u, err := url.Parse(strings.TrimSpace(raw))
	// 解析不了就当没有协议。
	if err != nil {
		return ""
	}
	// 协议转小写再比，避免大小写写法漏判。
	return strings.ToLower(u.Scheme)
}

// plainMQTT 判断 MQTT 地址是不是明文协议；空地址按 WAN 地址自动拼，不算明文。
func plainMQTT(raw string) bool {
	// 按协议认明文，加密的和空的都放过。
	switch schemeOf(raw) {
	// 这几种都不加密，链路上能被抓包。
	case "tcp", "mqtt", "ws":
		return true
	// 其余（ssl、tls、mqtts、wss 或留空）不算明文。
	default:
		return false
	}
}

// envTruthy 1/true/yes/on 视为打开。
func envTruthy(key string) bool {
	// 按环境里的写法决定开关，认不出就当关掉。
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	// 这些写法都当成开关打开。
	case "1", "true", "yes", "on":
		return true
	// 其余写法都当成开关关掉。
	default:
		return false
	}
}

// envOr 空则用开发默认。
func envOr(key, def string) string {
	// 环境里写了才覆盖，空的保持默认。
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}
