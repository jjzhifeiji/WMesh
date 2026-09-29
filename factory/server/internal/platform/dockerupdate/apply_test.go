// 厂服务 tar 由帮手 docker load；API 只写包并等待就绪。夹带的 postgres 不重建库。
package dockerupdate

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// 测试里冒充引擎，把调用记在内存里。
type fakeDocker struct {
	mu      sync.Mutex  // 挡住并发，避免两路同时改这份状态。
	inspect inspectResp // 检查接口要回给调用方的容器配置。
	loaded  []byte      // 测试里收到的镜像包原文，本不该有。
	created []created   // 记下新建过的容器名字和参数。
	started []string    // 记下启动过的容器身份，用来对顺序。
	stopped []string    // 记下停止过的容器身份，用来对顺序。
	removed []string    // 记下删除过的容器身份，用来对顺序。
	renamed []string    // 记下改过名的容器身份，用来对结果。
}

// 一次新建时的名字和交给引擎的参数。
type created struct {
	Name string     // 新建时临时起的名字，用来核对。
	Body createBody // 提交给引擎的创建参数，含镜像和命令。
}

// 把假引擎各条接口接到内存记录上。
func (f *fakeDocker) handler() http.Handler {
	// 建一个分发器来挂假接口。
	mux := http.NewServeMux()
	// 收下镜像包并记下来，假装已经载入。
	mux.HandleFunc("POST /v1.44/images/load", func(w http.ResponseWriter, r *http.Request) {
		// 把流里的字节全部读完。
		b, _ := io.ReadAll(r.Body)
		// 先占住锁，避免并发写乱。
		f.mu.Lock()
		// 记下收到的镜像包，用来核对有没有误载。
		f.loaded = b
		// 放开锁，别挡住后面的人。
		f.mu.Unlock()
		// 把结构写进响应，再交给后面。
		_ = json.NewEncoder(w).Encode(streamLine{Stream: "Loaded image: postgres:18-alpine\n"})
		// 把结构写进响应，再交给后面。
		_ = json.NewEncoder(w).Encode(streamLine{Stream: "Loaded image: wmesh-factory-app:v2\n"})
	})
	// 按问到的容器回一份检查结果。
	mux.HandleFunc("GET /v1.44/containers/{id}/json", func(w http.ResponseWriter, r *http.Request) {
		// 从路径里取出容器身份。
		id := r.PathValue("id")
		// 先占住锁，避免并发写乱。
		f.mu.Lock()
		// 定下当前容器，再交给后面。
		me := f.inspect
		// 放开锁，别挡住后面的人。
		f.mu.Unlock()
		// 不是当前这个容器，还要再对一下名字。
		if id != me.ID && id != strings.TrimPrefix(me.Name, "/") {
			// 先写下响应状态码。
			w.WriteHeader(http.StatusNotFound)
			// 准备一份测试或签名用的字节。
			_, _ = w.Write([]byte(`{"message":"not found"}`))
			return
		}
		// 把结构写进响应，再交给后面。
		_ = json.NewEncoder(w).Encode(me)
	})
	// 记下新建请求，回一个假的容器身份。
	mux.HandleFunc("POST /v1.44/containers/create", func(w http.ResponseWriter, r *http.Request) {
		// 从地址查询里把参数取出来。
		name := r.URL.Query().Get("name")
		// 准备放下正文，再交给后面。
		var body createBody
		// 从输入里解出结构。
		_ = json.NewDecoder(r.Body).Decode(&body)
		// 定下身份，再交给后面。
		id := "id-" + name
		// 先占住锁，避免并发写乱。
		f.mu.Lock()
		// 把这一段接进结果，顺序要保持住。
		f.created = append(f.created, created{Name: name, Body: body})
		// 放开锁，别挡住后面的人。
		f.mu.Unlock()
		// 先写下响应状态码。
		w.WriteHeader(http.StatusCreated)
		// 把结构写进响应，再交给后面。
		_ = json.NewEncoder(w).Encode(createResp{ID: id})
	})
	// 记下启动了哪一个，并不真的跑起来。
	mux.HandleFunc("POST /v1.44/containers/{id}/start", func(w http.ResponseWriter, r *http.Request) {
		// 先占住锁，避免并发写乱。
		f.mu.Lock()
		// 把这一段接进结果，顺序要保持住。
		f.started = append(f.started, r.PathValue("id"))
		// 放开锁，别挡住后面的人。
		f.mu.Unlock()
		// 先写下响应状态码。
		w.WriteHeader(http.StatusNoContent)
	})
	// 记下停了哪一个，用来核对顺序。
	mux.HandleFunc("POST /v1.44/containers/{id}/stop", func(w http.ResponseWriter, r *http.Request) {
		// 先占住锁，避免并发写乱。
		f.mu.Lock()
		// 把这一段接进结果，顺序要保持住。
		f.stopped = append(f.stopped, r.PathValue("id"))
		// 放开锁，别挡住后面的人。
		f.mu.Unlock()
		// 先写下响应状态码。
		w.WriteHeader(http.StatusNoContent)
	})
	// 记下改名，用来核对最后落到的名字。
	mux.HandleFunc("POST /v1.44/containers/{id}/rename", func(w http.ResponseWriter, r *http.Request) {
		// 先占住锁，避免并发写乱。
		f.mu.Lock()
		// 把这一段接进结果，顺序要保持住。
		f.renamed = append(f.renamed, r.PathValue("id")+"->"+r.URL.Query().Get("name"))
		// 放开锁，别挡住后面的人。
		f.mu.Unlock()
		// 先写下响应状态码。
		w.WriteHeader(http.StatusNoContent)
	})
	// 记下删除，用来核对旧容器被清掉。
	mux.HandleFunc("DELETE /v1.44/containers/{id}", func(w http.ResponseWriter, r *http.Request) {
		// 从路径里取出容器身份。
		id := r.PathValue("id")
		// 先占住锁，避免并发写乱。
		f.mu.Lock()
		// 把这一段接进结果，顺序要保持住。
		f.removed = append(f.removed, id)
		// 放开锁，别挡住后面的人。
		f.mu.Unlock()
		// 先写下响应状态码。
		w.WriteHeader(http.StatusNoContent)
	})
	return mux
}

