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
	Typ      string `json:"typ"`               // ack
	AssetID  string `json:"assetId,omitempty"` // 对应工程；策略回执可空
	Revision int64  `json:"revision"`          // 已接受的修订
}

// Message 厂签发与本机验签必须字节一致。
func Message(factoryID, clientID uuid.UUID, in Intent) []byte {
	// 准备放下资产，再交给后面。
	var asset uuid.UUID
	// 小信封里带了资产身份才拿去核对。
	if in.AssetID != "" {
		// 没有出错就按成功返回，不用再补救。
		if id, err := uuid.Parse(in.AssetID); err == nil {
			// 定下资产，再交给后面。
			asset = id
		}
	}
	// 准备放修订、上限、租约或长度的八字节。
	var revb, maxb, ttl, dlen [8]byte
	// 收成整数再参加后面的计算。
	binary.BigEndian.PutUint64(revb[:], uint64(in.Revision))
	// 收成整数再参加后面的计算。
	binary.BigEndian.PutUint64(maxb[:], uint64(in.MaxCachedProjects))
	// 收成整数再参加后面的计算。
	binary.BigEndian.PutUint64(ttl[:], uint64(in.KeyTTLSeconds))
	// 收成整数再参加后面的计算。
	binary.BigEndian.PutUint64(dlen[:], uint64(len(in.Digest)))
	// 先标成不保留解包钥，需要时再改成要留。
	persist := byte(0)
	// 这次要求留下解包钥才去保存。
	if in.PersistUnwrapKey {
		// 改成要保留解包钥。
		persist = 1
	}
	// 按需要的长度把缓冲准备好。
	out := make([]byte, 0, 16+16+len(in.Typ)+8+16+8+len(in.Digest)+8+len(in.CacheScope)+1+8+1)
	// 接上工厂身份，对端才能认出是哪一家。
	out = append(out, factoryID[:]...)
	// 接上本机或接收方身份，绑定才不会串。
	out = append(out, clientID[:]...)
	// 按约定顺序接上这一段，不能前后颠倒。
	out = append(out, in.Typ...)
	// 接上修订或版本的字节，顺序不能换。
	out = append(out, revb[:]...)
	// 按约定顺序接上这一段，不能前后颠倒。
	out = append(out, asset[:]...)
	// 按约定顺序接上这一段，不能前后颠倒。
	out = append(out, dlen[:]...)
	// 接上摘要，完整性才能对上这一份。
	out = append(out, in.Digest...)
	// 按约定顺序接上这一段，不能前后颠倒。
	out = append(out, maxb[:]...)
	// 按约定顺序接上这一段，不能前后颠倒。
	out = append(out, in.CacheScope...)
	// 按约定顺序接上这一段，不能前后颠倒。
	out = append(out, persist)
	// 按约定顺序接上这一段，不能前后颠倒。
	out = append(out, ttl[:]...)
	// 先标成正文不加密，需要时再改成要加密。
	encrypt := byte(0)
	// 要求加密示教器库才把这个标志打开。
	if in.EncryptPouch {
		// 改成正文需要加密。
		encrypt = 1
	}
	// 按约定顺序接上这一段，不能前后颠倒。
	out = append(out, encrypt)
	return out
}

// Sign 用本厂签发钥签小信封；JSON 里没有正文。
func Sign(priv []byte, factoryID, clientID uuid.UUID, in Intent) ([]byte, error) {
	// 拼出两边必须字节一致的原文。
	in.Sig = nodekey.Sign(priv, Message(factoryID, clientID, in))
	// 交回把结构收成字节的结果。
	return json.Marshal(in)
}

// Verify 校验签发方、目标机；修订是否向前由调用方判定。
func Verify(pub []byte, factoryID, clientID uuid.UUID, raw []byte) (Intent, error) {
	// 准备承接解出来的小信封。
	var in Intent
	// 没能把字节还原成结构就停，避免带着残缺继续。
	if err := json.Unmarshal(raw, &in); err != nil {
		return Intent{}, err
	}
	// 不满足就停住或跳过，避免做错下一步。
	if !nodekey.Verify(pub, Message(factoryID, clientID, in), in.Sig) {
		return Intent{}, errBadSig
	}
	return in, nil
}

// HasBody 控制面夹带了正文或成员，视为实现错误。
func HasBody(raw []byte) bool {
	// 先窥一眼有没有正文、成员或包装。
	var peek map[string]json.RawMessage
	// 已经有值就按有内容处理，不要覆盖成空。
	if json.Unmarshal(raw, &peek) != nil {
		return true
	}
	// 带了正文字段就当成控制面夹带了正文。
	if _, ok := peek["content"]; ok {
		return true
	}
	// 带了成员字段就当成控制面夹带了正文。
	if _, ok := peek["members"]; ok {
		return true
	}
	// 带了包装字段就当成控制面夹带了正文。
	if _, ok := peek["wrap"]; ok {
		return true
	}
	// 交回扫描里面有没有封装魔数的结果。
	return bytesHasWM2(raw)
}

// bytesHasWM2 控制面出现 WM2 魔数即视为夹带正文。
func bytesHasWM2(raw []byte) bool {
	// 按下标扫过去，直到越界或提前结束。
	for i := 0; i+3 <= len(raw); i++ {
		// 连续三个字母对上魔数才算信封。
		if raw[i] == 'W' && raw[i+1] == 'M' && raw[i+2] == '2' {
			return true
		}
	}
	return false
}

// 小信封签名对不上时交回的错误。
var errBadSig = errIntent("invalid intent signature")

// 控制面错误，用这段文本当错误值。
type errIntent string

// Error 英文错误原文。
func (e errIntent) Error() string { return string(e) }
