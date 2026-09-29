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

// 引擎接口版本，和假服务走同一条路径。
const apiPrefix = "/v1.44"

// client 调本机 Docker Engine；不碰业务库。
type client struct {
	http *http.Client // 访问引擎用的客户端，测试里换成假服务。
	base string       // 接口根地址，套接字模式只用来占位。
}

// newUnixClient 经 unix socket 访问 Docker。
func newUnixClient(sock string) *client {
	// 定下传输，再交给后面。
	tr := &http.Transport{
		// 经本机套接字连引擎，不走普通网络。
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			// 定下目录值，再交给后面。
			d := net.Dialer{Timeout: 5 * time.Second}
			// 交回在时限内拨到套接字的结果。
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
	// 交回去掉末尾多余的斜杠的结果。
	return &client{http: &http.Client{Timeout: 15 * time.Second}, base: strings.TrimRight(base, "/")}
}

// 载入镜像时引擎吐出的一行结果。
type streamLine struct {
	Stream string `json:"stream"` // 载入进度里的一行说明。
	Error  string `json:"error"`  // 引擎报错，非空则整段载入作废。
}

// 引擎返回的容器详情，用来克隆。
type inspectResp struct {
	ID     string   `json:"Id"`   // 容器身份，换包和改名都靠它。
	Name   string   `json:"Name"` // 容器名字，换完要改回这个。
	Config struct { // 启动配置，克隆新容器时要抄走。
		User         string              `json:"User"`         // 运行用户，换容器时原样带走。
		Env          []string            `json:"Env"`          // 环境变量，换容器时原样带走。
		Cmd          []string            `json:"Cmd"`          // 启动命令，换容器时原样带走。
		Image        string              `json:"Image"`        // 镜像引用，指出容器要跑哪一份。
		Labels       map[string]string   `json:"Labels"`       // 容器标签，换容器时原样带走。
		Entrypoint   []string            `json:"Entrypoint"`   // 入口命令，换容器时原样带走。
		WorkingDir   string              `json:"WorkingDir"`   // 工作目录，换容器时原样带走。
		ExposedPorts map[string]struct{} `json:"ExposedPorts"` // 声明要暴露的端口，换容器时带走。
	} `json:"Config"`
	HostConfig      json.RawMessage `json:"HostConfig"`      // 宿主侧配置原文，含端口和卷。
	NetworkSettings netSettings     `json:"NetworkSettings"` // 接到哪些网络，克隆时按名接回。
	Mounts          []mountPoint    `json:"Mounts"`          // 已经挂上的卷和目录。
}

// 容器上的一块挂载，类型和路径分开记。
type mountPoint struct {
	Type        string `json:"Type"`        // volume / bind
	Name        string `json:"Name"`        // 命名卷
	Source      string `json:"Source"`      // 宿主机路径
	Destination string `json:"Destination"` // 容器内路径
}

// 容器接到哪些网络，克隆时按名接回。
type netSettings struct {
	Networks map[string]json.RawMessage `json:"Networks"` // 网络名到接入参数，克隆时按名接回。
}

// 新建容器之后引擎返回的身份。
type createResp struct {
	ID string `json:"Id"` // 容器身份，换包和改名都靠它。
}

// loadImages 把 tar 流交给 Docker；返回已载入的镜像名或 ID。
func (c *client) loadImages(ctx context.Context, tar io.Reader) ([]string, error) {
	// 向引擎发出这一条请求。
	res, err := c.do(ctx, http.MethodPost, apiPrefix+"/images/load", "application/x-tar", tar)
	// 没能向引擎发出这一条请求就停，避免带着残缺继续。
	if err != nil {
		return nil, err
	}
	// 离开时关掉，避免句柄或连接漏掉。
	defer res.Close()
	// 交回解析载入内容，再交给后面的结果。
	return parseLoaded(res)
}

// inspect 读容器配置，用来克隆本 app 容器。
func (c *client) inspect(ctx context.Context, id string) (inspectResp, error) {
	// 把身份编进路径，避免被斜杠拆开。
	res, err := c.do(ctx, http.MethodGet, apiPrefix+"/containers/"+url.PathEscape(id)+"/json", "", nil)
	// 没能把身份编进路径，避免被斜杠拆开就停，避免带着残缺继续。
	if err != nil {
		return inspectResp{}, err
	}
	// 离开时关掉，避免句柄或连接漏掉。
	defer res.Close()
	// 准备承接容器详情。
	var out inspectResp
	// 没能从输入里解出结构就停，避免带着残缺继续。
	if err := json.NewDecoder(res).Decode(&out); err != nil {
		return inspectResp{}, err
	}
	return out, nil
}

// create 新建容器，返回 ID。
func (c *client) create(ctx context.Context, name string, body any) (string, error) {
	// 把结构收成字节，再交给后面。
	raw, err := json.Marshal(body)
	// 没能把结构收成字节就停，避免带着残缺继续。
	if err != nil {
		return "", err
	}
	// 把名字编进查询，避免被拆开。
	path := apiPrefix + "/containers/create?name=" + url.QueryEscape(name)
	// 把内存里的字节包成可读流。
	res, err := c.do(ctx, http.MethodPost, path, "application/json", bytes.NewReader(raw))
	// 没能把内存里的字节包成可读流就停，避免带着残缺继续。
	if err != nil {
		return "", err
	}
	// 离开时关掉，避免句柄或连接漏掉。
	defer res.Close()
	// 准备放下输出，再交给后面。
	var out createResp
	// 没能从输入里解出结构就停，避免带着残缺继续。
	if err := json.NewDecoder(res).Decode(&out); err != nil {
		return "", err
	}
	// 是空的就改用默认，或按没有处理。
	if out.ID == "" {
		// 带上原因交回去，调用方才能知道为何停下。
		return "", fmt.Errorf("docker create returned empty id")
	}
	return out.ID, nil
}

// start 启动已创建的容器。
func (c *client) start(ctx context.Context, id string) error {
	// 把身份编进路径，避免被斜杠拆开。
	res, err := c.do(ctx, http.MethodPost, apiPrefix+"/containers/"+url.PathEscape(id)+"/start", "", nil)
	// 没能把身份编进路径，避免被斜杠拆开就停，避免带着残缺继续。
	if err != nil {
		return err
	}
	// 关掉并把关闭时的错误一起交回去。
	return res.Close()
}

// stop 先停再换，避免两个 app 抢同一宿主机端口。
func (c *client) stop(ctx context.Context, id string) error {
	// 把身份编进路径，避免被斜杠拆开。
	res, err := c.do(ctx, http.MethodPost, apiPrefix+"/containers/"+url.PathEscape(id)+"/stop?t=15", "", nil)
	// 没能把身份编进路径，避免被斜杠拆开就停，避免带着残缺继续。
	if err != nil {
		return err
	}
	// 关掉并把关闭时的错误一起交回去。
	return res.Close()
}

// rename 把新容器改回原来的名字，方便 compose 认。
func (c *client) rename(ctx context.Context, id, name string) error {
	// 把名字编进查询，避免被拆开。
	path := apiPrefix + "/containers/" + url.PathEscape(id) + "/rename?name=" + url.QueryEscape(name)
	// 向引擎发出这一条请求。
	res, err := c.do(ctx, http.MethodPost, path, "", nil)
	// 没能向引擎发出这一条请求就停，避免带着残缺继续。
	if err != nil {
		return err
	}
	// 关掉并把关闭时的错误一起交回去。
	return res.Close()
}

// remove 删掉旧容器或上次失败留下的 next。
func (c *client) remove(ctx context.Context, id string) error {
	// 把身份编进路径，避免被斜杠拆开。
	res, err := c.do(ctx, http.MethodDelete, apiPrefix+"/containers/"+url.PathEscape(id)+"?force=true", "", nil)
	// 没能把身份编进路径，避免被斜杠拆开就停，避免带着残缺继续。
	if err != nil {
		return err
	}
	// 关掉并把关闭时的错误一起交回去。
	return res.Close()
}

// removeIfExists 没有则忽略，避免换包前清残留失败。
func (c *client) removeIfExists(ctx context.Context, id string) error {
	// 删除，再交给后面，再交给后面。
	err := c.remove(ctx, id)
	// 没有出错就按成功返回，不用再补救。
	if err == nil || isNotFound(err) {
		return nil
	}
	return err
}

// 引擎非成功响应，带状态码和说明。
type statusError struct {
	code int    // 引擎返回的状态码，用来区分找不到。
	msg  string // 引擎返回的说明，原样交回调用方。
}

// Error 把 Docker 非 2xx 正文原样带回调用方。
func (e statusError) Error() string { return e.msg }

// isNotFound 容器已经没了就当成功，换包半途可重试。
func isNotFound(err error) bool {
	// 准备承接引擎返回的状态错误。
	var se statusError
	// 交回收成具体那一种错误的结果。
	return errors.As(err, &se) && se.code == http.StatusNotFound
}

// do 发一条 Docker API；非 2xx 把正文当错误。
func (c *client) do(ctx context.Context, method, path, contentType string, body io.Reader) (io.ReadCloser, error) {
	// 组好一条可以取消的请求。
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	// 没能组好一条可以取消的请求就停，避免带着残缺继续。
	if err != nil {
		return nil, err
	}
	// 调用方指定了类型才写到头上。
	if contentType != "" {
		// 把这个头或标志写上。
		req.Header.Set("Content-Type", contentType)
	}
	// 把组好的请求发出去。
	resp, err := c.http.Do(req)
	// 没能把组好的请求发出去就停，避免带着残缺继续。
	if err != nil {
		return nil, err
	}
	// 不是成功状态就把正文当成错误。
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// 把流里的字节全部读完。
		b, _ := io.ReadAll(resp.Body)
		// 关掉响应体，避免句柄漏掉。
		_ = resp.Body.Close()
		// 去掉两头空白再使用。
		msg := strings.TrimSpace(string(b))
		// 错误说明是空的就改用状态文本。
		if msg == "" {
			// 定下待签原文，再交给后面。
			msg = resp.Status
		}
		return nil, statusError{code: resp.StatusCode, msg: msg}
	}
	// 没有正文就交回一个空的读口。
	if resp.StatusCode == http.StatusNoContent || resp.Body == nil {
		// 交回把内存里的字节包成可读流的结果。
		return io.NopCloser(bytes.NewReader(nil)), nil
	}
	return resp.Body, nil
}

