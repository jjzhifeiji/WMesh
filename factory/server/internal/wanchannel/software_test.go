package wanchannel

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/nodekey"
)

// 中途断开后再拉一次，带 Range 接着下，拼出的整包摘要对得上；暂存留到本厂收下才删。
func TestPullSoftwareResumes(t *testing.T) {
	// 暂存放进本测的临时目录，不碰别的测试。
	t.Setenv("TMPDIR", t.TempDir())
	// 造一个工厂身份，暂存目录按它分开。
	fid := uuid.New()
	// 生成本厂签发钥，拉包时用来签名。
	priv := testPriv(t)
	// 包字节拼长一点，断在中间才看得出续传。
	body := bytes.Repeat([]byte("0123456789abcdef"), 4096)
	// 算出包摘要，响应头和暂存文件名都用它。
	sum := digest.Sum(body)
	// 记下每次请求带的区间。
	ranges := make(chan string, 4)
	// 第一次断在中间，之后按区间正常回。
	var calls atomic.Int32
	// 替身平台：首次回一半就断开，之后按区间续传。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 记下这次带的区间。
		ranges <- r.Header.Get("Range")
		// 头里带上摘要，客户端据此核对。
		setRawHeaders(w, sum)
		// 第一次只回一半就断开连接。
		if calls.Add(1) == 1 {
			// 回一半就断开，模拟弱网掉线。
			abortHalf(w, body)
		}
		// 之后按区间正常回，标准件处理 206。
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(body))
	}))
	// 测完关掉替身，避免端口一直占住。
	t.Cleanup(srv.Close)
	// 本测不取消，上下文只为满足接口。
	ctx := context.Background()
	// 第一次中途断开，应交回不可达。
	if _, err := PullSoftwareBody(ctx, srv.URL, fid, priv, "client_apk", 3, sum); !errors.Is(err, domain.ErrWANUnreachable) {
		// 断开没报不可达就停测。
		t.Fatalf("first pull %v", err)
	}
	// 按工厂、种类、版本和摘要找暂存文件。
	part := partPath(filepath.Join(os.TempDir(), softwareDirName, fid.String()), "client_apk", 3, sum)
	// 看暂存下了多少。
	have := partSize(part)
	// 没留下或已是整包都不对。
	if have == 0 || have >= int64(len(body)) {
		// 暂存长度不对就停测。
		t.Fatalf("part size %d", have)
	}
	// 第二次接着下，应拿到整包。
	got, err := PullSoftwareBody(ctx, srv.URL, fid, priv, "client_apk", 3, sum)
	// 失败或拼出的不是整包就停测。
	if err != nil || !bytes.Equal(got, body) {
		// 续传没拼对就停测。
		t.Fatalf("resume %d %v", len(got), err)
	}
	// 第一次不该带区间。
	if r := <-ranges; r != "" {
		// 首次就带了区间，停测。
		t.Fatalf("first range %q", r)
	}
	// 第二次应从已下的位置接着要。
	if r := <-ranges; r != "bytes="+strconv.FormatInt(have, 10)+"-" {
		// 续传区间不对就停测。
		t.Fatalf("resume range %q", r)
	}
	// 本厂还没收下，整包暂存应留着，收下失败重试不必重下。
	if n := partSize(part); n != int64(len(body)) {
		// 暂存没留整包就停测。
		t.Fatalf("part after verify %d", n)
	}
	// 本厂收下后清掉这一版的暂存。
	DropSoftwarePart(fid, "client_apk", 3, sum)
	// 收下后暂存应已删掉。
	if _, err := os.Stat(part); !os.IsNotExist(err) {
		// 暂存还留着就停测。
		t.Fatalf("part kept %v", err)
	}
}

