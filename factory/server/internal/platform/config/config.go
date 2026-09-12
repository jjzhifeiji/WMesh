// Package config 集中读取厂内进程的环境变量并做最基本校验；不含业务规则，也不连库。
package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// Config 是厂内进程一次启动用到的全部外部配置。
type Config struct {
	HTTPAddr        string        // 监听地址
	DSN             string        // 维护库连接串；按工厂身份从这里建/开各厂库，账号需 CREATEDB
	WebDir          string        // 已构建管理端目录；空表示只提供 API
	BootstrapToken  string        // 建厂引导共享密码，只用于 /internal/bootstrap；空则关闭该入口
	WANURL          string        // WAN HTTP 根地址，厂出站认领；空则不能认领
	OSS             OSS           // 厂内对象存储；点云/图片本体落这里，不进 WAN
	ShutdownTimeout time.Duration // 优雅退出最长等待
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
	c := Config{
		HTTPAddr:       envOr("WMESH_HTTP_ADDR", ":8081"),
		DSN:            envOr("WMESH_DSN", devDSN),
		WebDir:         strings.TrimSpace(os.Getenv("WMESH_WEB_DIR")),
		BootstrapToken: os.Getenv("WMESH_BOOTSTRAP_TOKEN"),
		WANURL:         strings.TrimSpace(os.Getenv("WMESH_WAN_URL")),
		OSS: OSS{
			Endpoint:  strings.TrimRight(strings.TrimSpace(os.Getenv("WMESH_OSS_ENDPOINT")), "/"),
			Bucket:    strings.TrimSpace(os.Getenv("WMESH_OSS_BUCKET")),
			AccessKey: os.Getenv("WMESH_OSS_ACCESS_KEY"),
			SecretKey: os.Getenv("WMESH_OSS_SECRET_KEY"),
		},
		ShutdownTimeout: 10 * time.Second,
	}
	if v := os.Getenv("WMESH_SHUTDOWN_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return Config{}, fmt.Errorf("WMESH_SHUTDOWN_TIMEOUT: %w", err)
		}
		c.ShutdownTimeout = d
	}
	return c, c.validate()
}

func (c Config) validate() error {
	var errs []error
	if c.WebDir != "" {
		if st, err := os.Stat(c.WebDir); err != nil || !st.IsDir() {
			errs = append(errs, fmt.Errorf("WMESH_WEB_DIR %q is not a directory", c.WebDir))
		}
	}
	// 接了 OSS 就必须给全桶和密钥，半配置比不配更难排查。
	if c.OSS.Enabled() && (c.OSS.Bucket == "" || c.OSS.AccessKey == "" || c.OSS.SecretKey == "") {
		errs = append(errs, errors.New("WMESH_OSS_BUCKET, WMESH_OSS_ACCESS_KEY and WMESH_OSS_SECRET_KEY are required with WMESH_OSS_ENDPOINT"))
	}
	return errors.Join(errs...)
}

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}
