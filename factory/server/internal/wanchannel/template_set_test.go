package wanchannel

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

// template_set 指令按签名原文解出名单和完整标记，交给本厂；坏编号整条丢掉。
func TestApplyTemplateSet(t *testing.T) {
	// 两份仍在 WAN 的工程模版。
	p1, p2 := uuid.New(), uuid.New()
	// 按 WAN 约定拼一条全集指令原文。
	raw := `{"typ":"template_set","kind":"project","full":true,"templateIds":["` + p1.String() + `","` + p2.String() + `"]}`
	// 解成指令，字段名对不上就解不出名单。
	var cmd Cmd
	// 解不开就停。
	if err := json.Unmarshal([]byte(raw), &cmd); err != nil {
		// 指令原文解不开。
		t.Fatal(err)
	}
	// 记下本厂收到的种类、完整标记和名单。
	var gotKind string
	// 记下完整标记。
	var gotFull bool
	// 记下名单。
	var got []uuid.UUID
	// 记下被调了几次。
	calls := 0
	// 假本厂：记下收到了什么。
	set := func(kind string, full bool, keep []uuid.UUID) error {
		// 记一次调用。
		calls++
		// 记下种类、完整标记和名单。
		gotKind, gotFull, got = kind, full, keep
		return nil
	}
	// 记下标脏的种类。
	failed := func(kind, what string, err error) { t.Fatalf("failed %s %s %v", kind, what, err) }
	// 落这条指令。
	if err := applyCmd(nil, "", cmd, nil, failed, nil, nil, nil, nil, set, nil, nil, nil, nil); err != nil {
		// 落地报错就停。
		t.Fatal(err)
	}
	// 种类、完整标记和名单都要对上。
	if calls != 1 || gotKind != "project" || !gotFull || len(got) != 2 || got[0] != p1 || got[1] != p2 {
		// 名单没按约定交给本厂。
		t.Fatalf("calls %d kind %q full %v keep %v", calls, gotKind, gotFull, got)
	}
	// 名单里混进坏编号。
	cmd.TemplateIDs = append(cmd.TemplateIDs, "bad")
	// 坏名单不该交给本厂。
	if err := applyCmd(nil, "", cmd, nil, failed, nil, nil, nil, nil, set, nil, nil, nil, nil); err != nil || calls != 1 {
		// 按残缺名单撤会误删。
		t.Fatalf("bad ids: calls %d err %v", calls, err)
	}
	// 完整的空名单也要交给本厂，WAN 全删时靠它撤光。
	empty := Cmd{Typ: CmdTemplateSet, Kind: "project", Full: true}
	// 落空名单。
	if err := applyCmd(nil, "", empty, nil, failed, nil, nil, nil, nil, set, nil, nil, nil, nil); err != nil || calls != 2 || len(got) != 0 || !gotFull {
		// 空名单没交给本厂。
		t.Fatalf("empty: calls %d keep %v err %v", calls, got, err)
	}
}