// 拼一份当前应用容器的检查结果。
func appInspect() inspectResp {
	return inspectResp{
		ID:   "oldapp",
		Name: "/wmesh-factory-app-1",
		Config: struct {
			User         string              `json:"User"`         // 运行用户，换容器时原样带走。
			Env          []string            `json:"Env"`          // 环境变量，换容器时原样带走。
			Cmd          []string            `json:"Cmd"`          // 启动命令，换容器时原样带走。
			Image        string              `json:"Image"`        // 镜像引用，指出容器要跑哪一份。
			Labels       map[string]string   `json:"Labels"`       // 容器标签，换容器时原样带走。
			Entrypoint   []string            `json:"Entrypoint"`   // 入口命令，换容器时原样带走。
			WorkingDir   string              `json:"WorkingDir"`   // 工作目录，换容器时原样带走。
			ExposedPorts map[string]struct{} `json:"ExposedPorts"` // 声明要暴露的端口，换容器时带走。
		}{
			User:  "0:0",
			Env:   []string{"WMESH_DOCKER_UPDATE=1"},
			Image: "wmesh-factory-app:dev",
			Labels: map[string]string{
				"com.docker.compose.service": "app",
				"com.docker.compose.image":   "wmesh-factory-app:dev",
			},
			Entrypoint:   []string{"/usr/local/bin/wmesh"},
			ExposedPorts: map[string]struct{}{"8080/tcp": {}},
		},
		HostConfig: []byte(`{"PortBindings":{"8080/tcp":[{"HostPort":"52081"}]},"RestartPolicy":{"Name":"unless-stopped"}}`),
		NetworkSettings: netSettings{
			Networks: map[string]json.RawMessage{"wmesh-factory_default": []byte(`{}`)},
		},
		Mounts: []mountPoint{{
			Type: "volume", Name: "wmesh-factory_update", Destination: defaultDir,
		}},
	}
}

