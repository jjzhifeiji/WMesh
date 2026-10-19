package wanchannel

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/domain"
)

// SoftwareMeta 是 WAN 该种类当前最高版本，不含字节。
type SoftwareMeta struct {
	Kind        string `json:"kind"`        // factory_service / client_apk
	Version     int64  `json:"version"`     // 当前最高
	VersionName string `json:"versionName"` // 给人看的版本名
	Digest      []byte `json:"digest"`      // SHA-256
}

// 旧 WAN 按版本回的 JSON 软件包，字节只在这一步出现。
type softwareOffer struct {
	Kind        string `json:"kind"`        // factory_service / client_apk
	Version     int64  `json:"version"`     // 单调整数
	VersionName string `json:"versionName"` // 给人看的版本名
	Digest      []byte `json:"digest"`      // SHA-256
	Body        []byte `json:"body"`        // 包字节
}

// LatestSoftware 已认领厂会话看该种类当前最高版本，不含字节。
func LatestSoftware(ctx context.Context, wanHTTP string, factoryID uuid.UUID, priv []byte, kind string) (SoftwareMeta, error) {
	// 用厂钥签查询，没有签名平台会拒绝。
	p := newPuller(wanHTTP, factoryID, priv)
	// 预备装最高版，失败时不把零值当成真版本。
	var meta SoftwareMeta
	// 查不到最高版就交回，调用方不要当成本地已有。
	if err := p.get(ctx, "/v1/software/latest?kind="+url.QueryEscape(kind), &meta); err != nil {
		return SoftwareMeta{}, err
	}
	return meta, nil
}

// 拉包落盘和看门狗的参数；测试可改短。
var (
	softwareClient = &http.Client{}   // 拉包不设总时长，靠读间隔看门狗收尾
	softwareIdle   = 60 * time.Second // 这么久没有新字节才算卡死
)

// 拉包相关的响应头、目录名和旧格式上限。
const (
	softwareDigestHeader = "X-WMesh-Digest"                 // WAN 回包带的 SHA-256，十六进制
	softwareDirName      = "wmesh-software"                 // 临时目录下的暂存目录名
	legacyPullBytes      = maxPullBytes/3*4 + 4 + (1 << 20) // 旧 JSON 回包：base64 胀三分之一再留信封
)

// PullSoftwareBody 已认领厂会话按版本拉包字节；边下边写本厂暂存文件，断了下次接着下。
// want 是问最高版拿到的摘要，可空；非空时暂存文件按它命名，下完必须对上。
func PullSoftwareBody(ctx context.Context, wanHTTP string, factoryID uuid.UUID, priv []byte, kind string, version int64, want []byte) ([]byte, error) {
	// 用厂钥签拉包，对不上就拿不到字节。
	p := newPuller(wanHTTP, factoryID, priv)
	// 下到本厂暂存文件，核完摘要再交回字节。
	return p.pullSoftware(ctx, softwareDir(factoryID), kind, version, want)
}

// DropSoftwarePart 本厂已收下这一版后删掉它的暂存；没收下前留着，重试不必重下。
func DropSoftwarePart(factoryID uuid.UUID, kind string, version int64, sum []byte) {
	// 按拉包时同一套规则找到暂存，删不掉或没有都不算错。
	_ = os.Remove(partPath(softwareDir(factoryID), kind, version, sum))
}

// 本厂暂存目录：按厂分开，一个进程跑多家厂也不串。
func softwareDir(factoryID uuid.UUID) string {
	// 临时目录下按工厂身份分一层。
	return filepath.Join(os.TempDir(), softwareDirName, factoryID.String())
}

