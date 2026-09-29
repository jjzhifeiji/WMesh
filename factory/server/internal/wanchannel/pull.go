package wanchannel

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/nodekey"
)

const maxPullBytes = 256 << 20 // 与 WAN 上传上限一致；JSON 带正文会再胀一截

// 带着厂钥去拉正文的会话，没有签名会被拒绝。
type puller struct {
	wanHTTP   string       // 平台网页根地址，签名请求拼在后面
	factoryID uuid.UUID    // 本厂稳定身份，写进签名头
	priv      []byte       // 本厂签发私钥，只用于签名
	client    *http.Client // 拉正文的客户端，超时要够长
}

// 给这家厂签 HTTPS 拉正文。
func newPuller(wanHTTP string, factoryID uuid.UUID, priv []byte) *puller {
	// 去掉尾斜杠并带上长超时，大包才拉得完。
	return &puller{
		wanHTTP:   strings.TrimRight(strings.TrimSpace(wanHTTP), "/"),
		factoryID: factoryID,
		priv:      priv,
		client:    &http.Client{Timeout: 10 * time.Minute},
	}
}

// 带厂钥签名 GET JSON。
func (p *puller) get(ctx context.Context, path string, dst any) error {
	// 组好带取消的读取，超时才能停掉。
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.wanHTTP+path, nil)
	// 请求组不出来就当平台不可达。
	if err != nil {
		return domain.ErrWANUnreachable
	}
	// 补上厂钥签名，没有签名平台会拒绝。
	p.sign(req)
	// 送出拉取，网络失败就当平台不可达。
	res, err := p.client.Do(req)
	// 送不出去就当不可达，调用方可以再试。
	if err != nil {
		return domain.ErrWANUnreachable
	}
	// 读完就关掉响应，连接才能复用。
	defer res.Body.Close()
	// 多读一字节用来发现超限，避免整包灌进内存。
	body, err := io.ReadAll(io.LimitReader(res.Body, maxPullBytes+1))
	// 响应读不完就当不可达，避免用半截正文。
	if err != nil {
		return domain.ErrWANUnreachable
	}
	// 超过体积上限就当内容损坏，拒绝入库。
	if int64(len(body)) > maxPullBytes {
		return domain.ErrIntegrity
	}
	// 平台拒绝就把状态收成业务错误。
	if res.StatusCode >= 300 {
		// 按状态和英文原因交回，调用方不要当成功。
		return mapHTTPStatus(res.StatusCode, body)
	}
	// 调用方不要正文就到此为止。
	if dst == nil {
		return nil
	}
	// 正文解不开就当不可达，避免用残缺数据。
	if err := json.Unmarshal(body, dst); err != nil {
		return domain.ErrWANUnreachable
	}
	return nil
}

// 带厂钥签名 POST JSON。
func (p *puller) post(ctx context.Context, path string, dst any) error {
	// 组好带取消的提交，超时才能停掉。
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.wanHTTP+path, nil)
	// 请求组不出来就当平台不可达。
	if err != nil {
		return domain.ErrWANUnreachable
	}
	// 补上厂钥签名，没有签名平台会拒绝。
	p.sign(req)
	// 送出提交，网络失败就当平台不可达。
	res, err := p.client.Do(req)
	// 送不出去就当不可达，调用方可以再试。
	if err != nil {
		return domain.ErrWANUnreachable
	}
	// 读完就关掉响应，连接才能复用。
	defer res.Body.Close()
	// 只读到上限，异常页面不能灌满内存。
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	// 响应读不完就当不可达，避免用半截正文。
	if err != nil {
		return domain.ErrWANUnreachable
	}
	// 平台拒绝就把状态收成业务错误。
	if res.StatusCode >= 300 {
		// 按状态和英文原因交回，调用方不要当成功。
		return mapHTTPStatus(res.StatusCode, body)
	}
	// 调用方不要正文就到此为止。
	if dst == nil {
		return nil
	}
	// 正文解不开就当不可达，避免用残缺回执。
	if err := json.Unmarshal(body, dst); err != nil {
		return domain.ErrWANUnreachable
	}
	return nil
}

