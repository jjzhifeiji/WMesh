// 现场申请、云端批准：新厂接入和找回初始超管密码。
package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/platform/nodekey"
	"wmesh/global/internal/service"
)

// 立管理员并登录，后面的审批都要这张票。
func onboardingAdmin(t *testing.T, h *Harness) string {
	// 失败行号落到调用它的用例。
	t.Helper()
	// 立云端超管，立不住没有人能批准。
	if err := h.WAN.BootstrapAdmin(context.Background(), "w", "wan-secret"); err != nil {
		// 不符即停：立云端超管失败。
		t.Fatal(err)
	}
	// 登录拿会话。
	tok, err := h.WAN.Login(context.Background(), "w", "wan-secret")
	// 没有票后面进不去。
	if err != nil {
		// 不符即停：登录失败。
		t.Fatal(err)
	}
	return tok
}

// 走完申请、批准和认领，交回已认领的工厂身份。
func enrollByApproval(t *testing.T, h *Harness, tok, name, login string) uuid.UUID {
	// 失败行号落到调用它的用例。
	t.Helper()
	// 准备本测上下文。
	ctx := context.Background()
	// 现场申请。
	ticket, err := h.WAN.Channel.ApplyOnboarding(ctx, name, login, "超管", "1.1.37", "10.0.0.1")
	// 申请写不下就停。
	if err != nil {
		// 不符即停：申请失败。
		t.Fatal(err)
	}
	// 云端批准。
	if _, err := h.WAN.Factories.ApproveOnboarding(ctx, tok, ticket.RequestID, ""); err != nil {
		// 不符即停：批准失败。
		t.Fatal(err)
	}
	// 申请密钥当建厂码用，换出待认领身份。
	offer, err := h.WAN.OfferEnroll(ctx, ticket.Secret)
	// 换不到身份就停。
	if err != nil {
		// 不符即停：换身份失败。
		t.Fatal(err)
	}
	// 生成厂钥。
	pub, _, err := nodekey.Generate()
	// 生成不了就停。
	if err != nil {
		// 不符即停：生成厂钥失败。
		t.Fatal(err)
	}
	// 交钥完成认领。
	if err := h.WAN.ConfirmEnroll(ctx, offer.FactoryID, pub); err != nil {
		// 不符即停：认领失败。
		t.Fatal(err)
	}
	return offer.FactoryID
}

// 批准前名录没有这家厂；批准时可改名，申请密钥能走完认领，名录显示初始超管。
func TestOnboardingEnrollApproved(t *testing.T) {
	// 准备本测上下文。
	ctx := context.Background()
	// 起云端库和服务。
	h := New(t)
	// 拿管理员票。
	tok := onboardingAdmin(t, h)
	// 现场提交申请，名字前后空白要被去掉。
	ticket, err := h.WAN.Channel.ApplyOnboarding(ctx, " 现场厂 ", "boss", "老板", "1.1.37", "10.0.0.1")
	// 申请写不下就停。
	if err != nil {
		// 不符即停：申请失败。
		t.Fatal(err)
	}
	// 核对码必须是六位。
	if len(ticket.VerifyCode) != 6 || ticket.Secret == "" {
		// 不符即停：凭据不完整。
		t.Fatalf("ticket %+v", ticket)
	}
	// 读待接入列表。
	pending, err := h.WAN.Factories.PendingOnboarding(ctx, tok)
	// 列表必须只有这一条，核对码和云端一致。
	if err != nil || len(pending.Requests) != 1 || pending.Requests[0].VerifyCode != ticket.VerifyCode || pending.Requests[0].FactoryName != "现场厂" {
		// 不符即停：待接入列表不对。
		t.Fatalf("pending %+v err %v", pending, err)
	}
	// 读名录。
	dir, err := h.WAN.Directory(ctx, tok)
	// 批准前不能进名录。
	if err != nil || len(dir.Factories) != 0 {
		// 不符即停：批准前已进名录。
		t.Fatalf("directory before approve %+v err %v", dir, err)
	}
	// 批准前申请密钥不能当建厂码。
	if _, err := h.WAN.OfferEnroll(ctx, ticket.Secret); !errors.Is(err, domain.ErrInvalidEnrollment) {
		// 不符即停：批准前就能认领。
		t.Fatalf("offer before approve: %v", err)
	}
	// 管理员批准并修正厂名。
	approved, err := h.WAN.Factories.ApproveOnboarding(ctx, tok, ticket.RequestID, "改名厂")
	// 批准必须成功，且绑上工厂。
	if err != nil || approved.FactoryID == nil {
		// 不符即停：批准失败。
		t.Fatalf("approve %+v err %v", approved, err)
	}
	// 查现场进度。
	progress, err := h.WAN.Channel.OnboardingStatus(ctx, ticket.Secret)
	// 现场要看到已批准和修正后的厂名。
	if err != nil || progress.Status != "approved" || progress.FactoryName != "改名厂" {
		// 不符即停：进度不对。
		t.Fatalf("progress %+v err %v", progress, err)
	}
	// 再批一次应被拒。
	if _, err := h.WAN.Factories.ApproveOnboarding(ctx, tok, ticket.RequestID, ""); !errors.Is(err, domain.ErrOnboardingClosed) {
		// 不符即停：重复批准没拦住。
		t.Fatalf("approve twice: %v", err)
	}
	// 申请密钥换出待认领身份。
	offer, err := h.WAN.OfferEnroll(ctx, ticket.Secret)
	// 身份必须和批准时一致。
	if err != nil || offer.FactoryID != *approved.FactoryID || offer.Name != "改名厂" || offer.SALogin != "boss" {
		// 不符即停：待认领身份不对。
		t.Fatalf("offer %+v err %v", offer, err)
	}
	// 再读名录。
	dir, err = h.WAN.Directory(ctx, tok)
	// 名录要显示初始超管。
	if err != nil || len(dir.Factories) != 1 || dir.Factories[0].SALogin != "boss" || dir.Factories[0].SADisplay != "老板" {
		// 不符即停：名录没带初始超管。
		t.Fatalf("directory %+v err %v", dir, err)
	}
}