// 按版本拉包：有暂存就带 Range 续传，碰上旧 WAN 回 JSON 就按老格式解。
func (p *puller) pullSoftware(ctx context.Context, dir, kind string, version int64, want []byte) ([]byte, error) {
	// 先建好本厂暂存目录，建不了就没法续传。
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	// 知道摘要才有暂存文件可续；不知道就等响应头给。
	part := ""
	// 已下到的字节数，从这里接着要。
	var have int64
	// 摘要已知就按它找暂存文件。
	if len(want) > 0 {
		// 暂存文件名带种类、版本和摘要。
		part = partPath(dir, kind, version, want)
		// 看已下了多少，没有文件就从零开始。
		have = partSize(part)
	}
	// 暂存比上限还大就是坏的，删掉从头下。
	if have > maxPullBytes {
		// 删掉超限的暂存，下面从零开始。
		_ = os.Remove(part)
		// 从零开始重下。
		have = 0
	}
	// 请求挂在可取消的上下文上，看门狗到点就取消它。
	rctx, cancel := context.WithCancel(ctx)
	// 离开时取消，含提前返回。
	defer cancel()
	// 读间隔看门狗：这么久没进展就取消请求，不设总时长。
	dog := time.AfterFunc(softwareIdle, cancel)
	// 离开时停掉看门狗。
	defer dog.Stop()
	// 种类和版本都放进查询，避免拉错包。
	path := "/v1/channel/pull/software?kind=" + url.QueryEscape(kind) + "&version=" + strconv.FormatInt(version, 10)
	// 组好带取消的拉取，看门狗才能停掉它。
	req, err := http.NewRequestWithContext(rctx, http.MethodGet, p.wanHTTP+path, nil)
	// 请求组不出来就当平台不可达。
	if err != nil {
		return nil, domain.ErrWANUnreachable
	}
	// 声明要原始字节，新 WAN 才流式回包。
	req.Header.Set("Accept", "application/octet-stream")
	// 已有一截就只要剩下的。
	if have > 0 {
		// 从已下到的位置往后要。
		req.Header.Set("Range", "bytes="+strconv.FormatInt(have, 10)+"-")
	}
	// 补上厂钥签名，没有签名平台会拒绝。
	p.sign(req)
	// 送出拉取，不用长超时客户端，靠看门狗收尾。
	res, err := p.doWith(softwareClient, req)
	// 送不出去就当不可达，暂存留着下次续。
	if err != nil {
		return nil, domain.ErrWANUnreachable
	}
	// 读完就关掉响应，连接才能复用。
	defer res.Body.Close()
	// 拿到响应头算一次进展，看门狗往后拨。
	dog.Reset(softwareIdle)
	// 正文每读到字节都拨一次看门狗。
	body := &idleReader{r: res.Body, dog: dog}
	// 按状态分路：越界、拒绝、旧格式和原始字节。
	switch {
	// 续传起点已到末尾，本地多半已是整包，核摘要即可。
	case res.StatusCode == http.StatusRequestedRangeNotSatisfiable && have > 0:
		// 核完摘要再交回，坏了就删。
		return finishPart(dir, part, kind, version, want)
	// 平台拒绝就把状态收成业务错误。
	case res.StatusCode >= 300:
		// 只读到上限，异常页面不能灌满内存。
		msg, _ := io.ReadAll(io.LimitReader(body, 1<<20))
		// 按状态和英文原因交回，调用方不要当成功。
		return nil, mapHTTPStatus(res.StatusCode, msg)
	// 还没升级的 WAN 回 JSON，按老格式解。
	case strings.HasPrefix(res.Header.Get("Content-Type"), "application/json"):
		// 老格式整包在 JSON 里，解出来核摘要。
		return legacySoftware(body, want)
	}
	// 读出响应头里的摘要，下完按它核。
	got, err := hex.DecodeString(res.Header.Get(softwareDigestHeader))
	// 没带或不是 SHA-256 长度就不能核，当内容损坏。
	if err != nil || len(got) != sha256.Size {
		return nil, domain.ErrIntegrity
	}
	// 响应头摘要和暂存文件名对不上，暂存作废。
	if len(want) > 0 && !bytes.Equal(got, want) {
		// 删掉对不上的暂存，下次从头下。
		_ = os.Remove(part)
		return nil, domain.ErrIntegrity
	}
	// 事先不知道摘要就按响应头命名暂存文件。
	if len(want) == 0 {
		// 记下平台给的摘要，下完按它核。
		want = got
		// 按摘要定下暂存文件名。
		part = partPath(dir, kind, version, want)
	}
	// 默认整包重写，回 206 才接在后面。
	flags, off := os.O_CREATE|os.O_WRONLY|os.O_TRUNC, int64(0)
	// 平台按区间回了剩下那段，就接在暂存后面写。
	if res.StatusCode == http.StatusPartialContent {
		// 读出这段的起点，必须正好接上已下的。
		start, ok := contentRangeStart(res.Header.Get("Content-Range"))
		// 起点对不上就不写，暂存留着下次再续。
		if !ok || start != have {
			return nil, domain.ErrWANUnreachable
		}
		// 接在后面写，不清掉已下的。
		flags, off = os.O_CREATE|os.O_WRONLY|os.O_APPEND, have
	}
	// 打开暂存文件，按上面的方式写。
	f, err := os.OpenFile(part, flags, 0o600)
	// 打不开就没法落盘，交回原因。
	if err != nil {
		return nil, err
	}
	// 边下边写，最多比上限多读一字节用来发现超限。
	n, copyErr := io.Copy(f, io.LimitReader(body, maxPullBytes-off+1))
	// 写完就关上文件，关不上也算没写好。
	closeErr := f.Close()
	// 超过体积上限就当内容损坏，删掉暂存。
	if off+n > maxPullBytes {
		// 删掉超限的暂存，不留着续。
		_ = os.Remove(part)
		return nil, domain.ErrIntegrity
	}
	// 下一半断了或落盘失败，暂存留着下次接着下。
	if copyErr != nil || closeErr != nil {
		return nil, domain.ErrWANUnreachable
	}
	// 下完核摘要，对上才交回字节。
	return finishPart(dir, part, kind, version, want)
}