// 已有整包暂存时再拉一次，平台回 416，本地核完摘要交回整包，不再重下。
func TestPullSoftwareReusesFullPart(t *testing.T) {
	// 暂存放进本测的临时目录，不碰别的测试。
	t.Setenv("TMPDIR", t.TempDir())
	// 造一个工厂身份，暂存目录按它分开。
	fid := uuid.New()
	// 生成本厂签发钥，拉包时用来签名。
	priv := testPriv(t)
	// 包字节，第一次整包下完。
	body := bytes.Repeat([]byte("full-part-"), 2048)
	// 算出包摘要，响应头和暂存文件名都用它。
	sum := digest.Sum(body)
	// 记下每次请求带的区间。
	ranges := make(chan string, 4)
	// 记下每次回包的状态码。
	codes := make(chan int, 4)
	// 替身平台：按区间正常回，标准件处理 206 和 416。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 记下这次带的区间。
		ranges <- r.Header.Get("Range")
		// 头里带上摘要，客户端据此核对。
		setRawHeaders(w, sum)
		// 包一层记下状态码。
		rec := &codeWriter{ResponseWriter: w, code: http.StatusOK}
		// 按区间回包，起点到末尾时回 416。
		http.ServeContent(rec, r, "", time.Time{}, bytes.NewReader(body))
		// 送出这次的状态码。
		codes <- rec.code
	}))
	// 测完关掉替身，避免端口一直占住。
	t.Cleanup(srv.Close)
	// 本测不取消，上下文只为满足接口。
	ctx := context.Background()
	// 第一次整包下完，相当于下完但本厂没收下。
	if got, err := PullSoftwareBody(ctx, srv.URL, fid, priv, "client_apk", 8, sum); err != nil || !bytes.Equal(got, body) {
		// 首次没下完整就停测。
		t.Fatalf("first pull %d %v", len(got), err)
	}
	// 第二次应带着整包长度续传，平台回 416。
	got, err := PullSoftwareBody(ctx, srv.URL, fid, priv, "client_apk", 8, sum)
	// 失败或不是整包就停测。
	if err != nil || !bytes.Equal(got, body) {
		// 没从暂存交回整包就停测。
		t.Fatalf("second pull %d %v", len(got), err)
	}
	// 第一次从头要、整包回来。
	if r, c := <-ranges, <-codes; r != "" || c != http.StatusOK {
		// 首次请求不对就停测。
		t.Fatalf("first %q %d", r, c)
	}
	// 第二次从末尾要，平台回 416，不再传包字节。
	if r, c := <-ranges, <-codes; r != "bytes="+strconv.Itoa(len(body))+"-" || c != http.StatusRequestedRangeNotSatisfiable {
		// 又整包重下了就停测。
		t.Fatalf("second %q %d", r, c)
	}
}

// 记下回包状态码的包装，用来看平台回的是不是 416。
type codeWriter struct {
	http.ResponseWriter     // 真正写回包的那一个
	code                int // 最后写下的状态码
}

// WriteHeader 记下状态码再照常写。
func (w *codeWriter) WriteHeader(code int) {
	// 记下这次的状态码。
	w.code = code
	// 照常写出状态。
	w.ResponseWriter.WriteHeader(code)
}

