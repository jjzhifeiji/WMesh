// Package clientmqtt 管本厂内嵌 Client Broker：验本机会话、按机锁 Topic、转发小指令。
// 不管闭包正文，也不判业务对错。
package clientmqtt

import (
	"encoding/binary"
	"encoding/json"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/nodekey"
)

const (
	TypPolicy   = "policy"   // 本厂 Client 策略，只含键与修订
	TypClosure  = "closure"  // 闭包就绪：身份、修订、摘要，不含正文
	TypAck      = "ack"      // Client 回执
	TypPresence = "presence" // 示教器自报正在跑的 APK 版本
)

// Intent 是厂→Client 控制面小信封，禁止夹带正文。
type Intent struct {
	Typ               string `json:"typ"`                         // policy / closure
	Revision          int64  `json:"revision"`                    // 策略修订或工程修订
	AssetID           string `json:"assetId,omitempty"`           // 工程身份；策略可空
	Digest            []byte `json:"digest,omitempty"`            // 整包摘要；策略可空
	MaxCachedProjects int    `json:"maxCachedProjects,omitempty"` // 策略：工程份上限
	CacheScope        string `json:"cacheScope,omitempty"`        // 策略：all / current
	PersistUnwrapKey  bool   `json:"persistUnwrapKey"`            // 策略：解封钥可否落盘
	KeyTTLSeconds     int64  `json:"keyTtlSeconds"`               // 策略：登录时效秒
	EncryptPouch      bool   `json:"encryptPouch"`                // 策略：本机袋是否 SQLCipher
	Sig               []byte `json:"sig"`                         // 本厂签发钥对 Message 的签名
}

// Receipt 是 Client→厂回执，不含正文。
type Receipt struct {
	Typ      string `json:"typ"`                // ack
	AssetID  string `json:"assetId,omitempty"`  // 对应工程；策略回执可空
	Revision int64  `json:"revision"`           // 已接受的修订
}

// Message 厂签发与本机验签必须字节一致。
func Message(factoryID, clientID uuid.UUID, in Intent) []byte {
	var asset uuid.UUID
	if in.AssetID != "" {
		if id, err := uuid.Parse(in.AssetID); err == nil {
			asset = id
		}
	}
	var revb, maxb, ttl, dlen [8]byte
	binary.BigEndian.PutUint64(revb[:], uint64(in.Revision))
	binary.BigEndian.PutUint64(maxb[:], uint64(in.MaxCachedProjects))
	binary.BigEndian.PutUint64(ttl[:], uint64(in.KeyTTLSeconds))
	binary.BigEndian.PutUint64(dlen[:], uint64(len(in.Digest)))
	persist := byte(0)
	if in.PersistUnwrapKey {
		persist = 1
	}
	out := make([]byte, 0, 16+16+len(in.Typ)+8+16+8+len(in.Digest)+8+len(in.CacheScope)+1+8+1)
	out = append(out, factoryID[:]...)
	out = append(out, clientID[:]...)
	out = append(out, in.Typ...)
	out = append(out, revb[:]...)
	out = append(out, asset[:]...)
	out = append(out, dlen[:]...)
	out = append(out, in.Digest...)
	out = append(out, maxb[:]...)
	out = append(out, in.CacheScope...)
	out = append(out, persist)
	out = append(out, ttl[:]...)
	encrypt := byte(0)
	if in.EncryptPouch {
		encrypt = 1
	}
	out = append(out, encrypt)
	return out
}

// Sign 用本厂签发钥签小信封；JSON 里没有正文。
func Sign(priv []byte, factoryID, clientID uuid.UUID, in Intent) ([]byte, error) {
	in.Sig = nodekey.Sign(priv, Message(factoryID, clientID, in))
	return json.Marshal(in)
}

// Verify 校验签发方、目标机；修订是否向前由调用方判定。
func Verify(pub []byte, factoryID, clientID uuid.UUID, raw []byte) (Intent, error) {
	var in Intent
	if err := json.Unmarshal(raw, &in); err != nil {
		return Intent{}, err
	}
	if !nodekey.Verify(pub, Message(factoryID, clientID, in), in.Sig) {
		return Intent{}, errBadSig
	}
	return in, nil
}

// HasBody 控制面夹带了正文或成员，视为实现错误。
func HasBody(raw []byte) bool {
	var peek map[string]json.RawMessage
	if json.Unmarshal(raw, &peek) != nil {
		return true
	}
	if _, ok := peek["content"]; ok {
		return true
	}
	if _, ok := peek["members"]; ok {
		return true
	}
	if _, ok := peek["wrap"]; ok {
		return true
	}
	return bytesHasWM2(raw)
}

// bytesHasWM2 控制面出现 WM2 魔数即视为夹带正文。
func bytesHasWM2(raw []byte) bool {
	for i := 0; i+3 <= len(raw); i++ {
		if raw[i] == 'W' && raw[i+1] == 'M' && raw[i+2] == '2' {
			return true
		}
	}
	return false
}

var errBadSig = errIntent("invalid intent signature")

type errIntent string

// Error 英文错误原文。
func (e errIntent) Error() string { return string(e) }
