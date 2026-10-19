// 下行指令签名钥：启动时读出或生成，沿用库里旧行，重建服务后不变。
package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/platform/nodekey"
	global "wmesh/global/internal/service"
)

// 报文外层或签名原文里出现这段文字都算漏出，签名原文编成 base64 后子串查不出来。
func payloadHas(raw []byte, needle string) bool {
	// 外层原文里直接出现就算。
	if strings.Contains(string(raw), needle) {
		return true
	}
	// 解开外层，再看签名原文。
	var c global.Cmd
	// 解不开就只看外层。
	if json.Unmarshal(raw, &c) != nil {
		return false
	}
	// 签名原文里出现也算漏出。
	return strings.Contains(string(c.Signed), needle)
}

// 库里已有旧行就沿用；重建服务再载入，公钥不变。
func TestLoadChannelKeyReusesRow(t *testing.T) {
	// 准备本测上下文。
	ctx := context.Background()
	// 起云端库和服务。
	h := New(t)
	// 没载入前公钥为空，厂端照旧格式走。
	if pub := h.WAN.ChannelPublicKey(); pub != nil {
		// 没载入却有公钥就停。
		t.Fatalf("pub before load %x", pub)
	}
	// 预置一行，模拟 9 月发过包的旧环境。
	pub, priv, err := nodekey.Generate()
	// 生不出钥就停。
	if err != nil {
		// 不符即停：生不出钥。
		t.Fatal(err)
	}
	// 先写入旧行。
	if _, err := h.WAN.Store().PutWANSigningKey(ctx, pub, priv); err != nil {
		// 不符即停：预置旧行失败。
		t.Fatal(err)
	}
	// 载入应沿用这一行。
	if err := h.WAN.LoadChannelKey(ctx); err != nil {
		// 不符即停：载入失败。
		t.Fatal(err)
	}
	// 公钥要和旧行一致。
	if !bytes.Equal(h.WAN.ChannelPublicKey(), pub) {
		// 不符即停：没沿用旧行。
		t.Fatalf("pub %x want %x", h.WAN.ChannelPublicKey(), pub)
	}
	// 同一个库重建服务，模拟进程重启。
	again := global.NewService(h.WAN.Store())
	// 重启后再载入。
	if err := again.LoadChannelKey(ctx); err != nil {
		// 不符即停：重启后载入失败。
		t.Fatal(err)
	}
	// 重启后公钥不变，已钉的厂照常验签。
	if !bytes.Equal(again.ChannelPublicKey(), pub) {
		// 不符即停：重启后换了钥。
		t.Fatalf("restart pub %x", again.ChannelPublicKey())
	}
}

// 空库首次载入生成一把并落库，重建服务读到同一把。
func TestLoadChannelKeyGenerates(t *testing.T) {
	// 准备本测上下文。
	ctx := context.Background()
	// 起一块空的云端库。
	h := New(t)
	// 首次载入要生成并写入。
	if err := h.WAN.LoadChannelKey(ctx); err != nil {
		// 不符即停：首次载入失败。
		t.Fatal(err)
	}
	// 读库里那一行。
	row, err := h.WAN.Store().WANSigningKey(ctx)
	// 没落库就停。
	if err != nil {
		// 不符即停：没落库。
		t.Fatal(err)
	}
	// 内存里的公钥要和库里一致。
	if len(row.PublicKey) != 32 || !bytes.Equal(row.PublicKey, h.WAN.ChannelPublicKey()) {
		// 不符即停：内存和库不一致。
		t.Fatalf("row %x mem %x", row.PublicKey, h.WAN.ChannelPublicKey())
	}
	// 重建服务再载入，不能再生成一把。
	again := global.NewService(h.WAN.Store())
	// 重启后再载入。
	if err := again.LoadChannelKey(ctx); err != nil {
		// 不符即停：重启后载入失败。
		t.Fatal(err)
	}
	// 重启后公钥不变。
	if !bytes.Equal(again.ChannelPublicKey(), row.PublicKey) {
		// 不符即停：重启后换了钥。
		t.Fatalf("restart pub %x", again.ChannelPublicKey())
	}
}

// 库里公钥和私钥不是一对就拒绝载入，进程不该带着验不过的钥起来。
func TestLoadChannelKeyRejectsMismatch(t *testing.T) {
	// 准备本测上下文。
	ctx := context.Background()
	// 起云端库和服务。
	h := New(t)
	// 生成两对钥，交叉写进去。
	pubA, _, err := nodekey.Generate()
	// 生不出钥就停。
	if err != nil {
		// 不符即停：生不出钥。
		t.Fatal(err)
	}
	// 第二对只取私钥。
	_, privB, err := nodekey.Generate()
	// 生不出钥就停。
	if err != nil {
		// 不符即停：生不出钥。
		t.Fatal(err)
	}
	// 写入不配对的一行。
	if _, err := h.WAN.Store().PutWANSigningKey(ctx, pubA, privB); err != nil {
		// 不符即停：预置失败。
		t.Fatal(err)
	}
	// 不配对应拒绝载入。
	if err := h.WAN.LoadChannelKey(ctx); !errors.Is(err, domain.ErrInvalidKey) {
		// 不符即停：不配对也载入了。
		t.Fatalf("mismatch %v", err)
	}
}

// 辅助函数要能在签名原文里查出子串，外层查不到的也要算漏出。
func TestPayloadHasSeesSigned(t *testing.T) {
	// 签名原文里夹着包体字样。
	raw, err := json.Marshal(global.Cmd{Typ: global.CmdSoftware, Signed: []byte(`{"body":"svc-5"}`)})
	// 编不出就停。
	if err != nil {
		// 不符即停：编不出。
		t.Fatal(err)
	}
	// 外层原文是 base64，直接查不到。
	if strings.Contains(string(raw), "svc-5") {
		// 不符即停：前提不成立。
		t.Fatal("plain contains")
	}
	// 辅助函数要查得到。
	if !payloadHas(raw, "svc-5") {
		// 不符即停：查不出签名原文里的子串。
		t.Fatal("payloadHas missed signed")
	}
}
