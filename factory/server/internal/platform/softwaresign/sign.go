// Package softwaresign 拼软件包验签原文；不持钥、不存包。
package softwaresign

import (
	"encoding/binary"

	"github.com/google/uuid"
)

const prefix = "wmesh-software-v1" // 算法名，两边必须相同

// Message 按种类、版本、摘要和目标身份拼签名原文。
func Message(kind string, version int64, digest []byte, target uuid.UUID) []byte {
	// 准备放修订、上限、租约或长度的八字节。
	var ver [8]byte
	// 收成整数再参加后面的计算。
	binary.BigEndian.PutUint64(ver[:], uint64(version))
	// 按需要的长度把缓冲准备好。
	msg := make([]byte, 0, len(prefix)+len(kind)+1+8+len(digest)+16)
	// 接上固定前缀，两边拼出来的原文才一致。
	msg = append(msg, prefix...)
	// 接上种类，原文里才分得清是什么包。
	msg = append(msg, kind...)
	// 按约定顺序接上这一段，不能前后颠倒。
	msg = append(msg, 0)
	// 接上修订或版本的字节，顺序不能换。
	msg = append(msg, ver[:]...)
	// 接上摘要，完整性才能对上这一份。
	msg = append(msg, digest...)
	// 接上资产或目标身份，原文才绑得住。
	msg = append(msg, target[:]...)
	return msg
}
