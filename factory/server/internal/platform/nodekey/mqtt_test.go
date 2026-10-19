package nodekey

import (
	"encoding/hex"
	"testing"

	"github.com/google/uuid"
)

// 下行指令签名原文的固定向量；WAN 和厂端测试用同一串，两边字节必须一致。
const channelCmdVector = "776d6573682d77616e2d636d642d7631111111112222333344445555555555557b22747970223a22666163746f72795f7374617465222c22737461747573223a2264697361626c6564227d"

// 固定厂身份和指令原文拼出的签名原文要等于固定向量。
func TestChannelCmdPayloadVector(t *testing.T) {
	// 固定的目标厂身份，两边测试一样。
	fid := uuid.MustParse("11111111-2222-3333-4444-555555555555")
	// 拼出签名原文，和固定向量比。
	got := hex.EncodeToString(ChannelCmdPayload(fid, []byte(`{"typ":"factory_state","status":"disabled"}`)))
	// 字节不一致就停测，两边会互相验不过。
	if got != channelCmdVector {
		// 带上实际结果，便于对照。
		t.Fatalf("payload %s", got)
	}
}

// 指令签名原文绑定目标厂，也不和连接签名原文相同。
func TestChannelCmdPayloadBindsFactory(t *testing.T) {
	// 两家不同的厂。
	a, b := uuid.New(), uuid.New()
	// 同一条指令原文。
	cmd := []byte(`{"typ":"client_bind"}`)
	// 生成一对签发钥，私钥只在本测里用。
	pub, priv, err := Generate()
	// 生不出钥就停测。
	if err != nil {
		// 带上原因停测。
		t.Fatal(err)
	}
	// 签给甲厂的指令。
	sig := Sign(priv, ChannelCmdPayload(a, cmd))
	// 甲厂验得过才对。
	if !Verify(pub, ChannelCmdPayload(a, cmd), sig) {
		// 自己的指令验不过就停测。
		t.Fatal("own factory rejected")
	}
	// 转投给乙厂必须验不过。
	if Verify(pub, ChannelCmdPayload(b, cmd), sig) {
		// 转投被接受就停测。
		t.Fatal("cmd for another factory accepted")
	}
}
