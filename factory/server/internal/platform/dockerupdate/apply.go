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
	mu        sync.Mutex                          // 挡住并发，避免两路同时改这份状态。
	api       *client                             // 访问本机引擎的客户端。
	sock      string                              // 引擎套接字所在的路径。
	self      string                              // 当前容器身份，不能把自己停掉。
	dir       string                              // 和帮手共用的更新目录。
	delay     time.Duration                       // 停旧容器前要等多久，好让确认先写完版本。
	waitReady func(context.Context, string) error // 怎么等帮手就绪，测试里可以换成直接通过。
}

// New 连本机 Docker；套接字不在则拒绝，避免假装装上。
func New(host, dir string) (*Installer, error) {
	// 时间路径，再交给后面。
	sock := unixPath(host)
	// 没能看这个路径是否存在就停，避免带着残缺继续。
	if _, err := os.Stat(sock); err != nil {
		// 套接字打不开，把原因交回去。
		return nil, fmt.Errorf("docker socket: %w", err)
	}
	// 读环境里有没有覆盖值。
	self := strings.TrimSpace(os.Getenv("HOSTNAME"))
	// 环境指定了当前容器就用它，避免认错。
	if v := strings.TrimSpace(os.Getenv("WMESH_DOCKER_SELF")); v != "" {
		// 定下当前身份，再交给后面。
		self = v
	}
	// 还是认不出当前容器就停下来。
	if self == "" {
		// 认不出当前容器，不能冒险去停。
		return nil, fmt.Errorf("cannot tell current container id")
	}
	// 目录是空的就改用默认更新位置。
	if strings.TrimSpace(dir) == "" {
		// 定下目录，再交给后面。
		dir = defaultDir
	}
	// 没能先把目录准备出来就停，避免带着残缺继续。
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
	// 没配换包能力就拒绝，避免假装已经装上。
	if in == nil || in.api == nil {
		// 没配换包，不能假装已经装上。
		return fmt.Errorf("docker installer not configured")
	}
	// 包是空的就不能拿去载入。
	if len(body) == 0 {
		// 镜像包是空的，不能拿去载入。
		return fmt.Errorf("empty image tar")
	}
	// 先占住锁，避免并发写乱。
	in.mu.Lock()
	// 离开时放开锁，免得把别人堵住。
	defer in.mu.Unlock()
	// 查看，再交给后面，再交给后面。
	me, err := in.api.inspect(ctx, in.self)
	// 没能查看，再交给后面就停，避免带着残缺继续。
	if err != nil {
		return err
	}
	// 没能拒绝基础设施，再交给后面就停，避免带着残缺继续。
	if err := rejectInfra(me); err != nil {
		return err
	}
	// 去掉固定前缀留下名字。
	name := strings.TrimPrefix(me.Name, "/")
	// 名字是空的就不能再继续。
	if name == "" {
		// 当前容器没有名字，没法改回原名。
		return fmt.Errorf("current container has no name")
	}
	// 没能写入包，再交给后面就停，避免带着残缺继续。
	if err := writeTar(in.dir, body); err != nil {
		return err
	}
	// 定下替换名字，再交给后面。
	swapName := name + swapSuffix
	// 上次换包半途留下的帮手先清掉，避免重名。
	if err := in.api.removeIfExists(ctx, swapName); err != nil {
		return err
	}
	// 帮手规格，再交给后面。
	helperID, err := in.api.create(ctx, swapName, helperSpec(me, name, in.sock, in.dir, in.delay))
	// 没能帮手规格，再交给后面就停，避免带着残缺继续。
	if err != nil {
		return err
	}
	// 没能启动，再交给后面就停，避免带着残缺继续。
	if err := in.api.start(ctx, helperID); err != nil {
		// 上次残留的容器有就删掉。
		_ = in.api.removeIfExists(ctx, swapName)
		return err
	}
	// 定下等待，再交给后面。
	wait := in.waitReady
	// 没有自定义的等待方法就去看状态文件。
	if wait == nil {
		// 定下等待，再交给后面。
		wait = waitStatus
	}
	// 没能等待，再交给后面就停，避免带着残缺继续。
	if err := wait(ctx, in.dir); err != nil {
		// 上次残留的容器有就删掉。
		_ = in.api.removeIfExists(ctx, swapName)
		return err
	}
	return nil
}

