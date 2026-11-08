// 第十一次补修和示教器约定的接口形状。
package httpapi_test

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"wmesh/factory/internal/httpapi"
	"wmesh/factory/internal/hub"
	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/platform/testpg"
)

// 报文形状和示教器约定对齐：焊次的 projectRevision、processes，改名和改正文拒空名，厂级工程保存回改写后的正文。
func TestRound11Contracts(t *testing.T) {
	// 连上测试库并建枢纽，连不上就停测。
	h, err := hub.New(testpg.AdminDSN())
	// 测试库连不上则停测。
	if err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("hub: %v", err)
	}
	// 为本测单独分配工厂，避免和别的库串。
	fid := id.New()
	// 测完删掉临时工厂并关掉枢纽。
	t.Cleanup(func() {
		// 删掉这个测试工厂，避免库一直留着。
		_ = h.Drop(fid)
		// 关掉枢纽，释放测试占用的连接。
		h.Close()
	})
	// 拉起临时服务，测完由清理关掉。
	srv := httptest.NewServer(httpapi.New(h, "").Router())
	// 测完关闭临时服务，把端口让出来。
	t.Cleanup(srv.Close)
	// 拼出本厂前缀，后面的路径都接在后面。
	base := "/v1/factories/" + fid.String()
	// 按约定身份建厂，初始超管当场有效。
	svc, saID := enrollSA(t, h, fid)
	// 发过本厂短码才建得了工艺。
	if err := svc.Store().PutFactoryShortCode(context.Background(), "F01"); err != nil {
		// 与预期不符就停测。
		t.Fatalf("short code: %v", err)
	}
	// 登录网页并取回会话。
	tok := loginWeb(t, srv, base, "sa", "secret")

	// 摘要按标准 Base64 的 SHA-256 发，和示教器一样。
	sum := base64.StdEncoding.EncodeToString(digest.Sum([]byte("工艺正文")))
	// 工艺身份，厂端不认识也要收。
	pid := id.New().String()
	// 示教器发的焊次报文。
	fact := `{"facts":[{"id":"` + id.New().String() + `","creatorId":"` + saID.String() + `","orgPath":[],"projectRevision":5,"processes":[{"processId":"` + pid + `","revision":3,"digest":"` + sum + `"}],"lengthMm":10,"durationSec":1,"occurredAt":"2026-09-16T01:00:00Z"}]}`
	// 上报焊次。
	code, body := do(t, srv, "POST", base+"/pad/weld-facts", tok, fact)
	// 新字段不能被当成多余字段拒掉，这条也要收下。
	if code != http.StatusOK || !strings.Contains(body, `"rejected":[]`) {
		// 与预期不符就停测。
		t.Fatalf("flush %d %s", code, body)
	}
	// 下钻读回。
	code, body = do(t, srv, "GET", base+"/weld-runs", tok, "")
	// 工程修订、工艺身份、修订、摘要都在。
	if code != http.StatusOK || !strings.Contains(body, `"projectRevision":5`) || !strings.Contains(body, pid) || !strings.Contains(body, `"revision":3`) || !strings.Contains(body, sum) {
		// 与预期不符就停测。
		t.Fatalf("runs %d %s", code, body)
	}

	// 建个人工艺。
	code, body = do(t, srv, "POST", base+"/assets", tok, `{"kind":"process","level":"personal","name":"个人焊","content":"{\"name\":\"个人焊\"}"}`)
	// 建失败就停测。
	if code != http.StatusCreated {
		// 与预期不符就停测。
		t.Fatalf("create process %d %s", code, body)
	}
	// 记下身份。
	procID := gjson(t, body, "id")
	// 发布。
	code, body = do(t, srv, "POST", base+"/assets/"+procID+"/publish", tok, `{"expected":`+gjson(t, body, "revision")+`}`)
	// 发布失败就停测。
	if code != http.StatusOK {
		// 与预期不符就停测。
		t.Fatalf("publish %d %s", code, body)
	}
	// 记下发布后的修订。
	rev := gjson(t, body, "revision")
	// 改正文 name 留空回 400 invalid name。
	code, body = do(t, srv, "POST", base+"/assets/"+procID+"/content", tok, `{"expected":`+rev+`,"content":"{\"name\":\"  \"}"}`)
	// 不是 400 就是没拦住。
	if code != http.StatusBadRequest || !strings.Contains(body, "invalid name") {
		// 与预期不符就停测。
		t.Fatalf("blank body %d %s", code, body)
	}
	// 示教器盖正文同样回 400。
	code, body = do(t, srv, "POST", base+"/pad/assets/"+procID+"/content", tok, `{"content":"{\"name\":\"\"}"}`)
	// 不是 400 就是没拦住。
	if code != http.StatusBadRequest || !strings.Contains(body, "invalid name") {
		// 与预期不符就停测。
		t.Fatalf("blank pad body %d %s", code, body)
	}
	// 改名留空回 400 invalid name。
	code, body = do(t, srv, "POST", base+"/assets/"+procID+"/rename", tok, `{"expected":`+rev+`,"name":"   "}`)
	// 不是 400 就是没拦住。
	if code != http.StatusBadRequest || !strings.Contains(body, "invalid name") {
		// 与预期不符就停测。
		t.Fatalf("blank rename %d %s", code, body)
	}
	// 约定的改名请求：expected 加 name，回 Asset。
	code, body = do(t, srv, "POST", base+"/assets/"+procID+"/rename", tok, `{"expected":`+rev+`,"name":"新名"}`)
	// 名字和修订都要回。
	if code != http.StatusOK || gjson(t, body, "name") != "新名" || gjson(t, body, "revision") == rev {
		// 与预期不符就停测。
		t.Fatalf("rename %d %s", code, body)
	}

	// 厂级工程引用这条个人工艺。
	code, body = do(t, srv, "POST", base+"/assets", tok, `{"kind":"project","level":"factory","name":"厂工程","content":"[{\"templateId\":\"11111111-1111-4111-8111-111111111111\",\"processId\":\"`+procID+`\"}]"}`)
	// 建成，回包带新正文和新工艺，正文里不再有个人工艺身份。
	if code != http.StatusCreated || !strings.Contains(body, `"personalCopies"`) || !strings.Contains(body, `\"processId\"`) || strings.Contains(gjson(t, body, "content"), procID) {
		// 与预期不符就停测。
		t.Fatalf("create project %d %s", code, body)
	}
	// 没引用个人工艺的保存，回包不多这两项。
	code, body = do(t, srv, "POST", base+"/assets", tok, `{"kind":"project","level":"factory","name":"空工程","content":"[]"}`)
	// 回包和以前一样。
	if code != http.StatusCreated || strings.Contains(body, `"personalCopies"`) || strings.Contains(body, `"content"`) {
		// 与预期不符就停测。
		t.Fatalf("create empty project %d %s", code, body)
	}
}
