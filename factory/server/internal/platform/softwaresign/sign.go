// Package softwaresign 拼软件包验签原文；不持钥、不存包。
package softwaresign

import (
	"encoding/binary"

	"github.com/google/uuid"
)

const prefix = "wmesh-software-v1" // 算法名，两边必须相同

// Message 按种类、版本、摘要和目标身份拼签名原文。
func Message(kind string, version int64, digest []byte, target uuid.UUID) []byte {
	var ver [8]byte
	binary.BigEndian.PutUint64(ver[:], uint64(version))
	msg := make([]byte, 0, len(prefix)+len(kind)+1+8+len(digest)+16)
	msg = append(msg, prefix...)
	msg = append(msg, kind...)
	msg = append(msg, 0)
	msg = append(msg, ver[:]...)
	msg = append(msg, digest...)
	msg = append(msg, target[:]...)
	return msg
}