// unixPath 从 unix:///path 或裸路径取出套接字文件。
func unixPath(host string) string {
	// 去掉两头空白再使用。
	host = strings.TrimSpace(host)
	// 没给引擎地址就用本机套接字。
	if host == "" {
		return sockPath
	}
	// 去掉固定前缀留下名字。
	host = strings.TrimPrefix(host, "unix://")
	// 没给引擎地址就用本机套接字。
	if host == "" {
		return sockPath
	}
	return host
}

// rejectInfra 禁止拿本安装器去动数据库或对象存储容器。
func rejectInfra(me inspectResp) error {
	// 先当认不出服务名，再从标签里找。
	svc := ""
	if me.Config.Labels != nil {
		// 从标签里记下服务名，用来判断是不是库。
		svc = me.Config.Labels["com.docker.compose.service"]
	}
	// 按服务名判断是不是库或存储，大小写不论。
	switch strings.ToLower(svc) {
	// 库和对象存储不能拿来换应用。
	case "db", "postgres", "oss":
		// 这是库或存储，不能拿来换应用。
		return fmt.Errorf("refusing to replace %s container", svc)
	}
	// 忽略大小写再比较。
	img := strings.ToLower(me.Config.Image)
	// 这是库或存储镜像，不能当成应用来换。
	if isInfraImage(img) {
		// 这是库或存储，不能拿来换应用。
		return fmt.Errorf("refusing to replace infra image %s", me.Config.Image)
	}
	return nil
}

// pickImage 只用厂服务镜像，忽略 tar 里可能夹带的 postgres/oss。
func pickImage(refs []string, current string, labels map[string]string) (string, error) {
	// 先记下当前镜像，找不到厂服务再退回它。
	wanted := current
	// 有标签才从里面找镜像名。
	if labels != nil {
		// 标签里写了镜像就优先用它。
		if v := labels["com.docker.compose.image"]; v != "" {
			// 标签里有镜像名就改用它。
			wanted = v
		}
	}
	// 镜像仓库，再交给后面。
	wantRepo := imageRepo(wanted)
	// 准备放下备选，再交给后面。
	var fallback string
	// 逐项处理，空的就不进入循环。
	for _, ref := range refs {
		// 这是库或存储镜像，不能当成应用来换。
		if isInfraImage(ref) {
			continue
		}
		// 整段引用或仓库名对上才是要的那份。
		if ref == wanted || imageRepo(ref) == wantRepo {
			return ref, nil
		}
		// 还没有备选镜像就先记下这一份。
		if fallback == "" {
			// 定下备选，再交给后面。
			fallback = ref
		}
	}
	// 已经有备选就不再覆盖。
	if fallback != "" {
		return fallback, nil
	}
	// 带上原因交回去，调用方才能知道为何停下。
	return "", fmt.Errorf("tar has no factory app image")
}

// imageRepo 去掉标签，用来对齐 wmesh-factory-app:dev 和 :v2。
func imageRepo(ref string) string {
	// 去掉两头空白再使用。
	ref = strings.TrimSpace(ref)
	// 这是摘要引用，没有标签可以去掉。
	if strings.HasPrefix(ref, "sha256:") {
		return ref
	}
	// 有标签就去掉，只留下仓库名。
	if i := strings.LastIndex(ref, ":"); i > 0 && !strings.Contains(ref[i:], "/") {
		return ref[:i]
	}
	return ref
}

// isInfraImage 库和对象存储镜像即使被 load 也不用来换 app。
func isInfraImage(ref string) bool {
	// 忽略大小写再比较。
	s := strings.ToLower(ref)
	// 交回看文本里有没有这段的结果。
	return strings.Contains(s, "postgres") || strings.Contains(s, "rustfs")
}