// 拒绝后不进名录、不能再批；过期的申请不能批，现场看到失效。
func TestOnboardingRejectAndExpire(t *testing.T) {
	// 准备本测上下文。
	ctx := context.Background()
	// 起云端库和服务。
	h := New(t)
	// 拿管理员票。
	tok := onboardingAdmin(t, h)
	// 第一条申请用来拒绝。
	rejected, err := h.WAN.Channel.ApplyOnboarding(ctx, "厂甲", "a", "甲", "", "")
	// 申请写不下就停。
	if err != nil {
		// 不符即停：申请失败。
		t.Fatal(err)
	}
	// 拒绝。
	if _, err := h.WAN.Factories.RejectOnboarding(ctx, tok, rejected.RequestID); err != nil {
		// 不符即停：拒绝失败。
		t.Fatal(err)
	}
	// 现场要看到已拒绝。
	if p, err := h.WAN.Channel.OnboardingStatus(ctx, rejected.Secret); err != nil || p.Status != "rejected" {
		// 不符即停：拒绝结果不对。
		t.Fatalf("progress %+v err %v", p, err)
	}
	// 拒绝后不能再批准。
	if _, err := h.WAN.Factories.ApproveOnboarding(ctx, tok, rejected.RequestID, ""); !errors.Is(err, domain.ErrOnboardingClosed) {
		// 不符即停：拒绝后还能批准。
		t.Fatalf("approve rejected: %v", err)
	}
	// 第二条申请用来过期。
	expired, err := h.WAN.Channel.ApplyOnboarding(ctx, "厂乙", "b", "乙", "", "")
	// 申请写不下就停。
	if err != nil {
		// 不符即停：申请失败。
		t.Fatal(err)
	}
	// 直接把到期时间改到过去。
	if err := h.DB.Exec("UPDATE onboarding_requests SET expires_at = now() - interval '1 minute' WHERE id = ?", expired.RequestID).Error; err != nil {
		// 不符即停：改到期时间失败。
		t.Fatal(err)
	}
	// 过期后不能批准。
	if _, err := h.WAN.Factories.ApproveOnboarding(ctx, tok, expired.RequestID, ""); !errors.Is(err, domain.ErrOnboardingClosed) {
		// 不符即停：过期后还能批准。
		t.Fatalf("approve expired: %v", err)
	}
	// 现场看到失效，密钥错也只回失效。
	for _, code := range []string{expired.Secret, "no-such-secret"} {
		// 查进度。
		if p, err := h.WAN.Channel.OnboardingStatus(ctx, code); err != nil || p.Status != service.OnboardingExpired {
			// 不符即停：失效状态不对。
			t.Fatalf("progress %+v err %v", p, err)
		}
	}
	// 待接入列表不再有过期和已拒绝的。
	if pending, err := h.WAN.Factories.PendingOnboarding(ctx, tok); err != nil || len(pending.Requests) != 0 {
		// 不符即停：列表没过滤。
		t.Fatalf("pending %+v err %v", pending, err)
	}
}