// 两家厂拉同一版本各用各的暂存：一家断在中间，另一家照常从头下完。
func TestPullSoftwarePartsPerFactory(t *testing.T) {
	// 暂存放进本测的临时目录，不碰别的测试。
	t.Setenv("TMPDIR", t.TempDir())
	// 造两家厂的身份。
	fidA, fidB := uuid.New(), uuid.New()
	// 生成签发钥，两家共用不影响暂存目录。
	priv := testPriv(t)
	// 两家拉同一份包。
	body := bytes.Repeat([]byte("factory-pack-"), 2048)
	// 算出包摘要。
	sum := digest.Sum(body)
	// 记下 B 厂请求带的区间。
	rangeB := make(chan string, 2)
	// 替身平台：A 厂断在中间，B 厂正常回。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 头里带上摘要。
		setRawHeaders(w, sum)
		// A 厂总是回一半就断开。
		if r.Header.Get("X-WMesh-Factory") == fidA.String() {
			// 回一半就断开。
			abortHalf(w, body)
		}
		// 记下 B 厂带的区间。
		rangeB <- r.Header.Get("Range")
		// B 厂按区间正常回。
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(body))
	}))
	// 测完关掉替身，避免端口一直占住。
	t.Cleanup(srv.Close)
	// 本测不取消，上下文只为满足接口。
	ctx := context.Background()
	// A 厂中途断开，暂存留着。
	if _, err := PullSoftwareBody(ctx, srv.URL, fidA, priv, "factory_service", 7, sum); !errors.Is(err, domain.ErrWANUnreachable) {
		// 断开没报不可达就停测。
		t.Fatalf("factory A %v", err)
	}
	// A 厂的暂存文件。
	partA := partPath(filepath.Join(os.TempDir(), softwareDirName, fidA.String()), "factory_service", 7, sum)
	// 记下 A 厂已下的长度。
	haveA := partSize(partA)
	// A 厂应留下一截。
	if haveA == 0 {
		// 没留下暂存就停测。
		t.Fatal("factory A part missing")
	}
	// B 厂拉同一版本，应从头下完。
	got, err := PullSoftwareBody(ctx, srv.URL, fidB, priv, "factory_service", 7, sum)
	// 失败或不是整包就停测。
	if err != nil || !bytes.Equal(got, body) {
		// B 厂没下完整就停测。
		t.Fatalf("factory B %d %v", len(got), err)
	}
	// B 厂不该接着 A 厂的暂存续传。
	if r := <-rangeB; r != "" {
		// 串用了别家暂存就停测。
		t.Fatalf("factory B range %q", r)
	}
	// A 厂的暂存应原样留着。
	if n := partSize(partA); n != haveA {
		// 别家的暂存被动了就停测。
		t.Fatalf("factory A part %d -> %d", haveA, n)
	}
}

// 下完摘要不对、或响应头摘要和暂存文件名对不上，都删掉暂存并报完整性错误。
func TestPullSoftwareDigestMismatchDropsPart(t *testing.T) {
	// 暂存放进本测的临时目录，不碰别的测试。
	t.Setenv("TMPDIR", t.TempDir())
	// 造一个工厂身份。
	fid := uuid.New()
	// 生成签发钥。
	priv := testPriv(t)
	// 真包和它的摘要。
	body := []byte("real-package-bytes")
	// 算出真包摘要。
	sum := digest.Sum(body)
	// 替身回的头摘要，按用例切换。
	var header atomic.Value
	// 先按真包摘要回头。
	header.Store(sum)
	// 替身平台：正文总是坏的，头摘要按用例给。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 头里带上用例指定的摘要。
		setRawHeaders(w, header.Load().([]byte))
		// 正文是坏的，和摘要对不上。
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader([]byte("tampered-package!!")))
	}))
	// 测完关掉替身，避免端口一直占住。
	t.Cleanup(srv.Close)
	// 本测不取消，上下文只为满足接口。
	ctx := context.Background()
	// 暂存文件路径。
	part := partPath(filepath.Join(os.TempDir(), softwareDirName, fid.String()), "client_apk", 4, sum)
	// 下完摘要对不上，应报完整性错误。
	if _, err := PullSoftwareBody(ctx, srv.URL, fid, priv, "client_apk", 4, sum); !errors.Is(err, domain.ErrIntegrity) {
		// 坏包没被拒就停测。
		t.Fatalf("tampered %v", err)
	}
	// 坏包的暂存应已删掉。
	if _, err := os.Stat(part); !os.IsNotExist(err) {
		// 坏暂存还留着就停测。
		t.Fatalf("bad part kept %v", err)
	}
	// 预先留一截暂存，看头摘要对不上时会不会删。
	if err := os.WriteFile(part, body[:4], 0o600); err != nil {
		// 写不了暂存就停测。
		t.Fatal(err)
	}
	// 头摘要换成别的，和暂存文件名对不上。
	header.Store(digest.Sum([]byte("other")))
	// 头摘要对不上，应报完整性错误。
	if _, err := PullSoftwareBody(ctx, srv.URL, fid, priv, "client_apk", 4, sum); !errors.Is(err, domain.ErrIntegrity) {
		// 对不上没被拒就停测。
		t.Fatalf("header mismatch %v", err)
	}
	// 对不上的暂存应已删掉。
	if _, err := os.Stat(part); !os.IsNotExist(err) {
		// 暂存还留着就停测。
		t.Fatalf("mismatched part kept %v", err)
	}
}

