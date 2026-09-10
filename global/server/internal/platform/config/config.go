// Package config 集中读取 WAN 进程的环境变量并做最基本校验；不含业务规则，也不连库。
package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// Config 是 WAN 进程一次启动用到的全部外部配置。
type Config struct {
	HTTPAddr            string        // 监听地址
	DSN                 string        // WAN 库连接串；只连 WAN 库，不连任何厂库
	WebDir              string        // 已构建管理端目录；空表示只提供 API
	FactoryBootstrapURL string        // 厂端引导地址，建厂时在厂库写入初始超管
	BootstrapToken      string        // 建厂引导共享口令，须与厂端一致
	AdminLogin          string        // 首次启动写入的唯一 WAN 管理员登录名；空则跳过引导
	AdminPassword       string        // 配套口令；只在进程内存里，不进日志
	OSS                 OSS           // WAN 自己的对象存储，放平台级资产；与各厂 OSS 互不相通
	ShutdownTimeout     time.Duration // 优雅退出最长等待
}

// OSS 是 S3 兼容对象存储的接入参数；Endpoint 为空表示暂未接 OSS。
type OSS struct {
	Endpoint  string // S3 端点，如 http://oss:9000
	Bucket    string // WAN 默认桶
	AccessKey string // 访问密钥；只在进程内存里，不进日志
	SecretKey string // 访问密钥口令；只在进程内存里，不进日志
}

// Enabled 表示配置齐全、可以接对象存储。
func (o OSS) Enabled() bool { return o.Endpoint != "" }

// 本地开发默认连 server/docker-compose.yml 起的测试库；生产必须显式给 WMESH_DSN。
const devDSN = "postgres://wmesh:wmesh@127.0.0.1:55432/wmesh?sslmode=disable"

// Load 从 WMESH_* 环境变量装配配置；缺必填项直接报错，不带隐含默认口令上线。
func Load() (Config, error) {
	c := Config{
		HTTPAddr:            envOr("WMESH_HTTP_ADDR", ":8080"),
		DSN:                 envOr("WMESH_DSN", devDSN),
		WebDir:              strings.TrimSpace(os.Getenv("WMESH_WEB_DIR")),
		FactoryBootstrapURL: envOr("WMESH_FACTORY_BOOTSTRAP_URL", "http://127.0.0.1:8081"),
		BootstrapToken:      os.Getenv("WMESH_BOOTSTRAP_TOKEN"),
		AdminLogin:          strings.TrimSpace(os.Getenv("WMESH_ADMIN_LOGIN")),
		AdminPassword:       os.Getenv("WMESH_ADMIN_PASSWORD"),
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
	if c.BootstrapToken == "" {
		errs = append(errs, errors.New("WMESH_BOOTSTRAP_TOKEN is required"))
	}
	// 给了登录名就必须给口令，避免半配置把管理员锁在门外。
	if c.AdminLogin != "" && c.AdminPassword == "" {
		errs = append(errs, errors.New("WMESH_ADMIN_PASSWORD is required with WMESH_ADMIN_LOGIN"))
	}
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