// 找回单批准后只能由这家厂用一次；没批准、别的厂、停用的厂都不行。
func TestSAResetApproval(t *testing.T) {
	// 准备本测上下文。
	ctx := context.Background()
	// 起云端库和服务。
	h := New(t)
	// 拿管理员票。
	tok := onboardingAdmin(t, h)
	// 接入两家厂。
	fid := enrollByApproval(t, h, tok, "厂甲", "boss-a")
	// 第二家用来验证不能串厂。
	other := enrollByApproval(t, h, tok, "厂乙", "boss-b")
	// 申请找回。
	first, err := h.WAN.Channel.RequestSAReset(ctx, fid, "1.1.37", "10.0.0.2")
	// 申请写不下就停。
	if err != nil {
		// 不符即停：申请找回失败。
		t.Fatal(err)
	}
	// 再申请一次，旧单作废，只留最新一条。
	ticket, err := h.WAN.Channel.RequestSAReset(ctx, fid, "1.1.37", "10.0.0.2")
	// 申请写不下就停。
	if err != nil {
		// 不符即停：申请找回失败。
		t.Fatal(err)
	}
	// 旧单对现场已失效。
	if p, err := h.WAN.Channel.OnboardingStatus(ctx, first.Secret); err != nil || p.Status != service.OnboardingExpired {
		// 不符即停：旧单没作废。
		t.Fatalf("old progress %+v err %v", p, err)
	}
	// 读待接入列表。
	pending, err := h.WAN.Factories.PendingOnboarding(ctx, tok)
	// 云端显示的账号以名录为准。
	if err != nil || len(pending.Requests) != 1 || pending.Requests[0].Kind != "reset_sa" || pending.Requests[0].SALogin != "boss-a" || pending.Requests[0].FactoryName != "厂甲" {
		// 不符即停：找回单不对。
		t.Fatalf("pending %+v err %v", pending, err)
	}
	// 没批准不能用。
	if err := h.WAN.Channel.ConsumeSAReset(ctx, fid, ticket.Secret); !errors.Is(err, domain.ErrInvalidEnrollment) {
		// 不符即停：没批准就能用。
		t.Fatalf("consume before approve: %v", err)
	}
	// 批准。
	if _, err := h.WAN.Factories.ApproveOnboarding(ctx, tok, ticket.RequestID, ""); err != nil {
		// 不符即停：批准失败。
		t.Fatal(err)
	}
	// 别的厂不能用这张单。
	if err := h.WAN.Channel.ConsumeSAReset(ctx, other, ticket.Secret); !errors.Is(err, domain.ErrInvalidEnrollment) {
		// 不符即停：串厂没拦住。
		t.Fatalf("consume by other: %v", err)
	}
	// 本厂第一次能用。
	if err := h.WAN.Channel.ConsumeSAReset(ctx, fid, ticket.Secret); err != nil {
		// 不符即停：用单失败。
		t.Fatal(err)
	}
	// 第二次不能用。
	if err := h.WAN.Channel.ConsumeSAReset(ctx, fid, ticket.Secret); !errors.Is(err, domain.ErrInvalidEnrollment) {
		// 不符即停：同一张单用了两次。
		t.Fatalf("consume twice: %v", err)
	}
	// 用掉后现场看到失效。
	if p, err := h.WAN.Channel.OnboardingStatus(ctx, ticket.Secret); err != nil || p.Status != service.OnboardingExpired {
		// 不符即停：用掉后状态不对。
		t.Fatalf("progress %+v err %v", p, err)
	}
	// 停用这家厂。
	if _, err := h.WAN.DisableFactory(ctx, tok, fid); err != nil {
		// 不符即停：停用失败。
		t.Fatal(err)
	}
	// 停用的厂不能申请找回。
	if _, err := h.WAN.Channel.RequestSAReset(ctx, fid, "", ""); !errors.Is(err, domain.ErrFactoryDisabled) {
		// 不符即停：停用的厂还能找回。
		t.Fatalf("request while disabled: %v", err)
	}
}

// 名字不合格不写申请。
func TestOnboardingRejectsBadNames(t *testing.T) {
	// 起云端库和服务。
	h := New(t)
	// 空厂名、空登录名、空显示名都要拒绝。
	for _, c := range [][3]string{{"", "a", "甲"}, {"厂", "  ", "甲"}, {"厂", "a", ""}} {
		// 提交不合格的申请。
		if _, err := h.WAN.Channel.ApplyOnboarding(context.Background(), c[0], c[1], c[2], "", ""); !errors.Is(err, domain.ErrInvalidName) {
			// 不符即停：不合格的名字没拦住。
			t.Fatalf("apply %v: %v", c, err)
		}
	}
}
