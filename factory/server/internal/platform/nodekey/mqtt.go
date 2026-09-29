package nodekey

import (
	"encoding/base64"
	"errors"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// MQTTConnectPayload 厂端 CONNECT/HTTPS 拉正文要签的原文，两边必须字节一致。
func MQTTConnectPayload(factoryID uuid.UUID, unix int64) []byte {
	// 把整数收成十进制文本。
	ts := strconv.FormatInt(unix, 10)
	// 按需要的长度把缓冲准备好。
	b := make([]byte, 0, 18+16+len(ts))
	// 接上固定前缀，两边拼出来的原文才一致。
	b = append(b, "wmesh-wan-mqtt-v1"...)
	// 接上工厂身份，对端才能认出是哪一家。
	b = append(b, factoryID[:]...)
	// 按约定顺序接上这一段，不能前后颠倒。
	b = append(b, ts...)
	return b
}

// FormatMQTTPassword 把时间窗和签名收成 MQTT 密码。
func FormatMQTTPassword(unix int64, sig []byte) string {
	// 交回把签名收成可以放进口令的文本的结果。
	return strconv.FormatInt(unix, 10) + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// ParseMQTTPassword 拆 MQTT 密码里的 unix 秒和签名。
func ParseMQTTPassword(password string) (unix int64, sig []byte, err error) {
	// 按分隔符拆成前后两段。
	unixStr, enc, ok := strings.Cut(password, ".")
	// 拆不开就当口令不合法。
	if !ok {
		return 0, nil, errMQTTPassword
	}
	// 把十进制文本收成整数。
	unix, err = strconv.ParseInt(unixStr, 10, 64)
	// 没能把十进制文本收成整数就停，避免带着残缺继续。
	if err != nil {
		return 0, nil, errMQTTPassword
	}
	// 把编码过的签名解回来。
	sig, err = base64.RawURLEncoding.DecodeString(enc)
	// 没能把编码过的签名解回来就停，避免带着残缺继续。
	if err != nil {
		return 0, nil, errMQTTPassword
	}
	return unix, sig, nil
}

// 连接口令拆不开时交回的错误。
var errMQTTPassword = errors.New("invalid mqtt password")

// SignMQTTPassword 用厂钥签发 CONNECT/HTTPS 密码。
func SignMQTTPassword(priv []byte, factoryID uuid.UUID, unix int64) string {
	// 交回消息连接消息体，再交给后面的结果。
	return FormatMQTTPassword(unix, Sign(priv, MQTTConnectPayload(factoryID, unix)))
}