// 新建容器时要提交的那份参数。
type createBody struct {
	Image            string              `json:"Image"`                      // 镜像引用，指出容器要跑哪一份。
	User             string              `json:"User,omitempty"`             // 运行用户，没有就留给引擎默认。
	Env              []string            `json:"Env,omitempty"`              // 环境变量，没有就不提交。
	Cmd              []string            `json:"Cmd,omitempty"`              // 启动命令，没有就不提交。
	Entrypoint       []string            `json:"Entrypoint,omitempty"`       // 入口命令，没有就不提交。
	Labels           map[string]string   `json:"Labels,omitempty"`           // 标签，没有就不提交。
	WorkingDir       string              `json:"WorkingDir,omitempty"`       // 工作目录，没有就不提交。
	ExposedPorts     map[string]struct{} `json:"ExposedPorts,omitempty"`     // 要暴露的端口，没有就不提交。
	HostConfig       any                 `json:"HostConfig,omitempty"`       // 宿主侧配置，端口和卷从当前容器抄来。
	NetworkingConfig any                 `json:"NetworkingConfig,omitempty"` // 要接上的网络，按名对应。
}

// 新建容器时要接上的网络。
type endpoints struct {
	EndpointsConfig map[string]map[string]any `json:"EndpointsConfig"` // 按网络名接上去，参数先留空。
}

// cloneApp 按当前 app 的环境、端口和网络建新容器，不带 db/oss 的卷。
func cloneApp(me inspectResp, image string) createBody {
	// 抄一份网络，再交给后面。
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
	// 准备承接解出来的对象。
	var m map[string]any
	// 有宿主配置才去解析，空的就跳过。
	if len(raw) > 0 {
		// 没能把字节还原成结构就停，避免带着残缺继续。
		if err := json.Unmarshal(raw, &m); err != nil {
			// 定下这份对象，再交给后面。
			m = nil
		}
	}
	// 宿主配置解出来了才拿掉互相冲突的项。
	if m != nil {
		// 拿掉网络模式，避免和按名接入的网络打架。
		delete(m, "NetworkMode")
		// 定下主机，再交给后面。
		host = m
	}
	// 没有网络就只返回宿主侧配置。
	if len(nets) == 0 {
		return host, nil
	}
	// 准备承接解出来的对象。
	eps := map[string]map[string]any{}
	// 每个网络都接上，参数先留空。
	for name := range nets {
		// 准备承接解出来的对象。
		eps[name] = map[string]any{}
	}
	return host, endpoints{EndpointsConfig: eps}
}

// 帮手容器的挂载和网络配置。
type helperHost struct {
	Binds       []string      `json:"Binds,omitempty"`  // 绑定挂载，宿主机路径对着容器路径。
	Mounts      []dockerMount `json:"Mounts,omitempty"` // 命名卷挂载，按卷名接回去。
	AutoRemove  bool          `json:"AutoRemove"`       // 退出后让引擎删掉帮手自己。
	NetworkMode string        `json:"NetworkMode"`      // 网络模式，帮手不占用应用端口。
}

// 帮手要挂进去的一卷或一块目录。
type dockerMount struct {
	Type   string `json:"Type"`   // volume / bind
	Source string `json:"Source"` // 卷名或宿主机路径
	Target string `json:"Target"` // 容器内路径
}

