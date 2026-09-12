// Package nodekey 做节点签发用的 Ed25519 密钥与签名；不处理工艺/工程内容。
package nodekey

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/subtle"
)

// Generate 生成本机或本厂签发密钥对：公钥 32 字节，私钥 64 字节。
func Generate() (pub, priv []byte, err error) {
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	return pubKey, privKey, nil
}

// Sign 用私钥签声明原文。
func Sign(priv, msg []byte) []byte {
	return ed25519.Sign(ed25519.PrivateKey(priv), msg)
}

// Verify 校验签发公钥与声明签名。
func Verify(pub, msg, sig []byte) bool {
	if len(pub) != ed25519.PublicKeySize || len(sig) != ed25519.SignatureSize {
		return false
	}
	return ed25519.Verify(ed25519.PublicKey(pub), msg, sig)
}

// Match 判断私钥是否对应这把公钥，避免只拷了凭证文件。
func Match(priv, pub []byte) bool {
	if len(priv) != ed25519.PrivateKeySize || len(pub) != ed25519.PublicKeySize {
		return false
	}
	want := ed25519.PrivateKey(priv).Public().(ed25519.PublicKey)
	return subtle.ConstantTimeCompare(want, pub) == 1
}