// 还没升级的 WAN 回 JSON，按老格式解出包字节。
func TestPullSoftwareLegacyJSON(t *testing.T) {
	// 暂存放进本测的临时目录，不碰别的测试。
	t.Setenv("TMPDIR", t.TempDir())
	// 造一个工厂身份。
	fid := uuid.New()
	// 生成签发钥。
	priv := testPriv(t)
	// 老格式里的包字节。
	body := []byte("legacy-json-package")
	// 算出包摘要。
	sum := digest.Sum(body)
	// 替身平台：不认原始字节头，照旧回 JSON。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// 按旧格式回整包 JSON。
		writeJSON(w, map[string]any{"kind": "client_apk", "version": 5, "digest": sum, "body": body})
	}))
	// 测完关掉替身，避免端口一直占住。
	t.Cleanup(srv.Close)
	// 带不带摘要都该收下，逐个试。
	for _, want := range [][]byte{nil, sum} {
		// 按老格式拉包。
		got, err := PullSoftwareBody(context.Background(), srv.URL, fid, priv, "client_apk", 5, want)
		// 失败或字节不对就停测。
		if err != nil || !bytes.Equal(got, body) {
			// 老格式没收下就停测。
			t.Fatalf("legacy %q %v", got, err)
		}
	}
}

// 拉包卡住时读间隔看门狗到点收尾，已下的一截留着。
func TestPullSoftwareIdleTimeout(t *testing.T) {
	// 暂存放进本测的临时目录，不碰别的测试。
	t.Setenv("TMPDIR", t.TempDir())
	// 记住原来的读间隔，测完改回去。
	old := softwareIdle
	// 读间隔调短，用例不必等一分钟。
	softwareIdle = 100 * time.Millisecond
	// 测完改回原来的读间隔。
	t.Cleanup(func() { softwareIdle = old })
	// 造一个工厂身份。
	fid := uuid.New()
	// 生成签发钥。
	priv := testPriv(t)
	// 包字节，回一半后卡住。
	body := bytes.Repeat([]byte("idle"), 4096)
	// 算出包摘要。
	sum := digest.Sum(body)
	// 用例结束时放开，卡住的请求才收尾。
	release := make(chan struct{})
	// 替身平台：回一半后卡住不动。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// 头里带上摘要。
		setRawHeaders(w, sum)
		// 声明整包长度。
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		// 先写状态。
		w.WriteHeader(http.StatusOK)
		// 只写一半。
		_, _ = w.Write(body[:len(body)/2])
		// 推出去，客户端才收得到。
		w.(http.Flusher).Flush()
		// 卡到用例放开。
		<-release
	}))
	// 测完关掉替身，避免端口一直占住。
	t.Cleanup(srv.Close)
	// 关替身前先放开卡住的请求，否则关不掉。
	t.Cleanup(func() { close(release) })
	// 记下开始时刻，看是不是很快收尾。
	start := time.Now()
	// 卡住后应由看门狗收尾，交回不可达。
	if _, err := PullSoftwareBody(context.Background(), srv.URL, fid, priv, "client_apk", 6, sum); !errors.Is(err, domain.ErrWANUnreachable) {
		// 没按不可达收尾就停测。
		t.Fatalf("idle %v", err)
	}
	// 两秒还没收尾就算看门狗没起作用。
	if d := time.Since(start); d > 2*time.Second {
		// 看门狗没起作用就停测。
		t.Fatalf("idle took %v", d)
	}
	// 已下的一截应留着，下次接着下。
	if n := partSize(partPath(filepath.Join(os.TempDir(), softwareDirName, fid.String()), "client_apk", 6, sum)); n != int64(len(body)/2) {
		// 暂存长度不对就停测。
		t.Fatalf("part %d", n)
	}
}

