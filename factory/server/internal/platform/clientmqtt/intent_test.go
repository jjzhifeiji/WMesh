package clientmqtt

import (
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
