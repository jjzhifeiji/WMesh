// Package dockerupdate 超管确认后由一次性帮手 docker load 并换本机 app 容器。
// 不管库和对象存储容器，也不做业务判定。
package dockerupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	sockPath     = "/var/run/docker.sock"  // 容器内挂上的 Docker 套接字
	defaultDir   = "/var/lib/wmesh/update" // 与帮手共用的 tar / 就绪文件
	tarFile      = "pending.tar"           // 确认后落下的镜像包
	statusFile   = "status.json"           // 帮手 load 并建好新容器后写就绪
	nextSuffix   = "-next"                 // 新容器临时名，换完再改回
	swapSuffix   = "-swap"                 // 一次性帮手，避免停自己时把后半截杀掉
	defaultDelay = 3 * time.Second         // 等确认接口写完已装版本再停旧进程
	pollEvery    = 200 * time.Millisecond  // 等帮手就绪
)

// Installer 把厂服务 tar 交给帮手；本进程不 docker load。
type Installer struct {
	mu        sync.Mutex
	api       *client
	sock      string
	self      string
	dir       string
	delay     time.Duration
	waitReady func(context.Context, string) error
}

// New 连本机 Docker；套接字不在则拒绝，避免假装装上。
func New(host, dir string) (*Installer, error) {
	sock := unixPath(host)
	if _, err := os.Stat(sock); err != nil {
		return nil, fmt.Errorf("docker socket: %w", err)
	}
	self := strings.TrimSpace(os.Getenv("HOSTNAME"))
	if v := strings.TrimSpace(os.Getenv("WMESH_DOCKER_SELF")); v != "" {
		self = v
	}
	if self == "" {
		return nil, fmt.Errorf("cannot tell current container id")
	}
	if strings.TrimSpace(dir) == "" {
		dir = defaultDir
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Installer{
		api: newUnixClient(sock), sock: sock, self: self, dir: dir,
		delay: defaultDelay, waitReady: waitStatus,
	}, nil
}

// Apply 只写 tar 并拉起帮手；load 成功才返回，失败则旧进程继续。
func (in *Installer) Apply(ctx context.Context, _ int64, body []byte) error {
	if in == nil || in.api == nil {
		return fmt.Errorf("docker installer not configured")
	}
	if len(body) == 0 {
		return fmt.Errorf("empty image tar")
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	me, err := in.api.inspect(ctx, in.self)
	if err != nil {
		return err
	}
	if err := rejectInfra(me); err != nil {
		return err
	}
	name := strings.TrimPrefix(me.Name, "/")
	if name == "" {
		return fmt.Errorf("current container has no name")
	}
	if err := writeTar(in.dir, body); err != nil {
		return err
	}
	swapName := name + swapSuffix
	// 上次换包半途留下的帮手先清掉，避免重名。
	if err := in.api.removeIfExists(ctx, swapName); err != nil {
		return err
	}
	helperID, err := in.api.create(ctx, swapName, helperSpec(me, name, in.sock, in.dir, in.delay))
	if err != nil {
		return err
	}
	if err := in.api.start(ctx, helperID); err != nil {
		_ = in.api.removeIfExists(ctx, swapName)
		return err
	}
	wait := in.waitReady
	if wait == nil {
		wait = waitStatus
	}
	if err := wait(ctx, in.dir); err != nil {
		_ = in.api.removeIfExists(ctx, swapName)
		return err
	}
	return nil
}

// unixPath 从 unix:///path 或裸路径取出套接字文件。
func unixPath(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return sockPath
	}
	host = strings.TrimPrefix(host, "unix://")
	if host == "" {
		return sockPath
	}
	return host
}

// rejectInfra 禁止拿本安装器去动数据库或对象存储容器。
func rejectInfra(me inspectResp) error {
	svc := ""
	if me.Config.Labels != nil {
		svc = me.Config.Labels["com.docker.compose.service"]
	}
	switch strings.ToLower(svc) {
	case "db", "postgres", "oss":
		return fmt.Errorf("refusing to replace %s container", svc)
	}
	img := strings.ToLower(me.Config.Image)
	if isInfraImage(img) {
		return fmt.Errorf("refusing to replace infra image %s", me.Config.Image)
	}
	return nil
}

// pickImage 只用厂服务镜像，忽略 tar 里可能夹带的 postgres/oss。
func pickImage(refs []string, current string, labels map[string]string) (string, error) {
	wanted := current
	if labels != nil {
		if v := labels["com.docker.compose.image"]; v != "" {
			wanted = v
		}
	}
	wantRepo := imageRepo(wanted)
	var fallback string
	for _, ref := range refs {
		if isInfraImage(ref) {
			continue
		}
		if ref == wanted || imageRepo(ref) == wantRepo {
			return ref, nil
		}
		if fallback == "" {
			fallback = ref
		}
	}
	if fallback != "" {
		return fallback, nil
	}
	return "", fmt.Errorf("tar has no factory app image")
}

// imageRepo 去掉标签，用来对齐 wmesh-factory-app:dev 和 :v2。
func imageRepo(ref string) string {
	ref = strings.TrimSpace(ref)
	if strings.HasPrefix(ref, "sha256:") {
		return ref
	}
	if i := strings.LastIndex(ref, ":"); i > 0 && !strings.Contains(ref[i:], "/") {
		return ref[:i]
	}
	return ref
}

// isInfraImage 库和对象存储镜像即使被 load 也不用来换 app。
func isInfraImage(ref string) bool {
	s := strings.ToLower(ref)
	return strings.Contains(s, "postgres") || strings.Contains(s, "rustfs")
}

type createBody struct {
	Image            string              `json:"Image"`
	User             string              `json:"User,omitempty"`
	Env              []string            `json:"Env,omitempty"`
	Cmd              []string            `json:"Cmd,omitempty"`
	Entrypoint       []string            `json:"Entrypoint,omitempty"`
	Labels           map[string]string   `json:"Labels,omitempty"`
	WorkingDir       string              `json:"WorkingDir,omitempty"`
	ExposedPorts     map[string]struct{} `json:"ExposedPorts,omitempty"`
	HostConfig       any                 `json:"HostConfig,omitempty"`
	NetworkingConfig any                 `json:"NetworkingConfig,omitempty"`
}

type endpoints struct {
	EndpointsConfig map[string]map[string]any `json:"EndpointsConfig"`
}

// cloneApp 按当前 app 的环境、端口和网络建新容器，不带 db/oss 的卷。
func cloneApp(me inspectResp, image string) createBody {
	host, netCfg := cloneNet(me.HostConfig, me.NetworkSettings.Networks)
	return createBody{
		Image:            image,
		User:             me.Config.User,
		Env:              me.Config.Env,
		Cmd:              me.Config.Cmd,
		Entrypoint:       me.Config.Entrypoint,
		Labels:           me.Config.Labels,
		WorkingDir:       me.Config.WorkingDir,
		ExposedPorts:     me.Config.ExposedPorts,
		HostConfig:       host,
		NetworkingConfig: netCfg,
	}
}

// cloneNet 沿用端口和卷，网络改接到同名网络；去掉 NetworkMode 以免和 Endpoints 打架。
func cloneNet(raw json.RawMessage, nets map[string]json.RawMessage) (host any, netCfg any) {
	var m map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &m); err != nil {
			m = nil
		}
	}
	if m != nil {
		delete(m, "NetworkMode")
		host = m
	}
	if len(nets) == 0 {
		return host, nil
	}
	eps := map[string]map[string]any{}
	for name := range nets {
		eps[name] = map[string]any{}
	}
	return host, endpoints{EndpointsConfig: eps}
}