// 确认安装只拉起帮手，本进程不自己载入。
func TestApplyKicksHelperWithoutLoad(t *testing.T) {
	// 准备一份当前容器的检查结果。
	fake := &fakeDocker{inspect: appInspect()}
	// 处理，再交给后面，再交给后面。
	srv := httptest.NewServer(fake.handler())
	// 登记用例结束时要做的收尾。
	t.Cleanup(srv.Close)
	// 向测试框架要一块临时目录。
	dir := t.TempDir()
	// 先装一份安装器，套接字和目录随后再填。
	in := &Installer{
		api: newHTTPClient(srv.URL), sock: sockPath, self: "oldapp", dir: dir,
		delay: time.Millisecond,
		// 测试里不等真的就绪，直接算通过。
		waitReady: func(context.Context, string) error { return nil },
	}
	// 没能取出上下文就停住本用例。
	if err := in.Apply(t.Context(), 2, []byte("tar-bytes")); err != nil {
		// 没能取出上下文就停住本用例。
		t.Fatal(err)
	}
	// 载入的包和预期不符就进入失败。
	if len(fake.loaded) != 0 {
		// 本进程不该自己载入镜像。
		t.Fatalf("api must not load: %q", fake.loaded)
	}
	// 拼出同一目录下的路径。
	got, err := os.ReadFile(filepath.Join(dir, tarFile))
	// 出错或结果对不上就停住本用例。
	if err != nil || !bytes.Equal(got, []byte("tar-bytes")) {
		// 看两段是不是一样和预期不符就停住。
		t.Fatalf("tar file %q %v", got, err)
	}
	// 新建记录和预期不符就进入失败。
	if len(fake.created) != 1 || fake.created[0].Name != "wmesh-factory-app-1-swap" {
		// 新建出来的参数不对就停住。
		t.Fatalf("created %#v", fake.created)
	}
	// 取出刚才记下的那一次新建，核对名字和镜像。
	help := fake.created[0]
	// 结果和预期不符就进入失败。
	if help.Body.Image != "wmesh-factory-app:dev" {
		// 帮手所用镜像不对就停住。
		t.Fatalf("helper image %s", help.Body.Image)
	}
	// 把结构收成字节，再交给后面。
	cmd, _ := json.Marshal(help.Body.Cmd)
	// 帮手命令和预期不符就进入失败。
	if !bytes.Contains(cmd, []byte("docker-swap")) || !bytes.Contains(cmd, []byte(dir)) {
		// 帮手命令不对就停住。
		t.Fatalf("helper cmd %s", cmd)
	}
	// 启动记录和预期不符就进入失败。
	if len(fake.started) != 1 || fake.started[0] != "id-wmesh-factory-app-1-swap" {
		// 和预期不符就停住本用例。
		t.Fatalf("started %v", fake.started)
	}
}

// 库容器不能拿来换应用，碰上就要拒绝。
func TestApplyRejectsDBContainer(t *testing.T) {
	// 准备一份当前容器的检查结果。
	me := appInspect()
	// 定下当前容器，再交给后面。
	me.Config.Labels["com.docker.compose.service"] = "db"
	me.Config.Image = "postgres:18-alpine"
	fake := &fakeDocker{inspect: me}
	// 处理，再交给后面，再交给后面。
	srv := httptest.NewServer(fake.handler())
	// 登记用例结束时要做的收尾。
	t.Cleanup(srv.Close)
	// 向测试框架要一块临时目录。
	in := &Installer{api: newHTTPClient(srv.URL), sock: sockPath, self: "oldapp", dir: t.TempDir(), delay: time.Millisecond}
	// 没有出错就按成功返回，不用再补救。
	if err := in.Apply(t.Context(), 2, []byte("tar-bytes")); err == nil {
		// 该拒绝的请求竟通过了就停住。
		t.Fatal("expected refuse")
	}
}

// 空包不能装，应当在落盘之前拒绝。
func TestApplyEmptyTar(t *testing.T) {
	// 向测试框架要一块临时目录。
	in := &Installer{api: newHTTPClient("http://127.0.0.1:1"), self: "oldapp", dir: t.TempDir()}
	// 没有出错就按成功返回，不用再补救。
	if err := in.Apply(t.Context(), 1, nil); err == nil {
		// 空包竟被接受就停住。
		t.Fatal("expected empty tar error")
	}
}

