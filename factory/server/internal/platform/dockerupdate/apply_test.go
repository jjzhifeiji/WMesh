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

type fakeDocker struct {
	mu      sync.Mutex
	inspect inspectResp
	loaded  []byte
	created []created
	started []string
	stopped []string
	removed []string
	renamed []string
}

type created struct {
	Name string
	Body createBody
}

func (f *fakeDocker) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1.44/images/load", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.loaded = b
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(streamLine{Stream: "Loaded image: postgres:18-alpine\n"})
		_ = json.NewEncoder(w).Encode(streamLine{Stream: "Loaded image: wmesh-factory-app:v2\n"})
	})
	mux.HandleFunc("GET /v1.44/containers/{id}/json", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		f.mu.Lock()
		me := f.inspect
		f.mu.Unlock()
		if id != me.ID && id != strings.TrimPrefix(me.Name, "/") {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"not found"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(me)
	})
	mux.HandleFunc("POST /v1.44/containers/create", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("name")
		var body createBody
		_ = json.NewDecoder(r.Body).Decode(&body)
		id := "id-" + name
		f.mu.Lock()
		f.created = append(f.created, created{Name: name, Body: body})
		f.mu.Unlock()
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(createResp{ID: id})
	})
	mux.HandleFunc("POST /v1.44/containers/{id}/start", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.started = append(f.started, r.PathValue("id"))
		f.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /v1.44/containers/{id}/stop", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.stopped = append(f.stopped, r.PathValue("id"))
		f.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /v1.44/containers/{id}/rename", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.renamed = append(f.renamed, r.PathValue("id")+"->"+r.URL.Query().Get("name"))
		f.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("DELETE /v1.44/containers/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		f.mu.Lock()
		f.removed = append(f.removed, id)
		f.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	return mux
}

func appInspect() inspectResp {
	return inspectResp{
		ID:   "oldapp",
		Name: "/wmesh-factory-app-1",
		Config: struct {
			User         string              `json:"User"`
			Env          []string            `json:"Env"`
			Cmd          []string            `json:"Cmd"`
			Image        string              `json:"Image"`
			Labels       map[string]string   `json:"Labels"`
			Entrypoint   []string            `json:"Entrypoint"`
			WorkingDir   string              `json:"WorkingDir"`
			ExposedPorts map[string]struct{} `json:"ExposedPorts"`
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

func TestApplyKicksHelperWithoutLoad(t *testing.T) {
	fake := &fakeDocker{inspect: appInspect()}
	srv := httptest.NewServer(fake.handler())
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	in := &Installer{
		api: newHTTPClient(srv.URL), sock: sockPath, self: "oldapp", dir: dir,
		delay:     time.Millisecond,
		waitReady: func(context.Context, string) error { return nil },
	}
	if err := in.Apply(t.Context(), 2, []byte("tar-bytes")); err != nil {
		t.Fatal(err)
	}
	if len(fake.loaded) != 0 {
		t.Fatalf("api must not load: %q", fake.loaded)
	}
	got, err := os.ReadFile(filepath.Join(dir, tarFile))
	if err != nil || !bytes.Equal(got, []byte("tar-bytes")) {
		t.Fatalf("tar file %q %v", got, err)
	}
	if len(fake.created) != 1 || fake.created[0].Name != "wmesh-factory-app-1-swap" {
		t.Fatalf("created %#v", fake.created)
	}
	help := fake.created[0]
	if help.Body.Image != "wmesh-factory-app:dev" {
		t.Fatalf("helper image %s", help.Body.Image)
	}
	cmd, _ := json.Marshal(help.Body.Cmd)
	if !bytes.Contains(cmd, []byte("docker-swap")) || !bytes.Contains(cmd, []byte(dir)) {
		t.Fatalf("helper cmd %s", cmd)
	}
	if len(fake.started) != 1 || fake.started[0] != "id-wmesh-factory-app-1-swap" {
		t.Fatalf("started %v", fake.started)
	}
}

func TestApplyRejectsDBContainer(t *testing.T) {
	me := appInspect()
	me.Config.Labels["com.docker.compose.service"] = "db"
	me.Config.Image = "postgres:18-alpine"
	fake := &fakeDocker{inspect: me}
	srv := httptest.NewServer(fake.handler())
	t.Cleanup(srv.Close)
	in := &Installer{api: newHTTPClient(srv.URL), sock: sockPath, self: "oldapp", dir: t.TempDir(), delay: time.Millisecond}
	if err := in.Apply(t.Context(), 2, []byte("tar-bytes")); err == nil {
		t.Fatal("expected refuse")
	}
}

func TestApplyEmptyTar(t *testing.T) {
	in := &Installer{api: newHTTPClient("http://127.0.0.1:1"), self: "oldapp", dir: t.TempDir()}
	if err := in.Apply(t.Context(), 1, nil); err == nil {
		t.Fatal("expected empty tar error")
	}
}

func TestHelperLoadsThenSwaps(t *testing.T) {
	fake := &fakeDocker{inspect: appInspect()}
	srv := httptest.NewServer(fake.handler())
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, tarFile), []byte("tar-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	api := newHTTPClient(srv.URL)
	if err := runHelper(t.Context(), api, dir, "oldapp", "wmesh-factory-app-1", 0); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(fake.loaded, []byte("tar-bytes")) {
		t.Fatalf("load body %q", fake.loaded)
	}
	st, err := readStatus(filepath.Join(dir, statusFile))
	if err != nil || !st.OK {
		t.Fatalf("status %+v %v", st, err)
	}
	if len(fake.created) != 1 || fake.created[0].Name != "wmesh-factory-app-1-next" {
		t.Fatalf("next %#v", fake.created)
	}
	if fake.created[0].Body.Image != "wmesh-factory-app:v2" {
		t.Fatalf("next image %s", fake.created[0].Body.Image)
	}
	if strings.Join(fake.stopped, ",") != "oldapp" || !strings.Contains(strings.Join(fake.renamed, ","), "wmesh-factory-app-1") {
		t.Fatalf("swap stop=%v rename=%v start=%v", fake.stopped, fake.renamed, fake.started)
	}
}

func TestSwapStopsOldStartsNew(t *testing.T) {
	fake := &fakeDocker{inspect: appInspect()}
	srv := httptest.NewServer(fake.handler())
	t.Cleanup(srv.Close)
	api := newHTTPClient(srv.URL)
	if err := swapContainers(t.Context(), api, "oldapp", "newapp", "wmesh-factory-app-1"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(fake.stopped, ",") != "oldapp" || strings.Join(fake.removed, ",") != "oldapp" {
		t.Fatalf("stop/remove %v %v", fake.stopped, fake.removed)
	}
	if strings.Join(fake.renamed, ",") != "newapp->wmesh-factory-app-1" {
		t.Fatalf("rename %v", fake.renamed)
	}
	if strings.Join(fake.started, ",") != "newapp" {
		t.Fatalf("start %v", fake.started)
	}
}

func TestPickImageSkipsPostgres(t *testing.T) {
	got, err := pickImage([]string{"postgres:18-alpine", "wmesh-factory-app:v2"}, "wmesh-factory-app:dev", map[string]string{
		"com.docker.compose.image": "wmesh-factory-app:dev",
	})
	if err != nil || got != "wmesh-factory-app:v2" {
		t.Fatalf("got %q %v", got, err)
	}
}

func TestParseLoaded(t *testing.T) {
	in := bytes.NewReader([]byte(`{"stream":"Loaded image: wmesh-factory-app:dev\n"}` + "\n" + `{"error":"broken tar"}` + "\n"))
	_, err := parseLoaded(in)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestWaitStatusFailed(t *testing.T) {
	dir := t.TempDir()
	if err := writeStatus(dir, helperStatus{Error: "broken tar"}); err != nil {
		t.Fatal(err)
	}
	if err := waitStatus(t.Context(), dir); err == nil || !strings.Contains(err.Error(), "broken tar") {
		t.Fatalf("err %v", err)
	}
}
