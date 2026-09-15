package wanchannel

import (
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

const maxPullBytes = 64 << 20 // 软件包拉回上限，与 WAN 上传一致

type puller struct {
	wanHTTP   string
	factoryID uuid.UUID
	priv      []byte
	client    *http.Client
}

// 给这家厂签 HTTPS 拉正文。
func newPuller(wanHTTP string, factoryID uuid.UUID, priv []byte) *puller {
	return &puller{
		wanHTTP:   strings.TrimRight(strings.TrimSpace(wanHTTP), "/"),
		factoryID: factoryID,
		priv:      priv,
		client:    &http.Client{Timeout: 120 * time.Second},
	}
}

// 带厂钥签名 GET JSON。
func (p *puller) get(ctx context.Context, path string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.wanHTTP+path, nil)
	if err != nil {
		return domain.ErrWANUnreachable
	}
	p.sign(req)
	res, err := p.client.Do(req)
	if err != nil {
		return domain.ErrWANUnreachable
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, maxPullBytes+1))
	if err != nil {
		return domain.ErrWANUnreachable
	}
	if int64(len(body)) > maxPullBytes {
		return domain.ErrIntegrity
	}
	if res.StatusCode >= 300 {
		return mapHTTPStatus(res.StatusCode, body)
	}
	if dst == nil {
		return nil
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return domain.ErrWANUnreachable
	}
	return nil
}

// 带厂钥签名 POST JSON。
func (p *puller) post(ctx context.Context, path string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.wanHTTP+path, nil)
	if err != nil {
		return domain.ErrWANUnreachable
	}
	p.sign(req)
	res, err := p.client.Do(req)
	if err != nil {
		return domain.ErrWANUnreachable
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return domain.ErrWANUnreachable
	}
	if res.StatusCode >= 300 {
		return mapHTTPStatus(res.StatusCode, body)
	}
	if dst == nil {
		return nil
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return domain.ErrWANUnreachable
	}
	return nil
}

// 写时间窗签名头。
func (p *puller) sign(req *http.Request) {
	unix := time.Now().Unix()
	sig := nodekey.Sign(p.priv, nodekey.MQTTConnectPayload(p.factoryID, unix))
	req.Header.Set("X-WMesh-Factory", p.factoryID.String())
	req.Header.Set("X-WMesh-Time", strconv.FormatInt(unix, 10))
	req.Header.Set("X-WMesh-Sign", base64.StdEncoding.EncodeToString(sig))
}

// 把 WAN HTTP 错误收回本侧业务错误。
func mapHTTPStatus(code int, body []byte) error {
	var wrap struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(body, &wrap)
	if wrap.Error != "" {
		return mapChannelError(wrap.Error)
	}
	if code == http.StatusUnauthorized {
		return domain.ErrUnauthorized
	}
	if code == http.StatusForbidden {
		return domain.ErrForbidden
	}
	if code == http.StatusNotFound {
		return domain.ErrNotFound
	}
	return fmt.Errorf("wan http %d", code)
}

type indexResp struct {
	Cmds []Cmd `json:"cmds"` // 当前指令清单
}

type leaseResp struct {
	Typ      string `json:"typ"`      // lease
	Lease    []byte `json:"lease"`     // 租约钥
	NotAfter string `json:"notAfter"` // 到期 RFC3339
}
