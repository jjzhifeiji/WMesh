// Package nodekey 做节点签发用的 Ed25519 密钥与签名；不处理工艺/工程内容。
package nodekey

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/subtle"
)

// Generate 生成本机或本厂签发密钥对：公钥 32 字节，私钥 64 字节。
func Generate() (pub, priv []byte, err error) {
	// 生成键，再交给后面。
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	// 没能生成键，再交给后面就停，避免带着残缺继续。
	if err != nil {
		return nil, nil, err
	}
	return pubKey, privKey, nil
}

// Sign 用私钥签声明原文。
func Sign(priv, msg []byte) []byte {
	// 交回私钥键，再交给后面的结果。
	return ed25519.Sign(ed25519.PrivateKey(priv), msg)
}

// Verify 校验签发公钥与声明签名。
func Verify(pub, msg, sig []byte) bool {
	// 对不上就换一路，避免把不符的当成通过。
	if len(pub) != ed25519.PublicKeySize || len(sig) != ed25519.SignatureSize {
		return false
	}
	// 交回公钥键，再交给后面的结果。
	return ed25519.Verify(ed25519.PublicKey(pub), msg, sig)
}

// Match 判断私钥是否对应这把公钥，避免只拷了凭证文件。
func Match(priv, pub []byte) bool {
	// 对不上就换一路，避免把不符的当成通过。
	if len(priv) != ed25519.PrivateKeySize || len(pub) != ed25519.PublicKeySize {
		return false
	}
	// 公钥，再交给后面，再交给后面。
	want := ed25519.PrivateKey(priv).Public().(ed25519.PublicKey)
	// 交回恒定时间比较，避免看出前缀的结果。
	return subtle.ConstantTimeCompare(want, pub) == 1
}