// helperSpec 用当前镜像跑帮手：load、建新容器、再换；不占用 app 端口。
func helperSpec(me inspectResp, name, sock, dir string, delay time.Duration) createBody {
	// 没给等待就用默认间隔，避免立刻停掉旧的。
	if delay <= 0 {
		// 定下等待时长，再交给后面。
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
	// 定下这份配置，再交给后面。
	h := helperHost{AutoRemove: true, NetworkMode: "none"}
	// 定下想要的，再交给后面。
	want := map[string]bool{sockPath: true, dir: true}
	// 逐块看挂载，只要套接字和更新目录。
	for _, m := range me.Mounts {
		// 不是套接字也不是更新目录就跳过。
		if !want[m.Destination] {
			continue
		}
		// 这是命名卷，按卷名挂回去。
		if m.Type == "volume" && m.Name != "" {
			// 把这一段接进结果，顺序要保持住。
			h.Mounts = append(h.Mounts, dockerMount{Type: "volume", Source: m.Name, Target: m.Destination})
			continue
		}
		// 有宿主机路径才做成绑定挂载。
		if m.Source != "" {
			// 把这一段接进结果，顺序要保持住。
			h.Binds = append(h.Binds, m.Source+":"+m.Destination)
		}
	}
	// 两边都没挂上就至少把套接字挂上。
	if len(h.Binds) == 0 && len(h.Mounts) == 0 {
		// 准备收集字符串结果。
		h.Binds = []string{sock + ":" + sockPath}
	}
	return h
}

// writeTar 把镜像包落到共享目录，并清掉上次就绪文件。
func writeTar(dir string, body []byte) error {
	// 没能先把目录准备出来就停，避免带着残缺继续。
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	// 拼出同一目录下的路径。
	_ = os.Remove(filepath.Join(dir, statusFile))
	// 拼出同一目录下的路径。
	tmp := filepath.Join(dir, tarFile+".tmp")
	// 镜像包没读好或没写好就停，避免往下用残缺结果。
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return err
	}
	// 换名成功才算写完，读的人不会看见半截。
	return os.Rename(tmp, filepath.Join(dir, tarFile))
}

// 帮手写回的就绪或失败结果。
type helperStatus struct {
	OK    bool   `json:"ok"`              // 新容器是否已经建好。
	Error string `json:"error,omitempty"` // 失败原因，成功时留空。
}

// waitStatus 等帮手 load 并建好新容器；失败不记已装。
func waitStatus(ctx context.Context, dir string) error {
	// 按固定间隔再看一次状态。
	tick := time.NewTicker(pollEvery)
	// 离开时停掉节拍，避免定时器一直响。
	defer tick.Stop()
	// 拼出同一目录下的路径。
	path := filepath.Join(dir, statusFile)
	// 一直看下去，直到就绪、失败或被取消。
	for {
		// 没有出错就按成功返回，不用再补救。
		if st, err := readStatus(path); err == nil {
			// 探活通过才把这次换成报成功。
			if !st.OK {
				// 失败却没写原因时补一句笼统说明。
				if st.Error == "" {
					// 帮手失败又没写原因，补一句交回。
					return fmt.Errorf("helper failed")
				}
				// 带上原因交回去，调用方才能知道为何停下。
				return fmt.Errorf("%s", st.Error)
			}
			return nil
		}
		// 等取消或时间到，先发生的那一路先处理。
		select {
		// 调用方取消了就停下，不再空等。
		case <-ctx.Done():
			// 交回取出取消的原因的结果。
			return ctx.Err()
		// 节拍到了再看一次就绪文件。
		case <-tick.C:
		}
	}
}

// readStatus 读帮手写下的就绪或失败。
func readStatus(path string) (helperStatus, error) {
	// 读出文件里的字节。
	b, err := os.ReadFile(path)
	// 没能读出文件里的字节就停，避免带着残缺继续。
	if err != nil {
		return helperStatus{}, err
	}
	// 准备放下状态，再交给后面。
	var st helperStatus
	// 没能把字节还原成结构就停，避免带着残缺继续。
	if err := json.Unmarshal(b, &st); err != nil {
		return helperStatus{}, err
	}
	return st, nil
}

// writeStatus 原子写下就绪，避免 API 读到半份 JSON。
func writeStatus(dir string, st helperStatus) error {
	// 把结构收成字节，再交给后面。
	raw, err := json.Marshal(st)
	// 没能把结构收成字节就停，避免带着残缺继续。
	if err != nil {
		return err
	}
	// 拼出同一目录下的路径。
	tmp := filepath.Join(dir, statusFile+".tmp")
	// 状态文件没读好或没写好就停，避免往下用残缺结果。
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	// 换名成功才算写完，读的人不会看见半截。
	return os.Rename(tmp, filepath.Join(dir, statusFile))
}