// 帮手应先载入再换容器，顺序不能反。
func TestHelperLoadsThenSwaps(t *testing.T) {
	// 准备一份当前容器的检查结果。
	fake := &fakeDocker{inspect: appInspect()}
	// 处理，再交给后面，再交给后面。
	srv := httptest.NewServer(fake.handler())
	// 登记用例结束时要做的收尾。
	t.Cleanup(srv.Close)
	// 向测试框架要一块临时目录。
	dir := t.TempDir()
	// 没能拼出同一目录下的路径就停住本用例。
	if err := os.WriteFile(filepath.Join(dir, tarFile), []byte("tar-bytes"), 0o600); err != nil {
		// 没能拼出同一目录下的路径就停住本用例。
		t.Fatal(err)
	}
	// 接到假引擎的地址上。
	api := newHTTPClient(srv.URL)
	// 没能取出上下文就停住本用例。
	if err := runHelper(t.Context(), api, dir, "oldapp", "wmesh-factory-app-1", 0); err != nil {
		// 没能取出上下文就停住本用例。
		t.Fatal(err)
	}
	// 载入的包和预期不符就进入失败。
	if !bytes.Equal(fake.loaded, []byte("tar-bytes")) {
		// 载入的包和预期不符就停住。
		t.Fatalf("load body %q", fake.loaded)
	}
	// 拼出同一目录下的路径。
	st, err := readStatus(filepath.Join(dir, statusFile))
	// 出错或结果对不上就停住本用例。
	if err != nil || !st.OK {
		// 状态和预期不符就停住。
		t.Fatalf("status %+v %v", st, err)
	}
	// 新建记录和预期不符就进入失败。
	if len(fake.created) != 1 || fake.created[0].Name != "wmesh-factory-app-1-next" {
		// 和预期不符就停住本用例。
		t.Fatalf("next %#v", fake.created)
	}
	// 新建记录和预期不符就进入失败。
	if fake.created[0].Body.Image != "wmesh-factory-app:v2" {
		// 和预期不符就停住本用例。
		t.Fatalf("next image %s", fake.created[0].Body.Image)
	}
	// 停止记录和预期不符就进入失败。
	if strings.Join(fake.stopped, ",") != "oldapp" || !strings.Contains(strings.Join(fake.renamed, ","), "wmesh-factory-app-1") {
		// 改名结果不对就停住。
		t.Fatalf("swap stop=%v rename=%v start=%v", fake.stopped, fake.renamed, fake.started)
	}
}

// 换容器时先停旧的，再把新的改名启动。
func TestSwapStopsOldStartsNew(t *testing.T) {
	// 准备一份当前容器的检查结果。
	fake := &fakeDocker{inspect: appInspect()}
	// 处理，再交给后面，再交给后面。
	srv := httptest.NewServer(fake.handler())
	// 登记用例结束时要做的收尾。
	t.Cleanup(srv.Close)
	// 接到假引擎的地址上。
	api := newHTTPClient(srv.URL)
	// 没能取出上下文就停住本用例。
	if err := swapContainers(t.Context(), api, "oldapp", "newapp", "wmesh-factory-app-1"); err != nil {
		// 没能取出上下文就停住本用例。
		t.Fatal(err)
	}
	// 停止记录和预期不符就进入失败。
	if strings.Join(fake.stopped, ",") != "oldapp" || strings.Join(fake.removed, ",") != "oldapp" {
		// 用分隔符把几段拼成一行和预期不符就停住。
		t.Fatalf("stop/remove %v %v", fake.stopped, fake.removed)
	}
	// 改名记录和预期不符就进入失败。
	if strings.Join(fake.renamed, ",") != "newapp->wmesh-factory-app-1" {
		// 改名结果不对就停住。
		t.Fatalf("rename %v", fake.renamed)
	}
	// 启动记录和预期不符就进入失败。
	if strings.Join(fake.started, ",") != "newapp" {
		// 用分隔符把几段拼成一行和预期不符就停住。
		t.Fatalf("start %v", fake.started)
	}
}

// 挑镜像时要跳过数据库，不能当成应用。
func TestPickImageSkipsPostgres(t *testing.T) {
	// 挑出镜像，再交给后面。
	got, err := pickImage([]string{"postgres:18-alpine", "wmesh-factory-app:v2"}, "wmesh-factory-app:dev", map[string]string{
		"com.docker.compose.image": "wmesh-factory-app:dev",
	})
	// 出错或结果对不上就停住本用例。
	if err != nil || got != "wmesh-factory-app:v2" {
		// 结果和预期不符就停住。
		t.Fatalf("got %q %v", got, err)
	}
}

// 从载入输出里抽出镜像名。
func TestParseLoaded(t *testing.T) {
	// 准备一份测试或签名用的字节。
	in := bytes.NewReader([]byte(`{"stream":"Loaded image: wmesh-factory-app:dev\n"}` + "\n" + `{"error":"broken tar"}` + "\n"))
	// 解析载入内容，再交给后面。
	_, err := parseLoaded(in)
	// 没有出错就按成功返回，不用再补救。
	if err == nil {
		// 该失败的一步竟通过了就停住。
		t.Fatal("expected error")
	}
}

// 帮手报失败时不能记成已经装好。
func TestWaitStatusFailed(t *testing.T) {
	// 向测试框架要一块临时目录。
	dir := t.TempDir()
	// 没能把成败写进状态文件就停住本用例。
	if err := writeStatus(dir, helperStatus{Error: "broken tar"}); err != nil {
		t.Fatal(err)
	}
	// 没有出错就按成功返回，不用再补救。
	if err := waitStatus(t.Context(), dir); err == nil || !strings.Contains(err.Error(), "broken tar") {
		// 该失败的时候没有失败。
		t.Fatalf("err %v", err)
	}
}