// 拉包卡住时下行照样及时落地；同版本不重复开拉，新版本照常开拉；取消后 Hold 及时返回。
func TestHoldSoftwarePullDoesNotBlock(t *testing.T) {
	// 暂存放进本测的临时目录，不碰别的测试。
	t.Setenv("TMPDIR", t.TempDir())
	// 造一个工厂身份，后面用来对回执。
	fid := uuid.New()
	// 生成本厂签发钥，连接时用来签名。
	priv := testPriv(t)
	// 留下代理句柄，后面才能往下行投指令。
	broker, mqttAddr := startAllowBroker(t)
	// 开始拉包就把版本送出来。
	entered := make(chan int64, 4)
	// 用例结束时放开，卡住的拉包才收尾。
	release := make(chan struct{})
	// 替身平台：最高版是厂包 v1，拉包一律卡住。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 按路径回租约、索引、最高版或卡住拉包。
		switch {
		// 租约路径回解包钥，通道才能继续拉索引。
		case strings.HasSuffix(r.URL.Path, "/lease"):
			// 回一条解包钥，通道才能继续拉索引。
			writeJSON(w, map[string]any{"typ": "lease", "lease": []byte("lease-key-32-bytes-long-enough!!"), "notAfter": time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)})
		// 索引路径回空清单。
		case strings.HasSuffix(r.URL.Path, "/index"):
			// 回空指令清单，表示当前没有待落地的。
			writeJSON(w, map[string]any{"cmds": []any{}})
		// 最高版只有厂包 v1。
		case strings.Contains(r.URL.Path, "/software/latest") && r.URL.Query().Get("kind") == "factory_service":
			// 回厂包 v1，通道会去拉它。
			writeJSON(w, map[string]any{"kind": "factory_service", "version": 1})
		// 拉包记下版本后卡住。
		case strings.Contains(r.URL.Path, "/pull/software"):
			// 读出要拉的版本。
			v, _ := strconv.ParseInt(r.URL.Query().Get("version"), 10, 64)
			// 送出这次拉的版本，满了就丢。
			select {
			// 告诉用例拉包开始了。
			case entered <- v:
			// 满了就不再塞。
			default:
			}
			// 卡到用例放开，模拟弱网拉大包。
			<-release
		// 未知路径拒绝，避免替身把不该成功的请求放行。
		default:
			// 这条路径拒绝，避免替身把不该成功的请求放行。
			http.NotFound(w, r)
		}
	}))
	// 测完关掉替身，避免端口一直占住。
	t.Cleanup(srv.Close)
	// 关替身前先放开卡住的拉包，否则关不掉。
	t.Cleanup(func() { close(release) })
	// 接住落地的绑定意图。
	got := make(chan ClientIntent, 1)
	// 记下各版本软件回调被调了几次。
	var v1, v2 atomic.Int32
	// 给这一轮限时，避免通道挂死拖住测试。
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	// 离开时取消超时，避免计时器一直留着。
	defer cancel()
	// 接住通道退出原因，主流程不用堵住。
	errCh := make(chan error, 1)
	// 后台跑通道，主流程才能等结果或停机。
	go func() {
		// 绑定回调送出意图，软件回调真去拉包。
		errCh <- Hold(ctx, "tcp://"+mqttAddr, srv.URL, fid, priv, Trust{}, nil, func(in ClientIntent) error {
			// 把绑定意图送进断言。
			got <- in
			return nil
		}, nil, nil, nil, func(sctx context.Context, raw json.RawMessage) error { // 软件回调按版本计数后真去拉包。
			// 预备装软件通知。
			var c Cmd
			// 解不开就当没收到。
			if err := json.Unmarshal(raw, &c); err != nil {
				return err
			}
			// v1 记一次。
			if c.Version == 1 {
				// 记一次 v1 回调。
				v1.Add(1)
			}
			// v2 记一次。
			if c.Version == 2 {
				// 记一次 v2 回调。
				v2.Add(1)
			}
			// 真去拉包，会卡在替身里。
			_, err := PullSoftwareBody(sctx, srv.URL, fid, priv, c.Kind, c.Version, nil)
			return err
		}, nil, nil, nil, nil, nil, nil)
	}()
	// 等首轮问包后开始拉 v1。
	waitVersion(t, ctx, errCh, entered, 1)
	// 造一个设备身份，绑定要对得上。
	cid := uuid.New()
	// 投一条绑定下行，主循环应立刻落地。
	publishDown(t, broker, fid, Cmd{Typ: CmdClientBind, ClientID: cid.String(), BindingRevision: 1})
	// 绑定、通道失败或一秒超时谁先到走谁。
	select {
	// 收到绑定就核对是不是那台。
	case in := <-got:
		// 落地的设备不对就停测。
		if in.ClientID != cid {
			// 绑定对象不对就停测。
			t.Fatalf("client %+v", in)
		}
	// 通道先退出就带上原因停测。
	case err := <-errCh:
		// 通道中途失败就停测，并带上原因。
		t.Fatalf("hold: %v", err)
	// 一秒还没落地就算被拉包卡住。
	case <-time.After(time.Second):
		// 绑定被卡住就停测。
		t.Fatal("bind blocked by software pull")
	}
	// 再通知一次 v1，正在拉就该跳过。
	publishDown(t, broker, fid, Cmd{Typ: CmdSoftware, Kind: "factory_service", Version: 1})
	// 通知 v2，新版本照常开拉。
	publishDown(t, broker, fid, Cmd{Typ: CmdSoftware, Kind: "factory_service", Version: 2})
	// 等 v2 也开始拉。
	waitVersion(t, ctx, errCh, entered, 2)
	// v1 只该处理过一次，v2 一次。
	if v1.Load() != 1 || v2.Load() != 1 {
		// 去重不对就停测。
		t.Fatalf("v1 %d v2 %d", v1.Load(), v2.Load())
	}
	// 取消通道，卡住的拉包随之中断。
	cancel()
	// 退出、或等过时限，谁先到走谁。
	select {
	// 通道退出就核对原因。
	case err := <-errCh:
		// 不是取消就停测，说明别的原因断了。
		if !errors.Is(err, context.Canceled) {
			// 退出原因不对就停测。
			t.Fatalf("hold after cancel: %v", err)
		}
	// 一秒还没退出就算被拉包拖住。
	case <-time.After(time.Second):
		// 取消后迟迟不退就停测。
		t.Fatal("hold blocked by software pull after cancel")
	}
}