type helperHost struct {
	Binds       []string      `json:"Binds,omitempty"`
	Mounts      []dockerMount `json:"Mounts,omitempty"`
	AutoRemove  bool          `json:"AutoRemove"`
	NetworkMode string        `json:"NetworkMode"`
}

type dockerMount struct {
	Type   string `json:"Type"`   // volume / bind
	Source string `json:"Source"` // 卷名或宿主机路径
	Target string `json:"Target"` // 容器内路径
}

// helperSpec 用当前镜像跑帮手：load、建新容器、再换；不占用 app 端口。
func helperSpec(me inspectResp, name, sock, dir string, delay time.Duration) createBody {
	if delay <= 0 {
		delay = defaultDelay
	}
	return createBody{
		Image: me.Config.Image,
		User:  "0:0",
		Cmd: []string{
			"docker-swap",
			"-sock", sock,
			"-dir", dir,
			"-old", me.ID,
			"-name", name,
			"-delay", delay.String(),
		},
		HostConfig: helperMounts(me, sock, dir),
	}
}

// helperMounts 把 docker.sock 和更新目录原样挂给帮手，tar 才能两边看见。
func helperMounts(me inspectResp, sock, dir string) helperHost {
	h := helperHost{AutoRemove: true, NetworkMode: "none"}
	want := map[string]bool{sockPath: true, dir: true}
	for _, m := range me.Mounts {
		if !want[m.Destination] {
			continue
		}
		if m.Type == "volume" && m.Name != "" {
			h.Mounts = append(h.Mounts, dockerMount{Type: "volume", Source: m.Name, Target: m.Destination})
			continue
		}
		if m.Source != "" {
			h.Binds = append(h.Binds, m.Source+":"+m.Destination)
		}
	}
	if len(h.Binds) == 0 && len(h.Mounts) == 0 {
		h.Binds = []string{sock + ":" + sockPath}
	}
	return h
}

// writeTar 把镜像包落到共享目录，并清掉上次就绪文件。
func writeTar(dir string, body []byte) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	_ = os.Remove(filepath.Join(dir, statusFile))
	tmp := filepath.Join(dir, tarFile+".tmp")
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, tarFile))
}

type helperStatus struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// waitStatus 等帮手 load 并建好新容器；失败不记已装。
func waitStatus(ctx context.Context, dir string) error {
	tick := time.NewTicker(pollEvery)
	defer tick.Stop()
	path := filepath.Join(dir, statusFile)
	for {
		if st, err := readStatus(path); err == nil {
			if !st.OK {
				if st.Error == "" {
					return fmt.Errorf("helper failed")
				}
				return fmt.Errorf("%s", st.Error)
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
}

// readStatus 读帮手写下的就绪或失败。
func readStatus(path string) (helperStatus, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return helperStatus{}, err
	}
	var st helperStatus
	if err := json.Unmarshal(b, &st); err != nil {
		return helperStatus{}, err
	}
	return st, nil
}

// writeStatus 原子写下就绪，避免 API 读到半份 JSON。
func writeStatus(dir string, st helperStatus) error {
	raw, err := json.Marshal(st)
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, statusFile+".tmp")
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, statusFile))
}
