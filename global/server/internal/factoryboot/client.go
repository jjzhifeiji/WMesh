// Package factoryboot 用 HTTP 通知厂端写入初始超管。
// 只认工厂稳定身份，不 import 厂内包，也不把密码存进 WAN。
package factoryboot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/domain"
)

// Client 把建厂引导发到厂端 /internal/bootstrap。
type Client struct {
	BaseURL string       // 厂端 HTTP 根地址
	Token   string       // 建厂引导共享密码
	HTTP    *http.Client // 空则用默认 30 秒超时
}

type bootReq struct {
	FactoryID string `json:"factoryId"` // 工厂稳定身份
	SALogin   string `json:"saLogin"`   // 初始超管登录名
	SADisplay string `json:"saDisplay"` // 初始超管显示名
}

type bootResp struct {
	PersonID        string `json:"personId"`        // 厂库里的账号身份
	ActivationToken string `json:"activationToken"` // 一次性 8 位激活码，禁止写入 WAN
	Error           string `json:"error"`           // 厂端返回的英文错误
}

// 厂端响应体上限，防止异常网关把整页 HTML 灌进来。
const maxBody = 64 << 10

// Bootstrap 在目标厂库写入待启用初始超管，激活码只带回调用方；任何失败都收成 ErrFactoryBootstrap。
func (c *Client) Bootstrap(ctx context.Context, factoryID uuid.UUID, saLogin, saDisplay string) (uuid.UUID, string, error) {
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	body, err := json.Marshal(bootReq{FactoryID: factoryID.String(), SALogin: saLogin, SADisplay: saDisplay})
	if err != nil {
		return uuid.Nil, "", err
	}
	url := strings.TrimRight(c.BaseURL, "/") + "/internal/bootstrap"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return uuid.Nil, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.Token)
	res, err := httpClient.Do(req)
	if err != nil {
		return uuid.Nil, "", fmt.Errorf("%w: %v", domain.ErrFactoryBootstrap, err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxBody))
	if err != nil {
		return uuid.Nil, "", fmt.Errorf("%w: read response: %v", domain.ErrFactoryBootstrap, err)
	}
	var out bootResp
	if jsonErr := json.Unmarshal(raw, &out); jsonErr != nil && res.StatusCode < 300 {
		return uuid.Nil, "", fmt.Errorf("%w: bad response body", domain.ErrFactoryBootstrap)
	}
	if res.StatusCode >= 300 {
		if out.Error != "" {
			return uuid.Nil, "", fmt.Errorf("%w: http %d: %s", domain.ErrFactoryBootstrap, res.StatusCode, out.Error)
		}
		return uuid.Nil, "", fmt.Errorf("%w: http %d", domain.ErrFactoryBootstrap, res.StatusCode)
	}
	personID, err := uuid.Parse(out.PersonID)
	if err != nil {
		return uuid.Nil, "", fmt.Errorf("%w: bad person id", domain.ErrFactoryBootstrap)
	}
	if out.ActivationToken == "" {
		return uuid.Nil, "", fmt.Errorf("%w: missing activation token", domain.ErrFactoryBootstrap)
	}
	return personID, out.ActivationToken, nil
}
