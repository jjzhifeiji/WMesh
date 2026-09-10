// Package factoryboot 用 HTTP 通知厂端写入初始超管。
// 只认工厂稳定身份，不 import 厂内包，也不把口令存进 WAN。
package factoryboot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Client 把建厂引导发到厂端 /internal/bootstrap。
type Client struct {
	BaseURL string       // 厂端 HTTP 根地址
	Token   string       // 建厂引导共享口令
	HTTP    *http.Client // 空则用默认 30 秒超时
}

type bootReq struct {
	FactoryID string `json:"factoryId"` // 工厂稳定身份
	SALogin   string `json:"saLogin"`   // 初始超管登录名
	SADisplay string `json:"saDisplay"` // 初始超管显示名
}

type bootResp struct {
	PersonID        string `json:"personId"`        // 厂库里的账号身份
	ActivationToken string `json:"activationToken"` // 一次性激活口令，禁止写入 WAN
	Error           string `json:"error"`
}

// Bootstrap 在目标厂库写入待启用初始超管，激活口令只带回调用方。
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
		return uuid.Nil, "", err
	}
	defer res.Body.Close()
	var out bootResp
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return uuid.Nil, "", err
	}
	if res.StatusCode >= 300 {
		if out.Error != "" {
			return uuid.Nil, "", fmt.Errorf("%s", out.Error)
		}
		return uuid.Nil, "", fmt.Errorf("factory bootstrap http %d", res.StatusCode)
	}
	personID, err := uuid.Parse(out.PersonID)
	if err != nil {
		return uuid.Nil, "", err
	}
	if out.ActivationToken == "" {
		return uuid.Nil, "", fmt.Errorf("factory bootstrap missing activation token")
	}
	return personID, out.ActivationToken, nil
}
