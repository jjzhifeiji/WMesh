// 建厂码认领、厂钥、MQTT 在线与离线。
package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/platform/nodekey"
)

// 验建厂码只能认领一次，厂钥绑死后不能改。
func TestFactoryEnrollment(t *testing.T) {
	// 准备本测上下文，没有它库和服务都开不了。
	ctx := context.Background()
	// 起云端库和服务，起不来整段验收作废。
	h := New(t)
	// 立云端超管失败就停，立不住后面没有人能登录。
	if err := h.WAN.BootstrapAdmin(ctx, "w", "wan-secret"); err != nil {
		// 不符即停：立云端超管失败。
		t.Fatal(err)
	}
	// 登录拿会话，没有票后面接口都进不去。
	tok, err := h.WAN.Login(ctx, "w", "wan-secret")
	// 登录拿会话失败就停，没有票后面接口都进不去。
	if err != nil {
		// 不符即停：登录拿会话失败。
		t.Fatal(err)
	}
	// 登记工厂「厂A」，建不成后面没有厂可授权。
	created, err := h.WAN.CreateFactory(ctx, tok, "厂A", "sa-a", "超管A")
	// 登记工厂「厂A」失败就停，建不成后面没有厂可授权。
	if err != nil {
		// 不符即停：登记工厂「厂A」失败。
		t.Fatal(err)
	}
	// 建厂结果结果是空的就停，这一步不能算通过。
	if created.EnrollmentToken == "" || created.SuperAdminID.String() == "" {
		// 不符即停：建厂结果结果是空的。
		t.Fatalf("missing enrollment: %+v", created)
	}
	// 用建厂码认领应被拒为建厂码无效，放行或错类都算没拦住。
	if _, err := h.WAN.OfferEnroll(ctx, "not-the-code"); !errors.Is(err, domain.ErrInvalidEnrollment) {
		// 不符即停：用建厂码认领应被拒为建厂码无效。
		t.Fatalf("bad code: %v", err)
	}
	// 用建厂码认领，认领失败厂就没接上云。
	offer, err := h.WAN.OfferEnroll(ctx, created.EnrollmentToken)
	// 用建厂码认领失败就停，认领失败厂就没接上云。
	if err != nil {
		// 不符即停：用建厂码认领失败。
		t.Fatal(err)
	}
	// 认领信息多项不符预期就停，说明没有按规则落下。
	if offer.FactoryID != created.Factory.ID || offer.SAPersonID != created.SuperAdminID || offer.SALogin != "sa-a" {
		// 不符即停：认领信息多项不符预期。
		t.Fatalf("offer mismatch: %+v", offer)
	}
	// 生成一对密钥，认领和改绑都用这把公钥。
	pub, _, err := nodekey.Generate()
	// 生成一对密钥失败就停，没有公钥认领就绑不成。
	if err != nil {
		// 不符即停：生成一对密钥失败。
		t.Fatal(err)
	}
	// 确认厂钥失败就停，厂钥确认失败就不能绑死。
	if err := h.WAN.ConfirmEnroll(ctx, offer.FactoryID, pub); err != nil {
		// 不符即停：确认厂钥失败。
		t.Fatal(err)
	}
	// 用建厂码认领应被拒为建厂码无效，放行或错类都算没拦住。
	if _, err := h.WAN.OfferEnroll(ctx, created.EnrollmentToken); !errors.Is(err, domain.ErrInvalidEnrollment) {
		// 不符即停：用建厂码认领应被拒为建厂码无效。
		t.Fatalf("code reused: %v", err)
	}
	// 确认厂钥失败就停，厂钥确认失败就不能绑死。
	if err := h.WAN.ConfirmEnroll(ctx, offer.FactoryID, pub); err != nil {
		// 不符即停：确认厂钥失败。
		t.Fatalf("idempotent confirm: %v", err)
	}
	// 生成一对密钥，认领和改绑都用这把公钥。
	other, _, err := nodekey.Generate()
	// 生成一对密钥失败就停，没有公钥认领就绑不成。
	if err != nil {
		// 不符即停：生成一对密钥失败。
		t.Fatal(err)
	}
	// 这里两段正文不该相同就停，这一步不能算通过。
	if bytes.Equal(pub, other) {
		// 不符即停：这里两段正文不该相同。
		t.Fatal("keys collided")
	}
	// 确认厂钥应被拒为厂钥已绑定，放行或错类都算没拦住。
	if err := h.WAN.ConfirmEnroll(ctx, offer.FactoryID, other); !errors.Is(err, domain.ErrFactoryKeyExists) {
		// 不符即停：确认厂钥应被拒为厂钥已绑定。
		t.Fatalf("other key: %v", err)
	}
}