// 等替身报某个版本开始拉，通道先退出或超时就停测。
func waitVersion(t *testing.T, ctx context.Context, errCh <-chan error, entered <-chan int64, want int64) {
	// 失败算在调用方行号上。
	t.Helper()
	// 一直等到这个版本开始拉。
	for {
		// 拉包开始、通道退出或超时，谁先到走谁。
		select {
		// 有版本开始拉，对上就结束等待。
		case v := <-entered:
			// 对上要等的版本就结束。
			if v == want {
				return
			}
		// 通道先退出就停测。
		case err := <-errCh:
			// 通道先退出就停测，并带上原因。
			t.Fatalf("hold: %v", err)
		// 到时还没开始就停测。
		case <-ctx.Done():
			// 到时还没等到这个版本就停测。
			t.Fatalf("no pull v%d", want)
		}
	}
}

// 生成一份签发私钥，生不出就停测。
func testPriv(t *testing.T) []byte {
	// 失败算在调用方行号上。
	t.Helper()
	// 生成本厂签发钥，拉包时用来签名。
	_, priv, err := nodekey.Generate()
	// 生不出就停测。
	if err != nil {
		// 这一步失败就停测，避免后面断言失真。
		t.Fatal(err)
	}
	return priv
}

// 写上原始字节回包的类型和摘要头。
func setRawHeaders(w http.ResponseWriter, sum []byte) {
	// 标明是原始字节。
	w.Header().Set("Content-Type", "application/octet-stream")
	// 带上摘要，客户端据此核对。
	w.Header().Set(softwareDigestHeader, hex.EncodeToString(sum))
}

// 声明整包长度却只写一半就断开连接，模拟弱网掉线。
func abortHalf(w http.ResponseWriter, body []byte) {
	// 声明整包长度。
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	// 先写状态。
	w.WriteHeader(http.StatusOK)
	// 只写一半。
	_, _ = w.Write(body[:len(body)/2])
	// 推出去，客户端才收得到这一半。
	w.(http.Flusher).Flush()
	// 中止处理，服务器直接断开连接。
	panic(http.ErrAbortHandler)
}
