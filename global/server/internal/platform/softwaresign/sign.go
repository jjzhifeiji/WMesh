// Package softwaresign 拼软件包验签原文；不持钥、不存包。
package softwaresign

import (
	"encoding/binary"

	"github.com/google/uuid"
)

const prefix = "wmesh-software-v1" // 算法名，两边必须相同

// Message 按种类、版本、摘要和目标身份拼签名原文。
func Message(kind string, version int64, digest []byte, target uuid.UUID) []byte {
	// 版本号放进定长缓冲。
	var ver [8]byte
	// 版本按大端写入，两边字节一致。
	binary.BigEndian.PutUint64(ver[:], uint64(version))
	// 按各段长度预留原文缓冲。
	msg := make([]byte, 0, len(prefix)+len(kind)+1+8+len(digest)+16)
	// 先写算法名，防止和别的原文混。
	msg = append(msg, prefix...)
	// 接着写入包的种类。
	msg = append(msg, kind...)
	// 用一个空字节隔开种类和后面的数。
	msg = append(msg, 0)
	// 接上已经排好字节的版本号。
	msg = append(msg, ver[:]...)
	// 接上包摘要，签的是这一份内容。
	msg = append(msg, digest...)
	// 最后写目标身份，签过的包不能挪给别人。
	msg = append(msg, target[:]...)
	return msg
}
