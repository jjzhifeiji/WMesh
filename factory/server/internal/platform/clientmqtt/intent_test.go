package clientmqtt

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/platform/nodekey"
)

// 签过的小信封能验过，夹带正文要被看出来。
func TestIntentSignVerifyAndNoBody(t *testing.T) {
	// 生成一对节点签发密钥。
	pub, priv, err := nodekey.Generate()
	// 没能生成一对节点签发密钥就停住本用例。
	if err != nil {
		// 没能生成一对节点签发密钥就停住本用例。
		t.Fatal(err)
	}
	// 新建这一份后面要用的对象。
	fid, cid := id.New(), id.New()
	// 新建这一份后面要用的对象。
	aid := id.New()
	// 收成普通文本再拿去比较或拼接。
	in := Intent{Typ: TypClosure, Revision: 3, AssetID: aid.String(), Digest: []byte{1, 2, 3, 4}}
	// 用私钥签这段原文。
	raw, err := Sign(priv, fid, cid, in)
	// 没能用私钥签这段原文就停住本用例。
	if err != nil {
		// 没能用私钥签这段原文就停住本用例。
		t.Fatal(err)
	}
	// 结果和预期不符就进入失败。
	if HasBody(raw) || strings.Contains(string(raw), `"content"`) || strings.Contains(string(raw), `"members"`) {
		// 连接口令里夹了正文就停住。
		t.Fatalf("body in mqtt: %s", raw)
	}
	// 用公钥核对签名是否相符。
	got, err := Verify(pub, fid, cid, raw)
	// 出错或结果对不上就停住本用例。
	if err != nil || got.Revision != 3 || got.AssetID != aid.String() {
		// 验签结果不对就停住。
		t.Fatalf("verify %+v %v", got, err)
	}
	// 没有出错就按成功返回，不用再补救。
	if _, err := Verify(pub, fid, id.New(), raw); err == nil {
		// 别的机器竟被接受就停住。
		t.Fatal("wrong client accepted")
	}
	// 组一条控制面小信封，用来签和验。
	pol := Intent{Typ: TypPolicy, Revision: 2, MaxCachedProjects: 3, CacheScope: "all"}
	// 用私钥签这段原文。
	praw, err := Sign(priv, fid, cid, pol)
	// 没能用私钥签这段原文就停住本用例。
	if err != nil {
		// 没能用私钥签这段原文就停住本用例。
		t.Fatal(err)
	}
	// 结果和预期不符就进入失败。
	if HasBody(praw) {
		// 检查控制面有没有夹带正文和预期不符就停住。
		t.Fatalf("policy body: %s", praw)
	}
	// 没能用公钥核对签名是否相符就停住本用例。
	if _, err := Verify(pub, fid, cid, praw); err != nil {
		// 没能用公钥核对签名是否相符就停住本用例。
		t.Fatal(err)
	}
	// 把结构收成字节，再交给后面。
	bad, _ := json.Marshal(map[string]any{"typ": TypClosure, "content": "WM2xxxx"})
	// 结果和预期不符就进入失败。
	if !HasBody(bad) {
		// 这段本该算正文却没算上。
		t.Fatal("content must count as body")
	}
}

// 签名的编码碰巧含 WM2 时，签好的下行不能被当成夹带正文。
func TestHasBodyIgnoresWM2InSig(t *testing.T) {
	// 生成一对节点签发密钥。
	pub, priv, err := nodekey.Generate()
	// 没能生成一对节点签发密钥就停住本用例。
	if err != nil {
		// 没能生成一对节点签发密钥就停住本用例。
		t.Fatal(err)
	}
	// 新建工厂、本机和资产身份。
	fid, cid, aid := id.New(), id.New(), id.New()
	// 准备放下签名编码含 WM2 的那条下行。
	var raw []byte
	// 逐个修订去签，约三千次里能碰上一次含 WM2 的签名。
	for rev := int64(1); rev <= 200000 && raw == nil; rev++ {
		// 组一条闭包就绪小信封，只换修订号。
		in := Intent{Typ: TypClosure, Revision: rev, AssetID: aid.String(), Digest: []byte{1, 2, 3, 4}}
		// 先单独算签名，好看它的编码里有没有 WM2。
		sig := nodekey.Sign(priv, Message(fid, cid, in))
		// 签名编码里没有 WM2 就换下一个修订。
		if !strings.Contains(base64.StdEncoding.EncodeToString(sig), "WM2") {
			continue
		}
		// 签出整条下行，留给后面核对。
		raw, err = Sign(priv, fid, cid, in)
		// 没能签出这条下行就停住本用例。
		if err != nil {
			// 没能签出这条下行就停住本用例。
			t.Fatal(err)
		}
	}
	// 一直没碰上或报文里没有 WM2，样本就不成立。
	if raw == nil || !strings.Contains(string(raw), "WM2") {
		// 找不到样本就停住本用例。
		t.Fatalf("no signature with WM2: %s", raw)
	}
	// 签名编码含 WM2 仍被当成夹带就进入失败。
	if HasBody(raw) {
		// 这条下行会被误丢，停住本用例。
		t.Fatalf("sig with WM2 counted as body: %s", raw)
	}
	// 这条下行照样要能验过签。
	if _, err := Verify(pub, fid, cid, raw); err != nil {
		// 验签失败就停住本用例。
		t.Fatal(err)
	}
}

// 在场上报的网络名或臂号带 WM2，不能当成夹带正文。
func TestHasBodyIgnoresWM2InPresence(t *testing.T) {
	// 两条在场上报，分别在网络名和臂号里带 WM2。
	cases := []map[string]any{
		{"typ": TypPresence, "version": 52, "networkName": "WM2-Line"},
		{"typ": TypPresence, "version": 52, "deviceSerial": "ARM-WM2-01"},
	}
	// 逐条编成报文再判断。
	for _, c := range cases {
		// 把在场上报编成报文。
		raw, _ := json.Marshal(c)
		// 在场上报被当成夹带就进入失败。
		if HasBody(raw) {
			// 这条在场会被忽略，停住本用例。
			t.Fatalf("presence counted as body: %s", raw)
		}
	}
}

// 正文、成员、包装三个键和解不开的报文仍算夹带。
func TestHasBodyKeysStillCount(t *testing.T) {
	// 三个键各编一条报文，值里不带 WM2。
	for _, key := range []string{"content", "members", "wrap"} {
		// 把带这个键的小信封编成报文。
		raw, _ := json.Marshal(map[string]any{"typ": TypClosure, key: "x"})
		// 带了这个键却没算夹带就进入失败。
		if !HasBody(raw) {
			// 该拦的正文漏过去了，停住本用例。
			t.Fatalf("%s must count as body", key)
		}
	}
	// 解不开的报文没算夹带就进入失败。
	if !HasBody([]byte("not json")) {
		// 解不开的报文漏过去了，停住本用例。
		t.Fatal("bad json must count as body")
	}
}
