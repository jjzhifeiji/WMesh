package service

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/platform/nodekey"
)

// 下行指令签名钥：复用 wan_signing_keys 那一行，只在内存里用。
type channelKey struct {
	pub  []byte // Ed25519 公钥，经 HTTPS 索引交给厂端钉住
	priv []byte // Ed25519 私钥，只用来签下行指令
}

// LoadChannelKey 启动时读出下行指令签名钥，库里没有就生成一把写入；失败进程不该起来。
func (s *Channel) LoadChannelKey(ctx context.Context) error {
	// 先读库里唯一那一行，旧环境发过包的直接沿用。
	row, err := s.store.WANSigningKey(ctx)
	// 没有就现生成一把写进去。
	if errors.Is(err, domain.ErrNotFound) {
		// 生成一对新钥，私钥只进库和内存。
		pub, priv, gerr := nodekey.Generate()
		// 生不出钥就交回，进程不能无钥对外发指令。
		if gerr != nil {
			return gerr
		}
		// 写入唯一一行，撞上别的进程刚写的就改读它。
		row, err = s.store.PutWANSigningKey(ctx, pub, priv)
		// 已有一行说明别处先写了，重读那一把。
		if errors.Is(err, domain.ErrSigningKeyExists) {
			// 重读唯一那一行，两边用同一把钥。
			row, err = s.store.WANSigningKey(ctx)
		}
	}
	// 读写库失败就交回，启动随之失败。
	if err != nil {
		return err
	}
	// 公钥和私钥必须是一对，否则厂端验签全不过。
	if !nodekey.Match(row.PrivateKey, row.PublicKey) {
		return domain.ErrInvalidKey
	}
	// 放进内存，之后签名不再碰库。
	s.cmdKey.Store(&channelKey{pub: row.PublicKey, priv: row.PrivateKey})
	return nil
}

// ChannelPublicKey 交出下行指令验签公钥，私钥不外露；没载入时为空。
func (s *Channel) ChannelPublicKey() []byte {
	// 取当前签名钥，没载入就当没有。
	key := s.cmdKey.Load()
	// 没载入签名钥就交空，厂端照旧格式走。
	if key == nil {
		return nil
	}
	// 拷一份交出去，调用方改不到内存里的钥。
	return append([]byte(nil), key.pub...)
}

// signCmd 给发往这一家厂的指令签名：原文和签名填进指令，旧字段保留给老厂。
func (k *kernel) signCmd(factoryID uuid.UUID, cmd Cmd) (Cmd, error) {
	// 取当前签名钥，只有没载入的测试会拿到空。
	key := k.cmdKey.Load()
	// 没载入签名钥就原样交回，照旧格式发。
	if key == nil {
		return cmd, nil
	}
	// 先清掉旧签名，原文里不能套着上一次的签名。
	cmd.Signed, cmd.Sig = nil, nil
	// 编出指令原文，签名就覆盖这些字节。
	raw, err := json.Marshal(cmd)
	// 编不出就交回，调用方不发这一条。
	if err != nil {
		return Cmd{}, err
	}
	// 填回原文，厂端验过后只认它。
	cmd.Signed = raw
	// 绑定目标厂签名，转投给别厂验不过。
	cmd.Sig = nodekey.Sign(key.priv, nodekey.ChannelCmdPayload(factoryID, raw))
	return cmd, nil
}
