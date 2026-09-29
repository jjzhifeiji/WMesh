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

// 建厂引导只带工厂身份和初始超管称呼。
type bootReq struct {
	FactoryID string `json:"factoryId"` // 工厂稳定身份
	SALogin   string `json:"saLogin"`   // 初始超管登录名
	SADisplay string `json:"saDisplay"` // 初始超管显示名
}

// 厂端回的账号和一次性激活码，失败时带英文原因。
type bootResp struct {
	PersonID        string `json:"personId"`        // 厂库里的账号身份
	ActivationToken string `json:"activationToken"` // 一次性 8 位激活码，禁止写入 WAN
	Error           string `json:"error"`           // 厂端返回的英文错误
}

// 厂端响应体上限，防止异常网关把整页 HTML 灌进来。
const maxBody = 64 << 10

// Bootstrap 在目标厂库写入待启用初始超管，激活码只带回调用方；任何失败都收成 ErrFactoryBootstrap。
func (c *Client) Bootstrap(ctx context.Context, factoryID uuid.UUID, saLogin, saDisplay string) (uuid.UUID, string, error) {
	// 优先用调用方给的客户端。
	httpClient := c.HTTP
	// 没指定就换短超时，避免引导一直挂着。
	if httpClient == nil {
		// 建厂引导用短超时，不跟拉包共用长等待。
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	// 编出引导正文，编不出就不发请求。
	body, err := json.Marshal(bootReq{FactoryID: factoryID.String(), SALogin: saLogin, SADisplay: saDisplay})
	// 正文编不出就交回，避免发出残缺引导。
	if err != nil {
		return uuid.Nil, "", err
	}
	// 拼上引导路径，避免双斜杠打到错误地址。
	url := strings.TrimRight(c.BaseURL, "/") + "/internal/bootstrap"
	// 带上取消来组请求，超时才能停掉。
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	// 请求组不出来就交回，这次引导不做。
	if err != nil {
		return uuid.Nil, "", err
	}
	// 标明是结构化正文，厂端才按字段解析。
	req.Header.Set("Content-Type", "application/json")
	// 带上共享口令，对不上厂端会拒绝。
	req.Header.Set("Authorization", "Bearer "+c.Token)
	// 送出引导，网络失败不能当成已经建成。
	res, err := httpClient.Do(req)
	// 送不出去就收成引导失败，调用方可以再试。
	if err != nil {
		// 网络失败收成引导错误，不要当成别的业务失败。
		return uuid.Nil, "", fmt.Errorf("%w: %v", domain.ErrFactoryBootstrap, err)
	}
	// 读完就关掉响应，连接才能复用。
	defer res.Body.Close()
	// 只读到上限，异常页面不能灌满内存。
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxBody))
	// 响应读不完就拒绝，避免用半截正文。
	if err != nil {
		// 读失败收成引导错误，不把半截响应当成激活码。
		return uuid.Nil, "", fmt.Errorf("%w: read response: %v", domain.ErrFactoryBootstrap, err)
	}
	// 预备装厂端回执，失败时也能取出英文原因。
	var out bootResp
	// 成功状态却解不开就拒绝，避免垃圾当成激活码。
	if jsonErr := json.Unmarshal(raw, &out); jsonErr != nil && res.StatusCode < 300 {
		// 正文损坏收成引导失败，不继续解析账号。
		return uuid.Nil, "", fmt.Errorf("%w: bad response body", domain.ErrFactoryBootstrap)
	}
	// 厂端拒绝就带上状态交回，不把失败当成已建好。
	if res.StatusCode >= 300 {
		// 有英文原因就一并交回，便于对照厂端日志。
		if out.Error != "" {
			// 把厂端原因收成引导失败，调用方不要当成功。
			return uuid.Nil, "", fmt.Errorf("%w: http %d: %s", domain.ErrFactoryBootstrap, res.StatusCode, out.Error)
		}
		// 没给原因也按引导失败交回，避免误当建成。
		return uuid.Nil, "", fmt.Errorf("%w: http %d", domain.ErrFactoryBootstrap, res.StatusCode)
	}
	// 账号身份不合法就拒绝，不能把坏编号当超管。
	personID, err := uuid.Parse(out.PersonID)
	// 编号解析失败就拒绝这次引导。
	if err != nil {
		// 坏编号收成引导失败，不把空身份交回去。
		return uuid.Nil, "", fmt.Errorf("%w: bad person id", domain.ErrFactoryBootstrap)
	}
	// 没有激活码就拒绝，否则超管无法启用。
	if out.ActivationToken == "" {
		// 缺激活码收成引导失败，调用方无法启用账号。
		return uuid.Nil, "", fmt.Errorf("%w: missing activation token", domain.ErrFactoryBootstrap)
	}
	return personID, out.ActivationToken, nil
}
