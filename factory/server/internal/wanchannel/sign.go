package wanchannel

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/nodekey"
)

// Trust 是本厂钉住的 WAN 指令验签公钥，和首次钉住时落库的回调。
type Trust struct {
	Key []byte             // 已钉的 WAN 公钥；空表示还没钉
	Pin func([]byte) error // 首次经 HTTPS 拿到公钥时落库；空则只在本次连接内存里用
}

// 钉钥落库时发现库里已是另一把，只在并发钉钥时出现；本次连接要退出重拨。
var errPinConflict = fmt.Errorf("%w: wan key pinned concurrently", domain.ErrIntegrity)

// 回包里的 WAN 公钥和已钉的不一致，整批拒收，要人工清 wan_trust。
var errKeyChanged = fmt.Errorf("%w: wan key changed", domain.ErrIntegrity)

// 一条连接的下行验签状态：主循环和后台补拉都会读写，先占锁再动。
type cmdGuard struct {
	mu        sync.Mutex         // 主循环、后台补拉都会读写钉钥
	factoryID uuid.UUID          // 本厂身份，签名原文绑定它
	https     bool               // WAN 地址是 https 才钉钥和强制验签
	key       []byte             // 当前钉住的 WAN 公钥；空表示还没钉
	pin       func([]byte) error // 首次钉住时落库的回调
	conflict  bool               // 钉钥撞上库里另一把，本次连接一律拒收
}

// 按连接参数建验签状态；http 下即便库里有旧钉钥也不强制。
func newCmdGuard(factoryID uuid.UUID, wanHTTP string, trust Trust) *cmdGuard {
	// 拷一份钉钥，外面改不到本次连接用的。
	return &cmdGuard{factoryID: factoryID, https: isHTTPS(wanHTTP), key: append([]byte(nil), trust.Key...), pin: trust.Pin}
}

// 当前验签用的钥和要不要强制：https 且已钉，或钉钥冲突时才强制。
func (g *cmdGuard) current() ([]byte, bool) {
	// 占住状态，避免读到钉了一半的钥。
	g.mu.Lock()
	// 读完就放开。
	defer g.mu.Unlock()
	// 只有 https 才强制，http 下一律照旧格式走。
	if !g.https {
		return nil, false
	}
	// 冲突时强制但不给钥，所有指令都验不过。
	if g.conflict {
		return nil, true
	}
	// 已有 32 字节钉钥才强制验签。
	return g.key, len(g.key) == ed25519.PublicKeySize
}

// 解开一条下行：要强制时验签取原文，不强制时照旧用外层字段。
func (g *cmdGuard) open(outer Cmd) (Cmd, error) {
	// 取当前钉钥和是否强制。
	key, enforce := g.current()
	// 按钉钥验签并取出真正的指令。
	return openCmd(key, g.factoryID, outer, enforce)
}

// 经证书校验的 HTTPS 索引带来 WAN 公钥：还没钉就钉住，已钉就核对。
func (g *cmdGuard) adopt(pub []byte) error {
	// 占住状态，钉钥和核对不和别的读写交错。
	g.mu.Lock()
	// 改完就放开。
	defer g.mu.Unlock()
	// http 下不钉，回包里的钥可能被中途换掉；回包没带钥也不动。
	if !g.https || len(pub) == 0 {
		return nil
	}
	// 冲突过的连接不再钉，等重拨后按库里那把核对。
	if g.conflict {
		return errPinConflict
	}
	// 已经钉过就只核对，不一致整批拒收。
	if len(g.key) > 0 {
		// 和已钉的一样就放行。
		if bytes.Equal(g.key, pub) {
			return nil
		}
		return errKeyChanged
	}
	// 长度不对的钥不钉，按坏内容拒收这一批。
	if len(pub) != ed25519.PublicKeySize {
		return domain.ErrInvalidKey
	}
	// 有落库回调才落库，失败分冲突和库错两种。
	if g.pin != nil {
		// 钉钥落库，同钥幂等。
		err := g.pin(pub)
		// 库里已是另一把，本次连接一律拒收并退出重拨。
		if errors.Is(err, domain.ErrIntegrity) {
			// 标成冲突，之后的指令全验不过。
			g.conflict = true
			return errPinConflict
		}
		// 落库失败只记日志，本次连接照样用这把钥，下次连上再钉。
		if err != nil {
			// 记下没钉上，钥本身经证书校验仍可信。
			slog.Warn("pin wan key failed, use in memory", "factory", g.factoryID, "err", err)
		}
	}
	// 换上这把钥，之后的下行立刻按它验签。
	g.key = append([]byte(nil), pub...)
	return nil
}

// 拉回的索引先钉钥再逐条验签：全部过才交回；有一条不过记日志，ok 为假。
func (g *cmdGuard) openIndex(idx indexResp) ([]Cmd, bool, error) {
	// 先钉住或核对回包里的公钥。
	if err := g.adopt(idx.WANPublicKey); err != nil {
		// 并发钉钥冲突要交回，本次连接退出重拨。
		if errors.Is(err, errPinConflict) {
			return nil, false, err
		}
		// 公钥变了或坏了，整批拒收并记日志，人工清 wan_trust。
		slog.Warn("wan index key rejected, skip batch", "factory", g.factoryID, "err", err)
		return nil, false, nil
	}
	// 按清单长度预留，验过的原文逐条放进来。
	out := make([]Cmd, 0, len(idx.Cmds))
	// 逐条验签，有一条不过整批不用。
	for _, c := range idx.Cmds {
		// 验签并取出真正的指令。
		in, err := g.open(c)
		// 有一条验不过就整批跳过，不落半批。
		if err != nil {
			// 记下被拒的类型，便于排查伪造或换钥。
			slog.Warn("wan index signature rejected, skip batch", "factory", g.factoryID, "typ", c.Typ, "err", err)
			return nil, false, nil
		}
		// 收下验过的指令。
		out = append(out, in)
	}
	return out, true, nil
}

// openCmd 解开一条下行：enforce 时必须带签名且对本厂验过，只用签名里的原文；否则照旧用外层字段。
func openCmd(key []byte, factoryID uuid.UUID, outer Cmd, enforce bool) (Cmd, error) {
	// 不强制就照旧格式走，签名字段不看。
	if !enforce {
		return outer, nil
	}
	// 没带原文或签名，已钉的厂不认。
	if len(outer.Signed) == 0 || len(outer.Sig) == 0 {
		return Cmd{}, domain.ErrIntegrity
	}
	// 按本厂身份验签，签给别厂的指令验不过。
	if !nodekey.Verify(key, nodekey.ChannelCmdPayload(factoryID, outer.Signed), outer.Sig) {
		return Cmd{}, domain.ErrIntegrity
	}
	// 预备装签名里的真正指令，外层字段一律不用。
	var in Cmd
	// 原文解不开也当坏内容拒收。
	if err := json.Unmarshal(outer.Signed, &in); err != nil {
		return Cmd{}, domain.ErrIntegrity
	}
	// 原文里不该再套签名，清掉免得往下传。
	in.Signed, in.Sig = nil, nil
	return in, nil
}