// 验通道上线回写版本，下线后状态清零。
func TestChannelPresence(t *testing.T) {
	// 准备本测上下文，没有它库和服务都开不了。
	ctx := context.Background()
	// 起云端库和服务，起不来整段验收作废。
	h := New(t)
	// 立云端超管失败就停，立不住后面没有人能登录。
	if err := h.WAN.BootstrapAdmin(ctx, "w", "wan-secret"); err != nil {
		// 不符即停：立云端超管失败。
		t.Fatal(err)
	}
	// 登录拿会话，没有票后面接口都进不去。
	tok, err := h.WAN.Login(ctx, "w", "wan-secret")
	// 登录拿会话失败就停，没有票后面接口都进不去。
	if err != nil {
		// 不符即停：登录拿会话失败。
		t.Fatal(err)
	}
	// 登记工厂「厂A」，建不成后面没有厂可授权。
	created, err := h.WAN.CreateFactory(ctx, tok, "厂A", "sa-a", "超管A")
	// 登记工厂「厂A」失败就停，建不成后面没有厂可授权。
	if err != nil {
		// 不符即停：登记工厂「厂A」失败。
		t.Fatal(err)
	}
	// 记下这家厂，停用和上报都指它。
	fid := created.Factory.ID
	// 核对该厂公钥应被拒为未登录，放行或错类都算没拦住。
	if err := h.WAN.RequireFactoryKey(ctx, fid); !errors.Is(err, domain.ErrUnauthorized) {
		// 不符即停：核对该厂公钥应被拒为未登录。
		t.Fatalf("before enroll: %v", err)
	}
	// 生成一对密钥，认领和改绑都用这把公钥。
	pub, _, err := nodekey.Generate()
	// 生成一对密钥失败就停，没有公钥认领就绑不成。
	if err != nil {
		// 不符即停：生成一对密钥失败。
		t.Fatal(err)
	}
	// 确认厂钥失败就停，厂钥确认失败就不能绑死。
	if err := h.WAN.ConfirmEnroll(ctx, fid, pub); err != nil {
		// 不符即停：确认厂钥失败。
		t.Fatal(err)
	}
	// 核对该厂公钥失败就停，公钥核对失败通道状态不明。
	if err := h.WAN.RequireFactoryKey(ctx, fid); err != nil {
		// 不符即停：核对该厂公钥失败。
		t.Fatal(err)
	}
	// 标通道在线失败就停，标在线失败名录不会变。
	if err := h.WAN.MarkChannelOnline(ctx, fid); err != nil {
		// 不符即停：标通道在线失败。
		t.Fatal(err)
	}
	// 把结果打成文本，以便检查有没有人员字段。
	raw, err := json.Marshal(map[string]any{
		"typ": "presence", "webVersion": 2, "webVersionName": "1.1.0",
		"serviceVersion": 3, "serviceVersionName": "1.2.0",
	})
	// 导出字段表失败就停，导不出就没法套正文。
	if err != nil {
		// 不符即停：导出字段表失败。
		t.Fatal(err)
	}
	// 送上厂端上线报文，送不进在线状态不会变。
	h.WAN.Channel.HandleFactoryUp(ctx, fid, raw)
	// 读工厂名录，名录读不到就无法对厂。
	dir, err := h.WAN.Directory(ctx, tok)
	// 读工厂名录失败就停，名录读不到就无法对厂。
	if err != nil {
		// 不符即停：读工厂名录失败。
		t.Fatal(err)
	}
	// 名录多项不符预期就停，说明没有按规则落下。
	if len(dir.Factories) != 1 || !dir.Factories[0].ChannelOnline || dir.Factories[0].ChannelConnectedAt == nil {
		// 不符即停：名录多项不符预期。
		t.Fatalf("online: %+v", dir.Factories)
	}
	// 名录版本回写不对就停，这一步不能算通过。
	if dir.Factories[0].WebVersion != 2 || dir.Factories[0].WebVersionName != "1.1.0" || dir.Factories[0].ServiceVersion != 3 || dir.Factories[0].ServiceVersionName != "1.2.0" {
		// 不符即停：名录版本回写不对。
		t.Fatalf("release: %+v", dir.Factories[0])
	}
	// 刷新通道心跳失败就停，没有结果不能继续验。
	if err := h.WAN.TouchChannel(ctx, fid); err != nil {
		// 不符即停：刷新通道心跳失败。
		t.Fatal(err)
	}
	// 标通道离线失败就停，标离线失败版本清不掉。
	if err := h.WAN.MarkChannelOffline(ctx, fid); err != nil {
		// 不符即停：标通道离线失败。
		t.Fatal(err)
	}
	// 读工厂名录，名录读不到就无法对厂。
	dir, err = h.WAN.Directory(ctx, tok)
	// 读工厂名录失败就停，名录读不到就无法对厂。
	if err != nil {
		// 不符即停：读工厂名录失败。
		t.Fatal(err)
	}
	// 名录通道不该仍在线或不该是空的就停，不能当通过。
	if dir.Factories[0].ChannelOnline || dir.Factories[0].ChannelDisconnectedAt == nil {
		// 不符即停：名录通道不该仍在线或不该是空的。
		t.Fatalf("offline: %+v", dir.Factories[0])
	}
	// 名录版本回写不对就停，这一步不能算通过。
	if dir.Factories[0].WebVersion != 0 || dir.Factories[0].ServiceVersion != 0 {
		// 不符即停：名录版本回写不对。
		t.Fatalf("offline still showing release: %+v", dir.Factories[0])
	}
	// 标通道在线失败就停，标在线失败名录不会变。
	if err := h.WAN.MarkChannelOnline(ctx, fid); err != nil {
		// 不符即停：标通道在线失败。
		t.Fatal(err)
	}
	// 清通道在线状态失败就停，没有结果不能继续验。
	if err := h.WAN.ResetChannelPresence(ctx); err != nil {
		// 不符即停：清通道在线状态失败。
		t.Fatal(err)
	}
	// 读工厂名录，名录读不到就无法对厂。
	dir, err = h.WAN.Directory(ctx, tok)
	// 读工厂名录失败就停，名录读不到就无法对厂。
	if err != nil {
		// 不符即停：读工厂名录失败。
		t.Fatal(err)
	}
	// 名录通道不该仍在线就停，这一步不能算通过。
	if dir.Factories[0].ChannelOnline {
		// 不符即停：名录通道不该仍在线。
		t.Fatalf("reset left online: %+v", dir.Factories[0])
	}
}
