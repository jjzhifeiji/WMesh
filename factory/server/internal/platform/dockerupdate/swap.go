package dockerupdate

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Swap 给帮手容器用：docker load、建新容器、等确认写完已装版本，再停旧起新。
func Swap(args []string) error {
	// 准备解析帮手的命令参数。
	fs := flag.NewFlagSet("docker-swap", flag.ContinueOnError)
	// 收成普通文本再拿去比较或拼接。
	sock := fs.String("sock", sockPath, "docker socket")
	// 收成普通文本再拿去比较或拼接。
	dir := fs.String("dir", defaultDir, "shared update directory")
	// 收成普通文本再拿去比较或拼接。
	oldID := fs.String("old", "", "container to replace")
	// 收成普通文本再拿去比较或拼接。
	name := fs.String("name", "", "original container name")
	// 时长，再交给后面，再交给后面。
	delay := fs.Duration("delay", defaultDelay, "wait after load before stopping the old container")
	// 没能解析帮手带来的参数就停，避免带着残缺继续。
	if err := fs.Parse(args); err != nil {
		return err
	}
	// 缺了旧容器或名字就不能换。
	if strings.TrimSpace(*oldID) == "" || strings.TrimSpace(*name) == "" {
		// 缺了旧容器或名字，不能换。
		return fmt.Errorf("docker-swap requires -old -name")
	}
	// 时间路径，再交给后面。
	api := newUnixClient(unixPath(*sock))
	// 拿出不会被取消的上下文。
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	// 用完就取消限时，避免上下文一直占着。
	defer cancel()
	// 交回帮手，再交给后面的结果。
	return runHelper(ctx, api, *dir, *oldID, *name, *delay)
}

// runHelper load 镜像并建好新容器后才写就绪；随后再换容器。
func runHelper(ctx context.Context, api *client, dir, oldID, name string, delay time.Duration) error {
	// 先把失败写进状态，再把原因交回去。
	fail := func(err error) error {
		// 取出失败时的说明。
		_ = writeStatus(dir, helperStatus{Error: err.Error()})
		return err
	}
	// 查看，再交给后面，再交给后面。
	me, err := api.inspect(ctx, oldID)
	// 没能查看，再交给后面就停，避免带着残缺继续。
	if err != nil {
		// 记上失败再把原因交回，避免假装已经换完。
		return fail(err)
	}
	// 没能拒绝基础设施，再交给后面就停，避免带着残缺继续。
	if err := rejectInfra(me); err != nil {
		// 记上失败再把原因交回，避免假装已经换完。
		return fail(err)
	}
	// 拼出同一目录下的路径。
	f, err := os.Open(filepath.Join(dir, tarFile))
	// 镜像包没读好或没写好就停，避免往下用残缺结果。
	if err != nil {
		// 记上失败再把原因交回，避免假装已经换完。
		return fail(err)
	}
	// 载入占用，再交给后面。
	refs, err := api.loadImages(ctx, f)
	// 把句柄关掉，避免一直占着。
	_ = f.Close()
	// 没能把句柄关掉，避免一直占着就停，避免带着残缺继续。
	if err != nil {
		// 记上失败再把原因交回，避免假装已经换完。
		return fail(err)
	}
	// 挑出镜像，再交给后面。
	image, err := pickImage(refs, me.Config.Image, me.Config.Labels)
	// 没能挑出镜像，再交给后面就停，避免带着残缺继续。
	if err != nil {
		// 记上失败再把原因交回，避免假装已经换完。
		return fail(err)
	}
	// 定下名字，再交给后面。
	nextName := name + nextSuffix
	// 没能上次残留的容器有就删掉就停，避免带着残缺继续。
	if err := api.removeIfExists(ctx, nextName); err != nil {
		// 记上失败再把原因交回，避免假装已经换完。
		return fail(err)
	}
	// 抄一份应用，再交给后面。
	newID, err := api.create(ctx, nextName, cloneApp(me, image))
	// 没能抄一份应用，再交给后面就停，避免带着残缺继续。
	if err != nil {
		// 记上失败再把原因交回，避免假装已经换完。
		return fail(err)
	}
	// 没能把成败写进状态文件就停，避免带着残缺继续。
	if err := writeStatus(dir, helperStatus{OK: true}); err != nil {
		return err
	}
	// 配了等待才睡一会儿，好让版本先写完。
	if delay > 0 {
		// 等取消或时间到，先发生的那一路先处理。
		select {
		// 调用方取消了就停下，不再空等。
		case <-ctx.Done():
			// 交回取出取消的原因的结果。
			return ctx.Err()
		// 等到设定的时间再停旧容器。
		case <-time.After(delay):
		}
	}
	// 交回替换，再交给后面的结果。
	return swapContainers(ctx, api, oldID, newID, name)
}

// swapContainers 停掉并删除旧 app，把新容器改回原名再启动。
func swapContainers(ctx context.Context, api *client, oldID, newID, name string) error {
	// 没有错，或者容器已经没了，都当成成功。
	if err := api.stop(ctx, oldID); err != nil && !isNotFound(err) {
		// 旧容器停不掉，把原因交回。
		return fmt.Errorf("stop old: %w", err)
	}
	// 没有错，或者容器已经没了，都当成成功。
	if err := api.remove(ctx, oldID); err != nil && !isNotFound(err) {
		// 旧容器删不掉，把原因交回。
		return fmt.Errorf("remove old: %w", err)
	}
	// 没能改名，再交给后面就停，避免带着残缺继续。
	if err := api.rename(ctx, newID, name); err != nil {
		// 新容器改不回原名，把原因交回。
		return fmt.Errorf("rename new: %w", err)
	}
	// 没能启动，再交给后面就停，避免带着残缺继续。
	if err := api.start(ctx, newID); err != nil {
		// 新容器没启动，把原因交回。
		return fmt.Errorf("start new: %w", err)
	}
	return nil
}