// 带厂钥签名 POST JSON 正文。
func (p *puller) postJSON(ctx context.Context, path string, payload, dst any) error {
	// 编出正文，编不出就不发，避免残缺上送。
	raw, err := json.Marshal(payload)
	// 编不出就当不可达，调用方不要当已送出。
	if err != nil {
		return domain.ErrWANUnreachable
	}
	// 带上取消和正文来组请求，超时才能停掉。
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.wanHTTP+path, bytes.NewReader(raw))
	// 请求组不出来就当平台不可达。
	if err != nil {
		return domain.ErrWANUnreachable
	}
	// 标明是结构化正文，平台才按字段解析。
	req.Header.Set("Content-Type", "application/json")
	// 补上厂钥签名，没有签名平台会拒绝。
	p.sign(req)
	// 送出上送，网络失败就当平台不可达。
	res, err := p.client.Do(req)
	// 送不出去就当不可达，调用方可以再试。
	if err != nil {
		return domain.ErrWANUnreachable
	}
	// 读完就关掉响应，连接才能复用。
	defer res.Body.Close()
	// 只读到上限，异常页面不能灌满内存。
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	// 响应读不完就当不可达，避免用半截回执。
	if err != nil {
		return domain.ErrWANUnreachable
	}
	// 平台拒绝就把状态收成业务错误。
	if res.StatusCode >= 300 {
		// 按状态和英文原因交回，调用方不要当成功。
		return mapHTTPStatus(res.StatusCode, body)
	}
	// 调用方不要正文就到此为止。
	if dst == nil {
		return nil
	}
	// 回执解不开就当不可达，避免当成已收下。
	if err := json.Unmarshal(body, dst); err != nil {
		return domain.ErrWANUnreachable
	}
	return nil
}

// 写时间窗签名头。
func (p *puller) sign(req *http.Request) {
	// 取当前时间做签名窗，过期会被拒绝。
	unix := time.Now().Unix()
	// 用签发私钥签这次请求，平台据此认厂。
	sig := nodekey.Sign(p.priv, nodekey.MQTTConnectPayload(p.factoryID, unix))
	// 写上工厂身份，平台才知道是哪一家。
	req.Header.Set("X-WMesh-Factory", p.factoryID.String())
	// 写上时间窗，过期签名会被拒绝。
	req.Header.Set("X-WMesh-Time", strconv.FormatInt(unix, 10))
	// 写上签名，对不上就拒绝这次拉取。
	req.Header.Set("X-WMesh-Sign", base64.StdEncoding.EncodeToString(sig))
}

// 把 WAN HTTP 错误收回本侧业务错误。
func mapHTTPStatus(code int, body []byte) error {
	// 预备装平台英文原因，没有就按状态码收。
	var wrap struct {
		Error string `json:"error"` // 平台返回的英文错误
	}
	// 解不开也不中断，后面按状态码收成错误。
	_ = json.Unmarshal(body, &wrap)
	// 平台给了英文原因就收成对应业务错误。
	if wrap.Error != "" {
		// 英文原因再收成业务错误，调用方不用猜字符串。
		return mapChannelError(wrap.Error)
	}
	// 未授权收成未登录，调用方不要当网络抖动。
	if code == http.StatusUnauthorized {
		return domain.ErrUnauthorized
	}
	// 被禁止就当无权，调用方不要重试同一请求。
	if code == http.StatusForbidden {
		return domain.ErrForbidden
	}
	// 找不到就当没有这份资源。
	if code == http.StatusNotFound {
		return domain.ErrNotFound
	}
	// 其余状态交回数字，调用方不要当成功。
	return fmt.Errorf("wan http %d", code)
}

// 平台当前指令清单，不含大正文。
type indexResp struct {
	Cmds []Cmd `json:"cmds"` // 当前指令清单
}

// 平台发来的解包钥和到期时间。
type leaseResp struct {
	Typ      string `json:"typ"`      // lease
	Lease    []byte `json:"lease"`    // 租约钥
	NotAfter string `json:"notAfter"` // 到期 RFC3339
}
