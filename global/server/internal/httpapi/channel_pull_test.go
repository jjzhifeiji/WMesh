package httpapi_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"wmesh/global/internal/httpapi"
	"wmesh/global/internal/platform/audit"
	"wmesh/global/internal/platform/blob"
	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/platform/nodekey"
	"wmesh/global/internal/service"
	"wmesh/global/internal/store"
)

// 带原始字节头能拿到整包和摘要头，续传回 206 不多记允许，旧厂端仍回 JSON。
func TestPullSoftwareRawStream(t *testing.T) {
	// 准备已认领厂和管理员会话。
	svc, tok, fid, priv := enrolledFactory(t)
	// 包字节随便拼一段，够长才看得出续传。
	body := bytes.Repeat([]byte("wmesh-pack-"), 50)
	// 先发布一版厂包，厂才拉得到。
	if _, err := svc.PublishSoftware(context.Background(), tok, service.SoftwareFactoryService, "1.0.0", 1, body); err != nil {
		// 发布失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	// 起临时云端，只在本测里访问。
	srv := httptest.NewServer(httpapi.New(svc, "test").Router())
	// 测完关掉临时服务，避免端口占着。
	t.Cleanup(srv.Close)
	// 要原始字节，不带区间，应整包回来。
	res := softwareGet(t, srv.URL, fid, priv, true, "")
	// 读完整包，读失败就停测。
	got := readAll(t, res)
	// 算出包的摘要，和响应头比。
	sum := sha256.Sum256(body)
	// 状态、类型、正文或摘要头对不上就停测。
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "application/octet-stream" || !bytes.Equal(got, body) {
		// 整包没拿对，带上现场停测。
		t.Fatalf("raw %d %q %d", res.StatusCode, res.Header.Get("Content-Type"), len(got))
	}
	// 摘要头和版本头都得对上。
	if res.Header.Get("X-WMesh-Digest") != hex.EncodeToString(sum[:]) || res.Header.Get("X-WMesh-Version") != "1" {
		// 响应头不对，带上现场停测。
		t.Fatalf("headers %v", res.Header)
	}
	// 记下续传前的允许审计条数。
	allows := countPullAllows(t, svc)
	// 从第四字节续传，应回剩下的部分。
	res = softwareGet(t, srv.URL, fid, priv, true, "bytes=4-")
	// 读完续传正文，读失败就停测。
	got = readAll(t, res)
	// 不是 206 或正文不是剩下那段就停测。
	if res.StatusCode != http.StatusPartialContent || !bytes.Equal(got, body[4:]) {
		// 续传没拿对，带上现场停测。
		t.Fatalf("range %d %d", res.StatusCode, len(got))
	}
	// 区间头要写明起止和总长。
	if want := "bytes 4-" + strconv.Itoa(len(body)-1) + "/" + strconv.Itoa(len(body)); res.Header.Get("Content-Range") != want {
		// 区间头不对，带上现场停测。
		t.Fatalf("content-range %q", res.Header.Get("Content-Range"))
	}
	// 续传不该多记允许审计。
	if n := countPullAllows(t, svc); n != allows {
		// 续传又记了允许，带上条数停测。
		t.Fatalf("resume audited %d -> %d", allows, n)
	}
	// 不带原始字节头，按旧厂端回 JSON。
	res = softwareGet(t, srv.URL, fid, priv, false, "")
	// 读完 JSON 正文，读失败就停测。
	raw := readAll(t, res)
	// 只收正文和摘要，用来核对旧格式。
	var offer struct {
		Body   []byte `json:"body"`   // 包字节，旧格式是 base64
		Digest []byte `json:"digest"` // 包摘要
	}
	// 状态不对或解不开就停测。
	if res.StatusCode != http.StatusOK || json.Unmarshal(raw, &offer) != nil {
		// 旧格式没拿到，带上现场停测。
		t.Fatalf("json %d %s", res.StatusCode, raw)
	}
	// 正文或摘要对不上就停测。
	if !bytes.Equal(offer.Body, body) || !bytes.Equal(offer.Digest, sum[:]) {
		// 旧格式内容不对就停测。
		t.Fatal("json offer mismatch")
	}
}

// 全局读时限很短、存储读得很慢时，流式拉包仍要完整下完，不被读时限掐断。
func TestPullSoftwareOutlivesReadTimeout(t *testing.T) {
	// 准备已认领厂和管理员会话。
	svc, tok, fid, priv := enrolledFactory(t)
	// 换上慢读存根，每次只给几个字节。
	svc.SetBlobs(&slowBlobs{Memory: blob.NewMemory()})
	// 四十字节，按每次四字节慢读要十次。
	body := bytes.Repeat([]byte("slow"), 10)
	// 先发布一版厂包，厂才拉得到。
	if _, err := svc.PublishSoftware(context.Background(), tok, service.SoftwareFactoryService, "1.0.0", 1, body); err != nil {
		// 发布失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	// 先不起服务，改短读时限再起。
	srv := httptest.NewUnstartedServer(httpapi.New(svc, "test").Router())
	// 读时限远短于整包慢读的时长。
	srv.Config.ReadTimeout = 150 * time.Millisecond
	// 改完时限再起服务。
	srv.Start()
	// 测完关掉临时服务，避免端口占着。
	t.Cleanup(srv.Close)
	// 要原始字节，应整包回来。
	res := softwareGet(t, srv.URL, fid, priv, true, "")
	// 读完整包，中途被掐断会读出错。
	got, err := io.ReadAll(res.Body)
	// 读完就关上响应。
	_ = res.Body.Close()
	// 读出错、状态不对或没读全就停测。
	if err != nil || res.StatusCode != http.StatusOK || !bytes.Equal(got, body) {
		// 被读时限掐断了，带上现场停测。
		t.Fatalf("slow pull %d %d %v", res.StatusCode, len(got), err)
	}
}

// 重启后首次拉包要先整包核摘要；读时限很短、存储读得很慢时也要完整下完。
func TestPullSoftwareFirstVerifyOutlivesReadTimeout(t *testing.T) {
	// 准备已认领厂和管理员会话。
	svc, tok, fid, priv := enrolledFactory(t)
	// 包字节放在同一份内存里，重启后接着用。
	mem := blob.NewMemory()
	// 发布走快存储，免得发布本身就慢。
	svc.SetBlobs(mem)
	// 四十字节，核摘要和下载各要慢读十来次。
	body := bytes.Repeat([]byte("cold"), 10)
	// 先发布一版厂包，厂才拉得到。
	if _, err := svc.PublishSoftware(context.Background(), tok, service.SoftwareFactoryService, "1.0.0", 1, body); err != nil {
		// 发布失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	// 同一块库另起服务，相当于进程重启，核对记录是空的。
	restarted := service.NewService(svc.Store())
	// 重启后接回同一份字节，但每次都慢读。
	restarted.SetBlobs(&slowBlobs{Memory: mem})
	// 先不起服务，改短读时限再起。
	srv := httptest.NewUnstartedServer(httpapi.New(restarted, "test").Router())
	// 读时限远短于首次整包核摘要的时长。
	srv.Config.ReadTimeout = 150 * time.Millisecond
	// 改完时限再起服务。
	srv.Start()
	// 测完关掉临时服务，避免端口占着。
	t.Cleanup(srv.Close)
	// 要原始字节，先核摘要再整包回来。
	res := softwareGet(t, srv.URL, fid, priv, true, "")
	// 读完整包，中途被掐断会读出错。
	got, err := io.ReadAll(res.Body)
	// 读完就关上响应。
	_ = res.Body.Close()
	// 读出错、状态不对或没读全就停测。
	if err != nil || res.StatusCode != http.StatusOK || !bytes.Equal(got, body) {
		// 核摘要时被读时限掐断了，带上现场停测。
		t.Fatalf("cold pull %d %d %v", res.StatusCode, len(got), err)
	}
}

// 重启后首次拉包整包核一次摘要，并发也只核一次；对象被换掉就拒发。
func TestOpenSoftwareVerifiesOnce(t *testing.T) {
	// 准备已认领厂和管理员会话。
	svc, tok, fid, _ := enrolledFactory(t)
	// 换上记打开次数的存根。
	blobs := &countBlobs{Memory: blob.NewMemory()}
	// 让服务用这份存根，发布和拉取都走它。
	svc.SetBlobs(blobs)
	// 本测不取消，上下文只为满足接口。
	ctx := context.Background()
	// 先发布一版厂包，厂才拉得到。
	if _, err := svc.PublishSoftware(ctx, tok, service.SoftwareFactoryService, "1.0.0", 1, []byte("pack-v1-body")); err != nil {
		// 发布失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	// 同一块库另起服务，相当于进程重启，核对记录是空的。
	restarted := service.NewService(svc.Store())
	// 重启后接回同一份存储。
	restarted.SetBlobs(blobs)
	// 打开次数清零，只数重启后的。
	blobs.opens.Store(0)
	// 并发几路一起拉，摘要只该核一次。
	var wg sync.WaitGroup
	// 收集各路结果，有一路失败就停测。
	errs := make(chan error, 4)
	// 起四路并发拉包。
	for range 4 {
		// 登记一路，等全部结束再断言。
		wg.Add(1)
		// 每一路打开包流再关上。
		go func() {
			// 这一路结束就销账。
			defer wg.Done()
			// 打开包流，摘要没核过会先核。
			pack, err := restarted.Updates.OpenSoftware(ctx, fid, service.SoftwareFactoryService, 1, false)
			// 打开成功就关上流。
			if err == nil {
				// 关上流，不留着连接。
				_ = pack.Body.Close()
			}
			// 把结果交给主流程。
			errs <- err
		}()
	}
	// 等四路都结束。
	wg.Wait()
	// 关掉结果通道，下面读完就停。
	close(errs)
	// 逐路看结果，有失败就停测。
	for err := range errs {
		// 这一路失败就停测。
		if err != nil {
			// 并发拉包失败，带上原因停测。
			t.Fatal(err)
		}
	}
	// 四路各开一次流，核摘要只多开一次。
	if n := blobs.opens.Load(); n != 5 {
		// 打开次数不对说明核了不止一次。
		t.Fatalf("opens %d", n)
	}
	// 对象键带摘要，按发布时的字节算出原件那一处。
	sum := sha256.Sum256([]byte("pack-v1-body"))
	// 换掉对象内容和长度，模拟存储里的包坏了。
	if err := blobs.Put(ctx, store.SoftwareObjectKey(service.SoftwareFactoryService, 1, sum[:]), []byte("tampered")); err != nil {
		// 换不掉就不能继续，以免误判通过。
		t.Fatal(err)
	}
	// 长度变了要重核，坏对象应拒发。
	if _, err := restarted.Updates.OpenSoftware(ctx, fid, service.SoftwareFactoryService, 1, false); !errors.Is(err, domain.ErrIntegrity) {
		// 坏对象被发出去了，带上原因停测。
		t.Fatalf("tampered %v", err)
	}
}

// 慢读存根：打开的流每次只给几个字节，还要等一会儿；请求断了就报错。
type slowBlobs struct {
	*blob.Memory // 字节照常存在内存里
}

// Open 包一层慢读流，读时跟着请求取消。
func (s *slowBlobs) Open(ctx context.Context, key string) (blob.Object, error) {
	// 先照常打开内存流。
	obj, err := s.Memory.Open(ctx, key)
	// 打不开就原样交回。
	if err != nil {
		return nil, err
	}
	// 套上慢读，带着打开时的上下文。
	return &slowObject{Object: obj, ctx: ctx}, nil
}

// 慢读流：每次读都等一会儿，只给四个字节。
type slowObject struct {
	blob.Object                 // 定位、总长和关闭照常
	ctx         context.Context // 打开时的上下文，取消了就报错
}

// Read 等一会儿再读，请求被取消就报错。
func (o *slowObject) Read(p []byte) (int, error) {
	// 先等一会儿，或请求先断。
	select {
	// 请求断了就报错，模拟存储读流被掐断。
	case <-o.ctx.Done():
		// 交回取消原因，读流到此为止。
		return 0, o.ctx.Err()
	// 等够了再读。
	case <-time.After(40 * time.Millisecond):
	}
	// 一次最多给四个字节。
	if len(p) > 4 {
		// 截成四字节，整包要读好多次。
		p = p[:4]
	}
	// 从内存流里读这几个字节。
	return o.Object.Read(p)
}

// 记打开次数的存根，用来看摘要核了几次。
type countBlobs struct {
	*blob.Memory              // 字节照常存在内存里
	opens        atomic.Int32 // 打开流的次数
}

// Open 记一次再照常打开。
func (c *countBlobs) Open(ctx context.Context, key string) (blob.Object, error) {
	// 记一次打开。
	c.opens.Add(1)
	// 照常打开内存流。
	return c.Memory.Open(ctx, key)
}

// 用厂钥签名拉 v1 厂包；raw 为真要原始字节，rng 非空就带区间。
func softwareGet(t *testing.T, base string, fid uuid.UUID, priv []byte, raw bool, rng string) *http.Response {
	// 标成辅助，失败行号落到调用它的测试。
	t.Helper()
	// 组好请求，组失败就停测。
	req, err := http.NewRequest(http.MethodGet, base+"/v1/channel/pull/software?kind=factory_service&version=1", nil)
	// 这一步失败就不能继续，以免误判通过。
	if err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	// 用当前时刻签名，过期会被拒绝。
	unix := time.Now().Unix()
	// 用厂钥签时刻，云端用来认厂。
	sig := nodekey.Sign(priv, nodekey.MQTTConnectPayload(fid, unix))
	// 写上工厂身份。
	req.Header.Set("X-WMesh-Factory", fid.String())
	// 带上签名时刻，过期会被拒绝。
	req.Header.Set("X-WMesh-Time", strconv.FormatInt(unix, 10))
	// 带上厂钥签名，对不上就未授权。
	req.Header.Set("X-WMesh-Sign", base64.StdEncoding.EncodeToString(sig))
	// 新厂端声明要原始字节。
	if raw {
		// 声明要原始字节，云端才走流式。
		req.Header.Set("Accept", "application/octet-stream")
	}
	// 续传时带上区间。
	if rng != "" {
		// 写上要的区间。
		req.Header.Set("Range", rng)
	}
	// 发出去，连不上就停测。
	res, err := http.DefaultClient.Do(req)
	// 这一步失败就不能继续，以免误判通过。
	if err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	return res
}

// 读完响应正文再关上，读失败就停测。
func readAll(t *testing.T, res *http.Response) []byte {
	// 标成辅助，失败行号落到调用它的测试。
	t.Helper()
	// 读完就关上响应，连接才能复用。
	defer res.Body.Close()
	// 读完全部正文。
	b, err := io.ReadAll(res.Body)
	// 读失败就停测。
	if err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	return b
}

// 数拉包允许审计有几条。
func countPullAllows(t *testing.T, svc *service.Service) int {
	// 标成辅助，失败行号落到调用它的测试。
	t.Helper()
	// 读出全部审计行。
	rows, err := svc.ListAudit(context.Background())
	// 读不到就停测。
	if err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	// 从零数起。
	n := 0
	// 逐行看是不是拉包允许。
	for _, r := range rows {
		// 拉包允许才计数。
		if r.Action == "pull_software" && r.Result == audit.Allow {
			// 记一条。
			n++
		}
	}
	return n
}