// 读出整份暂存核摘要：对上就交回字节，暂存留到本厂收下再删；对不上删掉报损坏。
func finishPart(dir, part, kind string, version int64, want []byte) ([]byte, error) {
	// 把暂存整份读出来，包不超过上限。
	body, err := os.ReadFile(part)
	// 读不出就当没下好，下次再试。
	if err != nil {
		return nil, domain.ErrWANUnreachable
	}
	// 超限或摘要对不上就是坏包，删掉从头下。
	if int64(len(body)) > maxPullBytes || !digest.Match(body, want) {
		// 删掉坏暂存，下次不再续它。
		_ = os.Remove(part)
		return nil, domain.ErrIntegrity
	}
	// 这一版的暂存先留着：收下失败重试时回 416，本地核完即可，不必重下。
	// 同种类更旧版本的暂存清掉，免得一直占盘。
	removeStaleParts(dir, kind, version)
	return body, nil
}

// 解还没升级的 WAN 回的 JSON 包；上限按解出的原始字节算。
func legacySoftware(r io.Reader, want []byte) ([]byte, error) {
	// 多读一字节用来发现超限，避免整包灌进内存。
	raw, err := io.ReadAll(io.LimitReader(r, legacyPullBytes+1))
	// 响应读不完就当不可达，避免用半截正文。
	if err != nil {
		return nil, domain.ErrWANUnreachable
	}
	// JSON 本身就超限，按内容损坏拒收。
	if int64(len(raw)) > legacyPullBytes {
		return nil, domain.ErrIntegrity
	}
	// 预备装包体，失败时不能把空字节当成功。
	var offer softwareOffer
	// 正文解不开就当不可达，避免用残缺数据。
	if err := json.Unmarshal(raw, &offer); err != nil {
		return nil, domain.ErrWANUnreachable
	}
	// 解出的原始字节超限，按内容损坏拒收。
	if int64(len(offer.Body)) > maxPullBytes {
		return nil, domain.ErrIntegrity
	}
	// 平台带了摘要就核，对不上拒收。
	if len(offer.Digest) > 0 && !digest.Match(offer.Body, offer.Digest) {
		return nil, domain.ErrIntegrity
	}
	// 问最高版时拿到过摘要也要对上。
	if len(want) > 0 && !digest.Match(offer.Body, want) {
		return nil, domain.ErrIntegrity
	}
	return offer.Body, nil
}

// 暂存文件路径：种类、版本和摘要都进文件名，换了摘要不会接错。
func partPath(dir, kind string, version int64, sum []byte) string {
	// 拼出种类加版本加摘要的文件名。
	return filepath.Join(dir, kind+"-v"+strconv.FormatInt(version, 10)+"-"+hex.EncodeToString(sum)+".part")
}

// 暂存已下的字节数；没有文件就是零。
func partSize(part string) int64 {
	// 看文件信息，没有就从零开始。
	fi, err := os.Stat(part)
	// 没有或看不了都从零开始。
	if err != nil {
		return 0
	}
	// 文件长度就是已下的字节数。
	return fi.Size()
}

// 删掉同种类更旧版本的暂存；新版本可能正在下，不碰。
func removeStaleParts(dir, kind string, version int64) {
	// 列出这一种类的全部暂存。
	parts, _ := filepath.Glob(filepath.Join(dir, kind+"-v*.part"))
	// 逐个看版本，比这一版旧的才删。
	for _, part := range parts {
		// 去掉种类前缀，剩下版本开头。
		rest := strings.TrimPrefix(filepath.Base(part), kind+"-v")
		// 取出版本那一段。
		ver, _, _ := strings.Cut(rest, "-")
		// 认得出且比这一版旧才删。
		if v, err := strconv.ParseInt(ver, 10, 64); err == nil && v < version {
			// 删掉过时的暂存，删不掉下次再说。
			_ = os.Remove(part)
		}
	}
}

// 读出 206 回包这一段的起点；格式不对就认不出。
func contentRangeStart(header string) (int64, bool) {
	// 只认字节单位的区间。
	spec, ok := strings.CutPrefix(strings.TrimSpace(header), "bytes ")
	// 不是字节区间就认不出。
	if !ok {
		return 0, false
	}
	// 拆出起点那一段。
	start, _, ok := strings.Cut(spec, "-")
	// 没有横杠就认不出。
	if !ok {
		return 0, false
	}
	// 起点收成整数。
	n, err := strconv.ParseInt(strings.TrimSpace(start), 10, 64)
	return n, err == nil
}

// 每读到字节就把看门狗往后拨；没进展到点由看门狗取消请求。
type idleReader struct {
	r   io.Reader   // 响应正文
	dog *time.Timer // 读间隔看门狗
}

// Read 照常读，读到字节就把看门狗往后拨。
func (r *idleReader) Read(p []byte) (int, error) {
	// 从响应正文里读一段。
	n, err := r.r.Read(p)
	// 有进展就重新计时。
	if n > 0 {
		// 看门狗往后拨一个间隔。
		r.dog.Reset(softwareIdle)
	}
	return n, err
}
