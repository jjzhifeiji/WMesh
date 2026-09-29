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
	// 秒数收成十进制，两边字节才能对齐。
	ts := strconv.FormatInt(unix, 10)
	// 按前缀、厂身份和秒数预留缓冲。
	b := make([]byte, 0, 18+16+len(ts))
	// 先写算法名前缀，防止和别的原文混。
	b = append(b, "wmesh-wan-mqtt-v1"...)
	// 再写入厂的稳定身份。
	b = append(b, factoryID[:]...)
	// 最后跟上秒数，时间窗才进签名。
	b = append(b, ts...)
	return b
}

// FormatMQTTPassword 把时间窗和签名收成 MQTT 密码。
func FormatMQTTPassword(unix int64, sig []byte) string {
	// 秒数和签名拼成密码，中间用点分开。
	return strconv.FormatInt(unix, 10) + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// ParseMQTTPassword 拆 MQTT 密码里的 unix 秒和签名。
func ParseMQTTPassword(password string) (unix int64, sig []byte, err error) {
	// 从点号拆出秒数和签名两段。
	unixStr, enc, ok := strings.Cut(password, ".")
	// 没有点号就不是合法密码。
	if !ok {
		return 0, nil, errMQTTPassword
	}
	// 秒数必须是十进制整数。
	unix, err = strconv.ParseInt(unixStr, 10, 64)
	// 秒数解析失败则整段密码作废。
	if err != nil {
		return 0, nil, errMQTTPassword
	}
	// 签名按无填充的网址安全编码还原。
	sig, err = base64.RawURLEncoding.DecodeString(enc)
	// 签名解码失败则整段密码作废。
	if err != nil {
		return 0, nil, errMQTTPassword
	}
	return unix, sig, nil
}

// 密码格式不对时交回的固定错误。
var errMQTTPassword = errors.New("invalid mqtt password")

// SignMQTTPassword 用厂钥签发 CONNECT/HTTPS 密码。
func SignMQTTPassword(priv []byte, factoryID uuid.UUID, unix int64) string {
	// 先签连接原文，再收成密码。
	return FormatMQTTPassword(unix, Sign(priv, MQTTConnectPayload(factoryID, unix)))
}
