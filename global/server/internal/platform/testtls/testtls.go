// Package testtls 在测试里现生成自建 CA 和它签的服务端证书。仅测试夹具，生产路径禁依赖。
package testtls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// PKI 是一套测试证书：自建 CA 和它签的服务端证书，SAN 含 127.0.0.1 与 localhost。
type PKI struct {
	CAPEM   []byte            // CA 证书 PEM，当厂端的 CA 文件用
	CertPEM []byte            // 服务端证书 PEM
	KeyPEM  []byte            // 服务端私钥 PEM
	Pool    *x509.CertPool    // 只含这份 CA 的池，客户端校验用
	Cert    tls.Certificate   // 服务端证书加私钥，监听直接用
	Leaf    *x509.Certificate // 解开的服务端证书，断言到期时间用
}

// New 现生成一份 CA 和服务端证书；生成失败就停测。
func New(t testing.TB) PKI {
	// 失败行指到调用用例，不指到夹具。
	t.Helper()
	// 生成 CA 私钥，签服务端证书要用。
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	// 私钥生不出来就没法签。
	if err != nil {
		// 停测并带上原因。
		t.Fatalf("ca key: %v", err)
	}
	// CA 模板：只能签证书，有效期覆盖整轮测试。
	caTpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "wmesh test ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	// CA 自签，得到证书原文。
	caDER, err := x509.CreateCertificate(rand.Reader, caTpl, caTpl, &caKey.PublicKey, caKey)
	// 自签失败就停测。
	if err != nil {
		// 停测并带上原因。
		t.Fatalf("ca cert: %v", err)
	}
	// 解开 CA 证书，签下级时当父证书。
	caCert, err := x509.ParseCertificate(caDER)
	// 解不开说明原文坏了。
	if err != nil {
		// 停测并带上原因。
		t.Fatalf("parse ca: %v", err)
	}
	// 生成服务端私钥。
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	// 私钥生不出来就停测。
	if err != nil {
		// 停测并带上原因。
		t.Fatalf("server key: %v", err)
	}
	// 服务端模板：只做服务端认证，SAN 写本机地址。
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(12 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:     []string{"localhost"},
	}
	// 用 CA 签服务端证书。
	der, err := x509.CreateCertificate(rand.Reader, tpl, caCert, &key.PublicKey, caKey)
	// 签不出来就停测。
	if err != nil {
		// 停测并带上原因。
		t.Fatalf("server cert: %v", err)
	}
	// 私钥编成 PKCS8，写 PEM 用。
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	// 编不出来就停测。
	if err != nil {
		// 停测并带上原因。
		t.Fatalf("marshal key: %v", err)
	}
	// 三份 PEM 先编好，写文件和加载都用它。
	p := PKI{
		CAPEM:   pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}),
		CertPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		KeyPEM:  pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}),
		Pool:    x509.NewCertPool(),
	}
	// 池里只放这份 CA，客户端只信它。
	p.Pool.AddCert(caCert)
	// 证书和私钥配成对，监听直接用。
	p.Cert, err = tls.X509KeyPair(p.CertPEM, p.KeyPEM)
	// 配不成对就停测。
	if err != nil {
		// 停测并带上原因。
		t.Fatalf("key pair: %v", err)
	}
	// 解开服务端证书，断言到期时间用。
	p.Leaf, err = x509.ParseCertificate(der)
	// 解不开就停测。
	if err != nil {
		// 停测并带上原因。
		t.Fatalf("parse leaf: %v", err)
	}
	return p
}

// ServerConfig 给监听用的 TLS 配置，带上服务端证书。
func (p PKI) ServerConfig() *tls.Config {
	// 至少 TLS1.2，和线上一致。
	return &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{p.Cert}}
}

// ClientConfig 给客户端用的 TLS 配置，只信这份 CA。
func (p PKI) ClientConfig() *tls.Config {
	// 只信这份 CA，校验服务端证书。
	return &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: p.Pool}
}

// WriteFiles 把 CA、证书、私钥写进临时目录，交回三个路径。
func (p PKI) WriteFiles(t testing.TB) (caFile, certFile, keyFile string) {
	// 失败行指到调用用例，不指到夹具。
	t.Helper()
	// 每例一个临时目录，测完自动清掉。
	dir := t.TempDir()
	// 拼出三个文件路径。
	caFile, certFile, keyFile = filepath.Join(dir, "ca.pem"), filepath.Join(dir, "server.pem"), filepath.Join(dir, "server.key")
	// 按路径逐个写入。
	for path, body := range map[string][]byte{caFile: p.CAPEM, certFile: p.CertPEM, keyFile: p.KeyPEM} {
		// 写不进去就停测。
		if err := os.WriteFile(path, body, 0o600); err != nil {
			// 停测并带上原因。
			t.Fatalf("write %s: %v", path, err)
		}
	}
	return caFile, certFile, keyFile
}