// parseLoaded 从 docker load 的 JSON 流取出镜像名。
func parseLoaded(r io.Reader) ([]string, error) {
	// 准备按行读取载入输出。
	dec := json.NewDecoder(r)
	// 准备收集字符串结果。
	var refs []string
	// 一直看下去，直到就绪、失败或被取消。
	for {
		// 准备承接载入输出的一行。
		var line streamLine
		// 没能从输入里解出结构就停，避免带着残缺继续。
		if err := dec.Decode(&line); err != nil {
			// 读到结尾就正常结束，不当成失败。
			if err == io.EOF {
				break
			}
			return nil, err
		}
		// 这一行报了错，整段载入作废。
		if line.Error != "" {
			// 带上原因交回去，调用方才能知道为何停下。
			return nil, fmt.Errorf("%s", line.Error)
		}
		// 去掉两头空白再使用。
		s := strings.TrimSpace(line.Stream)
		// 看当前字符落在哪一种片段里，分号才好切。
		switch {
		// 这一行是镜像身份，摘出来收集。
		case strings.HasPrefix(s, "Loaded image ID:"):
			// 把这一段接进结果，顺序要保持住。
			refs = append(refs, strings.TrimSpace(strings.TrimPrefix(s, "Loaded image ID:")))
		// 这一行是镜像名字，摘出来收集。
		case strings.HasPrefix(s, "Loaded image:"):
			// 把这一段接进结果，顺序要保持住。
			refs = append(refs, strings.TrimSpace(strings.TrimPrefix(s, "Loaded image:")))
		}
	}
	return refs, nil
}
