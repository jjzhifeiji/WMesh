package dockerupdate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const apiPrefix = "/v1.44"

// client 调本机 Docker Engine；不碰业务库。
type client struct {
	http *http.Client
	base string
}

// newUnixClient 经 unix socket 访问 Docker。
func newUnixClient(sock string) *client {
	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			d := net.Dialer{Timeout: 5 * time.Second}
			return d.DialContext(ctx, "unix", sock)
		},
	}
	return &client{
		http: &http.Client{Transport: tr, Timeout: 15 * time.Minute},
		base: "http://docker",
	}
}

// newHTTPClient 给测试接假 Docker。
func newHTTPClient(base string) *client {
	return &client{http: &http.Client{Timeout: 15 * time.Second}, base: strings.TrimRight(base, "/")}
}

type streamLine struct {
	Stream string `json:"stream"`
	Error  string `json:"error"`
}

type inspectResp struct {
	ID     string `json:"Id"`
	Name   string `json:"Name"`
	Config struct {
		User         string              `json:"User"`
		Env          []string            `json:"Env"`
		Cmd          []string            `json:"Cmd"`
		Image        string              `json:"Image"`
		Labels       map[string]string   `json:"Labels"`
		Entrypoint   []string            `json:"Entrypoint"`
		WorkingDir   string              `json:"WorkingDir"`
		ExposedPorts map[string]struct{} `json:"ExposedPorts"`
	} `json:"Config"`
	HostConfig      json.RawMessage `json:"HostConfig"`
	NetworkSettings netSettings     `json:"NetworkSettings"`
	Mounts          []mountPoint    `json:"Mounts"`
}

type mountPoint struct {
	Type        string `json:"Type"`        // volume / bind
	Name        string `json:"Name"`        // 命名卷
	Source      string `json:"Source"`      // 宿主机路径
	Destination string `json:"Destination"` // 容器内路径
}

type netSettings struct {
	Networks map[string]json.RawMessage `json:"Networks"`
}

type createResp struct {
	ID string `json:"Id"`
}

// loadImages 把 tar 流交给 Docker；返回已载入的镜像名或 ID。
func (c *client) loadImages(ctx context.Context, tar io.Reader) ([]string, error) {
	res, err := c.do(ctx, http.MethodPost, apiPrefix+"/images/load", "application/x-tar", tar)
	if err != nil {
		return nil, err
	}
	defer res.Close()
	return parseLoaded(res)
}

// inspect 读容器配置，用来克隆本 app 容器。
func (c *client) inspect(ctx context.Context, id string) (inspectResp, error) {
	res, err := c.do(ctx, http.MethodGet, apiPrefix+"/containers/"+url.PathEscape(id)+"/json", "", nil)
	if err != nil {
		return inspectResp{}, err
	}
	defer res.Close()
	var out inspectResp
	if err := json.NewDecoder(res).Decode(&out); err != nil {
		return inspectResp{}, err
	}
	return out, nil
}

// create 新建容器，返回 ID。
func (c *client) create(ctx context.Context, name string, body any) (string, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	path := apiPrefix + "/containers/create?name=" + url.QueryEscape(name)
	res, err := c.do(ctx, http.MethodPost, path, "application/json", bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	defer res.Close()
	var out createResp
	if err := json.NewDecoder(res).Decode(&out); err != nil {
		return "", err
	}
	if out.ID == "" {
		return "", fmt.Errorf("docker create returned empty id")
	}
	return out.ID, nil
}

// start 启动已创建的容器。
func (c *client) start(ctx context.Context, id string) error {
	res, err := c.do(ctx, http.MethodPost, apiPrefix+"/containers/"+url.PathEscape(id)+"/start", "", nil)
	if err != nil {
		return err
	}
	return res.Close()
}

// stop 先停再换，避免两个 app 抢同一宿主机端口。
func (c *client) stop(ctx context.Context, id string) error {
	res, err := c.do(ctx, http.MethodPost, apiPrefix+"/containers/"+url.PathEscape(id)+"/stop?t=15", "", nil)
	if err != nil {
		return err
	}
	return res.Close()
}

// rename 把新容器改回原来的名字，方便 compose 认。
func (c *client) rename(ctx context.Context, id, name string) error {
	path := apiPrefix + "/containers/" + url.PathEscape(id) + "/rename?name=" + url.QueryEscape(name)
	res, err := c.do(ctx, http.MethodPost, path, "", nil)
	if err != nil {
		return err
	}
	return res.Close()
}

// remove 删掉旧容器或上次失败留下的 next。
func (c *client) remove(ctx context.Context, id string) error {
	res, err := c.do(ctx, http.MethodDelete, apiPrefix+"/containers/"+url.PathEscape(id)+"?force=true", "", nil)
	if err != nil {
		return err
	}
	return res.Close()
}

// removeIfExists 没有则忽略，避免换包前清残留失败。
func (c *client) removeIfExists(ctx context.Context, id string) error {
	err := c.remove(ctx, id)
	if err == nil || isNotFound(err) {
		return nil
	}
	return err
}

type statusError struct {
	code int
	msg  string
}

// Error 把 Docker 非 2xx 正文原样带回调用方。
func (e statusError) Error() string { return e.msg }

// isNotFound 容器已经没了就当成功，换包半途可重试。
func isNotFound(err error) bool {
	var se statusError
	return errors.As(err, &se) && se.code == http.StatusNotFound
}

// do 发一条 Docker API；非 2xx 把正文当错误。
func (c *client) do(ctx context.Context, method, path, contentType string, body io.Reader) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		msg := strings.TrimSpace(string(b))
		if msg == "" {
			msg = resp.Status
		}
		return nil, statusError{code: resp.StatusCode, msg: msg}
	}
	if resp.StatusCode == http.StatusNoContent || resp.Body == nil {
		return io.NopCloser(bytes.NewReader(nil)), nil
	}
	return resp.Body, nil
}

// parseLoaded 从 docker load 的 JSON 流取出镜像名。
func parseLoaded(r io.Reader) ([]string, error) {
	dec := json.NewDecoder(r)
	var refs []string
	for {
		var line streamLine
		if err := dec.Decode(&line); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		if line.Error != "" {
			return nil, fmt.Errorf("%s", line.Error)
		}
		s := strings.TrimSpace(line.Stream)
		switch {
		case strings.HasPrefix(s, "Loaded image ID:"):
			refs = append(refs, strings.TrimSpace(strings.TrimPrefix(s, "Loaded image ID:")))
		case strings.HasPrefix(s, "Loaded image:"):
			refs = append(refs, strings.TrimSpace(strings.TrimPrefix(s, "Loaded image:")))
		}
	}
	return refs, nil
}
