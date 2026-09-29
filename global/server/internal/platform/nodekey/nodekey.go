// Package nodekey 做节点签发用的 Ed25519 密钥与签名；不处理工艺/工程内容。
package nodekey

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/subtle"
)

// Generate 生成本机或本厂签发密钥对：公钥 32 字节，私钥 64 字节。
func Generate() (pub, priv []byte, err error) {
	// 用系统随机数生成一对签发钥。
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	// 生成失败则空着交回，避免交出半套钥。
	if err != nil {
		return nil, nil, err
	}
	return pubKey, privKey, nil
}

// Sign 用私钥签声明原文。
func Sign(priv, msg []byte) []byte {
	// 用私钥对声明原文做签名。
	return ed25519.Sign(ed25519.PrivateKey(priv), msg)
}

// Verify 校验签发公钥与声明签名。
func Verify(pub, msg, sig []byte) bool {
	// 公钥或签名长度不对就直接否决。
	if len(pub) != ed25519.PublicKeySize || len(sig) != ed25519.SignatureSize {
		return false
	}
	// 长度合格才做签名校验。
	return ed25519.Verify(ed25519.PublicKey(pub), msg, sig)
}

// Match 判断私钥是否对应这把公钥，避免只拷了凭证文件。
func Match(priv, pub []byte) bool {
	// 私钥或公钥长度不对就不是一对。
	if len(priv) != ed25519.PrivateKeySize || len(pub) != ed25519.PublicKeySize {
		return false
	}
	// 从私钥推导出应对上的公钥。
	want := ed25519.PrivateKey(priv).Public().(ed25519.PublicKey)
	// 常数时间比对，避免从耗时猜密钥。
	return subtle.ConstantTimeCompare(want, pub) == 1
}
