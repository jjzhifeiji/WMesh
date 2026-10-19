// HTTP 适配：示教器下载 APK 边读边写，慢读也不被读时限掐断。
package httpapi_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"wmesh/factory/internal/httpapi"
	"wmesh/factory/internal/hub"
	"wmesh/factory/internal/platform/blob"
	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/platform/testpg"
	"wmesh/factory/internal/service"
)

// slowBlobs 包住内存存根：打开的流读得慢，请求取消就报错。
type slowBlobs struct {
	*blob.Memory // 真正存字节的内存存根
}

// Open 打开内存流，包上慢读和取消检查。
func (b slowBlobs) Open(ctx context.Context, key string) (blob.Object, error) {
	// 交给内存存根真正打开。
	obj, err := b.Memory.Open(ctx, key)
	// 没有该键就原样交回找不到。
	if err != nil {
		return nil, err
	}
	// 记下打开时的上下文，读时跟着它取消。
	return &slowObject{Object: obj, ctx: ctx}, nil
}

// slowObject 每次只读一小段并停一会儿，请求被取消就报错。
type slowObject struct {
	blob.Object                 // 真正的内存流，定位和关闭都交给它
	ctx         context.Context // 打开时的请求上下文，取消了就不再读
}

// Read 请求还在才读，每次最多一小段，读前先停一会儿。
func (o *slowObject) Read(p []byte) (int, error) {
	// 请求已被取消就报错，模拟对象存储读流被掐断。
	if err := o.ctx.Err(); err != nil {
		return 0, err
	}
	// 故意慢读，让整次下载远超读时限。
	time.Sleep(25 * time.Millisecond)
	// 每次最多给一小段，拉长下载时间。
	if len(p) > 512 {
		// 截短这次要读的长度。
		p = p[:512]
	}
	// 从真正的内存流读一段。
	return o.Object.Read(p)
}

// 服务器读时限很短、存储读得很慢时，重启后首次示教器下载要先核摘要，仍能完整下完，续传也照常。
func TestPadClientSoftwareSlowStream(t *testing.T) {
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
	// 同一套接口，建厂和登录走不限时的服务。
	api := httpapi.New(h, "boot-secret", "")
	// 拉起不限读时的服务，建厂开库慢也不被掐。
	srv := httptest.NewServer(api.Router())
	// 测完关闭临时服务，把端口让出来。
	t.Cleanup(srv.Close)
	// 再起一份读时限很短的服务，专门下载。
	slow := httptest.NewUnstartedServer(api.Router())
	// 读时限远短于整次下载，没清掉就会被掐断。
	slow.Config.ReadTimeout = 300 * time.Millisecond
	// 按设好的读时限开始监听。
	slow.Start()
	// 测完关闭短时限服务，把端口让出来。
	t.Cleanup(slow.Close)
	// 拼出本厂前缀，后面的路径都接在后面。
	base := "/v1/factories/" + fid.String()
	// 用共享密码建厂，激活码只出现在回包。
	code, body := do(t, srv, "POST", "/internal/bootstrap", "boot-secret", `{"factoryId":"`+fid.String()+`","saLogin":"sa","saDisplay":"超管"}`)
	// 不是已创建则本测失败。
	if code != http.StatusCreated {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("bootstrap %d %s", code, body)
	}
	// 按工厂打开已有库，没有库就当未初始化。
	svc, err := h.Service(context.Background(), fid)
	// 这一步失败则停测，避免在坏结果上继续。
	if err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("service: %v", err)
	}
	// 租约写不进去则停测，后面读正文会失败。
	if err := svc.Store().GrantLocalLease(context.Background()); err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("lease: %v", err)
	}
	// 取出激活码，启用账号时要用。
	act := gjson(t, body, "activationToken")
	// 用激活码启用账号，错码应当被拒绝。
	code, body = do(t, srv, "POST", base+"/activate", "", `{"loginName":"sa","activationToken":"`+act+`","password":"secret"}`)
	// 不是无内容则本测失败。
	if code != http.StatusNoContent {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("activate %d %s", code, body)
	}
	// 登录并取得会话，口令错误应当被拒绝。
	code, body = do(t, srv, "POST", base+"/pad/login", "", `{"loginName":"sa","password":"secret"}`)
	// 不是成功则本测失败。
	if code != http.StatusOK {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("pad login %d %s", code, body)
	}
	// 取出会话令牌，后面的请求要带上。
	padTok := gjson(t, body, "token")
	// 包字节放在同一份内存里，换存储后接着用。
	mem := blob.NewMemory()
	// 换上慢读存储，之后下载都边读边写。
	svc.SetBlobs(slowBlobs{Memory: mem})
	// 准备一段够长的包体，慢读下要一秒多。
	apk := bytes.Repeat([]byte("apk-slow-"), 2048)
	// 先放入一份客户端包，再走下载。
	if err := svc.Updates.IngestSoftware(context.Background(), service.SoftwareOffer{
		// 填上种类、版本和摘要，供后面下载核对。
		Kind: service.SoftwareClientAPK, Version: 51, VersionName: "6.1.0", Digest: digest.Sum(apk), Body: apk,
	}); err != nil {
		t.Fatalf("ingest: %v", err)
	}
	// 同一份字节再挂一次，核对记录清空，相当于重启后首次下载。
	svc.SetBlobs(slowBlobs{Memory: mem})
	// 从短时限服务整包下载，先整包核摘要，超过读时限也要下完。
	code, body = do(t, slow, "GET", base+"/pad/software/client/51", padTok, "")
	// 不是成功，或正文不完整则失败。
	if code != http.StatusOK || body != string(apk) {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("slow file %d %d bytes", code, len(body))
	}
	// 从短时限服务续传后半，也要完整下完。
	code, body = doRange(t, slow, base+"/pad/software/client/51", padTok, "bytes=4-")
	// 不是分段，或正文与预期不符则失败。
	if code != http.StatusPartialContent || body != string(apk[4:]) {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("slow range %d %d bytes", code, len(body))
	}
}
