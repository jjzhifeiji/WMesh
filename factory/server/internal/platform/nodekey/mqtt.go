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
	ts := strconv.FormatInt(unix, 10)
	b := make([]byte, 0, 18+16+len(ts))
	b = append(b, "wmesh-wan-mqtt-v1"...)
	b = append(b, factoryID[:]...)
	b = append(b, ts...)
	return b
}

// FormatMQTTPassword 把时间窗和签名收成 MQTT 密码。
func FormatMQTTPassword(unix int64, sig []byte) string {
	return strconv.FormatInt(unix, 10) + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// ParseMQTTPassword 拆 MQTT 密码里的 unix 秒和签名。
func ParseMQTTPassword(password string) (unix int64, sig []byte, err error) {
	unixStr, enc, ok := strings.Cut(password, ".")
	if !ok {
		return 0, nil, errMQTTPassword
	}
	unix, err = strconv.ParseInt(unixStr, 10, 64)
	if err != nil {
		return 0, nil, errMQTTPassword
	}
	sig, err = base64.RawURLEncoding.DecodeString(enc)
	if err != nil {
		return 0, nil, errMQTTPassword
	}
	return unix, sig, nil
}

var errMQTTPassword = errors.New("invalid mqtt password")

// SignMQTTPassword 用厂钥签发 CONNECT/HTTPS 密码。
func SignMQTTPassword(priv []byte, factoryID uuid.UUID, unix int64) string {
	return FormatMQTTPassword(unix, Sign(priv, MQTTConnectPayload(factoryID, unix)))
}
